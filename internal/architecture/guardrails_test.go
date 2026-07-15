package architecture

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
)

var persistentDDL = regexp.MustCompile(`(?i)\b(?:CREATE\s+(?:OR\s+REPLACE\s+)?(?:TABLE|VIEW|INDEX)|ALTER\s+TABLE|DROP\s+(?:TABLE|VIEW|INDEX))\b`)

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate architecture test")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
}

func goFiles(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != root && (strings.HasPrefix(entry.Name(), ".") || entry.Name() == "vendor") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(entry.Name(), ".go") && !strings.HasSuffix(entry.Name(), "_test.go") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	sort.Strings(files)
	return files
}

func ddlLiterals(t *testing.T, filename string) []string {
	t.Helper()
	parsed, err := parser.ParseFile(token.NewFileSet(), filename, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", filename, err)
	}
	var matches []string
	ast.Inspect(parsed, func(node ast.Node) bool {
		literal, ok := node.(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			return true
		}
		value, err := strconv.Unquote(literal.Value)
		if err == nil && persistentDDL.MatchString(value) {
			matches = append(matches, persistentDDL.FindString(value))
		}
		return true
	})
	return matches
}

func TestCoreStorageDDLHasExplicitOwners(t *testing.T) {
	root := repositoryRoot(t)
	allowed := map[string]bool{
		"internal/db/schema_lifecycle.go": true,
		"internal/db/unified_schema.go":   true,
		"internal/db/network_schema.go":   true,
		"internal/db/security_schema.go":  true,
	}

	for _, filename := range goFiles(t, filepath.Join(root, "internal", "db")) {
		relative, err := filepath.Rel(root, filename)
		if err != nil {
			t.Fatalf("relative path for %s: %v", filename, err)
		}
		if matches := ddlLiterals(t, filename); len(matches) > 0 && !allowed[filepath.ToSlash(relative)] {
			t.Errorf("%s contains persistent DDL %q; add storage changes to the versioned schema lifecycle", relative, matches)
		}
	}
}

func TestCommandAndPresentationAdaptersDoNotOwnPersistentDDL(t *testing.T) {
	root := repositoryRoot(t)
	directories := []string{
		filepath.Join(root, "cmd", "corkscrew"),
		filepath.Join(root, "internal", "server"),
		filepath.Join(root, "internal", "tui"),
	}
	for _, directory := range directories {
		for _, filename := range goFiles(t, directory) {
			if matches := ddlLiterals(t, filename); len(matches) > 0 {
				relative, _ := filepath.Rel(root, filename)
				t.Errorf("%s contains persistent DDL %q; adapters must call the schema lifecycle", relative, matches)
			}
		}
	}

	// Provider plugins may still return discovery-schema metadata from GetSchemas.
	// They must not execute that DDL or open storage themselves.
}

