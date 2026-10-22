package scanner

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudcontrol"
	cctypes "github.com/aws/aws-sdk-go-v2/service/cloudcontrol/types"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation"
	pb "github.com/jlgore/corkscrew/internal/proto"
)

// schemaRegistry serves CloudFormation resource schemas whose primaryIdentifier is the given
// JSON array, counting DescribeType calls per type. err fails every DescribeType call.
type schemaRegistry struct {
	fakeTypeRegistry

	mu      sync.Mutex
	schemas map[string]string
	err     error
	calls   map[string]int
}

func (f *schemaRegistry) DescribeType(
	_ context.Context, in *cloudformation.DescribeTypeInput, _ ...func(*cloudformation.Options),
) (*cloudformation.DescribeTypeOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	typeName := aws.ToString(in.TypeName)
	if f.calls == nil {
		f.calls = map[string]int{}
	}
	f.calls[typeName]++
	if f.err != nil {
		return nil, f.err
	}
	primary, ok := f.schemas[typeName]
	if !ok {
		return nil, fmt.Errorf("TypeNotFoundException: %s", typeName)
	}
	schema := `{"typeName":"` + typeName + `","primaryIdentifier":` + primary + `}`
	return &cloudformation.DescribeTypeOutput{Schema: aws.String(schema)}, nil
}

func (f *schemaRegistry) callsFor(typeName string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[typeName]
}

// strictCloudControl answers GetResource only for the identifier Cloud Control really uses
// for a type, as the real API does: anything else, an ARN included, is not found.
// ListResources answers for the types in listed, with each resource's properties.
type strictCloudControl struct {
	mu        sync.Mutex
	resources map[string]map[string]string // type -> identifier -> properties
	listed    map[string]bool
	gets      []string
	lists     []string
}

func (f *strictCloudControl) GetResource(
	_ context.Context, in *cloudcontrol.GetResourceInput, _ ...func(*cloudcontrol.Options),
) (*cloudcontrol.GetResourceOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	typeName, id := aws.ToString(in.TypeName), aws.ToString(in.Identifier)
	f.gets = append(f.gets, typeName+" "+id)
	props, ok := f.resources[typeName][id]
	if !ok {
		return nil, fmt.Errorf("ResourceNotFoundException: %s with identifier %s", typeName, id)
	}
	return &cloudcontrol.GetResourceOutput{ResourceDescription: &cctypes.ResourceDescription{
		Identifier: aws.String(id), Properties: aws.String(props),
	}}, nil
}

func (f *strictCloudControl) ListResources(
	_ context.Context, in *cloudcontrol.ListResourcesInput, _ ...func(*cloudcontrol.Options),
) (*cloudcontrol.ListResourcesOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	typeName := aws.ToString(in.TypeName)
	f.lists = append(f.lists, typeName)
	if !f.listed[typeName] {
		return nil, fmt.Errorf("AccessDeniedException: not allowed to list %s", typeName)
	}
	out := &cloudcontrol.ListResourcesOutput{}
	for id, props := range f.resources[typeName] {
		out.ResourceDescriptions = append(out.ResourceDescriptions, cctypes.ResourceDescription{
			Identifier: aws.String(id), Properties: aws.String(props),
		})
	}
	return out, nil
}

// explorerRef builds a ref the way ResourceExplorer.convertResults and scanServiceViaRE do:
// the ARN is both the Id and the "arn" attribute, and the CFN type is in "cfn_type".
func explorerRef(service, reType, cfnType, arn string) *pb.ResourceRef {
	return &pb.ResourceRef{
		Service: service, Type: reType, Id: arn, Region: "us-east-1",
		AccountId: "111111111111",
		BasicAttributes: map[string]string{
			"arn": arn, "resource_type": reType, "cfn_type": cfnType,
		},
	}
}

const bucketARN = "arn:aws:s3:::my-bucket"

