package proto

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestProtoSourceDirectoryHasNoGeneratedGo(t *testing.T) {
	matches, err := filepath.Glob(filepath.Join("..", "..", "proto", "*.pb.go"))
	if err != nil {
		t.Fatalf("glob generated proto files: %v", err)
	}
	if len(matches) > 0 {
		t.Fatalf("generated protobuf Go files belong under internal/proto, found under proto: %v", matches)
	}
}

func TestProtoSourcesTargetInternalProtoPackage(t *testing.T) {
	sources, err := filepath.Glob(filepath.Join("..", "..", "proto", "*.proto"))
	if err != nil {
		t.Fatalf("glob proto sources: %v", err)
	}
	if len(sources) == 0 {
		t.Fatal("no proto source files found")
	}

	want := []byte(`option go_package = "github.com/jlgore/corkscrew/internal/proto";`)
	for _, source := range sources {
		data, err := os.ReadFile(source)
		if err != nil {
			t.Fatalf("read %s: %v", source, err)
		}
		if !bytes.Contains(data, want) {
			t.Fatalf("%s must target github.com/jlgore/corkscrew/internal/proto", source)
		}
	}
}
