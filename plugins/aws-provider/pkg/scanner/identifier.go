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
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation"
	cfntypes "github.com/aws/aws-sdk-go-v2/service/cloudformation/types"
	"github.com/jlgore/corkscrew/internal/assert"
	pb "github.com/jlgore/corkscrew/internal/proto"
)

// Cloud Control's GetResource takes a type's primary identifier, as declared by the
// primaryIdentifier of its CloudFormation resource schema: the bucket name for
// AWS::S3::Bucket, the instance id for AWS::EC2::Instance, the ARN for
// AWS::ElasticLoadBalancingV2::LoadBalancer. Refs listed by Cloud Control already carry it as
// their Id. Refs found by Resource Explorer carry the resource's ARN instead, so their
// identifier is derived from the ARN here, and a type whose identifier cannot be derived is
// listed through Cloud Control instead (see resolveExplorerRefs).

const (
	// primaryIdentifierTypesMax caps the per-scanner schema cache. CloudFormation has about
	// 1,400 public AWS:: resource types, so a full cache fits with room to spare; past the
	// cap a schema is still used but not kept.
	primaryIdentifierTypesMax = 4096
	// primaryIdentifierPropertiesMax bounds a composite primary identifier. The widest real
	// ones have three or four properties; a schema with more is treated as malformed.
	primaryIdentifierPropertiesMax = 8
	// describeTypeTimeout bounds one cloudformation.DescribeType call including retries,
	// sized like getResourceTimeout for the same retryer.
	describeTypeTimeout = 45 * time.Second
	// ccIdentifierAttribute holds the derived Cloud Control identifier on an RE ref, so
	// DescribeResource does not derive it a second time.
	ccIdentifierAttribute = "cc_identifier"
	// schemaPropertyPrefix starts every JSON pointer in a primaryIdentifier.
	schemaPropertyPrefix = "/properties/"
)

// identifierCache holds each CFN type's primary identifier property names.
type identifierCache struct {
	mu      sync.Mutex
	primary map[string][]string
}

// UnresolvedIdentifierError is an RE ref whose Cloud Control identifier cannot be derived
// from its ARN, so GetResource cannot be called for it.
type UnresolvedIdentifierError struct {
	Type   string
	ARN    string
	Reason string
}

func (e *UnresolvedIdentifierError) Error() string {
	return fmt.Sprintf("%s: no Cloud Control identifier for %s: %s", e.Type, e.ARN, e.Reason)
}

// explorerARN returns the ARN of a ref found by Resource Explorer, or "" for a ref listed by
// Cloud Control, whose Id is already the identifier. RE refs carry their ARN as both the Id
// and the "arn" attribute (ResourceExplorer.convertResults); listType never sets "arn".
func explorerARN(ref *pb.ResourceRef) string {
	arn := ref.BasicAttributes["arn"]
	if arn == "" || ref.Id != arn {
		return ""
	}
	return arn
}

// ccIdentifier returns the identifier DescribeResource sends to GetResource for ref.
func (s *CloudControlScanner) ccIdentifier(
	ctx context.Context, typeName string, ref *pb.ResourceRef,
) (string, error) {
	arn := explorerARN(ref)
	if arn == "" {
		return ref.Id, nil
	}
	if id := ref.BasicAttributes[ccIdentifierAttribute]; id != "" {
		return id, nil
	}
	properties, err := s.primaryIdentifier(ctx, typeName)
	if err != nil {
		return "", err
	}
	id, ok := explorerIdentifier(typeName, properties, arn)
	if !ok {
		return "", &UnresolvedIdentifierError{Type: typeName, ARN: arn,
			Reason: fmt.Sprintf("primary identifier %v is not derivable from the ARN", properties)}
	}
	assert.That(id != "", "scanner: a derived identifier is not empty")
	return id, nil
}