func bucketScanner() (*CloudControlScanner, *strictCloudControl, *schemaRegistry) {
	cc := &strictCloudControl{resources: map[string]map[string]string{
		"AWS::S3::Bucket": {"my-bucket": `{"BucketName":"my-bucket","Arn":"` + bucketARN + `"}`},
	}}
	registry := &schemaRegistry{schemas: map[string]string{
		"AWS::S3::Bucket": `["/properties/BucketName"]`,
	}}
	s := &CloudControlScanner{
		client: cc, cfn: registry,
		identity: fakeIdentity{account: "111111111111"},
		cfg:      aws.Config{Region: "us-east-1"},
	}
	return s, cc, registry
}

// A Resource Explorer ref carries its ARN as Id, but Cloud Control's GetResource takes the
// type's primary identifier (the bucket name for AWS::S3::Bucket). Sending the ARN fails and
// the resource loses its configuration.
func TestDescribeResourceSendsPrimaryIdentifierForExplorerRef(t *testing.T) {
	s, cc, _ := bucketScanner()
	ref := explorerRef("s3", "s3:bucket", "AWS::S3::Bucket", bucketARN)

	res, err := s.DescribeResource(context.Background(), ref)
	if err != nil {
		t.Fatalf("DescribeResource: %v (GetResource calls %v)", err, cc.gets)
	}
	if !strings.Contains(res.RawData, `"BucketName":"my-bucket"`) {
		t.Fatalf("RawData = %q, want the bucket's configuration", res.RawData)
	}
	if res.Id != bucketARN || res.Arn != bucketARN {
		t.Fatalf("resource {Id:%q Arn:%q}, want both the ARN", res.Id, res.Arn)
	}
	if len(cc.gets) != 1 || cc.gets[0] != "AWS::S3::Bucket my-bucket" {
		t.Fatalf("GetResource calls = %v, want one with the bucket name", cc.gets)
	}
}

