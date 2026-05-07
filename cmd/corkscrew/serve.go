package main

import (
	"flag"
	"fmt"
	"log"

	"github.com/jlgore/corkscrew/internal/server"
)

// runServe starts the gRPC API server.
func runServe(args []string) {
	if err := runServeE(args); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

func runServeE(args []string) error {
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

	return server.StartAPIServer(*port)
}
