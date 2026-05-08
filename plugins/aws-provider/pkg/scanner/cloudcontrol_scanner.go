package scanner

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudcontrol"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation"
	cfntypes "github.com/aws/aws-sdk-go-v2/service/cloudformation/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	pb "github.com/jlgore/corkscrew/internal/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// CloudControlScanner implements UnifiedScannerProvider using AWS Cloud Control API.
//
// This is the round-2 spike: a parallel scanner that replaces the reflection-based
// UnifiedScanner with a single uniform call (ListResources / GetResource) for any
// resource type CloudFormation knows about. Curated service→type map below; can be
// replaced by cloudformation.ListTypes lookup later.
//
// Toggle from aws_provider.go via CORKSCREW_AWS_SCANNER=cloudcontrol.
type CloudControlScanner struct {
	client *cloudcontrol.Client
	cfn    *cloudformation.Client
	cfg    aws.Config

	mu        sync.RWMutex
	accountID string

	// discovered is the dynamically-discovered service→types map. Populated by
	// the first call to ensureDiscovery; nil falls through to curatedTypes.
	discovered     map[string][]string
	discoveryError error

	// Metrics
	listCalls    atomic.Int64
	getCalls     atomic.Int64
	listFailures atomic.Int64
	getFailures  atomic.Int64
}

// NewCloudControlScanner constructs a Cloud Control API-backed scanner.
func NewCloudControlScanner(cfg aws.Config) *CloudControlScanner {
	return &CloudControlScanner{
		client: cloudcontrol.NewFromConfig(cfg),
		cfn:    cloudformation.NewFromConfig(cfg),
		cfg:    cfg,
	}
}

// curatedTypes is the fallback map used when dynamic discovery via
// cloudformation.ListTypes hasn't run or failed. Covers the common cases.
var curatedTypes = map[string][]string{
	"s3": {
		"AWS::S3::Bucket",
	},
	"ec2": {
		"AWS::EC2::Instance",
		"AWS::EC2::VPC",
		"AWS::EC2::Subnet",
		"AWS::EC2::SecurityGroup",
		"AWS::EC2::InternetGateway",
		"AWS::EC2::NatGateway",
		"AWS::EC2::RouteTable",
		"AWS::EC2::Volume",
	},
	"lambda": {
		"AWS::Lambda::Function",
	},
	"iam": {
		"AWS::IAM::Role",
		"AWS::IAM::User",
		"AWS::IAM::Group",
		"AWS::IAM::Policy",
	},
	"rds": {
		"AWS::RDS::DBInstance",
		"AWS::RDS::DBCluster",
	},
	"dynamodb": {
		"AWS::DynamoDB::Table",
	},
	"kms": {
		"AWS::KMS::Key",
	},
	"sns": {
		"AWS::SNS::Topic",
	},
	"sqs": {
		"AWS::SQS::Queue",
	},
	"ecs": {
		"AWS::ECS::Cluster",
		"AWS::ECS::Service",
	},
	"eks": {
		"AWS::EKS::Cluster",
	},
	"cloudwatch": {
		"AWS::CloudWatch::Alarm",
	},
	"elasticloadbalancing": {
		"AWS::ElasticLoadBalancingV2::LoadBalancer",
	},
	"route53": {
		"AWS::Route53::HostedZone",
	},
	"secretsmanager": {
		"AWS::SecretsManager::Secret",
	},
	"cloudformation": {
		"AWS::CloudFormation::Stack",
	},
	"autoscaling": {
		"AWS::AutoScaling::AutoScalingGroup",
	},
}

// SupportedServices returns the services this scanner can enumerate.
// Triggers dynamic discovery on first call; falls back to the curated map
// if discovery fails. Note: this is best-effort — pass a context if you
// want a deadline; otherwise discovery runs with a default 30s budget.
func (s *CloudControlScanner) SupportedServices() []string {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s.ensureDiscovery(ctx)

	m := s.activeTypeMap()
	services := make([]string, 0, len(m))
	for svc := range m {
		services = append(services, svc)
	}
	sort.Strings(services)
	return services
}

// ScanService lists every resource of every CFN type known for the service.
func (s *CloudControlScanner) ScanService(ctx context.Context, serviceName string) ([]*pb.ResourceRef, error) {
	s.ensureAccountID(ctx)
	s.ensureDiscovery(ctx)

	m := s.activeTypeMap()
	types, ok := m[strings.ToLower(serviceName)]
	if !ok {
		log.Printf("CloudControl: service %q has no known CFN types; skipping", serviceName)
		return nil, nil
	}

	var all []*pb.ResourceRef
	for _, typeName := range types {
		refs, err := s.listType(ctx, serviceName, typeName)
		if err != nil {
			s.listFailures.Add(1)
			log.Printf("CloudControl: ListResources(%s) failed: %v", typeName, err)
			continue
		}
		all = append(all, refs...)
	}
	return all, nil
}

