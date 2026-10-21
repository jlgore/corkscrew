package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	pb "github.com/jlgore/corkscrew/internal/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/tools/cache"
)

const (
	// informerResyncPeriod is how often informers replay their cache as MODIFIED events.
	informerResyncPeriod = 30 * time.Second

	// cacheSyncTimeout bounds the initial list of a watch stream. A reflector whose list is
	// forbidden by RBAC retries forever, so without a bound the stream would neither deliver
	// the initial state nor report why.
	cacheSyncTimeout = 2 * time.Minute

	// watchEventBufferSize bounds how far informer handlers may run ahead of the gRPC sender.
	// Handlers block when it is full rather than drop: a dropped ADDED event is a resource the
	// client never observes.
	watchEventBufferSize = 1000
)

// errInformerCacheStopped ends a watch stream that Stop closed while the client was connected.
var errInformerCacheStopped = errors.New("informer cache stopped")

// InformerCache manages Kubernetes informers for efficient resource watching
type InformerCache struct {
	// watchClient must not carry a per-request timeout: informers hold their watch requests
	// open for minutes, and an http.Client timeout would cut every one of them short.
	watchClient dynamic.Interface
	// syncTimeout is cacheSyncTimeout in production; tests shorten it.
	syncTimeout time.Duration

	// resourceInformers holds the informers of the streams currently running, for
	// GetCachedResources and SyncStatus.
	resourceInformers map[string]cache.SharedIndexInformer
	mu                sync.RWMutex
	// stopCh is closed by Stop to end every running stream. No stream holds mu while it
	// waits, so Stop never waits on a stream.
	stopCh chan struct{}
}

// informerSet is the informers of one stream. Each stream gets its own factory because a
// factory starts an informer at most once: informers stopped when one client disconnected
// could never serve the next.
type informerSet struct {
	factory   dynamicinformer.DynamicSharedInformerFactory
	informers map[string]cache.SharedIndexInformer
}

// NewInformerCache creates a new informer cache that watches through watchClient.
func NewInformerCache(watchClient dynamic.Interface) *InformerCache {
	return &InformerCache{
		watchClient:       watchClient,
		syncTimeout:       cacheSyncTimeout,
		resourceInformers: make(map[string]cache.SharedIndexInformer),
		stopCh:            make(chan struct{}),
	}
}

// SetupInformers builds one stream's informers for the specified resource types. A type it
// cannot resolve would never be watched, so it is reported rather than skipped; the informers
// that could be built are returned alongside the joined error.
func (ic *InformerCache) SetupInformers(resourceTypes []string) (*informerSet, error) {
	if ic.watchClient == nil {
		return nil, errors.New("informer cache has no watch client")
	}
	set := &informerSet{
		factory: dynamicinformer.NewDynamicSharedInformerFactory(
			ic.watchClient, informerResyncPeriod,
		),
		informers: make(map[string]cache.SharedIndexInformer, len(resourceTypes)),
	}
	var errs []error
	for _, resourceType := range resourceTypes {
		resourceDef, err := ic.getResourceDefinition(resourceType)
		if err != nil {
			errs = append(errs, fmt.Errorf("informer for %s: %w", resourceType, err))
			continue
		}
		gvr := schema.GroupVersionResource{
			Group:    resourceDef.Group,
			Version:  resourceDef.Version,
			Resource: resourceDef.Name,
		}
		set.informers[resourceType] = set.factory.ForResource(gvr).Informer()
	}
	return set, errors.Join(errs...)
}

