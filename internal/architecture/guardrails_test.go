package architecture

import (
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
		filepath.Join(root, "pkg", "smartscan"),
	}
	for _, directory := range directories {
		for _, filename := range goFiles(t, directory) {
			if matches := ddlLiterals(t, filename); len(matches) > 0 {
				relative, _ := filepath.Rel(root, filename)
				t.Errorf("%s contains persistent DDL %q; adapters must call the schema lifecycle", relative, matches)
			}
		}
	}

	// Provider plugins are deliberately outside this rule. GetSchemas may return
	// discovery-schema metadata for official or custom providers; whether older
	// plugins still execute that SQL is tracked as separate provider cleanup.
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
