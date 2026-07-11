package main

import (
	"io"
	"os"
	"sync"
	"testing"
)

func TestRunCLIReturnsTopLevelExitCodes(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want int
	}{
		{name: "no args", args: nil, want: 1},
		{name: "help", args: []string{"help"}, want: 0},
		{name: "version", args: []string{"--version"}, want: 0},
		{name: "unknown command", args: []string{"wat"}, want: 1},
		{name: "unavailable command", args: []string{"diagram"}, want: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := captureCLIOutput(t, func() int {
				return runCLI(tt.args)
			})
			if got != tt.want {
				t.Fatalf("runCLI(%v) = %d, want %d", tt.args, got, tt.want)
			}
		})
	}
}

func captureCLIOutput(t *testing.T, fn func() int) int {
	t.Helper()

	oldStdout := os.Stdout
	oldStderr := os.Stderr

	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stderr pipe: %v", err)
	}

	os.Stdout = stdoutW
	os.Stderr = stderrW

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(io.Discard, stdoutR)
	}()
	go func() {
		defer wg.Done()
		_, _ = io.Copy(io.Discard, stderrR)
	}()

	code := fn()

	_ = stdoutW.Close()
	_ = stderrW.Close()
	wg.Wait()

	os.Stdout = oldStdout
	os.Stderr = oldStderr
	_ = stdoutR.Close()
	_ = stderrR.Close()

	return code
}
