package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	pb "github.com/jlgore/corkscrew/internal/proto"
	"github.com/jlgore/corkscrew/internal/shared"
	"github.com/jlgore/corkscrew/internal/shared/paging"
	"golang.org/x/time/rate"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	k8stesting "k8s.io/client-go/testing"
)

// newFailingServicesClient serves pods normally and fails every Service list.
func newFailingServicesClient() *dynamicfake.FakeDynamicClient {
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(),
		createTestPod("default", "pod-1"),
		createTestPod("default", "pod-2"),
		createTestService("default", "svc-1"),
	)
	client.PrependReactor("list", "services",
		func(k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, errors.New("etcdserver: request timed out")
		})
	return client
}

func TestBatchScanReportsFailedTypeAndKeepsResources(t *testing.T) {
	cluster := &ClusterConnection{
		Name:          "test",
		Config:        &rest.Config{},
		Clientset:     fake.NewSimpleClientset(),
		DynamicClient: newFailingServicesClient(),
		Healthy:       true,
	}
	p := &KubernetesProvider{
		config:   &ProviderConfig{},
		clusters: map[string]*ClusterConnection{cluster.Name: cluster},
	}
	p.initialized.Store(true)

	resp, err := p.BatchScan(context.Background(), &pb.BatchScanRequest{
		Services: []string{"pods", "services"},
	})
	if err != nil {
		t.Fatalf("BatchScan: %v", err)
	}
	if len(resp.Resources) != 2 {
		t.Fatalf("got %d resources, want the 2 pods", len(resp.Resources))
	}
	errs, notices := shared.SplitScanErrors(resp.Errors)
	if len(errs) != 1 || len(notices) != 0 {
		t.Fatalf("Errors = %v, want exactly one error entry", resp.Errors)
	}
	if shared.IsScanNotice(errs[0]) || !strings.Contains(errs[0], "services") {
		t.Fatalf("entry %q must be an error naming the services type", errs[0])
	}
}

func TestStreamResourceTypeStopsOnRepeatedContinueToken(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(),
		createTestPod("default", "pod-1"))
	lists := 0
	client.PrependReactor("list", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		lists++
		list := &unstructured.UnstructuredList{}
		list.SetContinue("same")
		list.Items = []unstructured.Unstructured{*createTestPod("default", "pod-1")}
		return true, list, nil
	})
	scanner := &ResourceScanner{
		clientFactory: &ClientFactory{dynamicClient: client, clientset: fake.NewSimpleClientset()},
		rateLimiter:   rate.NewLimiter(rate.Limit(100), 200),
	}

	resourceChan := make(chan *pb.Resource, 10)
	err := scanner.streamResourceType(context.Background(), "pods", resourceChan)
	if !errors.Is(err, paging.ErrTokenRepeated) {
		t.Fatalf("streamResourceType error = %v, want ErrTokenRepeated", err)
	}
	if lists > 3 {
		t.Fatalf("listed %d pages for a token that never advanced", lists)
	}
}
