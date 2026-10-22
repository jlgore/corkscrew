package fakecf

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Params are an operation's path parameters, such as {"account_id": "..."}.
type Params map[string]string

// Fault makes a route misbehave. Times is how many requests it applies to; zero means all.
type Fault struct {
	Status int           // respond with this HTTP status and a Cloudflare error envelope
	Repeat bool          // a list route serves its first page for every page asked for
	Delay  time.Duration // wait this long before answering, to hold requests in flight
	Times  int
}

// perPageDefault is the page size when a request doesn't ask for one.
const perPageDefault = 20

// Server is a fake Cloudflare API. Seed it with SetList and SetResult (or Load), point an SDK
// client at URL, and every request is answered from the seeds. A request no operation matches,
// a seed or response that breaks the schema, or a non-GET request fails the test.
type Server struct {
	t      testing.TB
	schema *schema
	srv    *httptest.Server

	mu       sync.Mutex
	lists    map[string][]any // seed key -> items
	results  map[string]any   // seed key -> result
	faults   map[string]*Fault
	requests map[string]int // operation key -> requests served
	inFlight map[string]int // operation key -> requests being served now
	peak     map[string]int // operation key -> most requests in flight at once
}

// New starts a fake API that closes when the test ends.
func New(t testing.TB) *Server {
	t.Helper()
	s, err := loadSchema()
	if err != nil {
		t.Fatalf("fakecf: %v", err)
	}
	server := &Server{
		t:        t,
		schema:   s,
		lists:    map[string][]any{},
		results:  map[string]any{},
		faults:   map[string]*Fault{},
		requests: map[string]int{},
		inFlight: map[string]int{},
		peak:     map[string]int{},
	}
	server.srv = httptest.NewServer(http.HandlerFunc(server.serve))
	t.Cleanup(server.srv.Close)
	return server
}

// URL is the base URL to give the SDK, with no trailing slash.
func (s *Server) URL() string { return s.srv.URL }

// SetList seeds the items a list operation returns for params. Each item is validated against
// the operation's result item type.
func (s *Server) SetList(op string, params Params, items ...any) {
	s.t.Helper()
	o := s.op(op, params)
	if !o.list {
		s.t.Fatalf("fakecf: %s doesn't return a list; use SetResult", op)
	}
	decoded := make([]any, 0, len(items))
	for i, item := range items {
		value := s.decode(op, item)
		s.check(fmt.Sprintf("%s item %d", op, i), o.elem, value)
		decoded = append(decoded, value)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := seedKey(op, params)
	s.lists[key] = append(s.lists[key], decoded...)
}

// SetResult seeds the result an operation that returns one value answers params with.
func (s *Server) SetResult(op string, params Params, result any) {
	s.t.Helper()
	o := s.op(op, params)
	if o.list {
		s.t.Fatalf("fakecf: %s returns a list; use SetList", op)
	}
	value := s.decode(op, result)
	s.check(op, o.Response, value)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.results[seedKey(op, params)] = value
}

// Fail makes requests to op with params misbehave as fault says.
func (s *Server) Fail(op string, params Params, fault Fault) {
	s.t.Helper()
	s.op(op, params)
	s.mu.Lock()
	defer s.mu.Unlock()
	f := fault
	s.faults[seedKey(op, params)] = &f
}

// Requests reports how many requests op has served.
func (s *Server) Requests(op string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.requests[op]
}

// MaxInFlight reports the most requests to op the fake was serving at once.
func (s *Server) MaxInFlight(op string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.peak[op]
}

// Seed is one entry of a fixture file: a list's items or a single result.
type Seed struct {
	Op     string            `json:"op"`
	Params Params            `json:"params"`
	Items  []json.RawMessage `json:"items,omitempty"`
	Result json.RawMessage   `json:"result,omitempty"`
}

// Load seeds every entry of a JSON fixture file (an array of Seed).
func (s *Server) Load(path string) {
	s.t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		s.t.Fatalf("fakecf: %v", err)
	}
	var seeds []Seed
	if err := json.Unmarshal(data, &seeds); err != nil {
		s.t.Fatalf("fakecf: %s: %v", path, err)
	}
	for _, seed := range seeds {
		if seed.Result != nil {
			s.SetResult(seed.Op, seed.Params, seed.Result)
			continue
		}
		items := make([]any, len(seed.Items))
		for i, item := range seed.Items {
			items[i] = item
		}
		s.SetList(seed.Op, seed.Params, items...)
	}
}