// The primary identifiers below are those of the published CloudFormation resource schemas.
func TestExplorerIdentifier(t *testing.T) {
	cases := []struct {
		typeName, property, arn, want string
	}{
		{"AWS::S3::Bucket", "BucketName", "arn:aws:s3:::my-bucket", "my-bucket"},
		{"AWS::EC2::Instance", "InstanceId",
			"arn:aws:ec2:us-east-1:111111111111:instance/i-0abc", "i-0abc"},
		{"AWS::EC2::VPC", "VpcId", "arn:aws:ec2:us-east-1:111111111111:vpc/vpc-1", "vpc-1"},
		{"AWS::EC2::Subnet", "SubnetId",
			"arn:aws:ec2:us-east-1:111111111111:subnet/subnet-1", "subnet-1"},
		{"AWS::EC2::SecurityGroup", "Id",
			"arn:aws:ec2:us-east-1:111111111111:security-group/sg-1", "sg-1"},
		{"AWS::IAM::Role", "RoleName", "arn:aws:iam::111111111111:role/admin", "admin"},
		{"AWS::IAM::Role", "RoleName",
			"arn:aws:iam::111111111111:role/service-role/team/deployer", "deployer"},
		{"AWS::IAM::User", "UserName", "arn:aws:iam::111111111111:user/ops/alice", "alice"},
		{"AWS::IAM::ManagedPolicy", "PolicyArn", "arn:aws:iam::111111111111:policy/p",
			"arn:aws:iam::111111111111:policy/p"},
		{"AWS::Lambda::Function", "FunctionName",
			"arn:aws:lambda:us-east-1:111111111111:function:handler", "handler"},
		{"AWS::ElasticLoadBalancingV2::LoadBalancer", "LoadBalancerArn",
			"arn:aws:elasticloadbalancing:us-east-1:111111111111:loadbalancer/app/web/50dc6c",
			"arn:aws:elasticloadbalancing:us-east-1:111111111111:loadbalancer/app/web/50dc6c"},
		{"AWS::RDS::DBInstance", "DBInstanceIdentifier",
			"arn:aws:rds:us-east-1:111111111111:db:orders", "orders"},
		{"AWS::RDS::DBCluster", "DBClusterIdentifier",
			"arn:aws:rds:us-east-1:111111111111:cluster:orders", "orders"},
		{"AWS::KMS::Key", "KeyId",
			"arn:aws:kms:us-east-1:111111111111:key/0c722e12-fa65", "0c722e12-fa65"},
		{"AWS::SNS::Topic", "TopicArn", "arn:aws:sns:us-east-1:111111111111:alerts",
			"arn:aws:sns:us-east-1:111111111111:alerts"},
		{"AWS::SQS::Queue", "QueueUrl", "arn:aws:sqs:us-east-1:111111111111:jobs",
			"https://sqs.us-east-1.amazonaws.com/111111111111/jobs"},
		{"AWS::SQS::Queue", "QueueUrl", "arn:aws-cn:sqs:cn-north-1:111111111111:jobs",
			"https://sqs.cn-north-1.amazonaws.com.cn/111111111111/jobs"},
		{"AWS::DynamoDB::Table", "TableName",
			"arn:aws:dynamodb:us-east-1:111111111111:table/orders", "orders"},
		{"AWS::ECS::Cluster", "ClusterName",
			"arn:aws:ecs:us-east-1:111111111111:cluster/prod", "prod"},
		{"AWS::EKS::Cluster", "Name", "arn:aws:eks:us-east-1:111111111111:cluster/prod", "prod"},
		{"AWS::CloudWatch::Alarm", "AlarmName",
			"arn:aws:cloudwatch:us-east-1:111111111111:alarm:cpu-high", "cpu-high"},
		{"AWS::SecretsManager::Secret", "Id",
			"arn:aws:secretsmanager:us-east-1:111111111111:secret:db-AbCdEf",
			"arn:aws:secretsmanager:us-east-1:111111111111:secret:db-AbCdEf"},
		{"AWS::CloudFormation::Stack", "StackId",
			"arn:aws:cloudformation:us-east-1:111111111111:stack/app/1b2c",
			"arn:aws:cloudformation:us-east-1:111111111111:stack/app/1b2c"},
		{"AWS::Route53::HostedZone", "Id", "arn:aws:route53:::hostedzone/Z123", "Z123"},
		{"AWS::Logs::LogGroup", "LogGroupName",
			"arn:aws:logs:us-east-1:111111111111:log-group:/aws/lambda/handler:*",
			"/aws/lambda/handler"},
		{"AWS::AutoScaling::AutoScalingGroup", "AutoScalingGroupName",
			"arn:aws:autoscaling:us-east-1:111111111111:autoScalingGroup:9f1e:" +
				"autoScalingGroupName/web", "web"},
		{"AWS::SSM::Parameter", "Name",
			"arn:aws:ssm:us-east-1:111111111111:parameter/app/db/url", "/app/db/url"},
		{"AWS::SSM::Parameter", "Name", "arn:aws:ssm:us-east-1:111111111111:parameter/flag",
			"flag"},
	}
	for _, c := range cases {
		got, ok := explorerIdentifier(c.typeName, []string{c.property}, c.arn)
		if !ok || got != c.want {
			t.Errorf("explorerIdentifier(%s, %s, %s) = %q, %t; want %q",
				c.typeName, c.property, c.arn, got, ok, c.want)
		}
	}
}

