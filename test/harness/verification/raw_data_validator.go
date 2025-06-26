package verification

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	elbv2types "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	rdstypes "github.com/aws/aws-sdk-go-v2/service/rds/types"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// RawDataValidator validates raw_data fields contain complete AWS API responses
type RawDataValidator struct {
	expectedFields map[string][]string
	awsTypeMap     map[string]reflect.Type
}

// NewRawDataValidator creates a new raw data validator
func NewRawDataValidator() *RawDataValidator {
	v := &RawDataValidator{
		expectedFields: make(map[string][]string),
		awsTypeMap:     make(map[string]reflect.Type),
	}
	v.initializeExpectedFields()
	v.initializeAWSTypes()
	return v
}

// initializeExpectedFields defines expected fields per resource type
func (v *RawDataValidator) initializeExpectedFields() {
	// EC2 Instance expected fields from DescribeInstances API
	v.expectedFields["AWS::EC2::Instance"] = []string{
		"InstanceId", "InstanceType", "State", "StateTransitionReason",
		"PrivateDnsName", "PrivateIpAddress", "PublicDnsName", "PublicIpAddress",
		"SubnetId", "VpcId", "Architecture", "BlockDeviceMappings",
		"IamInstanceProfile", "ImageId", "KeyName", "LaunchTime",
		"Monitoring", "NetworkInterfaces", "Placement", "Platform",
		"SecurityGroups", "Tags", "VirtualizationType", "CpuOptions",
		"HibernationOptions", "MetadataOptions", "EnclaveOptions",
		"BootMode", "PlatformDetails", "UsageOperation", "UsageOperationUpdateTime",
	}

	// VPC expected fields from DescribeVpcs API
	v.expectedFields["AWS::EC2::VPC"] = []string{
		"VpcId", "State", "CidrBlock", "DhcpOptionsId",
		"Tags", "InstanceTenancy", "IsDefault", "EnableDnsSupport",
		"EnableDnsHostnames", "CidrBlockAssociationSet", "Ipv6CidrBlockAssociationSet",
		"OwnerId",
	}

	// Subnet expected fields from DescribeSubnets API
	v.expectedFields["AWS::EC2::Subnet"] = []string{
		"SubnetId", "State", "VpcId", "CidrBlock",
		"AvailableIpAddressCount", "AvailabilityZone", "AvailabilityZoneId",
		"DefaultForAz", "MapPublicIpOnLaunch", "Tags", "AssignIpv6AddressOnCreation",
		"Ipv6CidrBlockAssociationSet", "CustomerOwnedIpv4Pool", "MapCustomerOwnedIpOnLaunch",
		"EnableDns64", "Ipv6Native", "PrivateDnsNameOptionsOnLaunch",
		"SubnetArn", "OwnerId", "EnableLniAtDeviceIndex",
	}

	// Security Group expected fields from DescribeSecurityGroups API
	v.expectedFields["AWS::EC2::SecurityGroup"] = []string{
		"GroupId", "GroupName", "Description", "IpPermissions",
		"IpPermissionsEgress", "OwnerId", "Tags", "VpcId",
	}

	// S3 Bucket expected fields from ListBuckets + GetBucketLocation + GetBucketTagging etc
	v.expectedFields["AWS::S3::Bucket"] = []string{
		"Name", "CreationDate", "BucketRegion", "Tags",
		"Versioning", "Logging", "Acl", "Policy",
		"Lifecycle", "Replication", "Encryption", "Website",
		"Cors", "Notification", "AccelerateConfiguration", "RequestPayment",
		"Analytics", "Metrics", "Inventory", "PublicAccessBlock",
		"OwnershipControls", "IntelligentTiering",
	}

	// RDS DB Instance expected fields from DescribeDBInstances API
	v.expectedFields["AWS::RDS::DBInstance"] = []string{
		"DBInstanceIdentifier", "DBInstanceClass", "Engine", "DBInstanceStatus",
		"MasterUsername", "DBName", "Endpoint", "AllocatedStorage",
		"InstanceCreateTime", "PreferredBackupWindow", "BackupRetentionPeriod",
		"DBSecurityGroups", "VpcSecurityGroups", "DBParameterGroups",
		"AvailabilityZone", "DBSubnetGroup", "PreferredMaintenanceWindow",
		"PendingModifiedValues", "LatestRestorableTime", "MultiAZ",
		"EngineVersion", "AutoMinorVersionUpgrade", "ReadReplicaSourceDBInstanceIdentifier",
		"ReadReplicaDBInstanceIdentifiers", "ReadReplicaDBClusterIdentifiers",
		"LicenseModel", "OptionGroupMemberships", "CharacterSetName",
		"StorageEncrypted", "KmsKeyId", "DbInstancePort", "DBClusterIdentifier",
		"StorageType", "TdeCredentialArn", "DbInstancePort", "DbiResourceId",
		"TagList", "DomainMemberships", "EnabledCloudwatchLogsExports",
		"ProcessorFeatures", "DeletionProtection", "AssociatedRoles",
		"ListenerEndpoint", "MaxAllocatedStorage", "EnabledCloudwatchLogsExports",
		"PerformanceInsightsEnabled", "PerformanceInsightsKMSKeyId",
		"PerformanceInsightsRetentionPeriod", "EnabledCloudwatchLogsExports",
		"ActivityStreamStatus", "ActivityStreamKmsKeyId", "ActivityStreamKinesisStreamName",
		"ActivityStreamMode", "ActivityStreamEngineNativeAuditFieldsIncluded",
		"AutomationMode", "CustomerOwnedIpEnabled", "BackupTarget",
		"NetworkType", "StorageThroughput", "MasterUserSecret",
		"CertificateDetails", "PercentProgress",
	}

	// Load Balancer expected fields from DescribeLoadBalancers API
	v.expectedFields["AWS::ElasticLoadBalancingV2::LoadBalancer"] = []string{
		"LoadBalancerArn", "DNSName", "CanonicalHostedZoneId", "CreatedTime",
		"LoadBalancerName", "Scheme", "VpcId", "State",
		"Type", "AvailabilityZones", "SecurityGroups", "IpAddressType",
		"CustomerOwnedIpv4Pool", "Tags",
	}

	// Target Group expected fields from DescribeTargetGroups API
	v.expectedFields["AWS::ElasticLoadBalancingV2::TargetGroup"] = []string{
		"TargetGroupArn", "TargetGroupName", "Protocol", "Port",
		"VpcId", "HealthCheckProtocol", "HealthCheckPort", "HealthCheckEnabled",
		"HealthCheckIntervalSeconds", "HealthCheckTimeoutSeconds", "HealthyThresholdCount",
		"UnhealthyThresholdCount", "HealthCheckPath", "Matcher",
		"LoadBalancerArns", "TargetType", "ProtocolVersion", "Tags",
		"IpAddressType",
	}

	// Lambda Function expected fields from GetFunction API
	v.expectedFields["AWS::Lambda::Function"] = []string{
		"FunctionName", "FunctionArn", "Runtime", "Role",
		"Handler", "CodeSize", "Description", "Timeout",
		"MemorySize", "LastModified", "CodeSha256", "Version",
		"VpcConfig", "DeadLetterConfig", "Environment", "KMSKeyArn",
		"TracingConfig", "MasterArn", "RevisionId", "Layers",
		"State", "StateReason", "StateReasonCode", "LastUpdateStatus",
		"LastUpdateStatusReason", "LastUpdateStatusReasonCode", "FileSystemConfigs",
		"PackageType", "ImageConfigResponse", "ImageConfig", "EphemeralStorage",
		"SnapStart", "RuntimeVersionConfig", "LoggingConfig", "Tags",
	}

	// DynamoDB Table expected fields from DescribeTable API
	v.expectedFields["AWS::DynamoDB::Table"] = []string{
		"TableName", "TableStatus", "CreationDateTime", "TableSizeBytes",
		"ItemCount", "TableArn", "TableId", "BillingModeSummary",
		"LocalSecondaryIndexes", "GlobalSecondaryIndexes", "StreamSpecification",
		"LatestStreamLabel", "LatestStreamArn", "GlobalTableVersion",
		"Replicas", "RestoreSummary", "SSEDescription", "ArchivalSummary",
		"TableClassSummary", "DeletionProtectionEnabled", "Tags",
		"AttributeDefinitions", "KeySchema", "ProvisionedThroughput",
		"StreamSpecification", "GlobalSecondaryIndexes", "LocalSecondaryIndexes",
		"Replicas", "RestoreSummary", "SSEDescription", "ArchivalSummary",
		"TableClassSummary", "DeletionProtectionEnabled", "OnDemandThroughputOverride",
	}

	// Route53 Hosted Zone expected fields
	v.expectedFields["AWS::Route53::HostedZone"] = []string{
		"Id", "Name", "CallerReference", "Config",
		"ResourceRecordSetCount", "LinkedService", "Tags",
	}

	// CloudFront Distribution expected fields
	v.expectedFields["AWS::CloudFront::Distribution"] = []string{
		"Id", "ARN", "Status", "LastModifiedTime",
		"InProgressInvalidationBatches", "DomainName", "ActiveTrustedSigners",
		"ActiveTrustedKeyGroups", "DistributionConfig", "AliasICPRecordals",
		"Tags",
	}

	// IAM Role expected fields
	v.expectedFields["AWS::IAM::Role"] = []string{
		"RoleName", "RoleId", "Arn", "CreateDate",
		"AssumeRolePolicyDocument", "Description", "MaxSessionDuration",
		"Path", "PermissionsBoundary", "Tags", "RoleLastUsed",
		"InlinePolicies", "AttachedPolicies",
	}

	// IAM User expected fields
	v.expectedFields["AWS::IAM::User"] = []string{
		"UserName", "UserId", "Arn", "CreateDate",
		"Path", "PasswordLastUsed", "PermissionsBoundary", "Tags",
		"InlinePolicies", "AttachedPolicies", "Groups",
	}

	// IAM Policy expected fields
	v.expectedFields["AWS::IAM::Policy"] = []string{
		"PolicyName", "PolicyId", "Arn", "Path",
		"DefaultVersionId", "AttachmentCount", "PermissionsBoundaryUsageCount",
		"IsAttachable", "Description", "CreateDate", "UpdateDate",
		"Tags", "PolicyVersionList",
	}

	// SNS Topic expected fields
	v.expectedFields["AWS::SNS::Topic"] = []string{
		"TopicArn", "Attributes", "Tags", "Subscriptions",
	}

	// SQS Queue expected fields
	v.expectedFields["AWS::SQS::Queue"] = []string{
		"QueueUrl", "Attributes", "Tags",
	}

	// Auto Scaling Group expected fields
	v.expectedFields["AWS::AutoScaling::AutoScalingGroup"] = []string{
		"AutoScalingGroupName", "AutoScalingGroupARN", "LaunchConfigurationName",
		"LaunchTemplate", "MixedInstancesPolicy", "MinSize", "MaxSize",
		"DesiredCapacity", "PredictedCapacity", "DefaultCooldown", "AvailabilityZones",
		"LoadBalancerNames", "TargetGroupARNs", "HealthCheckType", "HealthCheckGracePeriod",
		"Instances", "CreatedTime", "SuspendedProcesses", "PlacementGroup",
		"VPCZoneIdentifier", "EnabledMetrics", "Status", "Tags",
		"TerminationPolicies", "NewInstancesProtectedFromScaleIn", "ServiceLinkedRoleARN",
		"MaxInstanceLifetime", "CapacityRebalance", "WarmPoolConfiguration",
		"WarmPoolSize", "Context", "DesiredCapacityType", "DefaultInstanceWarmup",
		"TrafficSources", "InstanceMaintenancePolicy",
	}
}

