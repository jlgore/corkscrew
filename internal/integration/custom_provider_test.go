package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	providerapp "github.com/jlgore/corkscrew/internal/app/providers"
	scanapp "github.com/jlgore/corkscrew/internal/app/scan"
	"github.com/jlgore/corkscrew/internal/data"
	pb "github.com/jlgore/corkscrew/internal/proto"
	providerRuntime "github.com/jlgore/corkscrew/internal/provider"
	"github.com/jlgore/corkscrew/internal/scanexec"
	"github.com/jlgore/corkscrew/internal/testutil/providerfixture"
	providercatalog "github.com/jlgore/corkscrew/pkg/providers"
)

type initializationEvent struct {
	PID     int               `json:"pid"`
	Config  map[string]string `json:"config"`
	Session string            `json:"session"`
}

func TestManagedFixtureProviderOpensThroughRegistryAndRuntime(t *testing.T) {
	fixture := providerfixture.Build(t, "1.0.0")
	home := t.TempDir()
	t.Setenv("HOME", home)
	managedRoot := filepath.Join(home, ".corkscrew", "plugins")

	var warnings bytes.Buffer
	installed, err := providerRuntime.InstallCustom(
		fixture.ManifestPath, managedRoot, providercatalog.Shipped(), &warnings,
	)
	if err != nil {
		t.Fatalf("install fixture provider: %v", err)
	}
	if filepath.Base(installed.ManifestPath) != "plugin.json" {
		t.Fatalf("managed manifest = %q, want plugin.json", installed.ManifestPath)
	}

	runtime, err := providerRuntime.NewDefaultRuntime(&warnings)
	if err != nil {
		t.Fatalf("create default runtime: %v", err)
	}
	t.Cleanup(func() { _ = runtime.Close() })

	info, err := providerapp.GetInfo(context.Background(), runtime, providerfixture.Name, fixture.Config("first"))
	if err != nil {
		t.Fatalf("get fixture provider info: %v", err)
	}
	if info.Descriptor.Origin != providerRuntime.OriginCustom || !info.Descriptor.Installed {
		t.Fatalf("fixture descriptor = %#v, want installed custom provider", info.Descriptor)
	}
	if info.Runtime.Name != providerfixture.Name || info.Runtime.Version != "1.0.0" {
		t.Fatalf("fixture runtime info = %#v", info.Runtime)
	}
}

