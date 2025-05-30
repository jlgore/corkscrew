package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/jlgore/corkscrew/plugins/aws-provider/discovery"
)

func main() {
	var (
		serviceName = flag.String("service", "s3", "AWS service name to analyze")
		githubToken = flag.String("token", os.Getenv("GITHUB_TOKEN"), "GitHub token for API access")
	)
	flag.Parse()

	if *serviceName == "" {
		log.Fatal("Service name is required")
	}

	// Create service discovery instance
	sd := discovery.NewAWSServiceDiscovery(*githubToken)
	ctx := context.Background()

	fmt.Printf("Analyzing AWS service: %s\n\n", *serviceName)

	// Perform deep analysis of the service
	metadata, err := sd.AnalyzeService(ctx, *serviceName)
	if err != nil {
		log.Fatalf("Failed to analyze service: %v", err)
	}

	// Display results
	fmt.Printf("Service: %s\n", metadata.Name)
	fmt.Printf("Package: %s\n", metadata.PackagePath)
	fmt.Printf("Version: %s\n", metadata.Version)
	fmt.Printf("Last Updated: %s\n\n", metadata.LastUpdated.Format("2006-01-02 15:04:05"))

	fmt.Printf("Operations (%d):\n", metadata.OperationCount)
	for i, op := range metadata.Operations {
		fmt.Printf("  %3d. %s\n", i+1, op)
		if i >= 19 && len(metadata.Operations) > 20 {
			fmt.Printf("  ... and %d more\n", len(metadata.Operations)-20)
			break
		}
	}

	fmt.Printf("\nResources (%d):\n", metadata.ResourceCount)
	for i, res := range metadata.Resources {
		fmt.Printf("  %3d. %s\n", i+1, res)
		if i >= 19 && len(metadata.Resources) > 20 {
			fmt.Printf("  ... and %d more\n", len(metadata.Resources)-20)
			break
		}
	}

	// Example: Check if service supports specific operations
	fmt.Printf("\nChecking for common operations:\n")
	commonOps := []string{"List", "Get", "Create", "Delete", "Update"}
	for _, opPrefix := range commonOps {
		count := 0
		for _, op := range metadata.Operations {
			if len(op) > len(opPrefix) && op[:len(opPrefix)] == opPrefix {
				count++
			}
		}
		if count > 0 {
			fmt.Printf("  %s* operations: %d\n", opPrefix, count)
		}
	}
}