// initializeAWSTypes maps resource types to their AWS SDK struct types
func (v *RawDataValidator) initializeAWSTypes() {
	v.awsTypeMap["AWS::EC2::Instance"] = reflect.TypeOf(types.Instance{})
	v.awsTypeMap["AWS::EC2::VPC"] = reflect.TypeOf(types.Vpc{})
	v.awsTypeMap["AWS::EC2::Subnet"] = reflect.TypeOf(types.Subnet{})
	v.awsTypeMap["AWS::EC2::SecurityGroup"] = reflect.TypeOf(types.SecurityGroup{})
	v.awsTypeMap["AWS::S3::Bucket"] = reflect.TypeOf(s3types.Bucket{})
	v.awsTypeMap["AWS::RDS::DBInstance"] = reflect.TypeOf(rdstypes.DBInstance{})
	v.awsTypeMap["AWS::ElasticLoadBalancingV2::LoadBalancer"] = reflect.TypeOf(elbv2types.LoadBalancer{})
	v.awsTypeMap["AWS::ElasticLoadBalancingV2::TargetGroup"] = reflect.TypeOf(elbv2types.TargetGroup{})
}

// ValidationResult contains the results of raw data validation
type ValidationResult struct {
	ResourceType       string
	ResourceID         string
	Valid              bool
	Errors             []string
	Warnings           []string
	MissingFields      []string
	ExtraFields        []string
	DataSize           int
	FieldCount         int
	PulumiFieldCount   int
	UnmarshalSuccess   bool
	UnmarshalError     string
	FieldComparison    map[string]FieldComparisonResult
}