// Where the identifier is not certain, explorerIdentifier must not guess.
func TestExplorerIdentifierDoesNotGuess(t *testing.T) {
	cases := []struct {
		typeName   string
		properties []string
		arn        string
	}{
		// Composite identifiers.
		{"AWS::ECS::Service", []string{"ServiceArn", "Cluster"},
			"arn:aws:ecs:us-east-1:111111111111:service/prod/web"},
		// An Id without an override: some types' Id is the ARN, some a short id.
		{"AWS::IAM::Policy", []string{"Id"}, "arn:aws:iam::111111111111:policy/p"},
		// A qualified function ARN names a version, not the function.
		{"AWS::Lambda::Function", []string{"FunctionName"},
			"arn:aws:lambda:us-east-1:111111111111:function:handler:3"},
		// A path the generic rule cannot split.
		{"AWS::Example::Thing", []string{"ThingName"},
			"arn:aws:example:us-east-1:111111111111:thing/a/b"},
		// An override whose schema property differs is not applied, and the generic rule
		// refuses the path.
		{"AWS::IAM::Role", []string{"RoleId"}, "arn:aws:iam::111111111111:role/path/admin"},
		// An SQS ARN of an unknown partition has no known URL.
		{"AWS::SQS::Queue", []string{"QueueUrl"}, "arn:aws-iso:sqs:us-iso-east-1:1:jobs"},
		// Not an ARN.
		{"AWS::S3::Bucket", []string{"BucketName"}, "my-bucket"},
		{"AWS::S3::Bucket", []string{"BucketName"}, "arn:aws:s3:::"},
	}
	for _, c := range cases {
		if got, ok := explorerIdentifier(c.typeName, c.properties, c.arn); ok {
			t.Errorf("explorerIdentifier(%s, %v, %s) = %q, want no identifier",
				c.typeName, c.properties, c.arn, got)
		}
	}
}

func TestParsePrimaryIdentifier(t *testing.T) {
	got, err := parsePrimaryIdentifier(
		`{"primaryIdentifier":["/properties/ServiceArn","/properties/Cluster"]}`)
	if err != nil || len(got) != 2 || got[0] != "ServiceArn" || got[1] != "Cluster" {
		t.Fatalf("parsePrimaryIdentifier = %v, %v", got, err)
	}
	for _, schema := range []string{
		`{}`,
		`{"primaryIdentifier":[]}`,
		`{"primaryIdentifier":["/properties/A/B"]}`,
		`{"primaryIdentifier":["/definitions/A"]}`,
		`{"primaryIdentifier":["/properties/"]}`,
		`{"primaryIdentifier":["/properties/1","/properties/2","/properties/3",` +
			`"/properties/4","/properties/5","/properties/6","/properties/7","/properties/8",` +
			`"/properties/9"]}`,
		`not json`,
	} {
		if got, err := parsePrimaryIdentifier(schema); err == nil {
			t.Errorf("parsePrimaryIdentifier(%s) = %v, want an error", schema, got)
		}
	}
}

// One DescribeType per type per scanner, however many refs of the type are described.
func TestPrimaryIdentifierIsCachedPerType(t *testing.T) {
	s, cc, registry := bucketScanner()
	cc.resources["AWS::S3::Bucket"]["other"] = `{"BucketName":"other"}`
	for _, arn := range []string{bucketARN, "arn:aws:s3:::other", bucketARN} {
		ref := explorerRef("s3", "s3:bucket", "AWS::S3::Bucket", arn)
		if _, err := s.DescribeResource(context.Background(), ref); err != nil {
			t.Fatalf("DescribeResource(%s): %v", arn, err)
		}
	}
	if got := registry.callsFor("AWS::S3::Bucket"); got != 1 {
		t.Fatalf("DescribeType called %d times, want 1", got)
	}
}

// Past the cap a schema is used but not kept, so the cache cannot grow without bound.
func TestPrimaryIdentifierCacheIsBounded(t *testing.T) {
	s, _, registry := bucketScanner()
	s.identifiers.primary = map[string][]string{}
	for i := range primaryIdentifierTypesMax {
		s.identifiers.primary[fmt.Sprintf("AWS::Filler::T%d", i)] = []string{"Name"}
	}
	for range 2 {
		got, err := s.primaryIdentifier(context.Background(), "AWS::S3::Bucket")
		if err != nil || len(got) != 1 || got[0] != "BucketName" {
			t.Fatalf("primaryIdentifier = %v, %v", got, err)
		}
	}
	if len(s.identifiers.primary) != primaryIdentifierTypesMax {
		t.Fatalf("cache holds %d types, want the cap %d", len(s.identifiers.primary),
			primaryIdentifierTypesMax)
	}
	if got := registry.callsFor("AWS::S3::Bucket"); got != 2 {
		t.Fatalf("DescribeType called %d times, want 2 once the cache is full", got)
	}
}

