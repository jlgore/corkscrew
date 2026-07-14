package main

import (
	"context"
	"errors"
	"reflect"
	"testing"

	pb "github.com/jlgore/corkscrew/internal/proto"
)

func TestBatchScanServicesAggregatesResourcesStatsAndErrors(t *testing.T) {
	seen := []string{}
	scan := func(_ context.Context, req *pb.ScanServiceRequest) (*pb.ScanServiceResponse, error) {
		seen = append(seen, req.GetService())
		switch req.GetService() {
		case "zones":
			return &pb.ScanServiceResponse{
				Resources: []*pb.Resource{{Service: "zones", Type: "zone", Id: "zone-1"}},
			}, nil
		case "dns":
			return &pb.ScanServiceResponse{Errors: []string{"one record was skipped"}}, nil
		default:
			return nil, errors.New("unsupported")
		}
	}

	response := batchScanServices(context.Background(), []string{"zones", "dns", "bogus"}, scan)
	if !reflect.DeepEqual(seen, []string{"zones", "dns", "bogus"}) {
		t.Fatalf("services scanned = %#v", seen)
	}
	if len(response.Resources) != 1 || response.Resources[0].Id != "zone-1" {
		t.Fatalf("resources = %#v", response.Resources)
	}
	if response.Stats.GetTotalResources() != 1 || response.Stats.ServiceCounts["zones"] != 1 || response.Stats.ResourceCounts["zone"] != 1 {
		t.Fatalf("stats = %#v", response.Stats)
	}
	if response.Stats.GetFailedResources() != 2 || len(response.Errors) != 2 {
		t.Fatalf("errors/stats = %#v / %#v", response.Errors, response.Stats)
	}
}