// FieldComparisonResult contains comparison between Pulumi and raw data fields
type FieldComparisonResult struct {
	InPulumi    bool
	InRawData   bool
	PulumiValue interface{}
	RawValue    interface{}
	Match       bool
}

// ValidateResource validates a single resource's raw data
func (v *RawDataValidator) ValidateResource(resourceType, resourceID string, rawData, pulumiData map[string]interface{}) ValidationResult {
	result := ValidationResult{
		ResourceType:    resourceType,
		ResourceID:      resourceID,
		Valid:           true,
		Errors:          []string{},
		Warnings:        []string{},
		MissingFields:   []string{},
		ExtraFields:     []string{},
		FieldComparison: make(map[string]FieldComparisonResult),
	}

	// Check if raw_data exists
	if rawData == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "raw_data is nil")
		return result
	}

	// Calculate data size
	rawDataJSON, _ := json.Marshal(rawData)
	result.DataSize = len(rawDataJSON)

	// Count fields
	result.FieldCount = countFields(rawData)
	result.PulumiFieldCount = countFields(pulumiData)

	// Validate expected fields
	expectedFields, hasExpected := v.expectedFields[resourceType]
	if hasExpected {
		// Check for missing fields
		for _, field := range expectedFields {
			if !hasField(rawData, field) {
				result.MissingFields = append(result.MissingFields, field)
				result.Warnings = append(result.Warnings, fmt.Sprintf("Expected field '%s' not found in raw_data", field))
			}
		}

		// Check for extra fields (fields not in expected list)
		for field := range flattenMap(rawData) {
			if !contains(expectedFields, field) {
				result.ExtraFields = append(result.ExtraFields, field)
			}
		}
	} else {
		result.Warnings = append(result.Warnings, fmt.Sprintf("No expected fields defined for resource type %s", resourceType))
	}

	// Validate unmarshaling into AWS SDK struct
	if awsType, hasType := v.awsTypeMap[resourceType]; hasType {
		result.UnmarshalSuccess, result.UnmarshalError = v.validateUnmarshal(rawData, awsType)
		if !result.UnmarshalSuccess {
			result.Errors = append(result.Errors, fmt.Sprintf("Failed to unmarshal into AWS SDK struct: %s", result.UnmarshalError))
			result.Valid = false
		}
	}

	// Compare fields between Pulumi and raw data
	v.compareFields(pulumiData, rawData, result)

	// Additional validations
	if result.DataSize == 0 {
		result.Errors = append(result.Errors, "raw_data is empty")
		result.Valid = false
	}

	if result.FieldCount < 5 {
		result.Warnings = append(result.Warnings, fmt.Sprintf("raw_data has very few fields (%d), might be incomplete", result.FieldCount))
	}

	// Check if raw_data appears to be a full API response
	if !v.isFullAPIResponse(resourceType, rawData) {
		result.Warnings = append(result.Warnings, "raw_data might not contain full AWS API response")
	}

	return result
}