func (s *CloudControlScanner) listType(ctx context.Context, serviceName, typeName string) ([]*pb.ResourceRef, error) {
	s.listCalls.Add(1)

	var refs []*pb.ResourceRef
	var nextToken *string
	for {
		out, err := s.client.ListResources(ctx, &cloudcontrol.ListResourcesInput{
			TypeName:  aws.String(typeName),
			NextToken: nextToken,
		})
		if err != nil {
			return refs, err
		}

		for _, rd := range out.ResourceDescriptions {
			ref := &pb.ResourceRef{
				Service:   serviceName,
				Type:      typeName,
				Id:        aws.ToString(rd.Identifier),
				Region:    s.cfg.Region,
				AccountId: s.getAccountID(),
				BasicAttributes: map[string]string{
					"cfn_type": typeName,
				},
			}
			// Properties is a JSON blob — keep it on the ref so DescribeResource
			// can avoid a second round trip when the caller doesn't need a refetch.
			if rd.Properties != nil {
				ref.BasicAttributes["properties"] = aws.ToString(rd.Properties)
			}
			refs = append(refs, ref)
		}

		if out.NextToken == nil || *out.NextToken == "" {
			break
		}
		nextToken = out.NextToken
	}
	return refs, nil
}

// DescribeResource fetches full configuration via GetResource. Falls back to the
// properties already stashed on the ref if GetResource fails.
func (s *CloudControlScanner) DescribeResource(ctx context.Context, ref *pb.ResourceRef) (*pb.Resource, error) {
	resource := &pb.Resource{
		Provider:     "aws",
		Service:      ref.Service,
		Type:         ref.Type,
		Id:           ref.Id,
		Name:         ref.Name,
		Region:       ref.Region,
		AccountId:    ref.AccountId,
		Tags:         map[string]string{},
		DiscoveredAt: timestamppb.Now(),
	}

	typeName := ref.Type
	if ref.BasicAttributes != nil {
		if t, ok := ref.BasicAttributes["cfn_type"]; ok && t != "" {
			typeName = t
		}
	}

	// Try a fresh GetResource for the canonical config.
	getCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	s.getCalls.Add(1)
	out, err := s.client.GetResource(getCtx, &cloudcontrol.GetResourceInput{
		TypeName:   aws.String(typeName),
		Identifier: aws.String(ref.Id),
	})

	var properties string
	switch {
	case err == nil && out.ResourceDescription != nil:
		properties = aws.ToString(out.ResourceDescription.Properties)
	case ref.BasicAttributes["properties"] != "":
		// Fall back to what ListResources returned.
		s.getFailures.Add(1)
		log.Printf("CloudControl: GetResource(%s, %s) failed, using cached properties: %v", typeName, ref.Id, err)
		properties = ref.BasicAttributes["properties"]
	default:
		s.getFailures.Add(1)
		return resource, fmt.Errorf("GetResource(%s, %s): %w", typeName, ref.Id, err)
	}

	resource.RawData = properties
	if props, attrs := parseCloudControlProperties(properties); props != nil {
		if arn, ok := props["Arn"].(string); ok {
			resource.Arn = arn
		} else if arn, ok := props["arn"].(string); ok {
			resource.Arn = arn
		}
		if tags := extractTagsFromProperties(props); len(tags) > 0 {
			for k, v := range tags {
				resource.Tags[k] = v
			}
		}
		if attrJSON, err := json.Marshal(attrs); err == nil {
			resource.Attributes = string(attrJSON)
		}
	}

	return resource, nil
}

// GetMetrics implements the UnifiedScannerProvider interface.
func (s *CloudControlScanner) GetMetrics() interface{} {
	return map[string]int64{
		"list_calls":    s.listCalls.Load(),
		"get_calls":     s.getCalls.Load(),
		"list_failures": s.listFailures.Load(),
		"get_failures":  s.getFailures.Load(),
	}
}

// ensureDiscovery lazily populates s.discovered via cloudformation.ListTypes.
// Idempotent: runs at most once, then caches both success and failure. On
// failure, scans fall back to the curated map.
func (s *CloudControlScanner) ensureDiscovery(ctx context.Context) {
	s.mu.RLock()
	done := s.discovered != nil || s.discoveryError != nil
	cfn := s.cfn
	s.mu.RUnlock()
	if done {
		return
	}
	if cfn == nil {
		// Zero-value scanner (e.g. in unit tests) — skip dynamic discovery.
		s.mu.Lock()
		s.discoveryError = fmt.Errorf("no cloudformation client configured")
		s.mu.Unlock()
		return
	}

	discovered, err := s.discoverTypes(ctx)

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.discovered != nil || s.discoveryError != nil {
		return // raced; another caller finished first
	}
	if err != nil {
		s.discoveryError = err
		log.Printf("CloudControl: type discovery failed (%v); falling back to curated map", err)
		return
	}
	s.discovered = discovered
	log.Printf("CloudControl: discovered %d services / %d total types via cloudformation.ListTypes",
		len(discovered), countTypes(discovered))
}

