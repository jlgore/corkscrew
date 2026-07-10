package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/jlgore/corkscrew/internal/db"
)

// runQuack dispatches `corkscrew quack <subcommand>`. The quack command group
// exposes the DuckDB Quack client-server protocol: serving a local scan database
// over the wire so other corkscrew instances (or any Quack client) can query it.
//
// The client side needs no dedicated subcommand: pass a `quack:` URI to the
// existing `--database`/`--db` flags of scan and query.
func runQuack(args []string) {
	if len(args) == 0 {
		printQuackUsage()
		os.Exit(1)
	}
	switch args[0] {
	case "serve":
		runQuackServe(args[1:])
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

// runQuackServe opens a local DuckDB database and serves it over Quack, blocking
// until interrupted.
func runQuackServe(args []string) {
	fs := flag.NewFlagSet("quack serve", flag.ExitOnError)
	database := fs.String("database", "", "Local DuckDB database to expose (default: the unified corkscrew database)")
	listen := fs.String("listen", "localhost:9494", "Address to listen on, host:port")
	token := fs.String("token", "", "Fixed authentication token (min 4 chars). If empty, the server generates one")
	allowRemote := fs.Bool("allow-remote", false, "Allow binding to a non-local hostname (also implied when --listen is not local)")
	disableSSL := fs.Bool("disable-ssl", false, "Advertise plaintext HTTP to clients (Quack never terminates TLS itself)")
	if err := fs.Parse(args); err != nil {
		fatalf("failed to parse flags: %v", err)
	}

	if *token != "" && len(*token) < 4 {
		fatalf("--token must be at least 4 characters")
	}
	if db.IsRemoteTarget(*database) {
		fatalf("--database must be a local file for quack serve, got remote URI %q", *database)
	}

	ctx := context.Background()

	// Resolve and open the local database we want to expose. We reuse the
	// unified initializer so a fresh database gets its schema created.
	cfg, err := db.InitializeUnifiedDatabase(*database)
	if err != nil {
		fatalf("failed to open database: %v", err)
	}
	defer cfg.DB.Close()

	// Pin a single connection for the lifetime of the server. The Quack listener
	// runs on a background thread of the DuckDB instance; holding one connection
	// open keeps the instance (and therefore the listener) alive.
	conn, err := cfg.DB.Conn(ctx)
	if err != nil {
		fatalf("failed to acquire connection: %v", err)
	}
	defer conn.Close()

	if err := db.EnsureQuackLoaded(func(q string) error {
		_, err := conn.ExecContext(ctx, q)
		return err
	}); err != nil {
		fatalf("%v", err)
	}

	allowOther := *allowRemote || !isLocalListen(*listen)
	call := buildServeCall("quack:"+*listen, *token, allowOther, *disableSSL)

	rows, err := conn.QueryContext(ctx, call)
	if err != nil {
		fatalf("quack_serve failed: %v", err)
	}
	cols, _ := rows.Columns()
	fmt.Printf("🦆 Quack server started, exposing %s\n", cfg.DatabasePath)
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			rows.Close()
			fatalf("failed to read quack_serve result: %v", err)
		}
		for i, c := range cols {
			fmt.Printf("   %-14s %v\n", c+":", vals[i])
		}
	}
	rows.Close()

	if !isLocalListen(*listen) {
		fmt.Printf("\n⚠️  Quack does not terminate TLS. Front this server with a TLS-terminating\n")
		fmt.Printf("   reverse proxy (nginx/Caddy) for any non-local deployment.\n")
	}
	fmt.Printf("\n⏹️  Press Ctrl+C to stop the server\n")

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	fmt.Printf("\n👋 Shutting down Quack server\n")
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

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "Error: "+format+"\n", args...)
	os.Exit(1)
}
