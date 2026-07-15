package data

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/jlgore/corkscrew/internal/db"
)

// Column describes a query result column.
type Column struct {
	Name string
	Type string
}

// Result is the transport-neutral representation returned by read-only data
// sessions.
type Result struct {
	Columns []Column
	Rows    [][]any
}

type sessionOptions struct {
	databaseOptions []db.Option
	graphExtension  string
	scanWriter      ScanWriter
}

// SessionOption configures a data session.
type SessionOption func(*sessionOptions)

type databaseOpener func(context.Context, string, ...db.Option) (*sql.DB, error)

// WithDatabaseOptions passes connection options through to local DuckDB or
// remote Quack sessions.
func WithDatabaseOptions(options ...db.Option) SessionOption {
	return func(config *sessionOptions) {
		config.databaseOptions = append(config.databaseOptions, options...)
	}
}

// WithGraphExtension loads a trusted packaged graph extension in-process.
func WithGraphExtension(path string) SessionOption {
	return func(config *sessionOptions) { config.graphExtension = path }
}

// WithScanWriter substitutes the session's scan-persistence adapter. Production
// sessions use the default DuckDB graph store; tests inject a fake.
func WithScanWriter(writer ScanWriter) SessionOption {
	return func(config *sessionOptions) { config.scanWriter = writer }
}

// Session owns a local DuckDB or remote Quack connection and exposes normalized
// repositories plus guarded read-only SQL execution.
type Session struct {
	database       *sql.DB
	target         db.Target
	graphAvailable bool
	ownsDatabase   bool
	scanWriter     ScanWriter
}

func OpenSession(ctx context.Context, target string, options ...SessionOption) (*Session, error) {
	return openSessionWith(ctx, target, db.OpenDuckDB, options...)
}

func openSessionWith(ctx context.Context, target string, opener databaseOpener, options ...SessionOption) (*Session, error) {
	if !db.IsRemoteTarget(target) {
		var err error
		target, err = resolveLocalTarget(target)
		if err != nil {
			return nil, fmt.Errorf("resolve database target: %w", err)
		}
		resolved, err := db.GetUnifiedDatabasePath(target)
		if err != nil {
			return nil, fmt.Errorf("resolve database target: %w", err)
		}
		target = resolved
	}
	parsed, err := db.ParseTarget(target)
	if err != nil {
		return nil, err
	}
	config := sessionOptions{}
	for _, option := range options {
		option(&config)
	}
	if strings.TrimSpace(config.graphExtension) != "" {
		config.databaseOptions = append(config.databaseOptions, db.WithUnsignedExtensions())
	}
	database, err := opener(ctx, target, config.databaseOptions...)
	if err != nil {
		return nil, fmt.Errorf("open data session: %w", err)
	}
	if err := db.EnsureSchema(ctx, database); err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("establish data session schema: %w", err)
	}
	session := &Session{database: database, target: parsed, ownsDatabase: true, scanWriter: config.scanWriter}
	if session.scanWriter == nil {
		session.scanWriter = graphStoreWriter{store: db.NewGraphStore(database)}
	}
	if strings.TrimSpace(config.graphExtension) != "" {
		resolved, resolveErr := ResolveGraphExtension(config.graphExtension)
		if resolveErr != nil {
			_ = database.Close()
			return nil, resolveErr
		}
		if _, loadErr := database.ExecContext(ctx, "LOAD '"+db.SQLEscapeSingleQuotes(resolved)+"'"); loadErr != nil {
			_ = database.Close()
			return nil, fmt.Errorf("load graph extension %q: %w", resolved, loadErr)
		}
		session.graphAvailable = true
	}
	return session, nil
}

func resolveLocalTarget(target string) (string, error) {
	if target == "~" || strings.HasPrefix(target, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		if target == "~" {
			return home, nil
		}
		return filepath.Join(home, strings.TrimPrefix(target, "~/")), nil
	}
	if strings.HasPrefix(target, "~") {
		return "", fmt.Errorf("unsupported home-directory syntax %q; use ~/path", target)
	}
	return target, nil
}

