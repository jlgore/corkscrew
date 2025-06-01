package main

import (
	"bytes"
	"fmt"
	"text/template"
)

// ScannerGenerator generates scanner code for Kubernetes resources
type ScannerGenerator struct {
	templates *template.Template
}

// NewScannerGenerator creates a new scanner generator
func NewScannerGenerator() *ScannerGenerator {
	// Define scanner template
	tmplText := `
// Auto-generated scanner for {{.Kind}} resources
package generated

import (
	"context"
	"fmt"
	
	pb "github.com/jlgore/corkscrew/internal/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

type {{.Kind}}Scanner struct {
	dynamicClient dynamic.Interface
}

func New{{.Kind}}Scanner(dynamicClient dynamic.Interface) *{{.Kind}}Scanner {
	return &{{.Kind}}Scanner{
		dynamicClient: dynamicClient,
	}
}

func (s *{{.Kind}}Scanner) Scan(ctx context.Context, namespace string) ([]*pb.Resource, error) {
	gvr := schema.GroupVersionResource{
		Group:    "{{.Group}}",
		Version:  "{{.Version}}",
		Resource: "{{.Name}}",
	}
	
	var resources []*pb.Resource
	listOptions := metav1.ListOptions{}
	
	{{if .Namespaced}}
	// Namespaced resource
	var list *unstructured.UnstructuredList
	var err error
	
	if namespace != "" {
		list, err = s.dynamicClient.Resource(gvr).Namespace(namespace).List(ctx, listOptions)
	} else {
		// List from all namespaces
		list, err = s.dynamicClient.Resource(gvr).List(ctx, listOptions)
	}
	{{else}}
	// Cluster-scoped resource
	list, err := s.dynamicClient.Resource(gvr).List(ctx, listOptions)
	{{end}}
	
	if err != nil {
		return nil, fmt.Errorf("failed to list {{.Kind}}: %w", err)
	}
	
	for _, item := range list.Items {
		resource := s.convertToResource(&item)
		resources = append(resources, resource)
	}
	
	return resources, nil
}

func (s *{{.Kind}}Scanner) convertToResource(obj *unstructured.Unstructured) *pb.Resource {
	namespace := obj.GetNamespace()
	resourceID := fmt.Sprintf("default/%s/{{.Kind}}/%s", namespace, obj.GetName())
	
	resource := &pb.Resource{
		Provider:     "kubernetes",
		Service:      "{{if .Group}}{{.Group}}{{else}}core{{end}}",
		Type:         "{{.Kind}}",
		Id:           resourceID,
		Name:         obj.GetName(),
		Namespace:    namespace,
		ApiVersion:   obj.GetAPIVersion(),
		Kind:         obj.GetKind(),
		DiscoveredAt: timestamppb.Now(),
		Tags:         obj.GetLabels(),
		Metadata: map[string]string{
			"uid":               string(obj.GetUID()),
			"resourceVersion":   obj.GetResourceVersion(),
			"generation":        fmt.Sprintf("%d", obj.GetGeneration()),
			"creationTimestamp": obj.GetCreationTimestamp().Format(time.RFC3339),
		},
		Attributes: obj.Object,
	}
	
	// Add annotations as metadata
	for k, v := range obj.GetAnnotations() {
		resource.Metadata["annotation."+k] = v
	}
	
	return resource
}

// Watch monitors for changes to {{.Kind}} resources
func (s *{{.Kind}}Scanner) Watch(ctx context.Context, namespace string, eventChan chan<- *pb.ResourceEvent) error {
	gvr := schema.GroupVersionResource{
		Group:    "{{.Group}}",
		Version:  "{{.Version}}",
		Resource: "{{.Name}}",
	}
	
	watchOptions := metav1.ListOptions{
		Watch: true,
	}
	
	{{if .Namespaced}}
	var watcher watch.Interface
	var err error
	
	if namespace != "" {
		watcher, err = s.dynamicClient.Resource(gvr).Namespace(namespace).Watch(ctx, watchOptions)
	} else {
		watcher, err = s.dynamicClient.Resource(gvr).Watch(ctx, watchOptions)
	}
	{{else}}
	watcher, err := s.dynamicClient.Resource(gvr).Watch(ctx, watchOptions)
	{{end}}
	
	if err != nil {
		return fmt.Errorf("failed to watch {{.Kind}}: %w", err)
	}
	
	defer watcher.Stop()
	
	for event := range watcher.ResultChan() {
		obj, ok := event.Object.(*unstructured.Unstructured)
		if !ok {
			continue
		}
		
		resource := s.convertToResource(obj)
		resourceEvent := &pb.ResourceEvent{
			Type:     string(event.Type),
			Resource: resource,
		}
		
		select {
		case eventChan <- resourceEvent:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	
	return nil
}
`

	tmpl, err := template.New("scanner").Parse(tmplText)
	if err != nil {
		panic(fmt.Sprintf("failed to parse scanner template: %v", err))
	}

	return &ScannerGenerator{
		templates: tmpl,
	}
}

