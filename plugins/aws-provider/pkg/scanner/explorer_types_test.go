package scanner

import (
	"bufio"
	"context"
	"os"
	"sort"
	"strings"
	"testing"

	pb "github.com/jlgore/corkscrew/internal/proto"
)

// explorerTypeCase is one line of testdata/resource_explorer_types.txt: a Resource Explorer
// type and the CFN type it should map to, or "" when it has none.
type explorerTypeCase struct {
	explorerType string
	cfnType      string
}

func readExplorerTypes(t *testing.T) []explorerTypeCase {
	t.Helper()
	file, err := os.Open("testdata/resource_explorer_types.txt")
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer func() { _ = file.Close() }()

	var cases []explorerTypeCase
	lines := bufio.NewScanner(file)
	for lines.Scan() {
		line := strings.TrimSpace(lines.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			t.Fatalf("fixture line %q: want <explorer type> <cfn type | ->", line)
		}
		c := explorerTypeCase{explorerType: fields[0], cfnType: fields[1]}
		if c.cfnType == "-" {
			c.cfnType = ""
		}
		cases = append(cases, c)
	}
	if err := lines.Err(); err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return cases
}

// discoveredFrom builds the discovery maps the way discoverTypes does, from CFN type names.
func discoveredFrom(names []string) (map[string][]string, map[string]string) {
	types, reverse := map[string][]string{}, map[string]string{}
	for _, name := range names {
		addDiscoveredType(types, reverse, name)
	}
	for svc := range types {
		sort.Strings(types[svc])
	}
	return types, reverse
}

// Every Resource Explorer type in the fixture maps to its CFN type through the reverse map
// discovery builds, so its resources can be described, and every type with no CFN type is
// reported unmapped. The test logs the unmapped types so gaps stay visible.
//
// Discovery is simulated with the fixture's own CFN types plus AWS::IAM::Policy, an inline
// policy that iam:policy must not map to. The classic load balancer,
// AWS::ElasticLoadBalancing::LoadBalancer, is absent as it is from real discovery: it is a
// legacy type with no Cloud Control handlers.
func TestExplorerTypeFixture(t *testing.T) {
	cases := readExplorerTypes(t)
	names := []string{"AWS::IAM::Policy"}
	for _, c := range cases {
		if c.cfnType != "" {
			names = append(names, c.cfnType)
		}
	}
	types, reverse := discoveredFrom(names)

	var unmapped, wrong []string
	for _, c := range cases {
		got, ok := explorerCFNType(c.explorerType, reverse, types)
		if !ok {
			got = ""
			unmapped = append(unmapped, c.explorerType)
		}
		if got != c.cfnType {
			wrong = append(wrong, c.explorerType+" -> "+got+", want "+c.cfnType)
		}
	}
	t.Logf("%d of %d Resource Explorer types have no Cloud Control type: %s", len(unmapped),
		len(cases), strings.Join(unmapped, ", "))
	if len(wrong) > 0 {
		t.Fatalf("Resource Explorer types mapped wrongly:\n%s", strings.Join(wrong, "\n"))
	}
}

// An override names a CFN type discovery may not have found; Cloud Control cannot describe
// it then, so the type is unsupported.
func TestExplorerCFNTypeRequiresDiscoveredType(t *testing.T) {
	types, reverse := discoveredFrom([]string{"AWS::IAM::Role"})
	if got, ok := explorerCFNType("iam:policy", reverse, types); ok {
		t.Fatalf("iam:policy -> %q supported, want unsupported without AWS::IAM::ManagedPolicy",
			got)
	}
	if got, ok := explorerCFNType("iam:role", reverse, types); !ok || got != "AWS::IAM::Role" {
		t.Fatalf("iam:role -> %q, %t; want AWS::IAM::Role", got, ok)
	}
}

const (
	albARN = "arn:aws:elasticloadbalancing:us-east-1:111111111111:loadbalancer/app/web/50dc6c49"
	amiARN = "arn:aws:ec2:us-east-1:111111111111:image/ami-0abcdef1234567890"
)

// An ALB found by Resource Explorer gets its ELBv2 CFN type, so it can be described. An AMI
// has no Cloud Control type: it is skipped and reported once as an unsupported type, not
// emitted bare for every image.
func TestScanServiceViaExplorerMapsSubtypesAndReportsUnsupported(t *testing.T) {
	types, reverse := discoveredFrom([]string{
		"AWS::ElasticLoadBalancingV2::LoadBalancer", "AWS::EC2::Instance",
	})
	resetDiscovery(t)
	discovery.mu.Lock()
	discovery.ok, discovery.types, discovery.reverse = true, types, reverse
	discovery.mu.Unlock()

	s, _, registry := bucketScanner()
	registry.schemas["AWS::ElasticLoadBalancingV2::LoadBalancer"] =
		`["/properties/LoadBalancerArn"]`
	secondAMI := strings.Replace(amiARN, "0abcdef", "1111111", 1)
	s.SetResourceExplorer(&fakeExplorer{refs: []*pb.ResourceRef{
		explorerRef("elasticloadbalancing", "elasticloadbalancing:loadbalancer/app", "", albARN),
		explorerRef("ec2", "ec2:image", "", amiARN),
		explorerRef("ec2", "ec2:image", "", secondAMI),
	}})

	refs, err := s.ScanService(context.Background(), "elasticloadbalancing")
	if err != nil || len(refs) != 1 {
		t.Fatalf("ScanService = %v, %v; want only the load balancer", refIDs(refs), err)
	}
	attrs := refs[0].BasicAttributes
	if attrs["cfn_type"] != "AWS::ElasticLoadBalancingV2::LoadBalancer" ||
		attrs[ccIdentifierAttribute] != albARN {
		t.Fatalf("attributes = %v, want the ELBv2 load balancer identified by its ARN", attrs)
	}
	unsupported := s.UnsupportedTypes()
	if len(unsupported) != 1 || unsupported["ec2:image"] == "" {
		t.Fatalf("unsupported = %v, want ec2:image once", unsupported)
	}
}
