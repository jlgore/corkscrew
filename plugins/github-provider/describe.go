package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	pb "github.com/jlgore/corkscrew/internal/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const listCacheTTL = 10 * time.Minute

// listEntry caches a flattened ListResources scan so paged callers can iterate
// without re-running the full scan on every page request.
type listEntry struct {
	refs      []*pb.ResourceRef
	expiresAt time.Time
}

// describeByType dispatches on ResourceRef.Type and fetches the live resource
// from GitHub. Unsupported types return an error so the caller knows the
// operation isn't implemented (vs. the previous placeholder, which lied by
// returning a hardcoded "use ScanService" attribute).
func (p *GitHubProvider) describeByType(ctx context.Context, ref *pb.ResourceRef) (*pb.Resource, error) {
	switch ref.Type {
	case "organization":
		login := firstNonEmpty(ref.Name, p.org)
		org, _, err := p.client.Organizations.Get(ctx, login)
		if err != nil {
			return nil, fmt.Errorf("get organization %s: %w", login, err)
		}
		return p.resource("orgs", "organization", org.GetNodeID(), org.GetLogin(), org, map[string]string{
			"login":         org.GetLogin(),
			"billing_email": org.GetBillingEmail(),
		}), nil

	case "repository":
		owner, name, ok := repoCoordsFromRef(ref)
		if !ok {
			return nil, fmt.Errorf("cannot derive owner/name from ref (id=%q name=%q)", ref.Id, ref.Name)
		}
		repo, _, err := p.client.Repositories.Get(ctx, owner, name)
		if err != nil {
			return nil, fmt.Errorf("get repository %s/%s: %w", owner, name, err)
		}
		return p.repoResource(repo), nil

	case "branch_protection":
		owner, name, ok := parseRepoFromID(ref.Id)
		if !ok {
			return nil, fmt.Errorf("cannot parse owner/name from id %q", ref.Id)
		}
		branch := branchFromBranchProtectionID(ref.Id)
		if branch == "" {
			return nil, fmt.Errorf("cannot parse branch from id %q", ref.Id)
		}
		protection, _, err := p.client.Repositories.GetBranchProtection(ctx, owner, name, branch)
		if err != nil {
			return nil, fmt.Errorf("get branch protection %s/%s@%s: %w", owner, name, branch, err)
		}
		full := owner + "/" + name
		return p.resource("repos", "branch_protection",
			fmt.Sprintf("%s/branches/%s/protection", full, branch),
			branch, protection, map[string]string{
				"repository": full,
				"branch":     branch,
			}), nil

	case "ruleset":
		owner, name, ok := parseRepoFromID(ref.Id)
		if !ok {
			return nil, fmt.Errorf("cannot parse owner/name from id %q", ref.Id)
		}
		rulesetID := lastSegment(ref.Id)
		if rulesetID == "" {
			return nil, fmt.Errorf("cannot parse ruleset id from %q", ref.Id)
		}
		var ruleset map[string]interface{}
		if err := p.getREST(ctx, fmt.Sprintf("repos/%s/%s/rulesets/%s", owner, name, rulesetID), &ruleset); err != nil {
			return nil, fmt.Errorf("get ruleset %s/%s/%s: %w", owner, name, rulesetID, err)
		}
		full := owner + "/" + name
		rulesetName, _ := ruleset["name"].(string)
		return p.resource("repos", "ruleset",
			fmt.Sprintf("%s/rulesets/%s", full, rulesetID),
			rulesetName, ruleset, map[string]string{"repository": full}), nil

	case "finding":
		// Findings are synthesized from inventory + security scans, not
		// fetched. Return what we know from the ref so callers get a
		// well-formed Resource rather than an error.
		attrs := map[string]string{}
		for k, v := range ref.BasicAttributes {
			attrs[k] = v
		}
		attrs["note"] = "finding is synthetic; rerun ScanService(findings) for fresh state"
		return &pb.Resource{
			Provider:     "github",
			Service:      ref.Service,
			Type:         ref.Type,
			Id:           ref.Id,
			Name:         ref.Name,
			AccountId:    p.org,
			Attributes:   mustJSON(attrs),
			DiscoveredAt: timestamppb.Now(),
		}, nil

	default:
		return nil, fmt.Errorf("describe not implemented for type %q (supported: organization, repository, branch_protection, ruleset, finding)", ref.Type)
	}
}

// repoCoordsFromRef pulls (owner, name) from a ref. Repository resources
// store the FullName as Name, so split on "/". Falls back to parsing Id for
// types whose ID encodes "owner/name/...".
func repoCoordsFromRef(ref *pb.ResourceRef) (string, string, bool) {
	if ref.Name != "" && strings.Contains(ref.Name, "/") {
		parts := strings.SplitN(ref.Name, "/", 2)
		if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
			return parts[0], parts[1], true
		}
	}
	if owner, name, ok := parseRepoFromID(ref.Id); ok {
		return owner, name, true
	}
	if ref.BasicAttributes != nil {
		if full := ref.BasicAttributes["repository"]; strings.Contains(full, "/") {
			parts := strings.SplitN(full, "/", 2)
			if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
				return parts[0], parts[1], true
			}
		}
	}
	return "", "", false
}