// primaryIdentifier returns typeName's primary identifier property names from its
// CloudFormation schema, cached per scanner. A denied DescribeType is an *AccessDeniedError.
func (s *CloudControlScanner) primaryIdentifier(
	ctx context.Context, typeName string,
) ([]string, error) {
	if !strings.HasPrefix(typeName, "AWS::") {
		return nil, &UnresolvedIdentifierError{Type: typeName, Reason: "no CloudFormation type"}
	}
	s.identifiers.mu.Lock()
	cached, ok := s.identifiers.primary[typeName]
	s.identifiers.mu.Unlock()
	if ok {
		return cached, nil
	}
	if s.cfn == nil {
		return nil, errors.New("cloudformation:DescribeType: no CloudFormation client")
	}

	callCtx, cancel := context.WithTimeout(ctx, describeTypeTimeout)
	out, err := s.cfn.DescribeType(callCtx, &cloudformation.DescribeTypeInput{
		Type:     cfntypes.RegistryTypeResource,
		TypeName: aws.String(typeName),
	})
	cancel()
	if err != nil {
		err = fmt.Errorf("cloudformation:DescribeType: %w", err)
		if isAccessDeniedErr(err) {
			return nil, &AccessDeniedError{Type: typeName, Err: err}
		}
		return nil, fmt.Errorf("%s: %w", typeName, err)
	}
	properties, err := parsePrimaryIdentifier(aws.ToString(out.Schema))
	if err != nil {
		return nil, fmt.Errorf("%s: schema: %w", typeName, err)
	}

	s.identifiers.mu.Lock()
	defer s.identifiers.mu.Unlock()
	if s.identifiers.primary == nil {
		s.identifiers.primary = map[string][]string{}
	}
	if len(s.identifiers.primary) < primaryIdentifierTypesMax {
		s.identifiers.primary[typeName] = properties
	}
	assert.Thatf(len(s.identifiers.primary) <= primaryIdentifierTypesMax,
		"scanner: identifier cache within %d types", primaryIdentifierTypesMax)
	return properties, nil
}

// parsePrimaryIdentifier returns the top-level property names a resource schema's
// primaryIdentifier points at, e.g. ["BucketName"] for ["/properties/BucketName"].
func parsePrimaryIdentifier(schema string) ([]string, error) {
	var doc struct {
		PrimaryIdentifier []string `json:"primaryIdentifier"`
	}
	if err := json.Unmarshal([]byte(schema), &doc); err != nil {
		return nil, err
	}
	count := len(doc.PrimaryIdentifier)
	if count == 0 || count > primaryIdentifierPropertiesMax {
		return nil, fmt.Errorf("primaryIdentifier has %d properties, want 1 to %d",
			count, primaryIdentifierPropertiesMax)
	}
	properties := make([]string, 0, count)
	for _, pointer := range doc.PrimaryIdentifier {
		name, ok := strings.CutPrefix(pointer, schemaPropertyPrefix)
		if !ok || name == "" || strings.Contains(name, "/") {
			return nil, fmt.Errorf("primaryIdentifier %q is not a top-level property", pointer)
		}
		properties = append(properties, name)
	}
	assert.That(len(properties) == count, "scanner: one property per primaryIdentifier pointer")
	return properties, nil
}

// arnParts is a parsed arn:partition:service:region:account:resource.
type arnParts struct {
	arn       string
	partition string
	service   string
	region    string
	account   string
	resource  string
}

func parseARN(arn string) (arnParts, bool) {
	parts := strings.SplitN(arn, ":", 6)
	if len(parts) != 6 || parts[0] != "arn" || parts[1] == "" || parts[5] == "" {
		return arnParts{}, false
	}
	return arnParts{arn: arn, partition: parts[1], service: parts[2], region: parts[3],
		account: parts[4], resource: parts[5]}, true
}

// explorerIdentifier derives typeName's Cloud Control identifier from arn, given the
// primary identifier property names of its schema. It reports false rather than guess:
//
//   - A per-type override (identifierOverrides) wins when the schema names the property the
//     override expects.
//   - A composite identifier is not derivable.
//   - A property named Arn or ending in Arn is the ARN itself.
//   - A property named Id is ambiguous (some types' Id is the ARN, some a short id), so it
//     needs an override.
//   - Otherwise the identifier is the ARN's resource part when it has no type prefix
//     ("my-bucket"), or what follows the type prefix ("instance/i-1", "function:f") when
//     that contains no further '/' or ':'. Paths and qualifiers ("role/path/name",
//     "function:f:1") are ambiguous.
func explorerIdentifier(typeName string, properties []string, arn string) (string, bool) {
	parsed, ok := parseARN(arn)
	if !ok {
		return "", false
	}
	if len(properties) != 1 {
		return "", false
	}
	property := properties[0]
	if override, ok := identifierOverrides[typeName]; ok && override.property == property {
		return override.derive(parsed)
	}
	if property == "Arn" || strings.HasSuffix(property, "Arn") {
		return arn, true
	}
	if property == "Id" {
		return "", false
	}
	return afterResourceType(parsed.resource)
}

// afterResourceType returns the id in an ARN resource part: the whole part when it has no
// type prefix, else what follows the first '/' or ':' if that is a single segment.
func afterResourceType(resource string) (string, bool) {
	index := strings.IndexAny(resource, "/:")
	if index < 0 {
		return resource, resource != ""
	}
	id := resource[index+1:]
	if id == "" || strings.ContainsAny(id, "/:") {
		return "", false
	}
	return id, true
}

