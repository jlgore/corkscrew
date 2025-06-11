// This file contains hardcoded service methods that were removed during UnifiedScanner migration
// Keep this for reference only

package archive

import (
	"fmt"
	"strings"
	pb "github.com/jlgore/corkscrew/internal/proto"
)

// ARCHIVED: getHardcodedServices returns the original hardcoded service list for fallback
// Originally from aws_provider.go:78-86
func getHardcodedServices() []string {
	return []string{
		"s3", "ec2", "lambda", "rds", "dynamodb", "iam",
		"ecs", "eks", "elasticache", "cloudformation",
		"cloudwatch", "sns", "sqs", "kinesis", "glue",
		"secretsmanager", "ssm", "kms",
	}
}

// ARCHIVED: getHardcodedServiceInfos converts hardcoded service list to ServiceInfo for compatibility
// Originally from aws_provider.go:88-103
func getHardcodedServiceInfos() []*pb.ServiceInfo {
	hardcodedServices := getHardcodedServices()
	serviceInfos := make([]*pb.ServiceInfo, len(hardcodedServices))
	
	for i, serviceName := range hardcodedServices {
		serviceInfos[i] = &pb.ServiceInfo{
			Name:        serviceName,
			DisplayName: strings.Title(serviceName),
			PackageName: fmt.Sprintf("github.com/aws/aws-sdk-go-v2/service/%s", serviceName),
			ClientType:  fmt.Sprintf("%sClient", strings.Title(serviceName)),
		}
	}
	
	return serviceInfos
}

// ARCHIVED: Migration logic that was used in aws_provider.go:471-488
func getServicesWithMigrationLogic() []*pb.ServiceInfo {
	// Check if migration is enabled for service discovery
	if isMigrationEnabled() && !isFallbackModeEnabled() {
		// This would have used dynamic discovery
		return nil // Dynamic discovery would return services here
	} else {
		// Use hardcoded services for rollback/fallback
		return getHardcodedServiceInfos()
	}
}