// parseRepoFromID takes the first two slash-separated segments of an id like
// "owner/name/<rest>" and returns them. Returns ok=false for ids that don't
// follow that shape (e.g. node IDs, numeric team ids).
func parseRepoFromID(id string) (string, string, bool) {
	parts := strings.SplitN(id, "/", 3)
	if len(parts) < 2 {
		return "", "", false
	}
	if parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	// node IDs like "R_kgDOAbc..." don't contain slashes, but be defensive
	// against ids with no third segment that aren't owner/name (none today).
	return parts[0], parts[1], true
}

// branchFromBranchProtectionID extracts <branch> from
// "<owner>/<name>/branches/<branch>/protection". The branch portion may
// itself contain "/" (e.g. release/2026), so consume everything between the
// "branches/" marker and the trailing "/protection".
func branchFromBranchProtectionID(id string) string {
	const marker = "/branches/"
	const tail = "/protection"
	i := strings.Index(id, marker)
	if i < 0 {
		return ""
	}
	rest := id[i+len(marker):]
	if !strings.HasSuffix(rest, tail) {
		return rest
	}
	return strings.TrimSuffix(rest, tail)
}

func lastSegment(id string) string {
	if i := strings.LastIndex(id, "/"); i >= 0 {
		return id[i+1:]
	}
	return id
}

// listResourcesPaged backs ListResources. It caches the full flattened scan
// keyed by (service, filters) for listCacheTTL so paged readers don't replay
// the entire org scan per page request. Cursor is a stringified offset into
// the cached slice.
func (p *GitHubProvider) listResourcesPaged(ctx context.Context, req *pb.ListResourcesRequest) (*pb.ListResourcesResponse, error) {
	key := listCacheKey(req.Service, req.Filters)
	refs, err := p.refsForKey(ctx, key, req)
	if err != nil {
		return nil, err
	}

	offset := 0
	if req.NextToken != "" {
		if n, err := strconv.Atoi(req.NextToken); err == nil && n >= 0 {
			offset = n
		}
	}
	if offset > len(refs) {
		offset = len(refs)
	}

	page := refs[offset:]
	nextToken := ""
	if req.MaxResults > 0 && int(req.MaxResults) < len(page) {
		page = page[:req.MaxResults]
		nextToken = strconv.Itoa(offset + len(page))
	}

	return &pb.ListResourcesResponse{
		Resources:  page,
		TotalCount: int32(len(refs)),
		NextToken:  nextToken,
		Metadata:   map[string]string{"org": p.org},
	}, nil
}

func (p *GitHubProvider) refsForKey(ctx context.Context, key string, req *pb.ListResourcesRequest) ([]*pb.ResourceRef, error) {
	p.gcListCache()
	if cached, ok := p.listCache.Load(key); ok {
		entry := cached.(*listEntry)
		if time.Now().Before(entry.expiresAt) {
			return entry.refs, nil
		}
		p.listCache.Delete(key)
	}

	resp, err := p.ScanService(ctx, &pb.ScanServiceRequest{Service: req.Service, Filters: req.Filters})
	if err != nil {
		return nil, err
	}
	refs := make([]*pb.ResourceRef, 0, len(resp.Resources))
	for _, r := range resp.Resources {
		ref := &pb.ResourceRef{
			Id:        r.Id,
			Name:      r.Name,
			Type:      r.Type,
			Service:   r.Service,
			AccountId: r.AccountId,
			BasicAttributes: map[string]string{
				"provider": "github",
			},
		}
		// Carry through fields that DescribeResource will need to derive
		// owner/name without a second ScanService call.
		if attrs := decodeAttrs(r.Attributes); attrs != nil {
			if v := attrs["repository"]; v != "" {
				ref.BasicAttributes["repository"] = v
			}
			if v := attrs["branch"]; v != "" {
				ref.BasicAttributes["branch"] = v
			}
			if v := attrs["severity"]; v != "" {
				ref.BasicAttributes["severity"] = v
			}
		}
		refs = append(refs, ref)
	}
	p.listCache.Store(key, &listEntry{refs: refs, expiresAt: time.Now().Add(listCacheTTL)})
	return refs, nil
}

func (p *GitHubProvider) gcListCache() {
	now := time.Now()
	p.listCache.Range(func(k, v interface{}) bool {
		if now.After(v.(*listEntry).expiresAt) {
			p.listCache.Delete(k)
		}
		return true
	})
}

func listCacheKey(service string, filters map[string]string) string {
	keys := make([]string, 0, len(filters))
	for k := range filters {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(service)
	b.WriteByte('|')
	for _, k := range keys {
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(filters[k])
		b.WriteByte(';')
	}
	return b.String()
}

// decodeAttrs decodes the JSON-encoded Attributes string into a flat map.
// Uses encoding/json indirectly via mustParseStringMap; failures are silent
// because attrs are advisory metadata, not load-bearing.
func decodeAttrs(raw string) map[string]string {
	if raw == "" {
		return nil
	}
	out, _ := parseStringMap(raw)
	return out
}

// parseStringMap is split out so tests can exercise it directly.
func parseStringMap(raw string) (map[string]string, error) {
	out := map[string]string{}
	if raw == "" || raw == "null" {
		return out, nil
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, err
	}
	return out, nil
}