// op returns the operation op names, failing the test if there is none or params don't name
// exactly its path parameters.
func (s *Server) op(op string, params Params) *operation {
	s.t.Helper()
	o, ok := s.schema.byKey[op]
	if !ok {
		s.t.Fatalf("fakecf: no operation %q; add it to tools/forge-fakecf/operations.ts", op)
	}
	if len(params) != len(o.PathParams) {
		s.t.Fatalf("fakecf: %s takes path params %v, got %v", op, paramNames(o.PathParams), params)
	}
	for _, p := range o.PathParams {
		if _, ok := params[p.Name]; !ok {
			s.t.Fatalf("fakecf: %s takes path params %v, got %v", op, paramNames(o.PathParams), params)
		}
	}
	return o
}

func (s *Server) decode(op string, value any) any {
	s.t.Helper()
	data, ok := value.(json.RawMessage)
	if !ok {
		var err error
		if data, err = json.Marshal(value); err != nil {
			s.t.Fatalf("fakecf: %s: %v", op, err)
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var out any
	if err := decoder.Decode(&out); err != nil {
		s.t.Fatalf("fakecf: %s: %v", op, err)
	}
	return out
}

func (s *Server) check(what string, t *typ, value any) {
	s.t.Helper()
	if errs := s.schema.validate(t, value); len(errs) > 0 {
		s.t.Fatalf("fakecf: %s doesn't match the spec:\n  %s", what, strings.Join(errs, "\n  "))
	}
}

func (s *schema) validate(t *typ, value any) []string {
	v := validator{s: s}
	v.validate(t, value, "", true)
	return v.errs
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.reject(w, http.StatusMethodNotAllowed, "%s %s: the fake serves reads only", r.Method, r.URL.Path)
		return
	}
	o, params := s.match(r.URL.Path)
	if o == nil {
		s.reject(w, http.StatusNotFound,
			"%s %s matches no operation; add it to tools/forge-fakecf/operations.ts", r.Method, r.URL.Path)
		return
	}
	if unknown := unknownQuery(o, r); len(unknown) > 0 {
		s.reject(w, http.StatusBadRequest, "%s: query params %v aren't in the spec", o.Key(), unknown)
		return
	}
	key := seedKey(o.Key(), params)

	s.mu.Lock()
	s.requests[o.Key()]++
	s.inFlight[o.Key()]++
	s.peak[o.Key()] = max(s.peak[o.Key()], s.inFlight[o.Key()])
	fault := s.takeFault(key)
	items := s.lists[key]
	result, hasResult := s.results[key]
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.inFlight[o.Key()]--
		s.mu.Unlock()
	}()

	if fault != nil && fault.Delay > 0 {
		time.Sleep(fault.Delay)
	}
	if fault != nil && fault.Status != 0 {
		writeError(w, fault.Status)
		return
	}
	var body any
	if o.list {
		body = s.page(o, r, items, fault != nil && fault.Repeat)
	} else if hasResult {
		body = result
	} else if o.empty != nil {
		body = o.empty
	} else {
		writeError(w, http.StatusNotFound)
		return
	}
	if errs := s.schema.validate(o.Response, body); len(errs) > 0 {
		s.reject(w, http.StatusInternalServerError, "%s response doesn't match the spec:\n  %s",
			o.Key(), strings.Join(errs, "\n  "))
		return
	}
	writeOK(w, o, body)
}

// takeFault returns the fault for key, using up one of its Times. Call with s.mu held.
func (s *Server) takeFault(key string) *Fault {
	f, ok := s.faults[key]
	if !ok {
		return nil
	}
	if f.Times > 0 {
		f.Times--
		if f.Times == 0 {
			delete(s.faults, key)
		}
	}
	copied := *f
	return &copied
}

