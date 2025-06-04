//go:build generate
// +build generate

package main

// This file contains go:generate directives for code generation in the AWS provider.
// Run 'go generate ./...' or 'make generate' to execute all generators.

//go:generate go run ./cmd/analyzer -output generated/services.json -sdk-path $GOPATH/pkg/mod/github.com/aws/aws-sdk-go-v2@latest
//go:generate go run ./cmd/schema-generator -services generated/services.json -output-dir generated/schemas -format sql
//go:generate go run ./cmd/registry-generator -services generated/services.json -output generated/scanner_registry.go -package main
//go:generate go fmt ./generated/...

// Additional generators for specific components
//go:generate go run ./internal/generator/client_factory.go -output generated/client_factory.go
//go:generate go run ./internal/generator/scanner_templates.go -output generated/scanner_implementations.go

// Generate mock implementations for testing
//go:generate mockgen -source=scanner/interfaces.go -destination=mocks/scanner_mocks.go -package=mocks
//go:generate mockgen -source=discovery/interfaces.go -destination=mocks/discovery_mocks.go -package=mocks