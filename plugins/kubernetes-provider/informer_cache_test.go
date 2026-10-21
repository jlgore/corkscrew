package main

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	pb "github.com/jlgore/corkscrew/internal/proto"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"
)

const (
	// informerTestPods exceeds watchEventBufferSize several times over, so handlers must
	// block on the reader instead of overflowing the buffer.
	informerTestPods = 5000
	// informerTestDeadline bounds every wait in these tests; reaching it is a failure.
	informerTestDeadline = 30 * time.Second
)

var podsGVR = schema.GroupVersionResource{Version: "v1", Resource: "pods"}

func newFakeWatchClient(count int) *dynamicfake.FakeDynamicClient {
	objects := make([]k8sruntime.Object, 0, count)
	for i := 0; i < count; i++ {
		pod := &unstructured.Unstructured{}
		pod.SetAPIVersion("v1")
		pod.SetKind("Pod")
		pod.SetNamespace("default")
		pod.SetName(fmt.Sprintf("pod-%d", i))
		objects = append(objects, pod)
	}
	return dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		k8sruntime.NewScheme(),
		map[schema.GroupVersionResource]string{podsGVR: "PodList"},
		objects...,
	)
}

// streamPods runs StreamInformers for pods in a goroutine and returns its result channel.
func streamPods(
	t *testing.T, ctx context.Context, ic *InformerCache, send func(*pb.Resource) error,
) <-chan error {
	t.Helper()
	set, err := ic.SetupInformers([]string{"pods"})
	if err != nil {
		t.Fatalf("SetupInformers: %v", err)
	}
	result := make(chan error, 1)
	go func() {
		result <- ic.StreamInformers(ctx, set, send)
	}()
	return result
}

func waitResult(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(informerTestDeadline):
		t.Fatal("StreamInformers did not return")
		return nil
	}
}

// waitGoroutines waits, bounded, for the goroutine count to fall back to at most limit.
// goleak is not a dependency of this module, so the count stands in for it: every goroutine
// the stream started must have exited, and unrelated runtime goroutines rarely appear in
// between, which the caller's tolerance absorbs.
func waitGoroutines(t *testing.T, limit int) {
	t.Helper()
	deadline := time.Now().Add(informerTestDeadline)
	for runtime.NumGoroutine() > limit {
		if time.Now().After(deadline) {
			buf := make([]byte, 1<<20)
			n := runtime.Stack(buf, true)
			t.Fatalf("goroutines: %d > %d after the stream ended\n%s",
				runtime.NumGoroutine(), limit, buf[:n])
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestStreamInformersDeliversEveryAddedEvent(t *testing.T) {
	ic := NewInformerCache(newFakeWatchClient(informerTestPods))
	baseline := runtime.NumGoroutine()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var mu sync.Mutex
	seen := make(map[string]bool, informerTestPods)
	send := func(resource *pb.Resource) error {
		if resource.Tags["event_type"] != "ADDED" {
			return nil
		}
		mu.Lock()
		defer mu.Unlock()
		if len(seen) == 0 {
			// Stall the reader so the handlers fill eventChan; a dropping send would lose
			// the overflow here.
			time.Sleep(100 * time.Millisecond)
		}
		seen[resource.Name] = true
		if len(seen) == informerTestPods {
			cancel()
		}
		return nil
	}

	err := waitResult(t, streamPods(t, ctx, ic, send))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("StreamInformers: %v, want context.Canceled", err)
	}
	mu.Lock()
	got := len(seen)
	mu.Unlock()
	if got != informerTestPods {
		t.Fatalf("ADDED events for %d pods, want %d", got, informerTestPods)
	}
	if status := ic.SyncStatus(); len(status) != 0 {
		t.Fatalf("informers still tracked after the stream ended: %v", status)
	}
	// Tolerance 2: the race detector and timers may hold a transient goroutine.
	waitGoroutines(t, baseline+2)
}

// blockList makes every pods list block until release closes, so the cache never syncs.
// entered is closed when the first list arrives.
func blockList(client *dynamicfake.FakeDynamicClient) (entered, release chan struct{}) {
	entered = make(chan struct{})
	release = make(chan struct{})
	var once sync.Once
	client.PrependReactor("list", "pods",
		func(k8stesting.Action) (bool, k8sruntime.Object, error) {
			once.Do(func() { close(entered) })
			<-release
			return true, nil, errors.New("list released")
		})
	return entered, release
}

func TestStreamInformersSyncTimeout(t *testing.T) {
	client := newFakeWatchClient(0)
	_, release := blockList(client)
	defer close(release)
	ic := NewInformerCache(client)
	ic.syncTimeout = 200 * time.Millisecond

	send := func(*pb.Resource) error { return nil }
	err := waitResult(t, streamPods(t, context.Background(), ic, send))
	if err == nil || !strings.Contains(err.Error(), "did not sync") {
		t.Fatalf("StreamInformers: %v, want a sync timeout", err)
	}
}

func TestStopDuringBlockedSync(t *testing.T) {
	client := newFakeWatchClient(0)
	entered, release := blockList(client)
	defer close(release)
	ic := NewInformerCache(client)

	send := func(*pb.Resource) error { return nil }
	result := streamPods(t, context.Background(), ic, send)
	select {
	case <-entered:
	case <-time.After(informerTestDeadline):
		t.Fatal("informer never listed")
	}

	stopped := make(chan struct{})
	go func() {
		ic.Stop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(informerTestDeadline):
		t.Fatal("Stop deadlocked behind a blocked cache sync")
	}
	if err := waitResult(t, result); !errors.Is(err, errInformerCacheStopped) {
		t.Fatalf("StreamInformers: %v, want errInformerCacheStopped", err)
	}
}

func TestWatchResourceWithoutClient(t *testing.T) {
	ic := NewInformerCache(nil)
	if _, err := ic.WatchResource(context.Background(), podsGVR, ""); err == nil {
		t.Fatal("WatchResource without a watch client succeeded")
	}
	if _, err := ic.SetupInformers([]string{"pods"}); err == nil {
		t.Fatal("SetupInformers without a watch client succeeded")
	}
}
