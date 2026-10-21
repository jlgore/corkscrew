package architecture

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// pageTokenReference matches the continuation-token names provider SDKs use. A `for {}` loop that
// mentions one is a page walk.
var pageTokenReference = regexp.MustCompile(
	`\b(NextToken|NextPageToken|EndCursor|NextPage|NextLink|nextLink|SkipToken|Continue|pageToken|continueToken)\b`)

// unguardedPageLoops returns "file:line function" for each `for {}` page walk in src whose
// function never references the paging package. A page walk that trusts the server's token
// loops until the scope deadline when a server repeats it (issue 12).
func unguardedPageLoops(t *testing.T, fset *token.FileSet, path string, src []byte) []string {
	t.Helper()
	file, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	var found []string
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		body := string(src[fset.Position(fn.Body.Pos()).Offset:fset.Position(fn.Body.End()).Offset])
		guarded := strings.Contains(body, "paging.")
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			loop, ok := node.(*ast.ForStmt)
			if !ok || loop.Cond != nil || guarded {
				return true
			}
			start, end := fset.Position(loop.Pos()), fset.Position(loop.End())
			if pageTokenReference.Match(src[start.Offset:end.Offset]) {
				found = append(found, start.String()+" "+fn.Name.Name)
			}
			return true
		})
	}
	return found
}

func TestProviderPageLoopsUsePagingGuard(t *testing.T) {
	root := repositoryRoot(t)
	fset := token.NewFileSet()
	walks := 0
	for _, path := range goFiles(t, filepath.Join(root, "plugins")) {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if pageTokenReference.Match(src) {
			walks++
		}
		for _, loop := range unguardedPageLoops(t, fset, path, src) {
			relative := strings.TrimPrefix(loop, root+string(filepath.Separator))
			t.Errorf("unguarded page loop %s: use paging.New/Next (internal/shared/paging)", relative)
		}
	}
	// The scan must see the plugins' page walks at all, or it would pass vacuously.
	if walks == 0 {
		t.Fatal("found no files that reference page tokens; the scan is not looking at plugins")
	}
}

func TestUnguardedPageLoopDetectorCatchesALoop(t *testing.T) {
	src := []byte(`package p

func list(c client) {
	token := ""
	for {
		page := c.List(token)
		if page.NextToken == "" {
			return
		}
		token = page.NextToken
	}
}
`)
	if found := unguardedPageLoops(t, token.NewFileSet(), "p.go", src); len(found) != 1 {
		t.Fatalf("found = %v, want the one unguarded loop", found)
	}
	guarded := []byte(strings.Replace(string(src), "token := \"\"",
		"token := \"\"\n\tguard := paging.New(\"list\", paging.PagesMax)\n\t_ = guard", 1))
	if found := unguardedPageLoops(t, token.NewFileSet(), "p.go", guarded); len(found) != 0 {
		t.Fatalf("found = %v, want none once the function uses paging", found)
	}
}