func TestProviderPluginsDoNotOpenStorage(t *testing.T) {
	root := repositoryRoot(t)
	pluginsRoot := filepath.Join(root, "plugins")
	for _, filename := range goFiles(t, pluginsRoot) {
		parsed, err := parser.ParseFile(token.NewFileSet(), filename, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, spec := range parsed.Imports {
			path, _ := strconv.Unquote(spec.Path.Value)
			// database/sql and duckdb-go are direct storage handles; changestore
			// was the transitive path by which plugins opened their own DuckDB.
			if path == "database/sql" || strings.Contains(path, "duckdb-go") || strings.Contains(path, "internal/changestore") {
				relative, _ := filepath.Rel(root, filename)
				t.Errorf("%s imports storage implementation %s; provider plugins return data to the scan application for persistence", filepath.ToSlash(relative), path)
			}
		}
	}
}

func TestChangeTrackingStorageStaysRemoved(t *testing.T) {
	root := repositoryRoot(t)
	matches, err := filepath.Glob(filepath.Join(root, "internal", "changestore", "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) > 0 {
		t.Errorf("internal/changestore returned: %v; change tracking must flow through the scan application, not a plugin-owned DuckDB", matches)
	}
}

func TestLegacyScanAndPluginModulesStayRemoved(t *testing.T) {
	root := repositoryRoot(t)
	for _, directory := range []string{"pkg/smartscan", "pkg/plugins"} {
		matches, err := filepath.Glob(filepath.Join(root, directory, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		if len(matches) > 0 {
			t.Errorf("legacy module %s contains Go files: %v", directory, matches)
		}
	}
}

func TestLegacyGraphLoaderStaysRemoved(t *testing.T) {
	root := repositoryRoot(t)
	for _, relative := range []string{"internal/db/graph_loader.go", "internal/data/graph_loader.go"} {
		_, err := os.Stat(filepath.Join(root, relative))
		if err == nil {
			t.Errorf("legacy graph loader returned at %s; use the authoritative data session", relative)
		} else if !os.IsNotExist(err) {
			t.Fatalf("inspect %s: %v", relative, err)
		}
	}

	for _, filename := range goFiles(t, root) {
		content, err := os.ReadFile(filename)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(content), "GraphLoader") {
			relative, _ := filepath.Rel(root, filename)
			t.Errorf("%s depends on the retired GraphLoader; use internal/data", relative)
		}
	}
}

func TestHighLevelStorageOpeningFlowsThroughDataSession(t *testing.T) {
	root := repositoryRoot(t)
	for _, filename := range goFiles(t, root) {
		relative, _ := filepath.Rel(root, filename)
		relativePath := filepath.ToSlash(relative)
		if strings.HasPrefix(relativePath, "internal/db/") || strings.HasPrefix(relativePath, "internal/data/") {
			continue
		}
		content, err := os.ReadFile(filename)
		if err != nil {
			t.Fatal(err)
		}
		if bytes := string(content); strings.Contains(bytes, "OpenDuckDB(") || strings.Contains(bytes, "WrapSession(") || strings.Contains(bytes, "InitializeUnifiedDatabase(") || strings.Contains(bytes, "InitializeUnifiedDatabaseWithOptions(") {
			t.Errorf("%s opens storage outside internal/data", relativePath)
		}
	}
}

func TestScanApplicationIsTheOnlyScanExecutorCaller(t *testing.T) {
	root := repositoryRoot(t)
	for _, filename := range goFiles(t, root) {
		relative, _ := filepath.Rel(root, filename)
		relativePath := filepath.ToSlash(relative)
		content, err := os.ReadFile(filename)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(content), "scanexec.Execute(") && relativePath != "internal/app/scan/scan.go" {
			t.Errorf("%s invokes scan executor outside the scan application", relativePath)
		}
	}
}

func TestPluginCLIUsesProviderManagementApplication(t *testing.T) {
	filename := filepath.Join(repositoryRoot(t), "cmd", "corkscrew", "plugin.go")
	parsed, err := parser.ParseFile(token.NewFileSet(), filename, nil, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	foundApplication := false
	for _, spec := range parsed.Imports {
		path, _ := strconv.Unquote(spec.Path.Value)
		if path == "github.com/jlgore/corkscrew/internal/app/providers" {
			foundApplication = true
		}
		if path == "github.com/jlgore/corkscrew/internal/provider" || path == "github.com/jlgore/corkscrew/pkg/plugins" {
			t.Errorf("plugin CLI imports provider implementation %s", path)
		}
	}
	if !foundApplication {
		t.Error("plugin CLI does not use provider management application")
	}
}

func TestAdaptersDoNotOwnProviderRuntimeLifecycle(t *testing.T) {
	root := repositoryRoot(t)
	for _, directory := range []string{
		filepath.Join(root, "cmd", "corkscrew"),
		filepath.Join(root, "internal", "server"),
	} {
		for _, filename := range goFiles(t, directory) {
			parsed, err := parser.ParseFile(token.NewFileSet(), filename, nil, parser.ImportsOnly)
			if err != nil {
				t.Fatal(err)
			}
			for _, spec := range parsed.Imports {
				path, _ := strconv.Unquote(spec.Path.Value)
				if path == "github.com/jlgore/corkscrew/internal/provider" {
					relative, _ := filepath.Rel(root, filename)
					t.Errorf("%s imports provider runtime implementation; use internal/app/providers", relative)
				}
			}
		}
	}
}

func TestScanCLIAdapterDependsOnApplicationWorkflow(t *testing.T) {
	filename := filepath.Join(repositoryRoot(t), "cmd", "corkscrew", "scan.go")
	parsed, err := parser.ParseFile(token.NewFileSet(), filename, nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse scan adapter: %v", err)
	}

	var imports []string
	for _, spec := range parsed.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			t.Fatalf("parse scan import: %v", err)
		}
		imports = append(imports, path)
	}
	want := "github.com/jlgore/corkscrew/internal/app/scan"
	foundWorkflow := false
	for _, path := range imports {
		if path == want {
			foundWorkflow = true
		}
		if path == "github.com/jlgore/corkscrew/pkg/smartscan" ||
			path == "github.com/jlgore/corkscrew/internal/db" ||
			path == "github.com/jlgore/corkscrew/internal/client" {
			t.Errorf("scan CLI adapter imports implementation package %s", path)
		}
	}
	if !foundWorkflow {
		t.Errorf("scan CLI adapter imports %v, want %s", imports, want)
	}
}

func TestDataConsumersUseNormalizedResourceAndRelationshipContracts(t *testing.T) {
	root := repositoryRoot(t)
	directories := []string{
		filepath.Join(root, "cmd", "corkscrew"),
		filepath.Join(root, "internal", "server"),
		filepath.Join(root, "internal", "tui"),
		filepath.Join(root, "pkg", "graphquery"),
	}
	providerTables := regexp.MustCompile(`\b(?:aws|azure|gcp|kubernetes|github|cloudflare)_(?:resources|relationships)\b`)
	for _, directory := range directories {
		for _, filename := range goFiles(t, directory) {
			content, err := os.ReadFile(filename)
			if err != nil {
				t.Fatal(err)
			}
			if match := providerTables.Find(content); match != nil {
				relative, _ := filepath.Rel(root, filename)
				t.Errorf("%s embeds provider table %q; consume internal/data normalized access", relative, match)
			}
		}
	}
}

func TestGraphQueriesDoNotLaunchDuckDBSubprocess(t *testing.T) {
	root := repositoryRoot(t)
	for _, filename := range goFiles(t, filepath.Join(root, "pkg", "graphquery")) {
		parsed, err := parser.ParseFile(token.NewFileSet(), filename, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, spec := range parsed.Imports {
			path, _ := strconv.Unquote(spec.Path.Value)
			if path == "os/exec" {
				relative, _ := filepath.Rel(root, filename)
				t.Errorf("%s launches a subprocess; graph queries must load through internal/data", relative)
			}
		}
	}
}

func TestPackagedGraphExtensionTargetsEmbeddedDuckDBVersion(t *testing.T) {
	root := repositoryRoot(t)
	module, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	extensionMakefile, err := os.ReadFile(filepath.Join(root, "extensions", "corkscrew-graph", "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	driverVersion := regexp.MustCompile(`duckdb-go/v2 v2\.([0-9]{5})\.0`).FindSubmatch(module)
	extensionVersion := regexp.MustCompile(`DUCKDB_EXT_VERSION \?= v([0-9]+\.[0-9]+\.[0-9]+)`).FindSubmatch(extensionMakefile)
	if len(driverVersion) != 2 || len(extensionVersion) != 2 {
		t.Fatalf("could not parse DuckDB driver/extension versions")
	}
	encoded := string(driverVersion[1])
	want := fmt.Sprintf("%s.%d.%d", encoded[:1], parseVersionPart(t, encoded[1:3]), parseVersionPart(t, encoded[3:5]))
	if got := string(extensionVersion[1]); got != want {
		t.Fatalf("graph extension targets DuckDB %s, embedded driver requires %s", got, want)
	}
}

func parseVersionPart(t *testing.T, value string) int {
	t.Helper()
	parsed, err := strconv.Atoi(value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}
