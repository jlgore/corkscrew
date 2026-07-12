package tui

import (
	"strings"
	"testing"
)

func TestRouterCreatesAllDestinationViews(t *testing.T) {
	router := NewViewRouter()
	router.Resize(100, 30)

	tests := []struct {
		viewType ViewType
		want     string
	}{
		{ViewMain, "Corkscrew - Cloud Resource Scanner"},
		{ViewScan, "Scan execution workspace"},
		{ViewResults, "Resource inventory"},
		{ViewConfig, "Provider, region, and service settings"},
		{ViewDiagrams, "Architecture diagram workspace"},
		{ViewCompliance, "Compliance pack execution workspace"},
		{ViewQuery, "SQL query workspace"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if !router.IsViewAvailable(tt.viewType) {
				t.Fatalf("expected view %v to be available", tt.viewType)
			}

			router.SwitchView(tt.viewType, nil)
			if router.GetCurrentView() == nil {
				t.Fatalf("expected current view for %v", tt.viewType)
			}
			if got := router.View(); !strings.Contains(got, tt.want) {
				t.Fatalf("router view missing %q:\n%s", tt.want, got)
			}
		})
	}
}

func TestRouterAvailableViewsIncludesEveryRoutedDestination(t *testing.T) {
	router := NewViewRouter()
	available := router.GetAvailableViews()

	want := []ViewType{
		ViewMain,
		ViewScan,
		ViewResults,
		ViewConfig,
		ViewDiagrams,
		ViewCompliance,
		ViewQuery,
	}

	if len(available) != len(want) {
		t.Fatalf("available view count = %d, want %d (%v)", len(available), len(want), available)
	}
	for i, viewType := range want {
		if available[i] != viewType {
			t.Fatalf("available[%d] = %v, want %v", i, available[i], viewType)
		}
	}
}