// StreamInformers runs set until parent is done, Stop is called, send fails, or the caches do
// not sync within syncTimeout, and passes every event to send. The informers run on a stop
// channel derived from parent, so they end with the client's stream instead of outliving it.
// It returns only after every informer goroutine has exited and its handlers are removed.
func (ic *InformerCache) StreamInformers(
	parent context.Context, set *informerSet, send func(*pb.Resource) error,
) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	ic.mu.RLock()
	cacheStop := ic.stopCh
	ic.mu.RUnlock()

	eventChan := make(chan *pb.Resource, watchEventBufferSize)
	registrations, err := ic.addHandlers(ctx, set.informers, eventChan)
	if err != nil {
		removeHandlers(set.informers, registrations)
		return err
	}
	ic.trackInformers(set.informers, true)
	set.factory.Start(ctx.Done())

	var wg sync.WaitGroup
	wg.Go(func() {
		select {
		case <-cacheStop:
			cancel()
		case <-ctx.Done():
		}
	})
	// The sync wait runs beside the reader: the initial ADDED events can outnumber the buffer,
	// and the handlers only make progress while something receives.
	syncErr := make(chan error, 1)
	wg.Go(func() {
		syncErr <- ic.waitForSync(ctx, registrations)
	})
	err = forwardEvents(ctx, eventChan, syncErr, send)

	// Cancel first: Shutdown waits for the informers, which stop only once ctx is done, and
	// handlers blocked on a full eventChan are released by the same cancel.
	cancel()
	set.factory.Shutdown()
	removeHandlers(set.informers, registrations)
	ic.trackInformers(set.informers, false)
	wg.Wait()

	if err != nil {
		return err
	}
	if parent.Err() != nil {
		return parent.Err()
	}
	return errInformerCacheStopped
}

// forwardEvents sends events until ctx is done, send fails, or the sync wait fails. A nil
// return means ctx is done.
func forwardEvents(
	ctx context.Context,
	eventChan <-chan *pb.Resource,
	syncErr <-chan error,
	send func(*pb.Resource) error,
) error {
	for {
		select {
		case resource := <-eventChan:
			if err := send(resource); err != nil {
				return err
			}
		case err := <-syncErr:
			if err != nil {
				return err
			}
			// Synced; a nil channel is never ready, so stop selecting on it.
			syncErr = nil
		case <-ctx.Done():
			return nil
		}
	}
}

// addHandlers registers one event handler per informer. It returns the registrations made so
// far even on error, so the caller can remove them.
func (ic *InformerCache) addHandlers(
	ctx context.Context,
	informers map[string]cache.SharedIndexInformer,
	eventChan chan<- *pb.Resource,
) (map[string]cache.ResourceEventHandlerRegistration, error) {
	registrations := make(map[string]cache.ResourceEventHandlerRegistration, len(informers))
	for resourceType, informer := range informers {
		rt := resourceType
		registration, err := informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				ic.handleEvent(ctx, "ADDED", rt, obj, eventChan)
			},
			UpdateFunc: func(oldObj, newObj interface{}) {
				ic.handleEvent(ctx, "MODIFIED", rt, newObj, eventChan)
			},
			DeleteFunc: func(obj interface{}) {
				ic.handleEvent(ctx, "DELETED", rt, obj, eventChan)
			},
		})
		if err != nil {
			return registrations, fmt.Errorf("event handler for %s: %w", resourceType, err)
		}
		registrations[resourceType] = registration
	}
	return registrations, nil
}

// removeHandlers removes the handlers addHandlers registered. A removal can only fail for an
// informer that is already stopped, which delivers nothing more, so the error is ignored.
func removeHandlers(
	informers map[string]cache.SharedIndexInformer,
	registrations map[string]cache.ResourceEventHandlerRegistration,
) {
	for resourceType, registration := range registrations {
		_ = informers[resourceType].RemoveEventHandler(registration)
	}
}

// waitForSync waits until every handler has received its informer's initial list, or until
// syncTimeout passes. It waits on the handler registrations rather than the informers, so
// sync means the initial events were delivered, not only cached.
func (ic *InformerCache) waitForSync(
	ctx context.Context, registrations map[string]cache.ResourceEventHandlerRegistration,
) error {
	syncCtx, cancel := context.WithTimeout(ctx, ic.syncTimeout)
	defer cancel()
	synced := make([]cache.InformerSynced, 0, len(registrations))
	for _, registration := range registrations {
		synced = append(synced, registration.HasSynced)
	}
	if cache.WaitForCacheSync(syncCtx.Done(), synced...) {
		return nil
	}
	if ctx.Err() != nil {
		// The stream ended first; that is not a sync failure.
		return nil
	}
	return fmt.Errorf("informer caches did not sync within %s (is list permitted?)",
		ic.syncTimeout)
}