func (s *Session) Close() error {
	if s == nil || s.database == nil || !s.ownsDatabase {
		return nil
	}
	return s.database.Close()
}

func (s *Session) IsRemote() bool { return s != nil && s.target.IsRemote }

// Target returns the resolved database target owned by the session.
func (s *Session) Target() string {
	if s == nil {
		return ""
	}
	return s.target.Raw
}

func (s *Session) GraphAvailable() bool { return s != nil && s.graphAvailable }

func (s *Session) Inventory() *Inventory { return NewInventory(s) }

func (s *Session) Relationships() *Relationships { return NewRelationships(s) }

func (s *Session) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if s == nil || s.database == nil {
		return nil, fmt.Errorf("data session is closed")
	}
	return s.database.QueryContext(ctx, query, args...)
}

func (s *Session) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return s.database.QueryRowContext(ctx, query, args...)
}

// Connection acquires a pinned connection from an initialized session for
// adapter protocols that require connection-local state.
func (s *Session) Connection(ctx context.Context) (*sql.Conn, error) {
	if s == nil || s.database == nil {
		return nil, fmt.Errorf("data session is closed")
	}
	return s.database.Conn(ctx)
}

func (s *Session) Ping(ctx context.Context) error {
	if s == nil || s.database == nil {
		return fmt.Errorf("data session is closed")
	}
	return s.database.PingContext(ctx)
}

// ReadOnly validates and runs exactly one query in a read-only transaction. The
// transaction is always rolled back, including successful queries.
func (s *Session) ReadOnly(ctx context.Context, statement string, args ...any) (_ *Result, resultErr error) {
	if s == nil || s.database == nil {
		return nil, fmt.Errorf("data session is closed")
	}
	if err := ValidateReadOnlyStatement(statement); err != nil {
		return nil, err
	}
	connection, err := s.database.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire read-only connection: %w", err)
	}
	defer connection.Close()
	if _, err := connection.ExecContext(ctx, "BEGIN TRANSACTION READ ONLY"); err != nil {
		return nil, fmt.Errorf("begin read-only query: %w", err)
	}
	defer func() {
		_, err := connection.ExecContext(context.WithoutCancel(ctx), "ROLLBACK")
		if resultErr == nil && err != nil {
			resultErr = fmt.Errorf("rollback read-only query: %w", err)
		}
	}()

	rows, err := connection.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, fmt.Errorf("execute read-only query: %w", err)
	}
	defer rows.Close()
	columnNames, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("read query columns: %w", err)
	}
	columnTypes, err := rows.ColumnTypes()
	if err != nil {
		return nil, fmt.Errorf("read query column types: %w", err)
	}
	result := &Result{Columns: make([]Column, len(columnNames))}
	for index, name := range columnNames {
		result.Columns[index] = Column{Name: name, Type: columnTypes[index].DatabaseTypeName()}
	}
	for rows.Next() {
		values := make([]any, len(columnNames))
		pointers := make([]any, len(columnNames))
		for index := range values {
			pointers[index] = &values[index]
		}
		if err := rows.Scan(pointers...); err != nil {
			return nil, fmt.Errorf("scan query row: %w", err)
		}
		for index, value := range values {
			if bytes, ok := value.([]byte); ok {
				values[index] = string(bytes)
			}
		}
		result.Rows = append(result.Rows, values)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate query rows: %w", err)
	}
	return result, nil
}

