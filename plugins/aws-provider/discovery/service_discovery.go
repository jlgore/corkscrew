package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// ServiceDiscovery resolves the list of AWS service directories published in
// aws/aws-sdk-go-v2 by querying the GitHub git/trees API. The result is
// cached in-memory for cacheExpiry.
type ServiceDiscovery struct {
	mu            sync.RWMutex
	githubToken   string
	cacheExpiry   time.Duration
	lastDiscovery time.Time
	lastServices  []string
}

// GitHubTreeResponse represents the GitHub git/trees API response.
type GitHubTreeResponse struct {
	Tree []GitHubTreeItem `json:"tree"`
}

// GitHubTreeItem represents a single item in a GitHub tree.
type GitHubTreeItem struct {
	Path string `json:"path"`
	Type string `json:"type"`
	Mode string `json:"mode"`
	Url  string `json:"url"`
	Sha  string `json:"sha"`
}

func NewAWSServiceDiscovery(githubToken string) *ServiceDiscovery {
	return &ServiceDiscovery{
		githubToken: githubToken,
		cacheExpiry: 24 * time.Hour,
	}
}

// DiscoverAWSServices returns the alphabetised list of AWS service directory
// names. Subsequent calls within cacheExpiry return the cached list unless
// forceRefresh is true.
func (sd *ServiceDiscovery) DiscoverAWSServices(ctx context.Context, forceRefresh bool) ([]string, error) {
	sd.mu.Lock()
	defer sd.mu.Unlock()

	if !forceRefresh && time.Since(sd.lastDiscovery) < sd.cacheExpiry && len(sd.lastServices) > 0 {
		out := make([]string, len(sd.lastServices))
		copy(out, sd.lastServices)
		return out, nil
	}

	services, err := sd.fetchServicesFromGitHub(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to discover services from GitHub: %w", err)
	}

	sort.Strings(services)
	sd.lastServices = services
	sd.lastDiscovery = time.Now()

	out := make([]string, len(services))
	copy(out, services)
	return out, nil
}

func (sd *ServiceDiscovery) fetchServicesFromGitHub(ctx context.Context) ([]string, error) {
	url := "https://api.github.com/repos/aws/aws-sdk-go-v2/git/trees/main?recursive=1"

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	if sd.githubToken != "" {
		req.Header.Set("Authorization", fmt.Sprintf("token %s", sd.githubToken))
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "Corkscrew-AWS-Plugin-Discovery/1.0")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch from GitHub: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitHub API returned status %d: %s", resp.StatusCode, string(body))
	}

	var treeResp GitHubTreeResponse
	if err := json.NewDecoder(resp.Body).Decode(&treeResp); err != nil {
		return nil, fmt.Errorf("failed to decode GitHub response: %w", err)
	}

	seen := make(map[string]bool)
	for _, item := range treeResp.Tree {
		if item.Type != "tree" || !strings.HasPrefix(item.Path, "service/") {
			continue
		}
		parts := strings.Split(item.Path, "/")
		if len(parts) < 2 {
			continue
		}
		name := parts[1]
		if strings.HasPrefix(name, ".") || strings.Contains(name, "internal") || name == "types" {
			continue
		}
		seen[name] = true
	}

	services := make([]string, 0, len(seen))
	for name := range seen {
		services = append(services, name)
	}
	return services, nil
}