func TestManagedInstallationReplacesFixtureAndReservesOfficialNames(t *testing.T) {
	managedRoot := t.TempDir()
	first := providerfixture.Build(t, "1.0.0")
	if _, err := providerRuntime.InstallCustom(first.ManifestPath, managedRoot, providercatalog.Shipped(), nil); err != nil {
		t.Fatalf("install fixture v1: %v", err)
	}
	second := providerfixture.Build(t, "2.0.0")
	installed, err := providerRuntime.InstallCustom(second.ManifestPath, managedRoot, providercatalog.Shipped(), nil)
	if err != nil {
		t.Fatalf("replace fixture with v2: %v", err)
	}
	if installed.Manifest.Version != "2.0.0" || filepath.Base(installed.ManifestPath) != "plugin.json" {
		t.Fatalf("replacement = %#v, want normalized v2 installation", installed)
	}
	entries, err := os.ReadDir(managedRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != providerfixture.Name {
		t.Fatalf("managed root after replacement = %v, want one active installation", entries)
	}
	installations, err := providerRuntime.DiscoverInstallations([]providerRuntime.Root{{Path: managedRoot, Origin: providerRuntime.OriginCustom}})
	if err != nil {
		t.Fatal(err)
	}
	registry, err := providerRuntime.NewRegistry(providercatalog.Shipped(), installations)
	if err != nil {
		t.Fatal(err)
	}
	runtime := providerRuntime.NewRuntime(registry, providerRuntime.HashicorpLauncher{}, &bytes.Buffer{})
	t.Cleanup(func() { _ = runtime.Close() })
	info, err := providerapp.GetInfo(context.Background(), runtime, providerfixture.Name, second.Config("replacement"))
	if err != nil || info.Runtime.Version != "2.0.0" {
		t.Fatalf("replacement runtime version = %#v, %v; want 2.0.0", info.Runtime, err)
	}

	reserved := providerfixture.Build(t, "1.0.0")
	manifest, err := os.ReadFile(reserved.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	manifest = []byte(strings.Replace(string(manifest), "name: fixture-cloud", "name: aws", 1))
	if err := os.WriteFile(reserved.ManifestPath, manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := providerRuntime.InstallCustom(reserved.ManifestPath, managedRoot, providercatalog.Shipped(), nil); err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("install official-name fixture error = %v, want reserved-name rejection", err)
	}
}

func TestRuntimeReusesConfigurationsAndTerminatesEveryFixtureProcess(t *testing.T) {
	fixture := providerfixture.Build(t, "1.0.0")
	managedRoot := filepath.Join(t.TempDir(), ".corkscrew", "plugins")
	if _, err := providerRuntime.InstallCustom(fixture.ManifestPath, managedRoot, providercatalog.Shipped(), nil); err != nil {
		t.Fatal(err)
	}
	installations, err := providerRuntime.DiscoverInstallations([]providerRuntime.Root{{Path: managedRoot, Origin: providerRuntime.OriginCustom}})
	if err != nil {
		t.Fatal(err)
	}
	registry, err := providerRuntime.NewRegistry(providercatalog.Shipped(), installations)
	if err != nil {
		t.Fatal(err)
	}
	runtime := providerRuntime.NewRuntime(registry, providerRuntime.HashicorpLauncher{}, &bytes.Buffer{})
	t.Cleanup(func() { _ = runtime.Close() })

	ctx := context.Background()
	firstConfig := fixture.Config("first")
	first, err := runtime.Open(ctx, providerfixture.Name, firstConfig)
	if err != nil {
		t.Fatal(err)
	}
	reused, err := runtime.Open(ctx, providerfixture.Name, map[string]string{"state_dir": fixture.StateDirectory, "session": "first", "fail_scope": "scope-fail"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := runtime.Open(ctx, providerfixture.Name, fixture.Config("second"))
	if err != nil {
		t.Fatal(err)
	}
	if first != reused || first == second {
		t.Fatalf("sessions: first=%p reused=%p second=%p", first, reused, second)
	}
	if err := first.Require(providerRuntime.CapabilityBatchScan); err != nil {
		t.Fatalf("require batch_scan: %v", err)
	}
	if err := first.Require(providerRuntime.CapabilityStreamScan); err == nil {
		t.Fatal("require undeclared stream_scan = nil, want capability error")
	}

	events := readInitializationEvents(t, filepath.Join(fixture.StateDirectory, "initializations.jsonl"))
	if len(events) != 2 || events[0].Session == events[1].Session {
		t.Fatalf("initialization events = %#v, want one per distinct configuration", events)
	}
	if err := runtime.Close(); err != nil {
		t.Fatalf("close runtime: %v", err)
	}
	for _, event := range events {
		waitForProcessExit(t, event.PID)
	}
}

func TestFixtureProviderApplicationWorkflows(t *testing.T) {
	fixture, runtime := installedFixtureRuntime(t)
	ctx := context.Background()
	config := fixture.Config("workflows")

	discovery, err := providerapp.DiscoverServices(ctx, runtime, providerfixture.Name, config, &pb.DiscoverServicesRequest{})
	if err != nil {
		t.Fatalf("discover fixture services: %v", err)
	}
	if len(discovery.Services) != 1 || discovery.Services[0].Name != "widgets" || len(discovery.Services[0].ResourceTypes) != 1 {
		t.Fatalf("fixture discovery = %#v", discovery)
	}

	listed, err := providerapp.ListResources(ctx, runtime, providerfixture.Name, config, &pb.ListResourcesRequest{
		Service: "widgets", ResourceType: "widget", Region: "scope-a",
	})
	if err != nil {
		t.Fatalf("list fixture resources: %v", err)
	}
	if len(listed.Resources) != 2 || listed.Resources[0].Id != "fixture://shared" || listed.Resources[1].Id != "fixture://scope-a/widget" {
		t.Fatalf("fixture resource list = %#v", listed.Resources)
	}

	described, err := providerapp.DescribeResource(ctx, runtime, providerfixture.Name, config, &pb.DescribeResourceRequest{
		ResourceRef: listed.Resources[1], IncludeRelationships: true, IncludeTags: true,
	})
	if err != nil {
		t.Fatalf("describe fixture resource: %v", err)
	}
	if described.Resource == nil || described.Resource.Id != "fixture://scope-a/widget" || len(described.Resource.Relationships) != 1 {
		t.Fatalf("fixture description = %#v", described)
	}

	schemas, err := providerapp.GetSchemas(ctx, runtime, providerfixture.Name, config, &pb.GetSchemasRequest{Services: []string{"widgets"}, Format: "json"})
	if err != nil {
		t.Fatalf("get fixture schemas: %v", err)
	}
	if len(schemas.Schemas) != 1 || schemas.Schemas[0].Name != "fixture_widgets" || schemas.Schemas[0].ResourceType != "widget" {
		t.Fatalf("fixture schemas = %#v", schemas.Schemas)
	}
}

func TestFixtureProviderMultiScopeScanAggregatesAndDeduplicates(t *testing.T) {
	fixture, runtime := installedFixtureRuntime(t)
	session, err := runtime.Open(context.Background(), providerfixture.Name, fixture.Config("successful-scan"))
	if err != nil {
		t.Fatal(err)
	}
	var events []scanexec.Event
	outcome, err := scanexec.Execute(context.Background(), session.Provider(), scanexec.Plan{
		Provider: providerfixture.Name, Scopes: []string{"scope-a", "scope-b"},
		Services: []string{"widgets"}, IncludeRelationships: true, MaxConcurrency: 2,
	}, func(event scanexec.Event) { events = append(events, event) })
	if err != nil {
		t.Fatalf("execute successful fixture scan: %v", err)
	}
	if outcome.Status != scanexec.StatusComplete || len(outcome.Resources) != 3 || outcome.Stats.TotalResources != 3 {
		t.Fatalf("successful fixture outcome = %#v", outcome)
	}
	assertScopeEvents(t, events, map[scanexec.EventKind]int{
		scanexec.EventScopeStarted: 2, scanexec.EventScopeCompleted: 2,
	})
}

func TestFixtureProviderPartialScopeFailurePreservesResultsAndReturnsTypedError(t *testing.T) {
	fixture, runtime := installedFixtureRuntime(t)
	session, err := runtime.Open(context.Background(), providerfixture.Name, fixture.Config("partial-scan"))
	if err != nil {
		t.Fatal(err)
	}
	var events []scanexec.Event
	outcome, err := scanexec.Execute(context.Background(), session.Provider(), scanexec.Plan{
		Provider: providerfixture.Name, Scopes: []string{"scope-a", "scope-fail"},
		Services: []string{"widgets"}, IncludeRelationships: true, MaxConcurrency: 2,
	}, func(event scanexec.Event) { events = append(events, event) })
	var partial *scanexec.PartialError
	if !errors.As(err, &partial) || len(partial.FailedScopes) != 1 || partial.FailedScopes[0] != "scope-fail" {
		t.Fatalf("partial fixture error = %v (%#v)", err, partial)
	}
	if outcome.Status != scanexec.StatusPartial || len(outcome.Resources) != 2 {
		t.Fatalf("partial fixture outcome = %#v", outcome)
	}
	assertScopeEvents(t, events, map[scanexec.EventKind]int{
		scanexec.EventScopeStarted: 2, scanexec.EventScopeCompleted: 1, scanexec.EventScopeFailed: 1,
	})
}

func TestFullFixtureScanPersistsThroughNormalizedDataAccess(t *testing.T) {
	configPath := installedDefaultFixture(t, filepath.Join(t.TempDir(), "configured.duckdb"))
	databasePath := filepath.Join(t.TempDir(), "fixture.duckdb")

	outcome, err := scanapp.Run(context.Background(), scanapp.Request{
		Provider: providerfixture.Name, Regions: "scope-a,scope-b",
		Services: "widgets", IncludeRelationships: true, MaxConcurrency: 2,
		ConfigPath: configPath, DatabasePath: databasePath, DatabaseExplicit: true,
	}, scanapp.Dependencies{})
	if err != nil {
		t.Fatalf("run full fixture scan: %v", err)
	}
	if len(outcome.Resources) != 3 || len(outcome.Events) != 4 || !hasScanWarning(outcome.Warnings, scanapp.WarningRuntime) {
		t.Fatalf("application outcome = %#v", outcome)
	}

	session, err := data.OpenSession(context.Background(), databasePath)
	if err != nil {
		t.Fatalf("open normalized data session: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	assertReadOnlyCount(t, session, "SELECT COUNT(*) FROM custom_provider_resources WHERE provider = ?", providerfixture.Name, 3)
	assertReadOnlyCount(t, session, "SELECT COUNT(*) FROM all_cloud_resources WHERE provider = ?", providerfixture.Name, 3)
	count, err := session.Inventory().Count(context.Background(), data.InventoryFilter{Provider: providerfixture.Name})
	if err != nil || count != 3 {
		t.Fatalf("normalized fixture inventory count = %d, %v; want 3", count, err)
	}
	resources, err := session.Inventory().List(context.Background(), data.InventoryFilter{Provider: providerfixture.Name}, data.Page{Limit: 10})
	if err != nil || len(resources) != 3 || resources[0].Provider != providerfixture.Name {
		t.Fatalf("normalized fixture inventory = %#v, %v", resources, err)
	}
	relationships, err := session.Relationships().List(context.Background(), data.RelationshipFilter{
		Provider: providerfixture.Name, Type: "uses",
	}, data.Page{Limit: 10})
	if err != nil || len(relationships) != 2 {
		t.Fatalf("normalized fixture relationships = %#v, %v", relationships, err)
	}
	for _, relationship := range relationships {
		if relationship.ToID != "fixture://shared" || relationship.Direction != "outbound" {
			t.Fatalf("fixture relationship = %#v", relationship)
		}
	}
}

func TestFullFixtureScanAppliesPersistenceFailurePolicy(t *testing.T) {
	invalidDatabaseTarget := t.TempDir()
	configPath := installedDefaultFixture(t, invalidDatabaseTarget)
	base := scanapp.Request{
		Provider: providerfixture.Name, Regions: "scope-a", Services: "widgets",
		IncludeRelationships: true, MaxConcurrency: 1, ConfigPath: configPath,
	}

	explicit := base
	explicit.DatabasePath = invalidDatabaseTarget
	explicit.DatabaseExplicit = true
	_, err := scanapp.Run(context.Background(), explicit, scanapp.Dependencies{})
	if err == nil || !strings.Contains(err.Error(), "explicitly requested database") {
		t.Fatalf("explicit persistence error = %v, want fatal explicit-database error", err)
	}

	outcome, err := scanapp.Run(context.Background(), base, scanapp.Dependencies{})
	if err != nil {
		t.Fatalf("optional persistence failure returned error: %v", err)
	}
	if !hasScanWarning(outcome.Warnings, scanapp.WarningPersistence) {
		t.Fatalf("optional persistence warnings = %#v, want structured persistence warning", outcome.Warnings)
	}
}

func TestFullFixtureApplicationPersistsPartialOutcomeAndReturnsTypedError(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "partial.duckdb")
	configPath := installedDefaultFixture(t, databasePath)
	outcome, err := scanapp.Run(context.Background(), scanapp.Request{
		Provider: providerfixture.Name, Regions: "scope-a,scope-fail", Services: "widgets",
		IncludeRelationships: true, MaxConcurrency: 2, ConfigPath: configPath,
		DatabasePath: databasePath, DatabaseExplicit: true,
	}, scanapp.Dependencies{})
	var partial *scanexec.PartialError
	if !errors.As(err, &partial) {
		t.Fatalf("application error = %v, want *scanexec.PartialError", err)
	}
	if outcome.Status != scanexec.StatusPartial || len(outcome.Resources) != 2 || !outcome.Persisted {
		t.Fatalf("partial application outcome = %#v", outcome)
	}
	assertScopeEvents(t, outcome.Events, map[scanexec.EventKind]int{
		scanexec.EventScopeStarted: 2, scanexec.EventScopeCompleted: 1, scanexec.EventScopeFailed: 1,
	})

	session, openErr := data.OpenSession(context.Background(), databasePath)
	if openErr != nil {
		t.Fatalf("open partial database: %v", openErr)
	}
	t.Cleanup(func() { _ = session.Close() })
	assertReadOnlyCount(t, session, "SELECT COUNT(*) FROM all_cloud_resources WHERE provider = ?", providerfixture.Name, 2)
	assertReadOnlyCount(t, session, "SELECT COUNT(*) FROM scan_metadata WHERE provider = ? AND status = 'partial'", providerfixture.Name, 1)
}

func hasScanWarning(warnings []scanapp.Warning, kind scanapp.WarningKind) bool {
	for _, warning := range warnings {
		if warning.Kind == kind {
			return true
		}
	}
	return false
}

func installedDefaultFixture(t *testing.T, configuredDatabasePath string) string {
	t.Helper()
	fixture := providerfixture.Build(t, "1.0.0")
	home := t.TempDir()
	t.Setenv("HOME", home)
	managedRoot := filepath.Join(home, ".corkscrew", "plugins")
	if _, err := providerRuntime.InstallCustom(fixture.ManifestPath, managedRoot, providercatalog.Shipped(), nil); err != nil {
		t.Fatalf("install default fixture: %v", err)
	}
	configPath := filepath.Join(t.TempDir(), "corkscrew.yaml")
	config := fmt.Sprintf(`version: "2.0"
providers:
  fixture-cloud:
    enabled: true
    regions: [scope-a, scope-b]
    services: [widgets]
    config:
      session: full-scan
      state_dir: %q
      fail_scope: scope-fail
database:
  path: %q
output:
  default_format: json
`, fixture.StateDirectory, configuredDatabasePath)
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatalf("write fixture config: %v", err)
	}
	return configPath
}

func assertReadOnlyCount(t *testing.T, session *data.Session, statement string, provider string, want int64) {
	t.Helper()
	result, err := session.ReadOnly(context.Background(), statement, provider)
	if err != nil {
		t.Fatalf("read normalized fixture count: %v", err)
	}
	if len(result.Rows) != 1 || len(result.Rows[0]) != 1 {
		t.Fatalf("fixture count rows = %#v", result.Rows)
	}
	got, ok := result.Rows[0][0].(int64)
	if !ok || got != want {
		t.Fatalf("fixture count = %#v, want %d", result.Rows[0][0], want)
	}
}

func captureOutput(t *testing.T, run func() error) (string, error) {
	t.Helper()
	oldStdout := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = writer
	done := make(chan string, 1)
	go func() {
		captured, _ := io.ReadAll(reader)
		done <- string(captured)
	}()
	runErr := run()
	_ = writer.Close()
	os.Stdout = oldStdout
	output := <-done
	_ = reader.Close()
	return output, runErr
}

func assertScopeEvents(t *testing.T, events []scanexec.Event, expected map[scanexec.EventKind]int) {
	t.Helper()
	counts := make(map[scanexec.EventKind]int)
	for _, event := range events {
		if event.Provider != providerfixture.Name || event.Scope == "" || event.Total != 2 {
			t.Fatalf("malformed structured scope event: %#v", event)
		}
		counts[event.Kind]++
	}
	for kind, count := range expected {
		if counts[kind] != count {
			t.Fatalf("scope event counts = %v, want %s=%d", counts, kind, count)
		}
	}
}

func installedFixtureRuntime(t *testing.T) (providerfixture.Fixture, *providerRuntime.Runtime) {
	t.Helper()
	fixture := providerfixture.Build(t, "1.0.0")
	managedRoot := filepath.Join(t.TempDir(), "plugins")
	if _, err := providerRuntime.InstallCustom(fixture.ManifestPath, managedRoot, providercatalog.Shipped(), nil); err != nil {
		t.Fatalf("install fixture: %v", err)
	}
	installations, err := providerRuntime.DiscoverInstallations([]providerRuntime.Root{{Path: managedRoot, Origin: providerRuntime.OriginCustom}})
	if err != nil {
		t.Fatalf("discover fixture installation: %v", err)
	}
	registry, err := providerRuntime.NewRegistry(providercatalog.Shipped(), installations)
	if err != nil {
		t.Fatalf("register fixture installation: %v", err)
	}
	runtime := providerRuntime.NewRuntime(registry, providerRuntime.HashicorpLauncher{}, &bytes.Buffer{})
	t.Cleanup(func() { _ = runtime.Close() })
	return fixture, runtime
}

func readInitializationEvents(t *testing.T, path string) []initializationEvent {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture initialization events: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	events := make([]initializationEvent, 0, len(lines))
	for _, line := range lines {
		var event initializationEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("decode fixture initialization event: %v", err)
		}
		events = append(events, event)
	}
	return events
}

func waitForProcessExit(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		err := syscall.Kill(pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("fixture provider process %d still exists after runtime shutdown", pid)
}
