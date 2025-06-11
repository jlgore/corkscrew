package config

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// discoverServices combines multiple discovery methods
func discoverServices() ([]string, error) {
	services := make(map[string]bool)
	
	// 1. Discover from go.mod
	goModServices, err := discoverServicesFromGoMod()
	if err == nil {
		for _, svc := range goModServices {
			services[svc] = true
		}
	}
	
	// 2. Discover from AWS SDK
	sdkServices, err := discoverServicesFromAWSSDK()
	if err == nil {
		for _, svc := range sdkServices {
			services[svc] = true
		}
	}
	
	// 3. Discover from GitHub API
	githubServices, err := discoverServicesFromGitHub()
	if err == nil {
		for _, svc := range githubServices {
			services[svc] = true
		}
	}
	
	// Convert to slice
	result := make([]string, 0, len(services))
	for svc := range services {
		result = append(result, svc)
	}
	
	return result, nil
}

// discoverServicesFromGoMod analyzes go.mod to find AWS service dependencies
func discoverServicesFromGoMod() ([]string, error) {
	goModPath := "go.mod"
	if _, err := os.Stat(goModPath); os.IsNotExist(err) {
		// Try in parent directory (for plugins)
		goModPath = filepath.Join("..", "..", "go.mod")
	}
	
	file, err := os.Open(goModPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	
	services := make(map[string]bool)
	scanner := bufio.NewScanner(file)
	
	// Pattern to match AWS SDK service imports
	// e.g., github.com/aws/aws-sdk-go-v2/service/s3
	servicePattern := regexp.MustCompile(`github\.com/aws/aws-sdk-go-v2/service/(\w+)`)
	
	for scanner.Scan() {
		line := scanner.Text()
		matches := servicePattern.FindStringSubmatch(line)
		if len(matches) > 1 {
			serviceName := matches[1]
			services[serviceName] = true
		}
	}
	
	// Convert to slice
	result := make([]string, 0, len(services))
	for svc := range services {
		result = append(result, svc)
	}
	
	return result, nil
}

// discoverServicesFromAWSSDK uses the AWS SDK's built-in service discovery
func discoverServicesFromAWSSDK() ([]string, error) {
	services := []string{}
	
	// Try to discover services from the AWS SDK installation
	// Look for service directories in the SDK
	possiblePaths := []string{
		// Check vendor directory
		"vendor/github.com/aws/aws-sdk-go-v2/service",
		// Check go module cache
		filepath.Join(os.Getenv("GOPATH"), "pkg/mod/github.com/aws/aws-sdk-go-v2@*/service"),
		// Check local module cache
		filepath.Join(os.Getenv("HOME"), "go/pkg/mod/github.com/aws/aws-sdk-go-v2@*/service"),
	}
	
	for _, basePath := range possiblePaths {
		// Use glob to find versioned directories
		matches, err := filepath.Glob(basePath)
		if err != nil {
			continue
		}
		
		for _, match := range matches {
			entries, err := os.ReadDir(match)
			if err != nil {
				continue
			}
			
			for _, entry := range entries {
				if entry.IsDir() {
					serviceName := entry.Name()
					// Filter out internal directories
					if !strings.HasPrefix(serviceName, ".") && 
					   !strings.HasPrefix(serviceName, "_") &&
					   serviceName != "internal" {
						services = append(services, serviceName)
					}
				}
			}
			
			// If we found services, we can stop looking
			if len(services) > 0 {
				break
			}
		}
	}
	
	return services, nil
}

// discoverServicesFromGitHub queries the GitHub API for AWS SDK service packages
func discoverServicesFromGitHub() ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	
	// GitHub API endpoint for listing contents
	url := "https://api.github.com/repos/aws/aws-sdk-go-v2/contents/service"
	
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	
	// Add GitHub token if available for higher rate limits
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	
	// Add User-Agent header (required by GitHub API)
	req.Header.Set("User-Agent", "corkscrew-service-discovery")
	
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}
	
	var contents []struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	
	if err := json.NewDecoder(resp.Body).Decode(&contents); err != nil {
		return nil, err
	}
	
	services := []string{}
	for _, item := range contents {
		if item.Type == "dir" && 
		   !strings.HasPrefix(item.Name, ".") && 
		   !strings.HasPrefix(item.Name, "_") &&
		   item.Name != "internal" {
			services = append(services, item.Name)
		}
	}
	
	return services, nil
}