package main

import (
	"strings"
	"testing"
)

func TestQuackServeTokenLocal(t *testing.T) {
	got, err := quackServeToken("", false)
	if err != nil || got != "" {
		t.Fatalf("empty local token = %q, %v; want Quack-generated (empty), nil", got, err)
	}
	if _, err := quackServeToken("abc", false); err == nil {
		t.Fatal("3-character local token accepted")
	}
	if got, err := quackServeToken("abcd", false); err != nil || got != "abcd" {
		t.Fatalf("4-character local token = %q, %v", got, err)
	}
}

func TestQuackServeTokenRemote(t *testing.T) {
	if _, err := quackServeToken(strings.Repeat("a", minRemoteQuackTokenLen-1), true); err == nil {
		t.Fatal("short remote token accepted")
	}
	long := strings.Repeat("a", minRemoteQuackTokenLen)
	if got, err := quackServeToken(long, true); err != nil || got != long {
		t.Fatalf("32-character remote token = %q, %v", got, err)
	}

	first, err := quackServeToken("", true)
	if err != nil {
		t.Fatalf("generate remote token: %v", err)
	}
	if len(first) != 2*generatedQuackTokenBytes {
		t.Fatalf("generated token length = %d, want %d", len(first), 2*generatedQuackTokenBytes)
	}
	second, err := quackServeToken("", true)
	if err != nil {
		t.Fatalf("generate remote token: %v", err)
	}
	if first == second {
		t.Fatal("two generated tokens are identical")
	}
}

func TestReadQueryInputRejectsOversize(t *testing.T) {
	const limit = 16
	got, err := readQueryInput(strings.NewReader(strings.Repeat("x", limit)), limit)
	if err != nil || len(got) != limit {
		t.Fatalf("input at the limit = %d bytes, %v", len(got), err)
	}
	if _, err := readQueryInput(strings.NewReader(strings.Repeat("x", limit+1)), limit); err == nil {
		t.Fatal("input one byte over the limit accepted")
	}
}
