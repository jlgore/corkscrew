package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"net/url"
	"strings"

	duckdb "github.com/duckdb/duckdb-go/v2"
)

// RemoteAlias is the catalog name under which a remote Quack server is attached.
// After connecting, `USE <RemoteAlias>` makes the remote the default catalog so
// that existing unqualified SQL (e.g. `SELECT * FROM aws_resources`) transparently
// targets the server.
const RemoteAlias = "corkscrew_remote"

// quackScheme is the URI scheme used by the DuckDB Quack client-server protocol.
const quackScheme = "quack:"

// Target describes a resolved database connection target. It is either a local
// DuckDB file path or a remote Quack server reached over the wire protocol.
type Target struct {
	// Raw is the original, unparsed target string.
	Raw string
	// IsRemote is true when Raw uses the `quack:` scheme.
	IsRemote bool
	// Endpoint is the `host[:port]` (or `[ipv6]:port`) portion of a quack URI,
	// with the scheme and any query string stripped. Empty for local targets.
	Endpoint string
	// Token is an authentication token parsed from a `?token=` query parameter.
	// Tokens supplied via OpenDuckDB options take precedence over this value.
	Token string
	// DisableSSL disables HTTPS for the remote connection. Quack assumes HTTPS
	// for non-local hosts; set this for plaintext local/dev servers.
	DisableSSL bool
}

// AttachURI returns the `quack:host[:port]` string passed to DuckDB's ATTACH.
func (t Target) AttachURI() string { return quackScheme + t.Endpoint }

// IsRemoteTarget reports whether s addresses a remote Quack server rather than a
// local file. Callers use this to skip filesystem setup (e.g. MkdirAll) for
// remote targets.
func IsRemoteTarget(s string) bool {
	return strings.HasPrefix(s, quackScheme)
}

// ParseTarget resolves a target string into a Target. Local paths are returned
// with IsRemote=false and no further interpretation. A `quack:` URI is parsed
// into its endpoint plus optional `?disable_ssl=` and `?token=` query options.
//
// Accepted remote forms:
//
//	quack:localhost
//	quack:host:9494
//	quack:[::1]:1234
//	quack://host:9494?disable_ssl=true
func ParseTarget(s string) (Target, error) {
	t := Target{Raw: s}
	if !IsRemoteTarget(s) {
		return t, nil
	}
	t.IsRemote = true

	rest := strings.TrimPrefix(s, quackScheme)
	if i := strings.IndexByte(rest, '?'); i >= 0 {
		vals, err := url.ParseQuery(rest[i+1:])
		if err != nil {
			return t, fmt.Errorf("invalid quack URI query string in %q: %w", s, err)
		}
		switch vals.Get("disable_ssl") {
		case "true", "1", "yes":
			t.DisableSSL = true
		}
		t.Token = vals.Get("token")
		rest = rest[:i]
	}

	// Tolerate an optional `//` authority prefix (quack://host).
	rest = strings.TrimPrefix(rest, "//")
	if rest == "" {
		return t, fmt.Errorf("quack URI is missing a host: %q", s)
	}
	if strings.ContainsAny(rest, "'\" \t\n") {
		return t, fmt.Errorf("quack URI host contains invalid characters: %q", s)
	}
	t.Endpoint = rest
	return t, nil
}

// connectOptions holds resolved connection options applied by OpenDuckDB.
type connectOptions struct {
	token      string
	disableSSL bool
}

// Option customizes how OpenDuckDB establishes a connection.
type Option func(*connectOptions)

// WithToken sets the authentication token for a remote Quack connection. It
// takes precedence over any token embedded in the target URI. Ignored for local
// targets.
func WithToken(token string) Option {
	return func(o *connectOptions) {
		if token != "" {
			o.token = token
		}
	}
}

// WithDisableSSL forces plaintext HTTP for a remote Quack connection.
func WithDisableSSL(disable bool) Option {
	return func(o *connectOptions) {
		if disable {
			o.disableSSL = true
		}
	}
}

