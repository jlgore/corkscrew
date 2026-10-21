package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/jlgore/corkscrew/internal/data"
	"github.com/jlgore/corkscrew/internal/db"
)

// runQuack dispatches `corkscrew quack <subcommand>`. The quack command group
// exposes the DuckDB Quack client-server protocol: serving a local scan database
// over the wire so other corkscrew instances (or any Quack client) can query it.
//
// The client side needs no dedicated subcommand: pass a `quack:` URI to the
// existing `--database`/`--db` flags of scan and query.
func runQuack(ctx context.Context, args []string) {
	if len(args) == 0 {
		printQuackUsage()
		os.Exit(1)
	}
	switch args[0] {
	case "serve":
		if err := runQuackServe(ctx, args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	case "help", "--help", "-h":
		printQuackUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown quack subcommand: %q\n\n", args[0])
		printQuackUsage()
		os.Exit(1)
	}
}

func printQuackUsage() {
	fmt.Print(`Usage: corkscrew quack <subcommand> [flags]

Serve a corkscrew DuckDB database over the Quack client-server protocol, or
connect to a remote one.

Subcommands:
  serve    Expose a local scan database as a Quack server

To connect as a client, pass a quack: URI to a command's database flag:
  corkscrew query --db quack:host:9494 --token <token> "SELECT ..."
  corkscrew scan  --database quack:host:9494 --token <token>

The target URI and token may also be supplied via the CORKSCREW_QUACK_URL and
CORKSCREW_QUACK_TOKEN environment variables.

Note: the graph commands operate on a local DuckDB file only; remote graph
traversal is not yet supported.
`)
}

// minLocalQuackTokenLen is the shortest fixed token accepted for a loopback-only server, where
// only local users can reach the port.
const minLocalQuackTokenLen = 4

// minRemoteQuackTokenLen is the shortest fixed token accepted once the server is reachable from
// other hosts. The token is the only authentication Quack has, so it must resist online guessing.
const minRemoteQuackTokenLen = 32

// generatedQuackTokenBytes is the entropy of a token corkscrew generates for a remote server
// when none is given: 256 bits, hex-encoded to 64 characters.
const generatedQuackTokenBytes = 32

// runQuackServe opens a local DuckDB database and serves it over Quack, blocking
// until the root context is cancelled (Ctrl+C / SIGTERM). It returns errors rather than
// exiting so the deferred session and connection cleanup always runs.
func runQuackServe(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("quack serve", flag.ExitOnError)
	database := fs.String("database", "", "Local DuckDB database to expose (default: the unified corkscrew database)")
	listen := fs.String("listen", "localhost:9494", "Address to listen on, host:port")
	token := fs.String("token", "", "Fixed authentication token (min 4 chars local, 32 remote). "+
		"If empty, one is generated")
	allowRemote := fs.Bool("allow-remote", false, "Allow binding to a non-local hostname (also implied when --listen is not local)")
	disableSSL := fs.Bool("disable-ssl", false, "Advertise plaintext HTTP to clients (Quack never terminates TLS itself)")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("failed to parse flags: %w", err)
	}

	allowOther := *allowRemote || !isLocalListen(*listen)
	serveToken, err := quackServeToken(*token, allowOther)
	if err != nil {
		return err
	}
	if db.IsRemoteTarget(*database) {
		return fmt.Errorf("--database must be a local file for quack serve, got remote URI %q",
			*database)
	}

	session, err := data.OpenSession(ctx, *database)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer session.Close()

	// Pin a single connection for the lifetime of the server. The Quack listener
	// runs on a background thread of the DuckDB instance; holding one connection
	// open keeps the instance (and therefore the listener) alive.
	conn, err := session.Connection(ctx)
	if err != nil {
		return fmt.Errorf("failed to acquire connection: %w", err)
	}
	defer conn.Close()

	if err := db.EnsureQuackLoaded(func(q string) error {
		_, err := conn.ExecContext(ctx, q)
		return err
	}); err != nil {
		return err
	}

	call := buildServeCall("quack:"+*listen, serveToken, allowOther, *disableSSL)
	if err := startQuackServer(ctx, conn, call, session.Target()); err != nil {
		return err
	}

	if !isLocalListen(*listen) {
		fmt.Printf("\n⚠️  Quack does not terminate TLS. Front this server with a TLS-terminating\n")
		fmt.Printf("   reverse proxy (nginx/Caddy) for any non-local deployment.\n")
	}
	fmt.Printf("\n⏹️  Press Ctrl+C to stop the server\n")

	<-ctx.Done()
	fmt.Printf("\n👋 Shutting down Quack server\n")
	return nil
}

