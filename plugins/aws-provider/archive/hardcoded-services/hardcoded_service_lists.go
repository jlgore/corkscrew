// This file contains all hardcoded service lists that were removed during UnifiedScanner migration
// Keep this for reference and potential emergency fallback

package archive

// Original hardcoded service list from aws_provider.go:80-86
func GetOriginalHardcodedServices() []string {
	return []string{
		"s3", "ec2", "lambda", "rds", "dynamodb", "iam",
		"ecs", "eks", "elasticache", "cloudformation",
		"cloudwatch", "sns", "sqs", "kinesis", "glue",
		"secretsmanager", "ssm", "kms",
	}
}

// Popular services from discovery/service_discovery.go:294-313
func GetPopularServices() []string {
	return []string{
		"s3", "ec2", "lambda", "rds", "dynamodb", "iam",
		"ecs", "eks", "elasticache", "cloudformation",
		"cloudwatch", "sns", "sqs", "kinesis", "glue",
		"route53", "redshift", "kms", "secretsmanager",
		"ssm", "apigateway", "apigatewayv2", "elbv2",
	}
}

// 18 services test list from tests/unified_scanner_18_services_test.go:65-84
func GetTestServices() []string {
	return []string{
		"s3", "ec2", "lambda", "rds", "dynamodb", "iam",
		"ecs", "eks", "elasticache", "cloudformation",
		"cloudwatch", "sns", "sqs", "kinesis", "glue",
		"route53", "redshift", "kms",
	}
}

// Known services from generator/aws_analyzer.go:364-377
func GetKnownServices() []string {
	return []string{
		"s3", "ec2", "lambda", "rds", "dynamodb",
		"iam", "ecs", "cloudformation", "sns", "sqs",
	}
}

// Common services from discovery/service_availability.go:93-97
func GetCommonServices() []string {
	return []string{
		"ec2", "s3", "iam", "lambda", "rds", "dynamodb",
		"sns", "sqs", "cloudformation", "cloudwatch",
		"ecs", "elasticache", "redshift", "route53",
		"apigateway", "kinesis", "glue",
	}
}