// trackInformers adds or removes a stream's informers from resourceInformers. A key another
// stream has since replaced is left alone.
func (ic *InformerCache) trackInformers(informers map[string]cache.SharedIndexInformer, add bool) {
	ic.mu.Lock()
	defer ic.mu.Unlock()
	for resourceType, informer := range informers {
		if add {
			ic.resourceInformers[resourceType] = informer
		} else if ic.resourceInformers[resourceType] == informer {
			delete(ic.resourceInformers, resourceType)
		}
	}
}

// handleEvent converts an informer event and sends it to eventChan. The send blocks, so a slow
// client slows the informer down instead of losing events; ctx releases it when the stream
// ends. Blocking here does not stall the informer itself, because client-go queues each
// handler's notifications separately.
func (ic *InformerCache) handleEvent(
	ctx context.Context, eventType, resourceType string, obj interface{},
	eventChan chan<- *pb.Resource,
) {
	unstructuredObj, ok := obj.(*unstructured.Unstructured)
	if !ok {
		// Try to handle DeletedFinalStateUnknown
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			return
		}
		unstructuredObj, ok = tombstone.Obj.(*unstructured.Unstructured)
		if !ok {
			return
		}
	}

	resource := ic.convertToResource(unstructuredObj, resourceType)
	if resource.Tags == nil {
		resource.Tags = make(map[string]string)
	}
	resource.Tags["event_type"] = eventType
	resource.Tags["event_time"] = time.Now().Format(time.RFC3339)

	select {
	case eventChan <- resource:
	case <-ctx.Done():
	}
}

// convertToResource converts an unstructured object to pb.Resource
func (ic *InformerCache) convertToResource(obj *unstructured.Unstructured, resourceType string) *pb.Resource {
	namespace := obj.GetNamespace()
	resourceID := fmt.Sprintf("default/%s/%s/%s", namespace, obj.GetKind(), obj.GetName())

	tags := obj.GetLabels()
	if tags == nil {
		tags = make(map[string]string)
	}
	tags["uid"] = string(obj.GetUID())
	tags["resourceVersion"] = obj.GetResourceVersion()
	tags["generation"] = fmt.Sprintf("%d", obj.GetGeneration())
	tags["creationTimestamp"] = obj.GetCreationTimestamp().Format(time.RFC3339)
	tags["informer_cache"] = "true"
	tags["api_version"] = obj.GetAPIVersion()
	tags["kind"] = obj.GetKind()

	resource := &pb.Resource{
		Provider:     "kubernetes",
		Service:      obj.GetAPIVersion(),
		Type:         obj.GetKind(),
		Id:           resourceID,
		Name:         obj.GetName(),
		Region:       namespace, // Using Region for namespace
		DiscoveredAt: timestamppb.Now(),
		Tags:         tags,
	}

	// Add annotations as tags
	for k, v := range obj.GetAnnotations() {
		resource.Tags["annotation."+k] = v
	}

	return resource
}

// GetCachedResources returns all cached resources of a specific type
func (ic *InformerCache) GetCachedResources(resourceType string) ([]*pb.Resource, error) {
	ic.mu.RLock()
	defer ic.mu.RUnlock()

	// Find the informer for this resource type
	var informer cache.SharedIndexInformer
	for key, inf := range ic.resourceInformers {
		if key == resourceType {
			informer = inf
			break
		}
	}

	if informer == nil {
		return nil, fmt.Errorf("no informer found for resource type %s", resourceType)
	}

	// Get all objects from the informer's store
	objects := informer.GetStore().List()

	var resources []*pb.Resource
	for _, obj := range objects {
		unstructuredObj, ok := obj.(*unstructured.Unstructured)
		if !ok {
			continue
		}
		resource := ic.convertToResource(unstructuredObj, resourceType)
		resources = append(resources, resource)
	}

	return resources, nil
}

// Stop ends every running stream. Streams hold mu only briefly and never across a sync wait,
// so Stop cannot block behind one; the streams themselves shut down asynchronously.
func (ic *InformerCache) Stop() {
	ic.mu.Lock()
	defer ic.mu.Unlock()

	close(ic.stopCh)
	ic.stopCh = make(chan struct{})
}

