package main

import (
	"fmt"
	"strings"

	pb "github.com/jlgore/corkscrew/internal/proto"
	"github.com/jlgore/corkscrew/plugins/aws-provider/pkg/scanner"
)

// Remove init function - debug prints break plugin protocol

// ARN patterns for different AWS services
var arnPatterns = map[string]map[string]string{
	"s3": {
		"Bucket": "arn:aws:s3:::%s", // S3 buckets don't have region/account in ARN
	},
	"ec2": {
		"Instance":       "arn:aws:ec2:%s:%s:instance/%s",
		"Volume":         "arn:aws:ec2:%s:%s:volume/%s",
		"SecurityGroup":  "arn:aws:ec2:%s:%s:security-group/%s",
		"Vpc":            "arn:aws:ec2:%s:%s:vpc/%s",
		"Subnet":         "arn:aws:ec2:%s:%s:subnet/%s",
	},
	"lambda": {
		"Function": "arn:aws:lambda:%s:%s:function:%s",
	},
	"rds": {
		"DBInstance": "arn:aws:rds:%s:%s:db:%s",
		"DBCluster":  "arn:aws:rds:%s:%s:cluster:%s",
	},
	"dynamodb": {
		"Table": "arn:aws:dynamodb:%s:%s:table/%s",
	},
	"iam": {
		"User":   "arn:aws:iam::%s:user/%s",
		"Role":   "arn:aws:iam::%s:role/%s",
		"Policy": "arn:aws:iam::%s:policy/%s",
	},
}

// Service-specific identifier extraction patterns
var identifierPatterns = map[string]map[string]string{
	"s3": {
		"ListBuckets": "Name", // S3 bucket name comes from Name field
	},
	"ec2": {
		"DescribeInstances":      "InstanceId",
		"DescribeVolumes":        "VolumeId",
		"DescribeSecurityGroups": "GroupId",
		"DescribeVpcs":           "VpcId",
		"DescribeSubnets":        "SubnetId",
	},
	"lambda": {
		"ListFunctions": "FunctionName",
	},
	"dynamodb": {
		"ListTables": "TableName",
	},
	"rds": {
		"DescribeDBInstances": "DBInstanceIdentifier",
		"DescribeDBClusters":  "DBClusterIdentifier",
	},
	"iam": {
		"ListUsers":    "UserName",
		"ListRoles":    "RoleName",
		"ListPolicies": "PolicyName",
	},
}

// ensureARNAsID ensures each resource has a proper ARN as its ID
func ensureARNAsID(s *scanner.UnifiedScanner, resource *pb.ResourceRef, serviceName string) {
	fmt.Printf("DEBUG: ensureARNAsID called for service=%s, resourceType=%s, resourceName=%s, currentID=%s\n", 
		serviceName, resource.Type, resource.Name, resource.Id)

	// If we already have an ARN, use it as ID
	if arn, ok := resource.BasicAttributes["arn"]; ok && strings.HasPrefix(arn, "arn:") {
		resource.Id = arn
		fmt.Printf("DEBUG: Using existing ARN from attributes: %s\n", arn)
		return
	}

	// If ID already looks like an ARN, we're done
	if strings.HasPrefix(resource.Id, "arn:") {
		resource.BasicAttributes["arn"] = resource.Id
		fmt.Printf("DEBUG: ID already is ARN: %s\n", resource.Id)
		return
	}

	// Extract proper resource identifier based on AWS patterns
	identifier := extractProperIdentifier(serviceName, resource)
	fmt.Printf("DEBUG: Extracted identifier: '%s'\n", identifier)
	if identifier == "" {
		fmt.Printf("DEBUG: No identifier found, cannot generate ARN\n")
		return // Can't construct ARN without identifier
	}

	// Extract account ID and region from scanner context or STS
	accountID := getAccountID()
	region := getRegion()
	if region == "" {
		region = resource.Region
	}
	fmt.Printf("DEBUG: accountID=%s, region=%s\n", accountID, region)

	// Construct ARN based on service and resource type
	arn := constructARN(serviceName, region, accountID, resource.Type, identifier)
	fmt.Printf("DEBUG: Constructed ARN: '%s'\n", arn)
	
	// Set both ID and arn attribute
	if arn != "" {
		resource.Id = arn
		resource.BasicAttributes["arn"] = arn
		fmt.Printf("DEBUG: Set resource.Id to ARN: %s\n", arn)
	} else {
		fmt.Printf("DEBUG: ARN construction failed, resource.Id remains: %s\n", resource.Id)
	}
}

