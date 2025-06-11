//go:build aws_services
// +build aws_services

package main

import (
	"log"
	
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/jlgore/corkscrew/plugins/aws-provider/pkg/client"
)

// InitializeGeneratedFactoryWrapper wraps the generated factory initialization
func InitializeGeneratedFactoryWrapper(cfg aws.Config) {
	log.Printf("Phase 2: Initializing generated client factory with %d registered services", len(client.ListRegisteredServices()))
	
	// The init() function in client_registry_init.go will have already
	// registered the constructors when the package was imported
	registeredServices := client.ListRegisteredServices()
	log.Printf("Available client constructors: %v", registeredServices)
	
	// The constructors are already registered via init(), so we just need to log this
	log.Printf("Client factory initialization completed - constructors ready for use")
}