// getResourceDefinition gets the resource definition for a given type
// This is a simplified version - in production, this would use the discovery client
func (ic *InformerCache) getResourceDefinition(resourceType string) (*ResourceDefinition, error) {
	// Common resource mappings
	commonResources := map[string]*ResourceDefinition{
		"pods": {
			Group:      "",
			Version:    "v1",
			Kind:       "Pod",
			Name:       "pods",
			Namespaced: true,
		},
		"services": {
			Group:      "",
			Version:    "v1",
			Kind:       "Service",
			Name:       "services",
			Namespaced: true,
		},
		"deployments": {
			Group:      "apps",
			Version:    "v1",
			Kind:       "Deployment",
			Name:       "deployments",
			Namespaced: true,
		},
		"replicasets": {
			Group:      "apps",
			Version:    "v1",
			Kind:       "ReplicaSet",
			Name:       "replicasets",
			Namespaced: true,
		},
		"statefulsets": {
			Group:      "apps",
			Version:    "v1",
			Kind:       "StatefulSet",
			Name:       "statefulsets",
			Namespaced: true,
		},
		"daemonsets": {
			Group:      "apps",
			Version:    "v1",
			Kind:       "DaemonSet",
			Name:       "daemonsets",
			Namespaced: true,
		},
		"configmaps": {
			Group:      "",
			Version:    "v1",
			Kind:       "ConfigMap",
			Name:       "configmaps",
			Namespaced: true,
		},
		"secrets": {
			Group:      "",
			Version:    "v1",
			Kind:       "Secret",
			Name:       "secrets",
			Namespaced: true,
		},
		"ingresses": {
			Group:      "networking.k8s.io",
			Version:    "v1",
			Kind:       "Ingress",
			Name:       "ingresses",
			Namespaced: true,
		},
	}

	if def, ok := commonResources[resourceType]; ok {
		return def, nil
	}

	return nil, fmt.Errorf("resource type %s not found", resourceType)
}

// WatchResource watches one resource type until ctx is done, then closes the returned channel.
// Each call builds its own informer: a shared one is started at most once, so a second watch
// of the same type would end immediately.
func (ic *InformerCache) WatchResource(
	ctx context.Context, gvr schema.GroupVersionResource, namespace string,
) (<-chan *pb.Resource, error) {
	if ic.watchClient == nil {
		return nil, errors.New("informer cache has no watch client")
	}
	eventChan := make(chan *pb.Resource, 100)
	informer := dynamicinformer.NewFilteredDynamicInformer(
		ic.watchClient, gvr, namespace, informerResyncPeriod, cache.Indexers{}, nil,
	).Informer()

	emit := func(eventType string, obj interface{}) {
		if resource := ic.processWatchEvent(eventType, obj); resource != nil {
			select {
			case eventChan <- resource:
			case <-ctx.Done():
			}
		}
	}
	registration, err := informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj interface{}) { emit("ADDED", obj) },
		UpdateFunc: func(oldObj, newObj interface{}) { emit("MODIFIED", newObj) },
		DeleteFunc: func(obj interface{}) { emit("DELETED", obj) },
	})
	if err != nil {
		return nil, fmt.Errorf("event handler for %s: %w", gvr.Resource, err)
	}

	go func() {
		// Run returns after its handlers have finished, so nothing sends after the close.
		informer.Run(ctx.Done())
		// The informer is stopped, so a removal error has nothing left to stop.
		_ = informer.RemoveEventHandler(registration)
		close(eventChan)
	}()

	return eventChan, nil
}

// processWatchEvent processes a watch event and returns a resource
func (ic *InformerCache) processWatchEvent(eventType string, obj interface{}) *pb.Resource {
	unstructuredObj, ok := obj.(*unstructured.Unstructured)
	if !ok {
		// Handle tombstone
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			return nil
		}
		unstructuredObj, ok = tombstone.Obj.(*unstructured.Unstructured)
		if !ok {
			return nil
		}
	}

	resource := ic.convertToResource(unstructuredObj, unstructuredObj.GetKind())
	if resource.Tags == nil {
		resource.Tags = make(map[string]string)
	}
	resource.Tags["watch_event"] = eventType
	resource.Tags["watch_time"] = time.Now().Format(time.RFC3339)

	return resource
}

// SyncStatus returns the sync status of all informers
func (ic *InformerCache) SyncStatus() map[string]bool {
	ic.mu.RLock()
	defer ic.mu.RUnlock()

	status := make(map[string]bool)
	for key, informer := range ic.resourceInformers {
		status[key] = informer.HasSynced()
	}

	return status
}
