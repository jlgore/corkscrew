package main

import (
	"context"
	"flag"
	"fmt"
	"log"

	"github.com/jlgore/corkscrew/internal/server"
)

// runServe starts the gRPC API server.
func runServe(ctx context.Context, args []string) {
	if err := runServeE(ctx, args); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

func runServeE(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)

	port := fs.Int("port", 9090, "Port to listen on")
	host := fs.String("host", "localhost", "Host to bind to")

	if err := fs.Parse(args); err != nil {
		return err
	}

	fmt.Printf("🚀 Starting Corkscrew gRPC API server...\n")
	fmt.Printf("📍 Listening on %s:%d\n", *host, *port)
	fmt.Printf("\n💡 Example commands to test the API:\n")
	fmt.Printf("  grpcurl -plaintext %s:%d list\n", *host, *port)
	fmt.Printf("  grpcurl -plaintext %s:%d corkscrew.api.CorkscrewAPI.HealthCheck\n", *host, *port)
	fmt.Printf("  grpcurl -plaintext %s:%d corkscrew.api.CorkscrewAPI.ListProviders\n", *host, *port)
	fmt.Printf("\n📖 For more gRPC client examples, visit the documentation\n")
	fmt.Printf("⏹️  Press Ctrl+C to stop the server\n\n")

	// StartAPIServer blocks and takes no context, so race it against the root context: the
	// root context owns SIGINT/SIGTERM, and without this Ctrl-C would no longer stop the server.
	served := make(chan error, 1)
	go func() { served <- server.StartAPIServer(*port) }()
	select {
	case err := <-served:
		return err
	case <-ctx.Done():
		fmt.Printf("\n👋 Shutting down Corkscrew gRPC API server\n")
		return nil
	}
}
