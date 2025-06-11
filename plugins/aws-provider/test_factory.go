//go:build test_factory && aws_services
// +build test_factory,aws_services

package main

import (
	"context"
	"log"

	"github.com/aws/aws-sdk-go-v2/aws"
)

func main() {
	config := aws.Config{Region: "us-east-1"}
	factory := NewDynamicClientFactory(config, nil)
	
	client, err := factory.CreateClient(context.Background(), "s3")
	if err != nil {
		log.Fatalf("Failed to create S3 client: %v", err)
	}
	
	log.Printf("Successfully created S3 client: %T", client)
}