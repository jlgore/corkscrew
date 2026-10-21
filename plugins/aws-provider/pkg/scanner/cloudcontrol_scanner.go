package scanner

import (
	"context"
	"encoding/json"
	"errors"
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
	"github.com/jlgore/corkscrew/internal/shared/paging"
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
	client   cloudControlAPI
	cfn      typeRegistryAPI
	identity callerIdentityAPI
	cfg      aws.Config

	mu        sync.RWMutex
	accountID string
	explorer  ResourceExplorer

	// unsupportedTypes records CFN types that Cloud Control couldn't list
	// (typically TypeNotFoundException or "not yet supported"). Reported back
	// to the caller in the scan response so gaps are visible.
	unsupportedTypes sync.Map // typeName -> string reason

	// Metrics
	listCalls    atomic.Int64
	getCalls     atomic.Int64
	listFailures atomic.Int64
	getFailures  atomic.Int64
	reQueries    atomic.Int64
}

// The AWS client surfaces the scanner calls, so tests can substitute fakes.
type cloudControlAPI interface {
	ListResources(ctx context.Context, in *cloudcontrol.ListResourcesInput,
		opts ...func(*cloudcontrol.Options)) (*cloudcontrol.ListResourcesOutput, error)
	GetResource(ctx context.Context, in *cloudcontrol.GetResourceInput,
		opts ...func(*cloudcontrol.Options)) (*cloudcontrol.GetResourceOutput, error)
}

type typeRegistryAPI interface {
	ListTypes(ctx context.Context, in *cloudformation.ListTypesInput,
		opts ...func(*cloudformation.Options)) (*cloudformation.ListTypesOutput, error)
}

type callerIdentityAPI interface {
	GetCallerIdentity(ctx context.Context, in *sts.GetCallerIdentityInput,
		opts ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error)
}

// NewCloudControlScanner constructs a Cloud Control API-backed scanner.
func NewCloudControlScanner(cfg aws.Config) *CloudControlScanner {
	return &CloudControlScanner{
		client:   cloudcontrol.NewFromConfig(cfg),
		cfn:      cloudformation.NewFromConfig(cfg),
		identity: sts.NewFromConfig(cfg),
		cfg:      cfg,
	}
}

