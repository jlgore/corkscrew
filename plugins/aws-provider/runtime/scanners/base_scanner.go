package scanners

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"reflect"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	pb "github.com/jlgore/corkscrew/internal/proto"
	"github.com/jlgore/corkscrew/plugins/aws-provider/classification"
	"github.com/jlgore/corkscrew/plugins/aws-provider/discovery"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// BaseScanner provides common functionality for all service scanners
type BaseScanner struct {
	ServiceName    string
	Region         string
	Client         interface{}
	Config         aws.Config
	Classifier     *classification.OperationClassifier
	ServiceHandler classification.ServiceHandler
}

// NewBaseScanner creates a new base scanner
func NewBaseScanner(serviceName string, client interface{}, config aws.Config) *BaseScanner {
	classifier := classification.NewOperationClassifier()
	
	// Get service handler
	var handler classification.ServiceHandler
	switch serviceName {
	case "s3":
		handler = &classification.S3Handler{}
	case "ec2":
		handler = &classification.EC2Handler{}
	case "iam":
		handler = &classification.IAMHandler{}
	case "lambda":
		handler = &classification.LambdaHandler{}
	case "dynamodb":
		handler = &classification.DynamoDBHandler{}
	default:
		// Use a generic handler
		handler = &GenericServiceHandler{serviceName: serviceName}
	}
	
	return &BaseScanner{
		ServiceName:    serviceName,
		Region:         config.Region,
		Client:         client,
		Config:         config,
		Classifier:     classifier,
		ServiceHandler: handler,
	}
}

// DiscoverResources performs phase 1: discover all resources using parameter-free operations
func (bs *BaseScanner) DiscoverResources(ctx context.Context) ([]discovery.AWSResourceRef, error) {
	var allResources []discovery.AWSResourceRef
	
	// Get all operations for this service using reflection
	clientValue := reflect.ValueOf(bs.Client)
	clientType := clientValue.Type()
	
	log.Printf("Discovering resources for service %s", bs.ServiceName)
	
	for i := 0; i < clientType.NumMethod(); i++ {
		method := clientType.Method(i)
		methodName := method.Name
		
		// Check if this is a List/Describe operation
		if !strings.HasPrefix(methodName, "List") && !strings.HasPrefix(methodName, "Describe") {
			continue
		}
		
		// Classify the operation
		classification := bs.Classifier.ClassifyOperation(bs.ServiceName, methodName, nil)
		
		if classification.CanAttempt {
			log.Printf("Attempting parameter-free operation: %s", methodName)
			resources, err := bs.executeDiscoveryOperation(ctx, methodName, classification.DefaultParams)
			if err != nil {
				log.Printf("Failed to execute %s: %v", methodName, err)
				continue
			}
			allResources = append(allResources, resources...)
		}
	}
	
	return allResources, nil
}

// EnrichResource performs phase 2: collect detailed configuration for a resource
func (bs *BaseScanner) EnrichResource(ctx context.Context, ref discovery.AWSResourceRef) (*pb.Resource, error) {
	resource := &pb.Resource{
		Provider:     "aws",
		Service:      bs.ServiceName,
		Type:         ref.Type,
		Id:           ref.ID,
		Name:         ref.Name,
		Region:       bs.Region,
		Arn:          ref.ARN,
		Tags:         make(map[string]string),
		DiscoveredAt: timestamppb.Now(),
	}
	
	// Copy metadata to tags
	for k, v := range ref.Metadata {
		if strings.HasPrefix(k, "tag_") {
			resource.Tags[strings.TrimPrefix(k, "tag_")] = v
		}
	}
	
	// Collect detailed configuration
	configData, err := bs.collectConfiguration(ctx, ref)
	if err != nil {
		log.Printf("Failed to collect configuration for %s %s: %v", ref.Type, ref.ID, err)
		// Continue with basic resource info
	} else if configData != nil {
		// Convert configuration to JSON
		rawDataBytes, err := json.Marshal(configData)
		if err != nil {
			log.Printf("Failed to marshal configuration data: %v", err)
		} else {
			resource.RawData = string(rawDataBytes)
		}
	}
	
	// Extract relationships (this could be enhanced based on the configuration data)
	resource.Relationships = bs.extractRelationships(ctx, ref, configData)
	
	return resource, nil
}

// executeDiscoveryOperation executes a parameter-free discovery operation
func (bs *BaseScanner) executeDiscoveryOperation(ctx context.Context, opName string, defaultParams map[string]interface{}) ([]discovery.AWSResourceRef, error) {
	clientValue := reflect.ValueOf(bs.Client)
	method := clientValue.MethodByName(opName)
	
	if !method.IsValid() {
		return nil, fmt.Errorf("method %s not found", opName)
	}
	
	// Create input
	methodType := method.Type()
	if methodType.NumIn() < 2 {
		return nil, fmt.Errorf("unexpected method signature")
	}
	
	inputType := methodType.In(1)
	input := reflect.New(inputType.Elem())
	
	// Apply default parameters if any
	if defaultParams != nil {
		bs.applyDefaultParams(input.Elem(), defaultParams)
	}
	
	// Call the method
	results := method.Call([]reflect.Value{
		reflect.ValueOf(ctx),
		input,
	})
	
	if len(results) < 2 {
		return nil, fmt.Errorf("unexpected return values")
	}
	
	