// OpenDuckDB opens a *sql.DB for the given target. For a local path it behaves
// exactly like sql.Open("duckdb", path). For a `quack:` URI it opens an
// in-memory local DuckDB, loads the quack extension, attaches the remote server,
// and switches the default catalog to it so existing SQL runs unchanged.
//
// Because database/sql pools connections and ATTACH/USE/LOAD state is partly
// per-connection, the remote setup runs in the driver's connection-init hook so
// every pooled connection is configured identically.
func OpenDuckDB(ctx context.Context, target string, opts ...Option) (*sql.DB, error) {
	t, err := ParseTarget(target)
	if err != nil {
		return nil, err
	}

	if !t.IsRemote {
		// Local file (or in-memory): preserve existing behavior precisely.
		return sql.Open("duckdb", target)
	}

	o := connectOptions{token: t.Token, disableSSL: t.DisableSSL}
	for _, fn := range opts {
		fn(&o)
	}

	attach := buildAttachStatement(t, o)

	connector, err := duckdb.NewConnector("", func(execer driver.ExecerContext) error {
		return initRemoteConn(ctx, execer, attach)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create quack connector for %s: %w", t.AttachURI(), err)
	}

	return sql.OpenDB(connector), nil
}

// buildAttachStatement renders the ATTACH statement that connects the local
// DuckDB instance to the remote Quack server. IF NOT EXISTS makes it safe to run
// on every pooled connection, since ATTACH is database-level shared state.
func buildAttachStatement(t Target, o connectOptions) string {
	var b strings.Builder
	fmt.Fprintf(&b, "ATTACH IF NOT EXISTS '%s' AS %s", t.AttachURI(), RemoteAlias)

	var attachOpts []string
	if o.token != "" {
		attachOpts = append(attachOpts, fmt.Sprintf("TOKEN '%s'", SQLEscapeSingleQuotes(o.token)))
	}
	if o.disableSSL {
		attachOpts = append(attachOpts, "DISABLE_SSL true")
	}
	if len(attachOpts) > 0 {
		fmt.Fprintf(&b, " (%s)", strings.Join(attachOpts, ", "))
	}
	b.WriteByte(';')
	return b.String()
}

// initRemoteConn configures a freshly opened pooled connection so it can serve
// queries against the attached remote: load quack (installing from core_nightly
// on first use), attach the server, and make it the default catalog.
func initRemoteConn(ctx context.Context, execer driver.ExecerContext, attach string) error {
	exec := func(q string) error {
		_, err := execer.ExecContext(ctx, q, nil)
		return err
	}

	if err := EnsureQuackLoaded(exec); err != nil {
		return err
	}

	if err := exec(attach); err != nil {
		return fmt.Errorf("failed to attach remote quack server: %w", err)
	}
	if err := exec(fmt.Sprintf("USE %s;", RemoteAlias)); err != nil {
		return fmt.Errorf("failed to switch to remote catalog %q: %w", RemoteAlias, err)
	}
	return nil
}

// EnsureQuackLoaded loads the quack extension, installing it on first use. The
// extension's repository moved between DuckDB releases (core_nightly on 1.5.2,
// core on later versions), so this tries the cheap LOAD first, then a core
// install, then a core_nightly install. exec runs one SQL statement.
func EnsureQuackLoaded(exec func(string) error) error {
	if err := exec("LOAD quack;"); err == nil {
		return nil
	}
	// quack is a core extension on newer DuckDB versions.
	if err := exec("INSTALL quack;"); err == nil {
		if err := exec("LOAD quack;"); err == nil {
			return nil
		}
	}
	// Fall back to core_nightly (DuckDB 1.5.2).
	if err := exec("INSTALL quack FROM core_nightly;"); err != nil {
		return fmt.Errorf("failed to install quack extension (needs network on first use): %w", err)
	}
	if err := exec("LOAD quack;"); err != nil {
		return fmt.Errorf("failed to load quack extension after install: %w", err)
	}
	return nil
}

// SQLEscapeSingleQuotes escapes a value for inclusion inside a single-quoted SQL
// string literal by doubling embedded single quotes. Exported for use when
// building Quack server/client statements from the CLI.
func SQLEscapeSingleQuotes(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}