// GenerateScanner generates scanner code for a specific resource
func (g *ScannerGenerator) GenerateScanner(resource *ResourceDefinition) (string, error) {
	var buf bytes.Buffer
	err := g.templates.Execute(&buf, resource)
	if err != nil {
		return "", fmt.Errorf("failed to generate scanner for %s: %w", resource.Kind, err)
	}
	return buf.String(), nil
}

// GenerateServiceScanner generates scanner code for an entire service/API group
func (g *ScannerGenerator) GenerateServiceScanner(serviceName string) (string, error) {
	// This would generate a scanner that handles all resources in a service
	_ = `
package generated

import (
	"context"
	"fmt"
	"sync"
	
	pb "github.com/jlgore/corkscrew/internal/proto"
	"k8s.io/client-go/dynamic"
)

type {{.ServiceName}}ServiceScanner struct {
	dynamicClient dynamic.Interface
	scanners      map[string]ResourceScanner
}

type ResourceScanner interface {
	Scan(ctx context.Context, namespace string) ([]*pb.Resource, error)
}

func New{{.ServiceName}}ServiceScanner(dynamicClient dynamic.Interface) *{{.ServiceName}}ServiceScanner {
	scanner := &{{.ServiceName}}ServiceScanner{
		dynamicClient: dynamicClient,
		scanners:      make(map[string]ResourceScanner),
	}
	
	// Initialize individual resource scanners
	{{range .Resources}}
	scanner.scanners["{{.Kind}}"] = New{{.Kind}}Scanner(dynamicClient)
	{{end}}
	
	return scanner
}

func (s *{{.ServiceName}}ServiceScanner) ScanAll(ctx context.Context, namespace string) ([]*pb.Resource, error) {
	var allResources []*pb.Resource
	var mu sync.Mutex
	var wg sync.WaitGroup
	
	for resourceType, scanner := range s.scanners {
		wg.Add(1)
		go func(rt string, sc ResourceScanner) {
			defer wg.Done()
			
			resources, err := sc.Scan(ctx, namespace)
			if err != nil {
				fmt.Printf("Failed to scan %s: %v\n", rt, err)
				return
			}
			
			mu.Lock()
			allResources = append(allResources, resources...)
			mu.Unlock()
		}(resourceType, scanner)
	}
	
	wg.Wait()
	return allResources, nil
}
`

	// For now, return a simple implementation
	return fmt.Sprintf("// ServiceScanner for %s\n// Implementation would be generated here", serviceName), nil
}

