package main

import (
	"context"
	"testing"
	"time"

	pb "github.com/jlgore/corkscrew/internal/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
)

func TestResourceScanner_ScanResources(t *testing.T) {
	// Create fake dynamic client with test data
	scheme := runtime.NewScheme()
	dynamicClient := dynamicfake.NewSimpleDynamicClient(scheme,
		createTestPod("test-namespace", "test-pod-1"),
		createTestPod("test-namespace", "test-pod-2"),
		createTestService("test-namespace", "test-service-1"),
	)

	// Create scanner
	clientFactory := &ClientFactory{
		dynamicClient: dynamicClient,
		clientset:     fake.NewSimpleClientset(),
	}
	scanner := &ResourceScanner{
		clientFactory: clientFactory,
		rateLimiter:   rate.NewLimiter(rate.Limit(100), 200),
	}

	// Test scanning pods
	ctx := context.Background()
	resources, err := scanner.ScanResources(ctx, []string{"pods"}, []string{"test-namespace"})
	
	require.NoError(t, err)
	assert.Len(t, resources, 2)
	assert.Equal(t, "Pod", resources[0].Type)
	assert.Equal(t, "test-pod-1", resources[0].Name)
}

func TestResourceScanner_StreamScanResources(t *testing.T) {
	// Create fake dynamic client
	scheme := runtime.NewScheme()
	dynamicClient := dynamicfake.NewSimpleDynamicClient(scheme,
		createTestPod("default", "pod-1"),
		createTestPod("default", "pod-2"),
		createTestPod("kube-system", "system-pod"),
	)

	clientFactory := &ClientFactory{
		dynamicClient: dynamicClient,
		clientset:     fake.NewSimpleClientset(),
	}
	scanner := &ResourceScanner{
		clientFactory: clientFactory,
		rateLimiter:   rate.NewLimiter(rate.Limit(100), 200),
	}

	// Test streaming
	ctx := context.Background()
	resourceChan := make(chan *pb.Resource, 10)
	
	go func() {
		err := scanner.StreamScanResources(ctx, []string{"pods"}, resourceChan)
		assert.NoError(t, err)
	}()

	// Collect streamed resources
	var resources []*pb.Resource
	timeout := time.After(2 * time.Second)
	
	for {
		select {
		case resource, ok := <-resourceChan:
			if !ok {
				goto done
			}
			resources = append(resources, resource)
		case <-timeout:
			t.Fatal("timeout waiting for resources")
		}
	}
done:

	assert.Len(t, resources, 3)
}

func TestResourceScanner_ScanWithLabelSelector(t *testing.T) {
	// Create pods with different labels
	pod1 := createTestPod("default", "pod-1")
	pod1.SetLabels(map[string]string{"app": "web", "tier": "frontend"})
	
	pod2 := createTestPod("default", "pod-2")
	pod2.SetLabels(map[string]string{"app": "db", "tier": "backend"})
	
	pod3 := createTestPod("default", "pod-3")
	pod3.SetLabels(map[string]string{"app": "web", "tier": "backend"})

	scheme := runtime.NewScheme()
	dynamicClient := dynamicfake.NewSimpleDynamicClient(scheme, pod1, pod2, pod3)

	clientFactory := &ClientFactory{
		dynamicClient: dynamicClient,
		clientset:     fake.NewSimpleClientset(),
	}
	scanner := &ResourceScanner{
		clientFactory: clientFactory,
		rateLimiter:   rate.NewLimiter(rate.Limit(100), 200),
	}

	// Test label selector
	ctx := context.Background()
	resources, err := scanner.ScanWithLabelSelector(ctx, "pods", "app=web", []string{"default"})
	
	require.NoError(t, err)
	assert.Len(t, resources, 2)
	
	// Verify both web pods were found
	names := []string{resources[0].Name, resources[1].Name}
	assert.Contains(t, names, "pod-1")
	assert.Contains(t, names, "pod-3")
}


// Helper functions

func createTestPod(namespace, name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata": map[string]interface{}{
				"name":              name,
				"namespace":         namespace,
				"uid":               "test-uid-" + name,
				"resourceVersion":   "1",
				"creationTimestamp": time.Now().Format(time.RFC3339),
			},
			"spec": map[string]interface{}{
				"containers": []interface{}{
					map[string]interface{}{
						"name":  "test-container",
						"image": "test-image",
					},
				},
			},
		},
	}
}

func createTestService(namespace, name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Service",
			"metadata": map[string]interface{}{
				"name":              name,
				"namespace":         namespace,
				"uid":               "test-uid-" + name,
				"resourceVersion":   "1",
				"creationTimestamp": time.Now().Format(time.RFC3339),
			},
			"spec": map[string]interface{}{
				"selector": map[string]interface{}{
					"app": "test",
				},
				"ports": []interface{}{
					map[string]interface{}{
						"port":       80,
						"targetPort": 8080,
					},
				},
			},
		},
	}
}