// validateUnmarshal attempts to unmarshal raw data into AWS SDK struct
func (v *RawDataValidator) validateUnmarshal(rawData map[string]interface{}, awsType reflect.Type) (bool, string) {
	// Convert raw data to JSON
	jsonData, err := json.Marshal(rawData)
	if err != nil {
		return false, fmt.Sprintf("failed to marshal raw data: %v", err)
	}

	// Create instance of AWS type
	instance := reflect.New(awsType).Interface()

	// Attempt to unmarshal
	if err := json.Unmarshal(jsonData, instance); err != nil {
		return false, fmt.Sprintf("unmarshal error: %v", err)
	}

	return true, ""
}

// compareFields compares fields between Pulumi and raw data
func (v *RawDataValidator) compareFields(pulumiData, rawData map[string]interface{}, result ValidationResult) {
	pulumiFlat := flattenMap(pulumiData)
	rawFlat := flattenMap(rawData)

	// Check all Pulumi fields
	for field, pulumiValue := range pulumiFlat {
		comparison := FieldComparisonResult{
			InPulumi:    true,
			PulumiValue: pulumiValue,
		}

		if rawValue, exists := rawFlat[field]; exists {
			comparison.InRawData = true
			comparison.RawValue = rawValue
			comparison.Match = fmt.Sprintf("%v", pulumiValue) == fmt.Sprintf("%v", rawValue)
		}

		result.FieldComparison[field] = comparison
	}

	// Check raw data fields not in Pulumi
	for field, rawValue := range rawFlat {
		if _, exists := result.FieldComparison[field]; !exists {
			result.FieldComparison[field] = FieldComparisonResult{
				InPulumi:  false,
				InRawData: true,
				RawValue:  rawValue,
			}
		}
	}
}

