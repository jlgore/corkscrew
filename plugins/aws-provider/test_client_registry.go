//go:build ignore
// +build ignore

package main

import (
	"fmt"
	"log"
	
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/jlgore/corkscrew/plugins/aws-provider/pkg/client"
)

func main() {
	log.Println("Testing client registry...")
	
	// Check what services are registered
	services := client.ListRegisteredServices()
	fmt.Printf("Registered services: %v\n", services)
	
	// Try to get S3 constructor
	s3Constructor := client.GetConstructor("s3")
	if s3Constructor != nil {
		fmt.Println("✓ S3 constructor found!")
	} else {
		fmt.Println("✗ S3 constructor NOT found!")
	}
	
	// Try to create a client factory
	cfg := aws.Config{Region: "us-east-1"}
	factory := client.NewClientFactory(cfg)
	
	// Try to get S3 client
	s3Client := factory.GetClient("s3")
	if s3Client != nil {
		fmt.Println("✓ S3 client created!")
		fmt.Printf("  Client type: %T\n", s3Client)
	} else {
		fmt.Println("✗ S3 client NOT created!")
	}
}