// ValidateReadOnlyStatement performs a conservative lexical check: one
// statement, a read-oriented leading keyword, and no mutation/control keywords
// outside comments or string literals.
func ValidateReadOnlyStatement(statement string) error {
	tokens, semicolons, err := sqlTokens(statement)
	if err != nil {
		return err
	}
	if len(tokens) == 0 {
		return fmt.Errorf("query cannot be empty")
	}
	if semicolons > 1 || (semicolons == 1 && !hasOnlyTrailingSemicolon(statement)) {
		return fmt.Errorf("exactly one query statement is required")
	}
	allowed := map[string]bool{"select": true, "with": true, "explain": true, "describe": true, "show": true, "summarize": true, "from": true}
	if !allowed[tokens[0]] {
		return fmt.Errorf("query must be read-only; %q statements are not allowed", tokens[0])
	}
	forbidden := map[string]bool{
		"insert": true, "update": true, "delete": true, "merge": true,
		"create": true, "drop": true, "alter": true, "truncate": true,
		"copy": true, "attach": true, "detach": true, "install": true,
		"load": true, "call": true, "set": true, "use": true, "vacuum": true,
	}
	for _, token := range tokens {
		if forbidden[token] {
			return fmt.Errorf("query contains forbidden operation %q", token)
		}
	}
	return nil
}

func sqlTokens(statement string) ([]string, int, error) {
	var tokens []string
	var current strings.Builder
	semicolons := 0
	flush := func() {
		if current.Len() > 0 {
			tokens = append(tokens, strings.ToLower(current.String()))
			current.Reset()
		}
	}
	for index := 0; index < len(statement); {
		char := statement[index]
		switch {
		case char == '\'' || char == '"':
			flush()
			quote := char
			index++
			closed := false
			for index < len(statement) {
				if statement[index] == quote {
					if index+1 < len(statement) && statement[index+1] == quote {
						index += 2
						continue
					}
					index++
					closed = true
					break
				}
				index++
			}
			if !closed {
				return nil, 0, fmt.Errorf("unterminated SQL quote")
			}
		case char == '-' && index+1 < len(statement) && statement[index+1] == '-':
			flush()
			index += 2
			for index < len(statement) && statement[index] != '\n' {
				index++
			}
		case char == '/' && index+1 < len(statement) && statement[index+1] == '*':
			flush()
			end := strings.Index(statement[index+2:], "*/")
			if end < 0 {
				return nil, 0, fmt.Errorf("unterminated SQL comment")
			}
			index += end + 4
		case char == ';':
			flush()
			semicolons++
			index++
		case unicode.IsLetter(rune(char)) || char == '_':
			current.WriteByte(char)
			index++
		default:
			flush()
			index++
		}
	}
	flush()
	return tokens, semicolons, nil
}

func hasOnlyTrailingSemicolon(statement string) bool {
	trimmed := strings.TrimSpace(statement)
	return strings.HasSuffix(trimmed, ";") && !strings.Contains(strings.TrimSuffix(trimmed, ";"), ";")
}

func ResolveGraphExtension(explicitPath string) (string, error) {
	if strings.TrimSpace(explicitPath) != "" {
		path, err := filepath.Abs(strings.TrimSpace(explicitPath))
		if err != nil {
			return "", err
		}
		if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
			return "", fmt.Errorf("graph extension not found at %q", path)
		}
		return path, nil
	}
	if environmentPath := strings.TrimSpace(os.Getenv("CORKSCREW_GRAPH_EXTENSION")); environmentPath != "" {
		return ResolveGraphExtension(environmentPath)
	}
	candidates := []string{
		"extensions/corkscrew-graph/build/corkscrew_graph.duckdb_extension",
		"extensions/corkscrew-graph/target/release/corkscrew_graph.duckdb_extension",
		"build/corkscrew_graph.duckdb_extension",
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, ".corkscrew", "extensions", "corkscrew_graph.duckdb_extension"))
	}
	for _, candidate := range candidates {
		if resolved, err := ResolveGraphExtension(candidate); err == nil {
			return resolved, nil
		}
	}
	return "", fmt.Errorf("could not find corkscrew_graph.duckdb_extension; use --extension or CORKSCREW_GRAPH_EXTENSION")
}