// GenerateScannerRegistry generates a registry of all scanners
func (g *ScannerGenerator) GenerateScannerRegistry(resources []*ResourceDefinition) (string, error) {
	registryTemplate := `
package generated

import (
	"k8s.io/client-go/dynamic"
)

// ScannerRegistry holds all auto-generated scanners
type ScannerRegistry struct {
	scanners map[string]ResourceScanner
}

// NewScannerRegistry creates a new scanner registry
func NewScannerRegistry(dynamicClient dynamic.Interface) *ScannerRegistry {
	registry := &ScannerRegistry{
		scanners: make(map[string]ResourceScanner),
	}
	
	// Register all scanners
	{{range .Resources}}
	registry.scanners["{{.Kind}}"] = New{{.Kind}}Scanner(dynamicClient)
	{{end}}
	
	return registry
}

// GetScanner returns a scanner for the specified resource type
func (r *ScannerRegistry) GetScanner(resourceType string) (ResourceScanner, bool) {
	scanner, exists := r.scanners[resourceType]
	return scanner, exists
}

// ListResourceTypes returns all registered resource types
func (r *ScannerRegistry) ListResourceTypes() []string {
	var types []string
	for resourceType := range r.scanners {
		types = append(types, resourceType)
	}
	return types
}
`

	tmpl, err := template.New("registry").Parse(registryTemplate)
	if err != nil {
		return "", err
	}

	data := struct {
		Resources []*ResourceDefinition
	}{
		Resources: resources,
	}

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, data)
	if err != nil {
		return "", err
	}

	return buf.String(), nil
}

// GenerateCRDScanner generates a generic scanner for any CRD
func (g *ScannerGenerator) GenerateCRDScanner() (string, error) {
	crdTemplate := `
package generated

import (
	"context"
	"fmt"
	
	pb "github.com/jlgore/corkscrew/internal/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

// GenericCRDScanner can scan any CRD dynamically
type GenericCRDScanner struct {
	dynamicClient dynamic.Interface
}

func NewGenericCRDScanner(dynamicClient dynamic.Interface) *GenericCRDScanner {
	return &GenericCRDScanner{
		dynamicClient: dynamicClient,
	}
}

func (s *GenericCRDScanner) ScanCRD(ctx context.Context, gvr schema.GroupVersionResource, namespace string) ([]*pb.Resource, error) {
	var resources []*pb.Resource
	listOptions := metav1.ListOptions{}
	
	var list *unstructured.UnstructuredList
	var err error
	
	if namespace != "" {
		list, err = s.dynamicClient.Resource(gvr).Namespace(namespace).List(ctx, listOptions)
	} else {
		list, err = s.dynamicClient.Resource(gvr).List(ctx, listOptions)
	}
	
	if err != nil {
		return nil, fmt.Errorf("failed to list %s: %w", gvr.Resource, err)
	}
	
	for _, item := range list.Items {
		resource := s.convertToResource(&item, gvr)
		resources = append(resources, resource)
	}
	
	return resources, nil
}

func (s *GenericCRDScanner) convertToResource(obj *unstructured.Unstructured, gvr schema.GroupVersionResource) *pb.Resource {
	namespace := obj.GetNamespace()
	resourceID := fmt.Sprintf("default/%s/%s/%s", namespace, obj.GetKind(), obj.GetName())
	
	resource := &pb.Resource{
		Provider:     "kubernetes",
		Service:      gvr.Group,
		Type:         obj.GetKind(),
		Id:           resourceID,
		Name:         obj.GetName(),
		Namespace:    namespace,
		ApiVersion:   obj.GetAPIVersion(),
		Kind:         obj.GetKind(),
		DiscoveredAt: timestamppb.Now(),
		Tags:         obj.GetLabels(),
		Metadata: map[string]string{
			"uid":               string(obj.GetUID()),
			"resourceVersion":   obj.GetResourceVersion(),
			"generation":        fmt.Sprintf("%d", obj.GetGeneration()),
			"creationTimestamp": obj.GetCreationTimestamp().Format(time.RFC3339),
			"crd":               "true",
		},
		Attributes: obj.Object,
	}
	
	// Add annotations as metadata
	for k, v := range obj.GetAnnotations() {
		resource.Metadata["annotation."+k] = v
	}
	
	return resource
}
`

	return crdTemplate, nil
}