// A denied DescribeType is an access_denied error naming the type, and GetResource is never
// sent the ARN in its place.
func TestDescribeResourceReportsDescribeTypeDenial(t *testing.T) {
	s, cc, registry := bucketScanner()
	registry.err = errors.New("AccessDeniedException: not authorized to perform " +
		"cloudformation:DescribeType")
	ref := explorerRef("s3", "s3:bucket", "AWS::S3::Bucket", bucketARN)

	res, err := s.DescribeResource(context.Background(), ref)
	var denied *AccessDeniedError
	if !errors.As(err, &denied) || denied.Type != "AWS::S3::Bucket" {
		t.Fatalf("err = %v, want an AccessDeniedError for AWS::S3::Bucket", err)
	}
	if res == nil || res.Id != bucketARN {
		t.Fatalf("resource = %v, want the ref's identity kept", res)
	}
	if len(cc.gets) != 0 {
		t.Fatalf("GetResource calls = %v, want none", cc.gets)
	}
}

// Refs listed by Cloud Control already carry the identifier, even when it is an ARN, and
// must be described exactly as before: no schema lookup, Id sent as is.
func TestDescribeResourceLeavesCloudControlRefsUnchanged(t *testing.T) {
	s, cc, registry := bucketScanner()
	topicARN := "arn:aws:sns:us-east-1:111111111111:alerts"
	cc.resources["AWS::SNS::Topic"] = map[string]string{topicARN: `{"TopicArn":"x"}`}
	refs := []*pb.ResourceRef{
		{Service: "s3", Type: "AWS::S3::Bucket", Id: "my-bucket",
			BasicAttributes: map[string]string{"cfn_type": "AWS::S3::Bucket"}},
		{Service: "sns", Type: "AWS::SNS::Topic", Id: topicARN,
			BasicAttributes: map[string]string{"cfn_type": "AWS::SNS::Topic"}},
	}
	for _, ref := range refs {
		if _, err := s.DescribeResource(context.Background(), ref); err != nil {
			t.Fatalf("DescribeResource(%s): %v", ref.Id, err)
		}
	}
	want := []string{"AWS::S3::Bucket my-bucket", "AWS::SNS::Topic " + topicARN}
	if len(cc.gets) != 2 || cc.gets[0] != want[0] || cc.gets[1] != want[1] {
		t.Fatalf("GetResource calls = %v, want %v", cc.gets, want)
	}
	if registry.callsFor("AWS::S3::Bucket")+registry.callsFor("AWS::SNS::Topic") != 0 {
		t.Fatal("DescribeType called for refs listed by Cloud Control")
	}
}

// A derived identifier that names a different resource must not attach its configuration.
func TestDescribeResourceRejectsAnotherResourcesARN(t *testing.T) {
	s, cc, _ := bucketScanner()
	cc.resources["AWS::S3::Bucket"]["my-bucket"] =
		`{"BucketName":"my-bucket","Arn":"arn:aws:s3:::someone-else"}`
	ref := explorerRef("s3", "s3:bucket", "AWS::S3::Bucket", bucketARN)

	res, err := s.DescribeResource(context.Background(), ref)
	if err == nil || !strings.Contains(err.Error(), "someone-else") {
		t.Fatalf("err = %v, want the ARN mismatch", err)
	}
	if res.RawData != "" {
		t.Fatalf("RawData = %q, want no configuration", res.RawData)
	}
}

// fakeExplorer answers QueryByService with refs.
type fakeExplorer struct {
	refs []*pb.ResourceRef
	err  error
}

func (f *fakeExplorer) QueryAllResources(context.Context) ([]*pb.ResourceRef, error) {
	return f.refs, f.err
}