// extractProperIdentifier extracts the correct identifier for a resource based on AWS service patterns
func extractProperIdentifier(serviceName string, resource *pb.ResourceRef) string {
	fmt.Printf("DEBUG: extractProperIdentifier - service=%s, type=%s, name=%s, id=%s\n", 
		serviceName, resource.Type, resource.Name, resource.Id)
	
	// Handle service-specific identifier extraction
	switch serviceName {
	case "s3":
		// For S3, the bucket name (stored in Name field) is the identifier
		if resource.Type == "Bucket" && resource.Name != "" {
			fmt.Printf("DEBUG: S3 Bucket - using name as identifier: %s\n", resource.Name)
			return resource.Name
		}
		fmt.Printf("DEBUG: S3 resource but not a bucket or no name: type=%s, name=%s\n", resource.Type, resource.Name)
	case "ec2":
		// For EC2, prefer specific ID fields over generic ones
		if instanceId, exists := resource.BasicAttributes["instanceid"]; exists {
			return instanceId
		}
		if volumeId, exists := resource.BasicAttributes["volumeid"]; exists {
			return volumeId
		}
		if groupId, exists := resource.BasicAttributes["groupid"]; exists {
			return groupId
		}
	case "lambda":
		// For Lambda, function name is the identifier
		if resource.Type == "Function" && resource.Name != "" {
			return resource.Name
		}
		if functionName, exists := resource.BasicAttributes["functionname"]; exists {
			return functionName
		}
	case "dynamodb":
		// For DynamoDB, table name is the identifier
		if resource.Type == "Table" && resource.Name != "" {
			return resource.Name
		}
		if tableName, exists := resource.BasicAttributes["tablename"]; exists {
			return tableName
		}
	case "iam":
		// For IAM, name is usually the identifier
		if resource.Name != "" {
			return resource.Name
		}
	}
	
	// Fallback to existing ID or Name
	if resource.Id != "" && !strings.HasPrefix(resource.Id, "arn:") {
		return resource.Id
	}
	if resource.Name != "" {
		return resource.Name
	}
	
	return ""
}

// constructARN builds an ARN from components
func constructARN(service, region, accountID, resourceType, identifier string) string {
	fmt.Printf("DEBUG: constructARN - service=%s, region=%s, accountID=%s, resourceType=%s, identifier=%s\n",
		service, region, accountID, resourceType, identifier)
	
	// Handle service-specific ARN patterns
	if servicePatterns, exists := arnPatterns[service]; exists {
		fmt.Printf("DEBUG: Found service patterns for %s\n", service)
		if pattern, exists := servicePatterns[resourceType]; exists {
			fmt.Printf("DEBUG: Found pattern for %s/%s: %s\n", service, resourceType, pattern)
			switch service {
			case "s3":
				// S3 ARNs don't include region/account
				arn := fmt.Sprintf(pattern, identifier)
				fmt.Printf("DEBUG: S3 ARN generated: %s\n", arn)
				return arn
			case "iam":
				// IAM ARNs don't include region
				arn := fmt.Sprintf(pattern, accountID, identifier)
				fmt.Printf("DEBUG: IAM ARN generated: %s\n", arn)
				return arn
			default:
				// Standard format with region and account
				arn := fmt.Sprintf(pattern, region, accountID, identifier)
				fmt.Printf("DEBUG: Standard ARN generated: %s\n", arn)
				return arn
			}
		} else {
			fmt.Printf("DEBUG: No pattern found for resourceType: %s\n", resourceType)
		}
	} else {
		fmt.Printf("DEBUG: No service patterns found for service: %s\n", service)
	}
	
	// Fallback to generic ARN format
	if identifier != "" {
		arn := fmt.Sprintf("arn:aws:%s:%s:%s:%s/%s", service, region, accountID, strings.ToLower(resourceType), identifier)
		fmt.Printf("DEBUG: Generic ARN generated: %s\n", arn)
		return arn
	}
	
	fmt.Printf("DEBUG: No identifier provided, returning empty ARN\n")
	return ""
}

// getAccountID retrieves the AWS account ID from STS GetCallerIdentity
func getAccountID() string {
	fmt.Printf("DEBUG: getAccountID called\n")
	
	// Try to get from cache first
	if accountID, ok := getFromCache("account_id"); ok {
		fmt.Printf("DEBUG: Found cached account ID: %s\n", accountID)
		return accountID
	}
	
	// Simplified - cannot get STS client without scanner context
	// Return empty for now
	return ""
}

// getRegion retrieves the current AWS region
func getRegion() string {
	// Default fallback - region will come from resource context
	return ""
}

// Simple in-memory cache for the scanner
var scannerCache = make(map[string]string)

// getFromCache is a helper to retrieve cached values
func getFromCache(key string) (string, bool) {
	value, exists := scannerCache[key]
	return value, exists
}

// setInCache is a helper to store cached values
func setInCache(key string, value string) {
	scannerCache[key] = value
}

// extractResourceIdentifier extracts the proper identifier from a resource based on service patterns
func extractResourceIdentifier(serviceName, operationName string, resource *pb.ResourceRef) string {
	// Check if we have a specific pattern for this service/operation
	if servicePatterns, exists := identifierPatterns[serviceName]; exists {
		if identifierField, exists := servicePatterns[operationName]; exists {
			// Try to extract the identifier from basic attributes
			if identifier, exists := resource.BasicAttributes[strings.ToLower(identifierField)]; exists {
				return identifier
			}
		}
	}
	
	// Fallback to existing ID or Name
	if resource.Id != "" && !strings.HasPrefix(resource.Id, "arn:") {
		return resource.Id
	}
	if resource.Name != "" {
		return resource.Name
	}
	
	return ""
}