// isFullAPIResponse checks if raw data appears to contain a full AWS API response
func (v *RawDataValidator) isFullAPIResponse(resourceType string, rawData map[string]interface{}) bool {
	// Check for common AWS API response patterns
	indicators := []string{
		"ResponseMetadata", "RequestId", "HTTPStatusCode",
		"HTTPHeaders", "RetryAttempts",
	}

	for _, indicator := range indicators {
		if _, exists := rawData[indicator]; exists {
			return true
		}
	}

	// Check minimum field count for resource type
	expectedFields, hasExpected := v.expectedFields[resourceType]
	if hasExpected {
		presentCount := 0
		for _, field := range expectedFields {
			if hasField(rawData, field) {
				presentCount++
			}
		}
		// Consider it a full response if at least 70% of expected fields are present
		return float64(presentCount)/float64(len(expectedFields)) >= 0.7
	}

	return true // Assume it's complete if we don't have expectations
}

// Helper functions

func countFields(data map[string]interface{}) int {
	count := 0
	var countRecursive func(interface{})
	countRecursive = func(v interface{}) {
		switch val := v.(type) {
		case map[string]interface{}:
			for _, value := range val {
				count++
				countRecursive(value)
			}
		case []interface{}:
			for _, item := range val {
				countRecursive(item)
			}
		}
	}
	countRecursive(data)
	return count
}

func hasField(data map[string]interface{}, field string) bool {
	parts := strings.Split(field, ".")
	current := data
	for i, part := range parts {
		if val, exists := current[part]; exists {
			if i == len(parts)-1 {
				return true
			}
			if nextMap, ok := val.(map[string]interface{}); ok {
				current = nextMap
			} else {
				return false
			}
		} else {
			return false
		}
	}
	return false
}

func flattenMap(data map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})
	var flatten func(interface{}, string)
	flatten = func(v interface{}, prefix string) {
		switch val := v.(type) {
		case map[string]interface{}:
			for key, value := range val {
				newKey := key
				if prefix != "" {
					newKey = prefix + "." + key
				}
				flatten(value, newKey)
			}
		default:
			result[prefix] = v
		}
	}
	flatten(data, "")
	return result
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// ValidateAll validates raw data for all resources
func (v *RawDataValidator) ValidateAll(resources []map[string]interface{}) []ValidationResult {
	results := make([]ValidationResult, 0, len(resources))
	
	for _, resource := range resources {
		resourceType, _ := resource["ResourceType"].(string)
		resourceID, _ := resource["ResourceID"].(string)
		rawData, _ := resource["raw_data"].(map[string]interface{})
		
		// Extract Pulumi data (all fields except raw_data)
		pulumiData := make(map[string]interface{})
		for k, v := range resource {
			if k != "raw_data" {
				pulumiData[k] = v
			}
		}
		
		result := v.ValidateResource(resourceType, resourceID, rawData, pulumiData)
		results = append(results, result)
	}
	
	return results
}