// discoverTypes enumerates all FULLY_MUTABLE public AWS::* types and groups
// them by service. Returns a map[service][]typeName.
func (s *CloudControlScanner) discoverTypes(ctx context.Context) (map[string][]string, error) {
	out := map[string][]string{}

	for _, prov := range []cfntypes.ProvisioningType{
		cfntypes.ProvisioningTypeFullyMutable,
		cfntypes.ProvisioningTypeImmutable,
	} {
		var nextToken *string
		for {
			resp, err := s.cfn.ListTypes(ctx, &cloudformation.ListTypesInput{
				Type:             cfntypes.RegistryTypeResource,
				Visibility:       cfntypes.VisibilityPublic,
				ProvisioningType: prov,
				NextToken:        nextToken,
			})
			if err != nil {
				return nil, fmt.Errorf("ListTypes(%s): %w", prov, err)
			}
			for _, t := range resp.TypeSummaries {
				name := aws.ToString(t.TypeName)
				if !strings.HasPrefix(name, "AWS::") {
					continue // skip third-party types
				}
				svc := serviceFromCFNType(name)
				if svc == "" {
					continue
				}
				out[svc] = append(out[svc], name)
			}
			if resp.NextToken == nil || *resp.NextToken == "" {
				break
			}
			nextToken = resp.NextToken
		}
	}

	for svc := range out {
		sort.Strings(out[svc])
	}
	return out, nil
}

// activeTypeMap returns the map of services→types currently in use.
// Prefers the dynamically-discovered map; falls back to the curated map.
func (s *CloudControlScanner) activeTypeMap() map[string][]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.discovered != nil {
		return s.discovered
	}
	return curatedTypes
}

// serviceFromCFNType maps "AWS::S3::Bucket" → "s3".
// Returns "" if the type name doesn't match the AWS::Service::Resource shape.
func serviceFromCFNType(typeName string) string {
	parts := strings.SplitN(typeName, "::", 3)
	if len(parts) < 3 || parts[0] != "AWS" || parts[1] == "" {
		return ""
	}
	svc := strings.ToLower(parts[1])
	// Collapse a few CFN service names to the corkscrew-internal shorthand
	// that other code paths use.
	switch svc {
	case "elasticloadbalancingv2":
		return "elasticloadbalancing"
	}
	return svc
}

func countTypes(m map[string][]string) int {
	n := 0
	for _, v := range m {
		n += len(v)
	}
	return n
}

func (s *CloudControlScanner) ensureAccountID(ctx context.Context) {
	s.mu.RLock()
	have := s.accountID != ""
	s.mu.RUnlock()
	if have {
		return
	}

	stsClient := sts.NewFromConfig(s.cfg)
	out, err := stsClient.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil || out.Account == nil {
		return
	}

	s.mu.Lock()
	s.accountID = aws.ToString(out.Account)
	s.mu.Unlock()
}

func (s *CloudControlScanner) getAccountID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.accountID
}

// parseCloudControlProperties parses the JSON properties string from Cloud Control.
// Returns the raw map and a flattened string-attribute map for convenience.
func parseCloudControlProperties(properties string) (map[string]interface{}, map[string]string) {
	if properties == "" {
		return nil, nil
	}
	var props map[string]interface{}
	if err := json.Unmarshal([]byte(properties), &props); err != nil {
		return nil, nil
	}

	attrs := make(map[string]string, len(props))
	for k, v := range props {
		switch tv := v.(type) {
		case string:
			attrs[k] = tv
		case bool:
			attrs[k] = fmt.Sprintf("%t", tv)
		case float64:
			attrs[k] = fmt.Sprintf("%v", tv)
		}
	}
	return props, attrs
}

// extractTagsFromProperties handles the two common tag shapes Cloud Control returns:
//   - []map[string]string with Key/Value entries (most services)
//   - map[string]string (S3 and a few others)
func extractTagsFromProperties(props map[string]interface{}) map[string]string {
	tags := map[string]string{}
	raw, ok := props["Tags"]
	if !ok {
		return nil
	}

	switch v := raw.(type) {
	case []interface{}:
		for _, item := range v {
			m, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			key, _ := m["Key"].(string)
			val, _ := m["Value"].(string)
			if key != "" {
				tags[key] = val
			}
		}
	case map[string]interface{}:
		for k, val := range v {
			if s, ok := val.(string); ok {
				tags[k] = s
			}
		}
	}

	if len(tags) == 0 {
		return nil
	}
	return tags
}
