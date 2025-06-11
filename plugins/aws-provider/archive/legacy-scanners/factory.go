package client

import (
	"fmt"
	"reflect"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/elasticache"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/kinesis"
	"github.com/aws/aws-sdk-go-v2/service/glue"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	"github.com/aws/aws-sdk-go-v2/service/redshift"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/aws/aws-sdk-go-v2/service/acm"
	"github.com/aws/aws-sdk-go-v2/service/apigatewayv2"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
)

// ClientFactory manages AWS service clients with lazy initialization
type ClientFactory struct {
	config  aws.Config
	mu      sync.RWMutex
	clients map[string]interface{}
}

// NewClientFactory creates a new client factory
func NewClientFactory(cfg aws.Config) *ClientFactory {
	return &ClientFactory{
		config:  cfg,
		clients: make(map[string]interface{}),
	}
}

// GetClient returns a client for the specified service, creating it if necessary
func (cf *ClientFactory) GetClient(serviceName string) interface{} {
	cf.mu.RLock()
	if client, exists := cf.clients[serviceName]; exists {
		cf.mu.RUnlock()
		return client
	}
	cf.mu.RUnlock()

	// Create client if it doesn't exist
	cf.mu.Lock()
	defer cf.mu.Unlock()

	// Double-check in case another goroutine created it
	if client, exists := cf.clients[serviceName]; exists {
		return client
	}

	client := cf.createClient(serviceName)
	if client != nil {
		cf.clients[serviceName] = client
	}

	return client
}

// createClient creates a new client for the specified service
func (cf *ClientFactory) createClient(serviceName string) interface{} {
	switch serviceName {
	case "s3":
		return s3.NewFromConfig(cf.config)
	case "ec2":
		return ec2.NewFromConfig(cf.config)
	case "lambda":
		return lambda.NewFromConfig(cf.config)
	case "rds":
		return rds.NewFromConfig(cf.config)
	case "dynamodb":
		return dynamodb.NewFromConfig(cf.config)
	case "iam":
		return iam.NewFromConfig(cf.config)
	case "ecs":
		return ecs.NewFromConfig(cf.config)
	case "eks":
		return eks.NewFromConfig(cf.config)
	case "elasticache":
		return elasticache.NewFromConfig(cf.config)
	case "cloudformation":
		return cloudformation.NewFromConfig(cf.config)
	case "cloudwatch":
		return cloudwatch.NewFromConfig(cf.config)
	case "sns":
		return sns.NewFromConfig(cf.config)
	case "sqs":
		return sqs.NewFromConfig(cf.config)
	case "kinesis":
		return kinesis.NewFromConfig(cf.config)
	case "glue":
		return glue.NewFromConfig(cf.config)
	case "route53":
		return route53.NewFromConfig(cf.config)
	case "redshift":
		return redshift.NewFromConfig(cf.config)
	case "sts":
		return sts.NewFromConfig(cf.config)
	case "kms":
		return kms.NewFromConfig(cf.config)
	case "secretsmanager":
		return secretsmanager.NewFromConfig(cf.config)
	case "ssm":
		return ssm.NewFromConfig(cf.config)
	case "acm":
		return acm.NewFromConfig(cf.config)
	case "apigatewayv2":
		return apigatewayv2.NewFromConfig(cf.config)
	case "elasticloadbalancingv2":
		return elasticloadbalancingv2.NewFromConfig(cf.config)
	default:
		return nil
	}
}

// GetAvailableServices returns a list of services that have clients available
func (cf *ClientFactory) GetAvailableServices() []string {
	return []string{
		"s3", "ec2", "lambda", "rds", "dynamodb", "iam",
		"ecs", "eks", "elasticache", "cloudformation",
		"cloudwatch", "sns", "sqs", "kinesis", "glue",
		"route53", "redshift", "sts", "kms", "secretsmanager",
		"ssm", "acm", "apigatewayv2", "elasticloadbalancingv2",
	}
}

// GetClientType returns the reflect.Type of the client for a service
func (cf *ClientFactory) GetClientType(serviceName string) reflect.Type {
	client := cf.GetClient(serviceName)
	if client == nil {
		return nil
	}
	return reflect.TypeOf(client)
}

// HasClient checks if a client is available for the specified service
func (cf *ClientFactory) HasClient(serviceName string) bool {
	return cf.GetClient(serviceName) != nil
}

// Close cleans up all clients (if needed)
func (cf *ClientFactory) Close() error {
	cf.mu.Lock()
	defer cf.mu.Unlock()

	// Clear all clients
	cf.clients = make(map[string]interface{})
	return nil
}

// GetClientMethodNames returns the names of all methods on a client
func (cf *ClientFactory) GetClientMethodNames(serviceName string) []string {
	client := cf.GetClient(serviceName)
	if client == nil {
		return nil
	}

	clientType := reflect.TypeOf(client)
	if clientType.Kind() == reflect.Ptr {
		clientType = clientType.Elem()
	}

	var methods []string
	for i := 0; i < clientType.NumMethod(); i++ {
		method := clientType.Method(i)
		methods = append(methods, method.Name)
	}

	return methods
}