func (f *fakeExplorer) QueryByService(context.Context, string) ([]*pb.ResourceRef, error) {
	return f.refs, f.err
}

func (f *fakeExplorer) QueryByResourceType(context.Context, string) ([]*pb.ResourceRef, error) {
	return f.refs, f.err
}

func (f *fakeExplorer) IsHealthy(context.Context) bool { return true }

// discovered installs a successful type discovery with the given RE-to-CFN reverse map.
func discovered(t *testing.T, reverse map[string]string) {
	t.Helper()
	resetDiscovery(t)
	discovery.mu.Lock()
	defer discovery.mu.Unlock()
	discovery.ok, discovery.reverse = true, reverse
	discovery.types = map[string][]string{}
}

const (
	clusterARN      = "arn:aws:ecs:us-east-1:111111111111:cluster/prod"
	serviceARN      = "arn:aws:ecs:us-east-1:111111111111:service/prod/web"
	serviceWestARN  = "arn:aws:ecs:us-west-2:111111111111:service/prod/api"
	serviceListedID = serviceARN + "|prod"
)

func ecsScanner(t *testing.T) (*CloudControlScanner, *strictCloudControl, *schemaRegistry) {
	t.Helper()
	discovered(t, map[string]string{
		"ecs:cluster": "AWS::ECS::Cluster", "ecs:service": "AWS::ECS::Service",
	})
	cc := &strictCloudControl{
		resources: map[string]map[string]string{
			"AWS::ECS::Cluster": {"prod": `{"ClusterName":"prod","Arn":"` + clusterARN + `"}`},
			"AWS::ECS::Service": {serviceListedID: `{"ServiceArn":"` + serviceARN + `"}`},
		},
		listed: map[string]bool{"AWS::ECS::Service": true},
	}
	registry := &schemaRegistry{schemas: map[string]string{
		"AWS::ECS::Cluster": `["/properties/ClusterName"]`,
		"AWS::ECS::Service": `["/properties/ServiceArn","/properties/Cluster"]`,
	}}
	s := &CloudControlScanner{
		client: cc, cfn: registry,
		identity: fakeIdentity{account: "111111111111"},
		cfg:      aws.Config{Region: "us-east-1"},
	}
	west := explorerRef("ecs", "ecs:service", "", serviceWestARN)
	west.Region = "us-west-2"
	s.SetResourceExplorer(&fakeExplorer{refs: []*pb.ResourceRef{
		explorerRef("ecs", "ecs:cluster", "", clusterARN),
		explorerRef("ecs", "ecs:service", "", serviceARN),
		west,
	}})
	return s, cc, registry
}

func refIDs(refs []*pb.ResourceRef) map[string]*pb.ResourceRef {
	out := map[string]*pb.ResourceRef{}
	for _, r := range refs {
		out[r.Id] = r
	}
	return out
}

// A type whose identifier is derivable keeps its RE ref, stamped with the identifier. A
// composite type is listed through Cloud Control instead, replacing its RE refs. Another
// region's RE ref is left to that region's scan.
func TestScanServiceViaExplorerResolvesOrListsEachType(t *testing.T) {
	s, cc, _ := ecsScanner(t)

	refs, err := s.ScanService(context.Background(), "ecs")
	if err != nil {
		t.Fatalf("ScanService: %v", err)
	}
	byID := refIDs(refs)
	if len(refs) != 2 || byID[clusterARN] == nil || byID[serviceListedID] == nil {
		t.Fatalf("refs = %v, want the cluster and the listed service, not us-west-2's", byID)
	}
	if got := byID[clusterARN].BasicAttributes[ccIdentifierAttribute]; got != "prod" {
		t.Fatalf("cluster identifier = %q, want prod", got)
	}
	if len(cc.lists) != 1 || cc.lists[0] != "AWS::ECS::Service" {
		t.Fatalf("ListResources calls = %v, want only the composite type", cc.lists)
	}

	for _, id := range []string{clusterARN, serviceListedID} {
		res, err := s.DescribeResource(context.Background(), byID[id])
		if err != nil || res.RawData == "" {
			t.Fatalf("DescribeResource(%s) = %v, %v; want its configuration", id, res, err)
		}
	}
}

