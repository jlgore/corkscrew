package scanner

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	pb "github.com/jlgore/corkscrew/internal/proto"
)

const (
	clusterEastARN = "arn:aws:ecs:us-east-1:111111111111:cluster/east"
	clusterWestARN = "arn:aws:ecs:us-west-2:111111111111:cluster/west"
)

// aggregatorScanner is a scanner for region whose Resource Explorer view sits on an
// aggregator index: every search returns refs from both us-east-1 and us-west-2, whichever
// region asks. Its Cloud Control client knows only its own region's cluster, as the real
// regional endpoint does.
func aggregatorScanner(region, clusterName, clusterARN string) (
	*CloudControlScanner, *strictCloudControl,
) {
	cc := &strictCloudControl{resources: map[string]map[string]string{
		"AWS::ECS::Cluster": {
			clusterName: `{"ClusterName":"` + clusterName + `","Arn":"` + clusterARN + `"}`,
		},
	}}
	registry := &schemaRegistry{schemas: map[string]string{
		"AWS::ECS::Cluster": `["/properties/ClusterName"]`,
	}}
	s := &CloudControlScanner{
		client: cc, cfn: registry,
		identity: fakeIdentity{account: "111111111111"},
		cfg:      aws.Config{Region: region},
	}
	east := explorerRef("ecs", "ecs:cluster", "", clusterEastARN)
	west := explorerRef("ecs", "ecs:cluster", "", clusterWestARN)
	west.Region = "us-west-2"
	s.SetResourceExplorer(&fakeExplorer{refs: []*pb.ResourceRef{east, west}})
	return s, cc
}

// With an aggregator index each regional scanner keeps only its own region's refs: its
// Cloud Control client cannot describe another region's resources, and that region's
// scanner owns them, so scanning both regions emits each resource exactly once.
func TestScanServiceViaExplorerKeepsOnlyItsRegion(t *testing.T) {
	discovered(t, map[string]string{"ecs:cluster": "AWS::ECS::Cluster"})
	eastScanner, eastCC := aggregatorScanner("us-east-1", "east", clusterEastARN)
	westScanner, westCC := aggregatorScanner("us-west-2", "west", clusterWestARN)

	seen := map[string]int{}
	for _, s := range []*CloudControlScanner{eastScanner, westScanner} {
		refs, err := s.ScanService(context.Background(), "ecs")
		if err != nil {
			t.Fatalf("%s ScanService: %v", s.cfg.Region, err)
		}
		if len(refs) != 1 || refs[0].Region != s.cfg.Region {
			t.Fatalf("%s refs = %v, want only its own region's cluster", s.cfg.Region,
				refIDs(refs))
		}
		for _, r := range refs {
			seen[r.Id]++
			res, err := s.DescribeResource(context.Background(), r)
			if err != nil || res.RawData == "" {
				t.Fatalf("%s DescribeResource(%s) = %v, %v", s.cfg.Region, r.Id, res, err)
			}
		}
	}
	if len(seen) != 2 || seen[clusterEastARN] != 1 || seen[clusterWestARN] != 1 {
		t.Fatalf("emitted %v, want each cluster exactly once", seen)
	}
	if len(eastCC.gets) != 1 || len(westCC.gets) != 1 {
		t.Fatalf("GetResource calls east %v, west %v; want one each, in its own region",
			eastCC.gets, westCC.gets)
	}
}

// Global services keep every ref, whichever region RE indexed it in, as Cloud Control
// listing does, and the refs are stamped Region "" so scans of several regions dedupe them.
// A ref RE itself marks global is kept the same way.
func TestScanServiceViaExplorerKeepsGlobalRefs(t *testing.T) {
	discovered(t, map[string]string{"s3:bucket": "AWS::S3::Bucket"})
	s, _, _ := bucketScanner()
	bucket := explorerRef("s3", "s3:bucket", "", bucketARN)
	bucket.Region = "eu-west-1" // RE indexes a bucket in its home region
	s.SetResourceExplorer(&fakeExplorer{refs: []*pb.ResourceRef{bucket}})

	refs, err := s.ScanService(context.Background(), "s3")
	if err != nil || len(refs) != 1 {
		t.Fatalf("ScanService = %v, %v; want the eu-west-1 bucket", refIDs(refs), err)
	}
	if refs[0].Region != "" {
		t.Fatalf("bucket Region = %q, want \"\" as listing stamps global services",
			refs[0].Region)
	}
}

// RE reports some resources of regional services in region "global" (no region in the ARN);
// they belong to no regional scanner, so every scanner keeps them, stamped Region "".
func TestOwnExplorerRefsKeepsRefsMarkedGlobal(t *testing.T) {
	s := &CloudControlScanner{cfg: aws.Config{Region: "us-east-1"}}
	refs := []*pb.ResourceRef{
		{Id: "a", Region: "global"},
		{Id: "b", Region: ""},
		{Id: "c", Region: "us-east-1"},
		{Id: "d", Region: "us-west-2"},
	}
	own := s.ownExplorerRefs("wafv2", refs)
	byID := refIDs(own)
	if len(own) != 3 || byID["a"] == nil || byID["b"] == nil || byID["c"] == nil {
		t.Fatalf("kept %v, want the global refs and the us-east-1 ref", byID)
	}
	if byID["a"].Region != "" || byID["b"].Region != "" || byID["c"].Region != "us-east-1" {
		t.Fatalf("regions = %q %q %q, want global refs stamped \"\"", byID["a"].Region,
			byID["b"].Region, byID["c"].Region)
	}
}
