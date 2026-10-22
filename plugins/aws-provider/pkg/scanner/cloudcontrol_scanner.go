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
	"github.com/jlgore/corkscrew/internal/assert"
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

	// getResourceTimeoutOverride replaces getResourceTimeout when positive.
	getResourceTimeoutOverride time.Duration

	mu        sync.RWMutex
	accountID string
	explorer  ResourceExplorer

	// unsupportedTypes records CFN types that Cloud Control couldn't list
	// (typically TypeNotFoundException or "not yet supported"). Reported back
	// to the caller in the scan response so gaps are visible.
	unsupportedTypes sync.Map // typeName -> string reason

	// identifiers caches each CFN type's primary identifier, used to derive the Cloud
	// Control identifier of refs found by Resource Explorer (see identifier.go).
	identifiers identifierCache

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
	DescribeType(ctx context.Context, in *cloudformation.DescribeTypeInput,
		opts ...func(*cloudformation.Options)) (*cloudformation.DescribeTypeOutput, error)
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
// search, kept to the scanner's region and global resources) and use
// CC.GetResource only for the type lookup.
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
		if listErr := s.typeListFailure(typeName, err); listErr != nil {
			errs = append(errs, listErr)
		}
	}
	return all, errors.Join(errs...)
}

// typeListFailure records a failed ListResources of typeName. A type Cloud Control cannot
// list is kept as an unsupported-type notice and yields nil; any other failure yields the
// error to report for the type.
func (s *CloudControlScanner) typeListFailure(typeName string, err error) error {
	if err == nil {
		return nil
	}
	if isUnsupportedTypeErr(err) {
		s.unsupportedTypes.Store(typeName, err.Error())
		return nil
	}
	s.listFailures.Add(1)
	log.Printf("CloudControl: ListResources(%s) failed: %v", typeName, err)
	return typeListError(typeName, err)
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

	// A ref found by Resource Explorer carries its ARN as Id; GetResource
	// takes the type's primary identifier, derived from the ARN.
	identifier, err := s.ccIdentifier(ctx, typeName, ref)
	if err != nil {
		return resource, fmt.Errorf("GetResource(%s, %s): %w", typeName, ref.Id, err)
	}
	properties, err := s.getProperties(ctx, typeName, identifier, ref)
	if err != nil {
		return resource, err
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

// getProperties fetches the resource's properties with GetResource, falling back to the
// properties ListResources cached on the ref. For a ref found by Resource Explorer it checks
// that the described resource has the ref's ARN, so an identifier derived wrongly can never
// attach another resource's configuration.
func (s *CloudControlScanner) getProperties(
	ctx context.Context, typeName, identifier string, ref *pb.ResourceRef,
) (string, error) {
	assert.That(identifier != "", "scanner: GetResource is sent an identifier")
	// The deadline spans all SDK retries of this one call, so it is sized to
	// the retryer (see getResourceTimeout) rather than to a single attempt.
	s.getCalls.Add(1)
	callCtx, cancel := context.WithTimeout(ctx, s.describeTimeout())
	out, err := s.client.GetResource(callCtx, &cloudcontrol.GetResourceInput{
		TypeName:   aws.String(typeName),
		Identifier: aws.String(identifier),
	})
	cancel()

	if err == nil && out.ResourceDescription != nil {
		properties := aws.ToString(out.ResourceDescription.Properties)
		if err := checkDescribedARN(properties, explorerARN(ref)); err != nil {
			s.getFailures.Add(1)
			return "", fmt.Errorf("GetResource(%s, %s): %w", typeName, identifier, err)
		}
		return properties, nil
	}
	s.getFailures.Add(1)
	if cached := ref.BasicAttributes["properties"]; cached != "" {
		// Fall back to what ListResources returned.
		log.Printf("CloudControl: GetResource(%s, %s) failed, using cached properties: %v",
			typeName, identifier, err)
		return cached, nil
	}
	if err == nil {
		err = errors.New("no resource description")
	}
	err = fmt.Errorf("GetResource(%s, %s): %w", typeName, identifier, err)
	if isAccessDeniedErr(err) {
		// The credential's scope, not a broken scan: the provider reports it as a notice.
		return "", &AccessDeniedError{Type: typeName, Err: err}
	}
	return "", err
}

// checkDescribedARN fails when arn is set and the described properties name a different Arn.
func checkDescribedARN(properties, arn string) error {
	if arn == "" {
		return nil
	}
	props, _ := parseCloudControlProperties(properties)
	described, ok := props["Arn"].(string)
	if !ok || described == "" || described == arn {
		return nil
	}
	return fmt.Errorf("described resource has Arn %s, want %s", described, arn)
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
	// getResourceTimeout bounds one GetResource call including SDK retries.
	// The provider's retryer (retryAttemptsMax 5, retryBackoffMax 5s, in
	// auth.go) backs off at most 16s across its retries, so 45s leaves
	// ~30s for the attempts themselves on slow handlers; an earlier 5s
	// budget cut calls off mid-retry. The caller runs 20 describes at once
	// (describeConcurrency) inside a 5-minute scope, so a hung handler now
	// holds one slot for 45s instead of the whole scope, and the resource
	// falls back to what ListResources returned.
	getResourceTimeout = 45 * time.Second
)

// describeTimeout returns the GetResource deadline. Tests set
// getResourceTimeoutOverride so a blocked call times out in milliseconds.
func (s *CloudControlScanner) describeTimeout() time.Duration {
	if s.getResourceTimeoutOverride > 0 {
		return s.getResourceTimeoutOverride
	}
	return getResourceTimeout
}

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
				addDiscoveredType(out, reverse, name)
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

// addDiscoveredType adds the CFN type name to the discovered service map and the reverse map
// for RE-driven enrichment, unless it is not of the AWS::Service::Resource shape.
func addDiscoveredType(types map[string][]string, reverse map[string]string, name string) {
	svc := serviceFromCFNType(name)
	if svc == "" {
		return
	}
	types[svc] = append(types[svc], name)
	if k := reFormatKey(name); k != "" {
		reverse[k] = name
	}
}

// scanServiceViaRE queries Resource Explorer for the service, keeps the refs
// this scanner owns (ownExplorerRefs), then looks up each result's CFN type
// (mapExplorerTypes). A type with no Cloud Control type is reported as
// unsupported and its refs skipped. Refs with a CFN type then get their
// Cloud Control identifier, or their type is listed through Cloud Control
// instead (resolveExplorerRefs).
//
// A failed query returns the refs from the pages before the failure along
// with the error, as listType does.
func (s *CloudControlScanner) scanServiceViaRE(ctx context.Context, serviceName string, re ResourceExplorer) ([]*pb.ResourceRef, error) {
	s.reQueries.Add(1)
	refs, err := re.QueryByService(ctx, strings.ToLower(serviceName))
	if err != nil {
		err = fmt.Errorf("ResourceExplorer.QueryByService(%s): %w", serviceName, err)
	}
	refs = s.ownExplorerRefs(serviceName, refs)
	refs = s.mapExplorerTypes(refs)
	// Resolve identifiers only for a query that completed: a failed one is
	// already reported, and listing types for a partial result could not
	// say which RE refs it replaced.
	if err != nil {
		return refs, err
	}
	return s.resolveExplorerRefs(ctx, serviceName, refs)
}

// explorerRegionGlobal is the region Resource Explorer reports for resources that belong to
// no region, such as IAM roles.
const explorerRegionGlobal = "global"

// ownExplorerRefs keeps the Resource Explorer refs this scanner owns. With an aggregator
// index a search returns every indexed region, but the scanner describes through its own
// region's Cloud Control client, and when several regions are scanned each region's scanner
// owns its refs. It keeps the scope Cloud Control listing has: every ref of a global service
// (whichever region RE indexed it in, as S3 buckets are indexed in their home region), and
// every ref RE marks global, both stamped Region "" as listType does so scans of several
// regions dedupe them, and otherwise the refs of the scanner's region.
//
// It filters the results rather than adding region: to the query because a region filter
// cannot express the global-service scope: an S3 bucket outside the scanner's region would
// be lost to every scanner.
func (s *CloudControlScanner) ownExplorerRefs(
	serviceName string, refs []*pb.ResourceRef,
) []*pb.ResourceRef {
	global := globalServices[strings.ToLower(serviceName)]
	own := make([]*pb.ResourceRef, 0, len(refs))
	for _, r := range refs {
		if global || r.Region == "" || r.Region == explorerRegionGlobal {
			r.Region = ""
			own = append(own, r)
		} else if r.Region == s.cfg.Region {
			own = append(own, r)
		}
	}
	if dropped := len(refs) - len(own); dropped > 0 {
		log.Printf("CloudControl: %s: left %d Resource Explorer refs of other regions to "+
			"their regions' scans", serviceName, dropped)
	}
	assert.That(len(own) <= len(refs), "scanner: keeping own RE refs keeps at most every ref")
	return own
}

// explorerUnsupportedReason is the unsupported_type reason for a Resource Explorer type that
// has no Cloud Control type.
const explorerUnsupportedReason = "Resource Explorer type has no Cloud Control " +
	"(CloudFormation registry) type"

// mapExplorerTypes stamps each RE ref with the CFN type of its Resource Explorer type as
// cfn_type, so DescribeResource can call Cloud Control, and fills in its account. A ref whose
// type has no Cloud Control type cannot be described: like a type Cloud Control cannot list,
// it is skipped and its type reported once as unsupported. Until discovery has succeeded
// there is no reverse map to tell which types Cloud Control supports, so only the overrides
// are applied and every ref is kept; the failed discovery already makes the scan partial.
func (s *CloudControlScanner) mapExplorerTypes(refs []*pb.ResourceRef) []*pb.ResourceRef {
	discovery.mu.Lock()
	reverse, types := discovery.reverse, discovery.types
	discovery.mu.Unlock()
	accountID := s.getAccountID()

	kept := make([]*pb.ResourceRef, 0, len(refs))
	for _, r := range refs {
		if r.BasicAttributes == nil {
			r.BasicAttributes = map[string]string{}
		}
		// Normalize Service: RE may set it to upper-case shorthand.
		r.Service = strings.ToLower(r.Service)
		if r.AccountId == "" {
			r.AccountId = accountID
		}
		if reverse == nil {
			if cfnType, ok := explorerTypeOverrides[strings.ToLower(r.Type)]; ok {
				r.BasicAttributes["cfn_type"] = cfnType
			}
			kept = append(kept, r)
			continue
		}
		cfnType, ok := explorerCFNType(r.Type, reverse, types)
		if !ok {
			s.unsupportedTypes.Store(r.Type, explorerUnsupportedReason)
			continue
		}
		r.BasicAttributes["cfn_type"] = cfnType
		kept = append(kept, r)
	}
	assert.That(len(kept) <= len(refs), "scanner: mapping RE types keeps at most every ref")
	if reverse == nil {
		assert.That(len(kept) == len(refs), "scanner: without discovery every RE ref is kept")
	}
	return kept
}

// explorerCFNType returns the CloudFormation type of a Resource Explorer type ("service:
// resource", as RE's ResourceType reports it) and whether Cloud Control supports it. It
// tries, in order:
//
//   - explorerTypeOverrides, for RE types whose name does not lead to the CFN type: subtypes
//     (elasticloadbalancing:loadbalancer/app), ARN service names that differ from the CFN
//     service (rds:db, es:domain), and names that match the wrong type (iam:policy).
//   - the reverse map, built from CFN names by reFormatKey (kms:key).
//   - the reverse map with hyphens removed: RE names resources as their ARNs do
//     (ec2:security-group), while CFN names have none (ec2:securitygroup).
//
// A CFN type is supported only if discovery found it, since an override can name a type
// Cloud Control has no handlers for.
func explorerCFNType(
	explorerType string, reverse map[string]string, types map[string][]string,
) (string, bool) {
	key := strings.ToLower(explorerType)
	cfnType, ok := explorerTypeOverrides[key]
	if !ok {
		cfnType, ok = reverse[key]
	}
	if !ok {
		cfnType, ok = reverse[strings.ReplaceAll(key, "-", "")]
	}
	if !ok {
		return "", false
	}
	assert.That(strings.HasPrefix(cfnType, "AWS::"), "scanner: RE types map to AWS:: types")
	return cfnType, discoveredType(types, cfnType)
}

// discoveredType reports whether discovery found cfnType. Each service's types are sorted.
func discoveredType(types map[string][]string, cfnType string) bool {
	serviceTypes := types[serviceFromCFNType(cfnType)]
	index := sort.SearchStrings(serviceTypes, cfnType)
	return index < len(serviceTypes) && serviceTypes[index] == cfnType
}

// explorerTypeOverrides maps the lowercase Resource Explorer types whose "service:resource"
// name does not lead to their CFN type through the reverse map (see explorerCFNType). The RE
// names are those of RE's supported resource types; testdata/resource_explorer_types.txt
// checks them against the reverse map.
var explorerTypeOverrides = map[string]string{
	// RE's iam:policy is a managed policy, which is AWS::IAM::ManagedPolicy;
	// AWS::IAM::Policy is an inline policy.
	"iam:policy": "AWS::IAM::ManagedPolicy",

	// ELBv2 resources carry the ARN service elasticloadbalancing, shared with the classic
	// load balancer (RE's bare elasticloadbalancing:loadbalancer), and RE splits load
	// balancers, listeners and rules by their ARN's subtype: app, net or gwy.
	"elasticloadbalancing:loadbalancer/app":  "AWS::ElasticLoadBalancingV2::LoadBalancer",
	"elasticloadbalancing:loadbalancer/net":  "AWS::ElasticLoadBalancingV2::LoadBalancer",
	"elasticloadbalancing:loadbalancer/gwy":  "AWS::ElasticLoadBalancingV2::LoadBalancer",
	"elasticloadbalancing:targetgroup":       "AWS::ElasticLoadBalancingV2::TargetGroup",
	"elasticloadbalancing:listener":          "AWS::ElasticLoadBalancingV2::Listener",
	"elasticloadbalancing:listener/app":      "AWS::ElasticLoadBalancingV2::Listener",
	"elasticloadbalancing:listener/net":      "AWS::ElasticLoadBalancingV2::Listener",
	"elasticloadbalancing:listener/gwy":      "AWS::ElasticLoadBalancingV2::Listener",
	"elasticloadbalancing:listener-rule":     "AWS::ElasticLoadBalancingV2::ListenerRule",
	"elasticloadbalancing:listener-rule/app": "AWS::ElasticLoadBalancingV2::ListenerRule",
	"elasticloadbalancing:listener-rule/net": "AWS::ElasticLoadBalancingV2::ListenerRule",
	"elasticloadbalancing:truststore":        "AWS::ElasticLoadBalancingV2::TrustStore",

	// RE types named after the ARN, whose service or resource differs from the CFN name.
	"acm:certificate":                "AWS::CertificateManager::Certificate",
	"airflow:environment":            "AWS::MWAA::Environment",
	"amplify:apps":                   "AWS::Amplify::App",
	"apigateway:apis":                "AWS::ApiGatewayV2::Api",
	"apigateway:restapis":            "AWS::ApiGateway::RestApi",
	"ec2:elastic-ip":                 "AWS::EC2::EIP",
	"ec2:fleet":                      "AWS::EC2::EC2Fleet",
	"elasticache:cluster":            "AWS::ElastiCache::CacheCluster",
	"elasticfilesystem:access-point": "AWS::EFS::AccessPoint",
	"elasticfilesystem:file-system":  "AWS::EFS::FileSystem",
	"es:domain":                      "AWS::OpenSearchService::Domain",
	"firehose:deliverystream":        "AWS::KinesisFirehose::DeliveryStream",
	"rds:cluster":                    "AWS::RDS::DBCluster",
	"rds:cluster-pg":                 "AWS::RDS::DBClusterParameterGroup",
	"rds:db":                         "AWS::RDS::DBInstance",
	"rds:es":                         "AWS::RDS::EventSubscription",
	"rds:og":                         "AWS::RDS::OptionGroup",
	"rds:pg":                         "AWS::RDS::DBParameterGroup",
	"rds:subgrp":                     "AWS::RDS::DBSubnetGroup",
	"states:activity":                "AWS::StepFunctions::Activity",
	"states:statemachine":            "AWS::StepFunctions::StateMachine",
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