// A denied DescribeType is an access_denied notice, and the type is listed instead so its
// resources keep their configuration.
func TestScanServiceViaExplorerListsTypeWhenDescribeTypeIsDenied(t *testing.T) {
	s, cc, registry := ecsScanner(t)
	registry.err = errors.New("AccessDeniedException: not authorized")
	cc.listed["AWS::ECS::Cluster"] = true

	refs, err := s.ScanService(context.Background(), "ecs")
	leaves := FlattenErrors(err)
	if len(leaves) != 2 {
		t.Fatalf("errors = %v, want one denial per type", leaves)
	}
	for _, leaf := range leaves {
		var denied *AccessDeniedError
		if !errors.As(leaf, &denied) || !strings.Contains(leaf.Error(), "DescribeType") {
			t.Fatalf("error %v, want an access_denied DescribeType notice", leaf)
		}
	}
	byID := refIDs(refs)
	if len(refs) != 2 || byID["prod"] == nil || byID[serviceListedID] == nil {
		t.Fatalf("refs = %v, want both types listed", byID)
	}
}

// A failed fallback listing is reported as an error, so the scan is partial, and the type's
// RE refs are kept rather than dropped.
func TestScanServiceViaExplorerKeepsRefsWhenListingFails(t *testing.T) {
	s, cc, _ := ecsScanner(t)
	cc.listed["AWS::ECS::Service"] = false
	s.client = &throttledList{strictCloudControl: cc}

	refs, err := s.ScanService(context.Background(), "ecs")
	leaves := FlattenErrors(err)
	if len(leaves) != 1 || !strings.Contains(leaves[0].Error(), "AWS::ECS::Service: Throttling") {
		t.Fatalf("errors = %v, want the throttled listing", leaves)
	}
	var denied *AccessDeniedError
	if errors.As(leaves[0], &denied) {
		t.Fatal("a throttled listing was reported as an access denial")
	}
	if byID := refIDs(refs); len(refs) != 2 || byID[serviceARN] == nil {
		t.Fatalf("refs = %v, want every RE ref of the region kept", byID)
	}
}

// throttledList fails every ListResources with a throttling error.
type throttledList struct {
	*strictCloudControl
}

func (*throttledList) ListResources(
	context.Context, *cloudcontrol.ListResourcesInput, ...func(*cloudcontrol.Options),
) (*cloudcontrol.ListResourcesOutput, error) {
	return nil, errors.New("Throttling: rate exceeded")
}

// RE's iam:policy is a managed policy; mapping it to the inline AWS::IAM::Policy would send
// every managed policy to the wrong type.
func TestScanServiceViaExplorerMapsManagedPolicies(t *testing.T) {
	discovered(t, map[string]string{"iam:policy": "AWS::IAM::Policy"})
	policyARN := "arn:aws:iam::111111111111:policy/deploy"
	s, _, registry := bucketScanner()
	registry.schemas["AWS::IAM::ManagedPolicy"] = `["/properties/PolicyArn"]`
	s.SetResourceExplorer(&fakeExplorer{refs: []*pb.ResourceRef{
		explorerRef("iam", "iam:policy", "", policyARN),
	}})

	refs, err := s.ScanService(context.Background(), "iam")
	if err != nil || len(refs) != 1 {
		t.Fatalf("ScanService = %v, %v", refs, err)
	}
	attrs := refs[0].BasicAttributes
	if attrs["cfn_type"] != "AWS::IAM::ManagedPolicy" || attrs[ccIdentifierAttribute] != policyARN {
		t.Fatalf("attributes = %v, want AWS::IAM::ManagedPolicy identified by its ARN", attrs)
	}
}