// page returns the {result, result_info} body for one page of items.
func (s *Server) page(o *operation, r *http.Request, items []any, repeat bool) map[string]any {
	query := r.URL.Query()
	perPage := atoiOr(query.Get("per_page"), atoiOr(query.Get("limit"), perPageDefault))
	start := 0
	pageNumber := 1
	switch o.paging {
	case pagingPage:
		pageNumber = atoiOr(query.Get("page"), 1)
		if !repeat {
			start = (pageNumber - 1) * perPage
		}
	case pagingCursor:
		if !repeat {
			start = atoiOr(query.Get("cursor"), 0)
		}
	case pagingNone:
		perPage = len(items)
	}
	start = min(max(start, 0), len(items))
	end := min(start+perPage, len(items))
	result := append([]any{}, items[start:end]...)

	body := map[string]any{"result": result}
	if o.info == nil {
		return body
	}
	fields, _ := s.schema.objectFields(o.info)
	totalPages := 0
	if perPage > 0 {
		totalPages = (len(items) + perPage - 1) / perPage
	}
	cursor := ""
	if end < len(items) {
		cursor = strconv.Itoa(end)
	}
	values := map[string]any{
		"page":        json.Number(strconv.Itoa(pageNumber)),
		"per_page":    json.Number(strconv.Itoa(perPage)),
		"count":       json.Number(strconv.Itoa(len(result))),
		"total_count": json.Number(strconv.Itoa(len(items))),
		"total_pages": json.Number(strconv.Itoa(totalPages)),
		"cursor":      cursor,
	}
	info := map[string]any{}
	for name, value := range values {
		if _, ok := fields[name]; ok {
			info[name] = value
		}
	}
	if len(info) > 0 {
		body["result_info"] = info
	}
	return body
}

// match finds the operation whose path template matches path, preferring literal segments.
func (s *Server) match(path string) (*operation, Params) {
	segments := strings.Split(strings.Trim(path, "/"), "/")
	var (
		best       *operation
		bestParams Params
		bestScore  = -1
	)
	for _, o := range s.schema.Operations {
		if len(o.segments) != len(segments) {
			continue
		}
		params := Params{}
		score := 0
		for i, seg := range o.segments {
			if strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}") {
				params[seg[1:len(seg)-1]] = segments[i]
				continue
			}
			if seg != segments[i] {
				score = -1
				break
			}
			score++
		}
		if score > bestScore {
			best, bestParams, bestScore = o, params, score
		}
	}
	return best, bestParams
}

func (s *Server) reject(w http.ResponseWriter, status int, format string, args ...any) {
	s.t.Errorf("fakecf: "+format, args...)
	writeError(w, status)
}

func writeOK(w http.ResponseWriter, o *operation, body any) {
	envelope := map[string]any{"success": true, "errors": []any{}, "messages": []any{}}
	if o.list {
		for key, value := range body.(map[string]any) {
			envelope[key] = value
		}
	} else {
		envelope["result"] = body
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(envelope)
}

func writeError(w http.ResponseWriter, status int) {
	w.Header().Set("Content-Type", "application/json")
	// The SDK retries 429 and 5xx; tell it not to wait.
	w.Header().Set("Retry-After-Ms", "0")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"success":  false,
		"errors":   []any{map[string]any{"code": status, "message": http.StatusText(status)}},
		"messages": []any{},
		"result":   nil,
	})
}

func unknownQuery(o *operation, r *http.Request) []string {
	known := make(map[string]bool, len(o.QueryParams))
	for _, p := range o.QueryParams {
		known[p.Name] = true
	}
	var unknown []string
	for name := range r.URL.Query() {
		if !known[name] {
			unknown = append(unknown, name)
		}
	}
	sort.Strings(unknown)
	return unknown
}

func seedKey(op string, params Params) string {
	names := make([]string, 0, len(params))
	for name := range params {
		names = append(names, name)
	}
	sort.Strings(names)
	var b strings.Builder
	b.WriteString(op)
	for _, name := range names {
		b.WriteString(" " + name + "=" + params[name])
	}
	return b.String()
}

func paramNames(params []param) []string {
	names := make([]string, len(params))
	for i, p := range params {
		names[i] = p.Name
	}
	return names
}

func atoiOr(s string, fallback int) int {
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}