// identifierOverride derives the identifier of a type whose ARN the generic rule gets wrong
// or cannot decide. property is the schema's primary identifier the override was written
// for; when the schema says otherwise the override is not used.
type identifierOverride struct {
	property string
	derive   func(arnParts) (string, bool)
}

// identifierOverrides covers irregular types among those the scanner enumerates.
var identifierOverrides = map[string]identifierOverride{
	"AWS::IAM::Role":                     {"RoleName", lastPathSegment("role/")},
	"AWS::IAM::User":                     {"UserName", lastPathSegment("user/")},
	"AWS::IAM::Group":                    {"GroupName", lastPathSegment("group/")},
	"AWS::IAM::InstanceProfile":          {"InstanceProfileName", instanceProfileName},
	"AWS::EC2::SecurityGroup":            {"Id", singleSegment("security-group/")},
	"AWS::Route53::HostedZone":           {"Id", singleSegment("hostedzone/")},
	"AWS::SecretsManager::Secret":        {"Id", wholeARN},
	"AWS::CloudFormation::Stack":         {"StackId", wholeARN},
	"AWS::SQS::Queue":                    {"QueueUrl", sqsQueueURL},
	"AWS::Logs::LogGroup":                {"LogGroupName", logGroupName},
	"AWS::AutoScaling::AutoScalingGroup": {"AutoScalingGroupName", autoScalingGroupName},
	"AWS::SSM::Parameter":                {"Name", ssmParameterName},
}

var instanceProfileName = lastPathSegment("instance-profile/")

// wholeARN is for types whose Id-like identifier is the ARN (a secret's Id, a stack's StackId).
func wholeARN(parsed arnParts) (string, bool) {
	return parsed.arn, true
}

// lastPathSegment is for IAM names, whose ARN carries the path: role/path/to/name.
func lastPathSegment(prefix string) func(arnParts) (string, bool) {
	return func(parsed arnParts) (string, bool) {
		rest, ok := strings.CutPrefix(parsed.resource, prefix)
		if !ok {
			return "", false
		}
		name := rest[strings.LastIndex(rest, "/")+1:]
		return name, name != ""
	}
}

// singleSegment is for an Id that is the one segment after prefix.
func singleSegment(prefix string) func(arnParts) (string, bool) {
	return func(parsed arnParts) (string, bool) {
		id, ok := strings.CutPrefix(parsed.resource, prefix)
		if !ok || id == "" || strings.ContainsAny(id, "/:") {
			return "", false
		}
		return id, true
	}
}

// sqsQueueURL builds https://sqs.<region>.<domain>/<account>/<name> from
// arn:<partition>:sqs:<region>:<account>:<name>. Partitions with an unknown domain are not
// derived.
func sqsQueueURL(parsed arnParts) (string, bool) {
	domain := map[string]string{
		"aws":        "amazonaws.com",
		"aws-us-gov": "amazonaws.com",
		"aws-cn":     "amazonaws.com.cn",
	}[parsed.partition]
	if domain == "" || parsed.region == "" || parsed.account == "" {
		return "", false
	}
	if strings.ContainsAny(parsed.resource, "/:") {
		return "", false
	}
	return fmt.Sprintf("https://sqs.%s.%s/%s/%s", parsed.region, domain, parsed.account,
		parsed.resource), true
}

// logGroupName takes log-group:/aws/lambda/f or log-group:/aws/lambda/f:*. Group names may
// contain '/' but never ':'.
func logGroupName(parsed arnParts) (string, bool) {
	rest, ok := strings.CutPrefix(parsed.resource, "log-group:")
	if !ok {
		return "", false
	}
	name := strings.TrimSuffix(rest, ":*")
	if name == "" || strings.Contains(name, ":") {
		return "", false
	}
	return name, true
}

// autoScalingGroupName takes autoScalingGroup:<uuid>:autoScalingGroupName/<name>.
func autoScalingGroupName(parsed arnParts) (string, bool) {
	const marker = ":autoScalingGroupName/"
	index := strings.Index(parsed.resource, marker)
	if !strings.HasPrefix(parsed.resource, "autoScalingGroup:") || index < 0 {
		return "", false
	}
	name := parsed.resource[index+len(marker):]
	return name, name != ""
}

// ssmParameterName takes parameter/name or parameter/a/b. A name containing '/' is
// hierarchical and SSM requires it to start with '/', which the ARN drops.
func ssmParameterName(parsed arnParts) (string, bool) {
	rest, ok := strings.CutPrefix(parsed.resource, "parameter/")
	if !ok || rest == "" {
		return "", false
	}
	if strings.Contains(rest, "/") {
		return "/" + rest, true
	}
	return rest, true
}