// globalServices are AWS services whose resources are not region-scoped:
// the same physical resource is returned by ListResources in every region.
// We stamp Region="" on those refs so the multi-region consumer can dedupe
// by (account, type, id) without losing regional info on actually-regional
// resources. Lowercase service shorthand (matches serviceFromCFNType output).
var globalServices = map[string]bool{
	"iam":               true,
	"s3":                true, // buckets are listed globally even though a bucket has a home region
	"route53":           true,
	"cloudfront":        true,
	"organizations":     true,
	"support":           true,
	"health":            true,
	"billing":           true,
	"globalaccelerator": true,
	"chatbot":           true,
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
// if discovery fails, which DiscoveryErr then reports.
func (s *CloudControlScanner) SupportedServices() []string {
	s.ensureDiscovery(context.Background())

	m := s.activeTypeMap()
	services := make([]string, 0, len(m))
	for svc := range m {
		services = append(services, svc)
	}
	sort.Strings(services)
	return services
}

// SetResourceExplorer wires a Resource Explorer client. When set, ScanService
// prefers RE for discovery and CC for enrichment.
func (s *CloudControlScanner) SetResourceExplorer(re ResourceExplorer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.explorer = re
}

// ScanService lists every resource of every CFN type known for the service.
// When a Resource Explorer is wired, prefer RE for discovery (one indexed
// search across regions) and use CC.GetResource only for the type lookup.
//
// It returns the refs it observed even when it also returns an error: the
// error joins one entry per type that failed, so the caller can report the
// service as incomplete without discarding what was listed.
func (s *CloudControlScanner) ScanService(ctx context.Context, serviceName string) ([]*pb.ResourceRef, error) {
	// Every ref is keyed by the account it came from. Without the account ID
	// the refs would be stored under "" and never match a real account.
	if err := s.ensureAccountID(ctx); err != nil {
		return nil, err
	}
	s.ensureDiscovery(ctx)

	s.mu.RLock()
	re := s.explorer
	s.mu.RUnlock()

	if re != nil {
		return s.scanServiceViaRE(ctx, serviceName, re)
	}

	m := s.activeTypeMap()
	types, ok := m[strings.ToLower(serviceName)]
	if !ok {
		log.Printf("CloudControl: service %q has no known CFN types; skipping", serviceName)
		return nil, nil
	}
	return s.scanTypes(ctx, serviceName, types)
}

// scanTypes lists each type of the service, keeping the refs of every type
// that answered and joining one error per type that did not.
func (s *CloudControlScanner) scanTypes(
	ctx context.Context, serviceName string, types []string,
) ([]*pb.ResourceRef, error) {
	// Dedupe by Id. Multiple CFN types under the same service often share
	// an identifier — e.g. AWS::S3::Bucket and AWS::S3::BucketPolicy both
	// use the bucket name as their resource ID, so iterating types naively
	// yields the same physical resource twice. Types are sorted alphabetically
	// in the discovered map, so first-wins typically picks the canonical
	// type (Bucket before BucketPolicy, Function before FunctionAlias, etc).
	seen := make(map[string]struct{})
	var all []*pb.ResourceRef
	var errs []error
	for _, typeName := range types {
		// listType returns the pages it got before failing; keep them.
		refs, err := s.listType(ctx, serviceName, typeName)
		for _, r := range refs {
			if _, dup := seen[r.Id]; dup {
				continue
			}
			seen[r.Id] = struct{}{}
			all = append(all, r)
		}
		if ctx.Err() != nil {
			// Every remaining type would fail the same way; report it once.
			errs = append(errs, ctx.Err())
			break
		}
		if err == nil {
			continue
		}
		if isUnsupportedTypeErr(err) {
			s.unsupportedTypes.Store(typeName, err.Error())
			continue
		}
		s.listFailures.Add(1)
		log.Printf("CloudControl: ListResources(%s) failed: %v", typeName, err)
		errs = append(errs, typeListError(typeName, err))
	}
	return all, errors.Join(errs...)
}

// typeListError names the type that failed. An access denial becomes an
// *AccessDeniedError so the provider can report it as a coverage notice: the
// credential's real scope, fixed by an IAM change, not a broken scan.
func typeListError(typeName string, err error) error {
	if isAccessDeniedErr(err) {
		return &AccessDeniedError{Type: typeName, Err: err}
	}
	return fmt.Errorf("%s: %w", typeName, err)
}

// isUnsupportedTypeErr matches the Cloud Control errors that mean "this
// type has nothing to enumerate here" rather than "the scan broke", so the
// type is reported as a notice and the scope stays complete. Includes:
//   - TypeNotFoundException / UnsupportedActionException (CFN registry lookups)
//   - "not currently supported" (CC's own gap message, e.g. per region)
//   - GeneralServiceException with "does not exist" (returned when a parent
//     resource the type depends on isn't set up, e.g. AWS::S3::AccessGrant
//     when there's no Access Grants Instance, so there is nothing to list)
//
// Access errors and handler failures are deliberately not here: resources
// of the type may exist, so treating them as unsupported would let a scan
// claim completeness over data it never saw (ADR 0023).
func isUnsupportedTypeErr(err error) bool {
	return errContainsAny(err, "TypeNotFoundException", "UnsupportedActionException",
		"not currently supported", "does not exist")
}

// isAccessDeniedErr matches IAM denials for a Cloud Control call.
func isAccessDeniedErr(err error) bool {
	return errContainsAny(err, "AccessDenied", "is not authorized")
}

func errContainsAny(err error, needles ...string) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for _, needle := range needles {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}

// UnsupportedTypes returns a snapshot of the CFN types that Cloud Control
// could not enumerate during this scanner's lifetime. Map of type → reason.
func (s *CloudControlScanner) UnsupportedTypes() map[string]string {
	out := map[string]string{}
	s.unsupportedTypes.Range(func(k, v any) bool {
		out[k.(string)] = v.(string)
		return true
	})
	return out
}

func (s *CloudControlScanner) listType(ctx context.Context, serviceName, typeName string) ([]*pb.ResourceRef, error) {
	s.listCalls.Add(1)

	// Globally-namespaced services return the same resources from every
	// region's ListResources call. Stamp Region="" on those refs so the
	// downstream multi-region consumer dedupes by (account, type, id)
	// without dropping legitimately-regional resources elsewhere.
	region := s.cfg.Region
	if globalServices[strings.ToLower(serviceName)] {
		region = ""
	}

	var refs []*pb.ResourceRef
	var nextToken *string
	guard := paging.New("cloudcontrol.ListResources("+typeName+")", paging.PagesMax)
	for {
		out, err := s.client.ListResources(ctx, &cloudcontrol.ListResourcesInput{
			TypeName:  aws.String(typeName),
			NextToken: nextToken,
		})
		if err != nil {
			return refs, err
		}

		for _, rd := range out.ResourceDescriptions {
			id := aws.ToString(rd.Identifier)
			// AWS-managed IAM artifacts (managed policies, service-linked role
			// policies) are global, identical across all accounts, and add up
			// to ~1,100 GetResource calls per scan when included. Skip them.
			// Customer/account-owned identifiers contain the 12-digit account
			// number; AWS-managed identifiers use the literal "aws" or the
			// "aws-managed" partition.
			if strings.Contains(id, ":iam::aws:") {
				continue
			}
			ref := &pb.ResourceRef{
				Service:   serviceName,
				Type:      typeName,
				Id:        id,
				Region:    region,
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
		if err := guard.Next(*out.NextToken); err != nil {
			return refs, err
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

	// No per-call timeout — bounded parallelism in the caller and the parent
	// context handle pacing. The previous 5s budget was tripping false-negative
	// "deadline exceeded" while the SDK was still mid-retry on slow handlers.
	s.getCalls.Add(1)
	out, err := s.client.GetResource(ctx, &cloudcontrol.GetResourceInput{
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
		"re_queries":    s.reQueries.Load(),
	}
}

const (
	// discoveryTimeout bounds one cloudformation.ListTypes run (~1,400 types
	// over a few dozen pages). Discovery runs detached from the caller's
	// context so one cancelled scope does not fail it for the scopes waiting
	// on the same run.
	discoveryTimeout = 30 * time.Second
	// discoveryRetryAfter spaces out retries of a failed discovery, so a scan
	// of many services does not re-run ListTypes (and its throttle cost) for
	// every service while CloudFormation is unavailable.
	discoveryRetryAfter = 5 * time.Minute
)

// discovery is the package-level type discovery cache. Type discovery via
// cloudformation.ListTypes pulls ~1,400 types and burns CFN throttle budget;
// it's the same answer for every CC scanner in a process (one per region),
// so it is shared across instances. Only success is kept for good: a failure
// is retried after discoveryRetryAfter.
var discovery struct {
	mu       sync.Mutex
	ok       bool
	types    map[string][]string
	reverse  map[string]string
	err      error
	failedAt time.Time
}

// ensureDiscovery runs cloudformation.ListTypes until it succeeds once per
// process. Until then scans fall back to the curated map and DiscoveryErr
// reports why. The lock is held across the run so concurrent scanners wait
// for one ListTypes run instead of each tripping CFN's throttle.
func (s *CloudControlScanner) ensureDiscovery(ctx context.Context) {
	if s.cfn == nil {
		// Zero-value scanner (e.g. in unit tests) — skip dynamic discovery.
		return
	}
	discovery.mu.Lock()
	defer discovery.mu.Unlock()
	if discovery.ok {
		return
	}
	if discovery.err != nil && time.Since(discovery.failedAt) < discoveryRetryAfter {
		return
	}
	runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), discoveryTimeout)
	defer cancel()
	types, reverse, err := s.discoverTypes(runCtx)
	if err != nil {
		discovery.err = err
		discovery.failedAt = time.Now()
		log.Printf("CloudControl: type discovery failed (%v); falling back to curated map", err)
		return
	}
	discovery.ok, discovery.err = true, nil
	discovery.types, discovery.reverse = types, reverse
	log.Printf("CloudControl: discovered %d services / %d total types via cloudformation.ListTypes",
		len(types), countTypes(types))
}

// DiscoveryErr returns why type discovery failed while scans are using the
// curated fallback map, or nil. The fallback covers a few dozen types of the
// ~1,400 discovery finds, so a scan made with it has missed most resources.
func DiscoveryErr() error {
	discovery.mu.Lock()
	defer discovery.mu.Unlock()
	if discovery.ok {
		return nil
	}
	return discovery.err
}

// DiscoveryErr reports the process-wide discovery state; see DiscoveryErr.
func (s *CloudControlScanner) DiscoveryErr() error {
	return DiscoveryErr()
}

// discoverTypes enumerates all FULLY_MUTABLE and IMMUTABLE public AWS::*
// types and groups them by service: map[service][]typeName. It also returns
// the "service:resource" → CFN type reverse map for RE-driven enrichment.
func (s *CloudControlScanner) discoverTypes(
	ctx context.Context,
) (map[string][]string, map[string]string, error) {
	out := map[string][]string{}
	reverse := map[string]string{}

	for _, prov := range []cfntypes.ProvisioningType{
		cfntypes.ProvisioningTypeFullyMutable,
		cfntypes.ProvisioningTypeImmutable,
	} {
		var nextToken *string
		guard := paging.New(fmt.Sprintf("cloudformation.ListTypes(%s)", prov), paging.PagesMax)
		for {
			resp, err := s.cfn.ListTypes(ctx, &cloudformation.ListTypesInput{
				Type:             cfntypes.RegistryTypeResource,
				Visibility:       cfntypes.VisibilityPublic,
				ProvisioningType: prov,
				NextToken:        nextToken,
			})
			if err != nil {
				return nil, nil, fmt.Errorf("ListTypes(%s): %w", prov, err)
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
				if k := reFormatKey(name); k != "" {
					reverse[k] = name
				}
			}
			if resp.NextToken == nil || *resp.NextToken == "" {
				break
			}
			if err := guard.Next(*resp.NextToken); err != nil {
				return nil, nil, err
			}
			nextToken = resp.NextToken
		}
	}

	for svc := range out {
		sort.Strings(out[svc])
	}
	return out, reverse, nil
}

// scanServiceViaRE queries Resource Explorer for the service, then looks up
// each result's CFN type via the reverse map. Resources whose CFN type can't
// be resolved are returned anyway (with whatever Type the RE result had);
// the caller can still enrich by passing them to DescribeResource, which
// falls back to the type already on the ref.
//
// A failed query returns the refs from the pages before the failure along
// with the error, as listType does.
func (s *CloudControlScanner) scanServiceViaRE(ctx context.Context, serviceName string, re ResourceExplorer) ([]*pb.ResourceRef, error) {
	s.reQueries.Add(1)
	refs, err := re.QueryByService(ctx, strings.ToLower(serviceName))
	if err != nil {
		err = fmt.Errorf("ResourceExplorer.QueryByService(%s): %w", serviceName, err)
	}

	discovery.mu.Lock()
	reverse := discovery.reverse
	discovery.mu.Unlock()
	accountID := s.getAccountID()

	for _, r := range refs {
		if r.BasicAttributes == nil {
			r.BasicAttributes = map[string]string{}
		}
		// RE returns ResourceType in "service:resource" format. If we recognize
		// it, stash the canonical CFN type on the ref so DescribeResource can
		// hit Cloud Control directly.
		key := strings.ToLower(r.Type)
		if reverse != nil {
			if cfnType, ok := reverse[key]; ok {
				r.BasicAttributes["cfn_type"] = cfnType
			}
		}
		// Normalize Service: RE may set it to upper-case shorthand.
		r.Service = strings.ToLower(r.Service)
		if r.AccountId == "" {
			r.AccountId = accountID
		}
	}
	return refs, err
}

// reFormatKey converts a CFN type name into the lowercase "service:resource"
// shape Resource Explorer uses. Returns "" if the input isn't AWS::*::*.
//
// Example: "AWS::ElasticLoadBalancingV2::LoadBalancer" → "elasticloadbalancingv2:loadbalancer"
func reFormatKey(cfnType string) string {
	parts := strings.SplitN(cfnType, "::", 3)
	if len(parts) < 3 || parts[0] != "AWS" {
		return ""
	}
	return strings.ToLower(parts[1]) + ":" + strings.ToLower(parts[2])
}

// activeTypeMap returns the map of services→types currently in use.
// Prefers the dynamically-discovered map; falls back to the curated map.
func (s *CloudControlScanner) activeTypeMap() map[string][]string {
	discovery.mu.Lock()
	defer discovery.mu.Unlock()
	if discovery.ok {
		return discovery.types
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

// ensureAccountID resolves the scanned account once via STS. It fails when
// STS does, rather than leaving every ref of the scan with AccountId "".
func (s *CloudControlScanner) ensureAccountID(ctx context.Context) error {
	s.mu.RLock()
	have := s.accountID != ""
	s.mu.RUnlock()
	if have {
		return nil
	}
	if s.identity == nil {
		return errors.New("resolve account id: no STS client")
	}

	out, err := s.identity.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return fmt.Errorf("resolve account id: sts.GetCallerIdentity: %w", err)
	}
	accountID := aws.ToString(out.Account)
	if accountID == "" {
		return errors.New("resolve account id: sts.GetCallerIdentity returned no account")
	}

	s.mu.Lock()
	s.accountID = accountID
	s.mu.Unlock()
	return nil
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