// quackServeToken validates a user-supplied token, or generates one for a remote server.
// An empty token on a loopback server is left to Quack to generate, as before.
func quackServeToken(token string, allowOther bool) (string, error) {
	if !allowOther {
		if token != "" && len(token) < minLocalQuackTokenLen {
			return "", fmt.Errorf("--token must be at least %d characters", minLocalQuackTokenLen)
		}
		return token, nil
	}
	if token == "" {
		raw := make([]byte, generatedQuackTokenBytes)
		if _, err := rand.Read(raw); err != nil {
			return "", fmt.Errorf("generate quack token: %w", err)
		}
		return hex.EncodeToString(raw), nil
	}
	if len(token) < minRemoteQuackTokenLen {
		return "", fmt.Errorf("--token must be at least %d characters when the server is "+
			"reachable from other hosts", minRemoteQuackTokenLen)
	}
	return token, nil
}

// startQuackServer runs the quack_serve CALL and prints the listener details it returns.
func startQuackServer(ctx context.Context, conn *sql.Conn, call, target string) error {
	rows, err := conn.QueryContext(ctx, call)
	if err != nil {
		return fmt.Errorf("quack_serve failed: %w", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return fmt.Errorf("failed to read quack_serve columns: %w", err)
	}
	fmt.Printf("🦆 Quack server started, exposing %s\n", target)
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return fmt.Errorf("failed to read quack_serve result: %w", err)
		}
		for i, c := range cols {
			fmt.Printf("   %-14s %v\n", c+":", vals[i])
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("failed to read quack_serve result: %w", err)
	}
	return nil
}

// buildServeCall renders the CALL quack_serve(...) statement.
func buildServeCall(uri, token string, allowOther, disableSSL bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "CALL quack_serve('%s'", db.SQLEscapeSingleQuotes(uri))
	if token != "" {
		fmt.Fprintf(&b, ", token := '%s'", db.SQLEscapeSingleQuotes(token))
	}
	if allowOther {
		b.WriteString(", allow_other_hostname := true")
	}
	if disableSSL {
		b.WriteString(", disable_ssl := true")
	}
	b.WriteByte(')')
	return b.String()
}

// isLocalListen reports whether a host:port binds only to the local machine.
func isLocalListen(listen string) bool {
	return strings.HasPrefix(listen, "localhost") ||
		strings.HasPrefix(listen, "127.") ||
		strings.HasPrefix(listen, "[::1]") ||
		strings.HasPrefix(listen, "::1")
}

// resolveQuackConn resolves the effective database target and connection options
// for a command's database flag. Precedence for the target: an explicitly-set
// flag value, then the CORKSCREW_QUACK_URL environment variable, then the
// default. For a remote quack: target, the token comes from the --token flag,
// falling back to CORKSCREW_QUACK_TOKEN.
func resolveQuackConn(flagVal, defaultVal, tokenFlag string) (string, []db.Option) {
	target := strings.TrimSpace(flagVal)
	if target == "" || target == defaultVal {
		if env := strings.TrimSpace(os.Getenv("CORKSCREW_QUACK_URL")); env != "" {
			target = env
		}
	}
	if target == "" {
		target = defaultVal
	}

	if !db.IsRemoteTarget(target) {
		return target, nil
	}

	token := strings.TrimSpace(tokenFlag)
	if token == "" {
		token = strings.TrimSpace(os.Getenv("CORKSCREW_QUACK_TOKEN"))
	}
	var opts []db.Option
	if token != "" {
		opts = append(opts, db.WithToken(token))
	}
	return target, opts
}