// GetListOperations returns all operations that appear to be List operations
func (cf *ClientFactory) GetListOperations(serviceName string) []string {
	methods := cf.GetClientMethodNames(serviceName)
	var listOps []string

	for _, method := range methods {
		if cf.isListOperation(method) {
			listOps = append(listOps, method)
		}
	}

	return listOps
}

// GetDescribeOperations returns all operations that appear to be Describe operations
func (cf *ClientFactory) GetDescribeOperations(serviceName string) []string {
	methods := cf.GetClientMethodNames(serviceName)
	var describeOps []string

	for _, method := range methods {
		if cf.isDescribeOperation(method) {
			describeOps = append(describeOps, method)
		}
	}

	return describeOps
}

// Helper methods

func (cf *ClientFactory) isListOperation(methodName string) bool {
	listPrefixes := []string{"List", "Describe"}
	
	for _, prefix := range listPrefixes {
		if len(methodName) > len(prefix) && methodName[:len(prefix)] == prefix {
			// Check if it looks like a list operation (plural or contains multiple)
			remaining := methodName[len(prefix):]
			if cf.looksLikeListOperation(remaining) {
				return true
			}
		}
	}
	
	return false
}

func (cf *ClientFactory) isDescribeOperation(methodName string) bool {
	return len(methodName) > 8 && methodName[:8] == "Describe" ||
		   len(methodName) > 3 && methodName[:3] == "Get"
}

func (cf *ClientFactory) looksLikeListOperation(remainder string) bool {
	// Check for plural forms or words that indicate listing
	pluralIndicators := []string{"s", "es", "ies"}
	listWords := []string{"All", "Many", "Multiple"}
	
	// Check for plural endings
	for _, indicator := range pluralIndicators {
		if len(remainder) > len(indicator) && remainder[len(remainder)-len(indicator):] == indicator {
			return true
		}
	}
	
	// Check for list-indicating words
	for _, word := range listWords {
		if remainder == word || len(remainder) > len(word) && remainder[:len(word)] == word {
			return true
		}
	}
	
	return false
}

// GetServiceDisplayName returns a formatted display name for a service
func (cf *ClientFactory) GetServiceDisplayName(serviceName string) string {
	displayNames := map[string]string{
		"s3":             "Simple Storage Service",
		"ec2":            "Elastic Compute Cloud",
		"lambda":         "Lambda",
		"rds":            "Relational Database Service",
		"dynamodb":       "DynamoDB",
		"iam":            "Identity and Access Management",
		"ecs":            "Elastic Container Service",
		"eks":            "Elastic Kubernetes Service",
		"elasticache":    "ElastiCache",
		"cloudformation": "CloudFormation",
		"cloudwatch":     "CloudWatch",
		"sns":            "Simple Notification Service",
		"sqs":            "Simple Queue Service",
		"kinesis":        "Kinesis",
		"glue":           "Glue",
		"route53":                 "Route 53",
		"redshift":                "Redshift",
		"sts":                     "Security Token Service",
		"kms":                     "Key Management Service",
		"secretsmanager":          "Secrets Manager",
		"ssm":                     "Systems Manager",
		"acm":                     "Certificate Manager",
		"apigatewayv2":            "API Gateway V2",
		"elasticloadbalancingv2":  "Elastic Load Balancing V2",
	}

	if displayName, exists := displayNames[serviceName]; exists {
		return displayName
	}

	return serviceName
}

// GetServiceDescription returns a description for a service
func (cf *ClientFactory) GetServiceDescription(serviceName string) string {
	descriptions := map[string]string{
		"s3":             "Object storage service for storing and retrieving data",
		"ec2":            "Virtual servers in the cloud",
		"lambda":         "Serverless compute service",
		"rds":            "Managed relational database service",
		"dynamodb":       "NoSQL database service",
		"iam":            "Identity and access management service",
		"ecs":            "Container orchestration service",
		"eks":            "Managed Kubernetes service",
		"elasticache":    "In-memory caching service",
		"cloudformation": "Infrastructure as code service",
		"cloudwatch":     "Monitoring and observability service",
		"sns":            "Publish-subscribe messaging service",
		"sqs":            "Message queuing service",
		"kinesis":        "Real-time data streaming service",
		"glue":           "Extract, transform, and load (ETL) service",
		"route53":                 "Domain name system (DNS) service",
		"redshift":                "Data warehousing service",
		"sts":                     "Temporary security credentials service",
		"kms":                     "Encryption key management service",
		"secretsmanager":          "Secrets storage and rotation service",
		"ssm":                     "Infrastructure management and automation",
		"acm":                     "SSL/TLS certificate management",
		"apigatewayv2":            "HTTP and WebSocket API management",
		"elasticloadbalancingv2":  "Application load balancing service",
	}

	if description, exists := descriptions[serviceName]; exists {
		return description
	}

	return fmt.Sprintf("AWS %s service", serviceName)
}