// GenerateReport generates a validation report
func (v *RawDataValidator) GenerateReport(results []ValidationResult) string {
	var report strings.Builder
	
	report.WriteString("Raw Data Validation Report\n")
	report.WriteString("==========================\n\n")
	
	totalResources := len(results)
	validResources := 0
	totalDataSize := 0
	resourceTypeStats := make(map[string]int)
	commonMissingFields := make(map[string]int)
	
	for _, result := range results {
		if result.Valid {
			validResources++
		}
		totalDataSize += result.DataSize
		resourceTypeStats[result.ResourceType]++
		
		for _, field := range result.MissingFields {
			commonMissingFields[field]++
		}
	}
	
	// Summary
	report.WriteString("Summary:\n")
	report.WriteString(fmt.Sprintf("- Total Resources: %d\n", totalResources))
	report.WriteString(fmt.Sprintf("- Valid Resources: %d (%.1f%%)\n", validResources, float64(validResources)/float64(totalResources)*100))
	report.WriteString(fmt.Sprintf("- Total Raw Data Size: %.2f MB\n", float64(totalDataSize)/1024/1024))
	report.WriteString(fmt.Sprintf("- Average Data Size per Resource: %.2f KB\n\n", float64(totalDataSize)/float64(totalResources)/1024))
	
	// Resource Type Distribution
	report.WriteString("Resource Type Distribution:\n")
	for resType, count := range resourceTypeStats {
		report.WriteString(fmt.Sprintf("- %s: %d\n", resType, count))
	}
	report.WriteString("\n")
	
	// Common Missing Fields
	if len(commonMissingFields) > 0 {
		report.WriteString("Common Missing Fields:\n")
		for field, count := range commonMissingFields {
			if count > 5 { // Only show fields missing in more than 5 resources
				report.WriteString(fmt.Sprintf("- %s: missing in %d resources\n", field, count))
			}
		}
		report.WriteString("\n")
	}
	
	// Detailed Issues
	report.WriteString("Detailed Issues:\n")
	for _, result := range results {
		if !result.Valid || len(result.Errors) > 0 || len(result.Warnings) > 0 {
			report.WriteString(fmt.Sprintf("\n%s - %s:\n", result.ResourceType, result.ResourceID))
			
			if len(result.Errors) > 0 {
				report.WriteString("  Errors:\n")
				for _, err := range result.Errors {
					report.WriteString(fmt.Sprintf("  - %s\n", err))
				}
			}
			
			if len(result.Warnings) > 0 {
				report.WriteString("  Warnings:\n")
				for _, warn := range result.Warnings {
					report.WriteString(fmt.Sprintf("  - %s\n", warn))
				}
			}
			
			if len(result.MissingFields) > 0 {
				report.WriteString(fmt.Sprintf("  Missing Fields: %s\n", strings.Join(result.MissingFields, ", ")))
			}
		}
	}
	
	// Field Comparison Summary
	report.WriteString("\n\nField Comparison Summary:\n")
	fieldsOnlyInPulumi := 0
	fieldsOnlyInRaw := 0
	fieldsMismatch := 0
	
	for _, result := range results {
		for _, comparison := range result.FieldComparison {
			if comparison.InPulumi && !comparison.InRawData {
				fieldsOnlyInPulumi++
			} else if !comparison.InPulumi && comparison.InRawData {
				fieldsOnlyInRaw++
			} else if comparison.InPulumi && comparison.InRawData && !comparison.Match {
				fieldsMismatch++
			}
		}
	}
	
	report.WriteString(fmt.Sprintf("- Fields only in Pulumi: %d\n", fieldsOnlyInPulumi))
	report.WriteString(fmt.Sprintf("- Fields only in Raw Data: %d\n", fieldsOnlyInRaw))
	report.WriteString(fmt.Sprintf("- Fields with mismatched values: %d\n", fieldsMismatch))
	
	return report.String()
}