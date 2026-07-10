package client

import (
	"strings"
	"testing"
)

func TestNewPluginClientMissingPluginErrorIsActionable(t *testing.T) {
	_, err := NewPluginClient("definitely-missing")
	if err == nil {
		t.Fatal("NewPluginClient() error = nil, want missing plugin error")
	}
	if !strings.Contains(err.Error(), "Please run 'corkscrew init'") {
		t.Fatalf("NewPluginClient() error = %q, want actionable init hint", err)
	}
}