// resolveExplorerRefs stamps each RE ref of refs with its Cloud Control identifier. For a
// type whose identifier cannot be derived for every ref (a composite identifier, an
// ambiguous ARN, a failed DescribeType), it lists the type through Cloud Control and uses
// those refs instead of the RE refs it covers, so no resource loses its configuration.
//
// It returns one error per type whose schema was denied (an access_denied notice) or whose
// fallback listing failed (an error, or a notice when denied). A type whose listing failed
// keeps its RE refs, which describe as unenriched.
func (s *CloudControlScanner) resolveExplorerRefs(
	ctx context.Context, serviceName string, refs []*pb.ResourceRef,
) ([]*pb.ResourceRef, error) {
	byType := map[string][]*pb.ResourceRef{}
	var typeNames []string
	for _, r := range refs {
		typeName := r.BasicAttributes["cfn_type"]
		if explorerARN(r) == "" || !strings.HasPrefix(typeName, "AWS::") {
			continue // listed by Cloud Control, or no CFN type to resolve against
		}
		if _, seen := byType[typeName]; !seen {
			typeNames = append(typeNames, typeName)
		}
		byType[typeName] = append(byType[typeName], r)
	}
	sort.Strings(typeNames)
	assert.That(len(typeNames) == len(byType), "scanner: each RE type is resolved once")

	var errs []error
	var listTypes []string
	for _, typeName := range typeNames {
		resolved, err := s.stampIdentifiers(ctx, typeName, byType[typeName])
		if err != nil {
			errs = append(errs, err)
		}
		if !resolved {
			listTypes = append(listTypes, typeName)
		}
	}
	if len(listTypes) == 0 {
		return refs, errors.Join(errs...)
	}
	out, err := s.listInsteadOfExplorer(ctx, serviceName, refs, listTypes)
	return out, errors.Join(append(errs, err)...)
}

// stampIdentifiers sets ccIdentifierAttribute on every ref of typeName. It reports false when
// any ref's identifier cannot be derived, and returns an error only for a denied
// DescribeType: other DescribeType failures fall back to listing, which loses nothing.
func (s *CloudControlScanner) stampIdentifiers(
	ctx context.Context, typeName string, refs []*pb.ResourceRef,
) (bool, error) {
	assert.That(len(refs) > 0, "scanner: a type is resolved for at least one ref")
	properties, err := s.primaryIdentifier(ctx, typeName)
	if err != nil {
		log.Printf("CloudControl: primary identifier of %s: %v; listing the type", typeName, err)
		var denied *AccessDeniedError
		if errors.As(err, &denied) {
			return false, err
		}
		return false, nil
	}
	ids := make([]string, len(refs))
	for i, r := range refs {
		id, ok := explorerIdentifier(typeName, properties, explorerARN(r))
		if !ok {
			log.Printf("CloudControl: %s identifier %v not derivable from %s; listing the type",
				typeName, properties, r.Id)
			return false, nil
		}
		ids[i] = id
	}
	for i, r := range refs {
		r.BasicAttributes[ccIdentifierAttribute] = ids[i]
	}
	return true, nil
}

// listInsteadOfExplorer lists each of listTypes through Cloud Control and replaces the RE
// refs the listing covers: every ref of a global service's type, else the refs in the
// scanner's region. RE refs of other regions are kept as they are. A type whose listing
// fails keeps all its RE refs.
func (s *CloudControlScanner) listInsteadOfExplorer(
	ctx context.Context, serviceName string, refs []*pb.ResourceRef, listTypes []string,
) ([]*pb.ResourceRef, error) {
	var errs []error
	var listed []*pb.ResourceRef
	replaced := map[string]bool{}
	for _, typeName := range listTypes {
		typeRefs, err := s.listType(ctx, serviceName, typeName)
		if err != nil {
			// A partial listing would duplicate the RE refs it overlaps; keep those instead.
			if listErr := s.typeListFailure(typeName, err); listErr != nil {
				errs = append(errs, listErr)
			}
			continue
		}
		replaced[typeName] = true
		listed = append(listed, typeRefs...)
	}

	global := globalServices[strings.ToLower(serviceName)]
	out := make([]*pb.ResourceRef, 0, len(refs)+len(listed))
	for _, r := range refs {
		covered := replaced[r.BasicAttributes["cfn_type"]] && explorerARN(r) != "" &&
			(global || r.Region == s.cfg.Region)
		if !covered {
			out = append(out, r)
		}
	}
	assert.That(len(out) <= len(refs), "scanner: replacing RE refs keeps at most every ref")
	out = append(out, listed...)
	if len(replaced) == 0 {
		assert.That(len(out) == len(refs), "scanner: no listing, no RE ref replaced")
	}
	return out, errors.Join(errs...)
}
