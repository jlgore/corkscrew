// Package scan implements the application workflow behind the scan command.
// CLI, TUI, and future API adapters can construct a Request without owning
// service expansion, environment precedence, or smart-scan option mapping.
package scan

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/jlgore/corkscrew/internal/db"
	"github.com/jlgore/corkscrew/pkg/smartscan"
)

// Request contains adapter-level scan inputs before normalization.
type Request struct {
	Provider                string
	Services                string
	Regions                 string
	OutputFormat            string
	ShowEmpty               bool
	ConfigPath              string
	MaxConcurrency          int
	SaveToFile              bool
	DatabasePath            string
	QuackToken              string
	DBProviderTableOverride string
	Namespace               string
	LabelSelector           string
	FieldSelector           string
	KubeconfigPath          string
	KubeContext             string
	IncludeRelationships    bool
}

// Dependencies makes the orchestration testable without loading a plugin.
// Nil fields use production defaults.
type Dependencies struct {
	Getenv func(string) string
	Output io.Writer
	Run    func(context.Context, smartscan.EnhancedScanOptions) error
}

// ServiceGroupExpansion records a named group expanded while preparing a scan.
type ServiceGroupExpansion struct {
	Name     string
	Services []string
}

var serviceGroups = map[string][]string{
	"compute":    {"ec2", "lambda", "ecs", "eks", "batch"},
	"storage":    {"s3", "ebs", "efs", "fsx", "backup"},
	"database":   {"rds", "dynamodb", "elasticache", "redshift", "documentdb"},
	"network":    {"vpc", "elb", "route53", "cloudfront", "apigateway"},
	"security":   {"iam", "kms", "secretsmanager", "acm", "guardduty"},
	"common":     {"s3", "ec2", "lambda", "rds", "iam"},
	"monitoring": {"cloudwatch", "logs", "xray", "sns", "sqs"},
}

// Run normalizes a request and executes the smart-scan use case.
func Run(ctx context.Context, request Request, dependencies Dependencies) error {
	getenv := dependencies.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	output := dependencies.Output
	if output == nil {
		output = os.Stdout
	}
	runner := dependencies.Run
	if runner == nil {
		runner = smartscan.RunEnhancedScan
	}

	options, expansions := Prepare(request, getenv)
	for _, expansion := range expansions {
		fmt.Fprintf(output, "📦 Expanding group '%s' to: %s\n", expansion.Name, strings.Join(expansion.Services, ", "))
	}
	return runner(ctx, options)
}

// Prepare applies application-level defaults and precedence without touching a
// provider plugin. It intentionally does not restrict provider names: official
// and custom providers travel through the same workflow and are validated by
// configuration/plugin resolution at the boundary that knows about them.
func Prepare(request Request, getenv func(string) string) (smartscan.EnhancedScanOptions, []ServiceGroupExpansion) {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}

	services, expansions := ExpandServices(request.Services)
	databasePath := strings.TrimSpace(request.DatabasePath)
	if databasePath == "" {
		databasePath = strings.TrimSpace(getenv("CORKSCREW_QUACK_URL"))
	}
	quackToken := strings.TrimSpace(request.QuackToken)
	if quackToken == "" && db.IsRemoteTarget(databasePath) {
		quackToken = strings.TrimSpace(getenv("CORKSCREW_QUACK_TOKEN"))
	}

	return smartscan.EnhancedScanOptions{
		Provider:                strings.TrimSpace(request.Provider),
		Regions:                 splitList(request.Regions),
		Services:                services,
		OutputFormat:            request.OutputFormat,
		SaveToFile:              request.SaveToFile,
		ShowEmpty:               request.ShowEmpty,
		ConfigPath:              strings.TrimSpace(request.ConfigPath),
		MaxConcurrency:          request.MaxConcurrency,
		DatabasePath:            databasePath,
		QuackToken:              quackToken,
		DBProviderTableOverride: strings.TrimSpace(request.DBProviderTableOverride),
		Namespace:               strings.TrimSpace(request.Namespace),
		LabelSelector:           strings.TrimSpace(request.LabelSelector),
		FieldSelector:           strings.TrimSpace(request.FieldSelector),
		KubeconfigPath:          strings.TrimSpace(request.KubeconfigPath),
		KubeContext:             strings.TrimSpace(request.KubeContext),
		IncludeRelationships:    request.IncludeRelationships,
	}, expansions
}

// ExpandServices expands named groups, trims entries, removes empty values, and
// preserves the first occurrence of each service.
func ExpandServices(value string) ([]string, []ServiceGroupExpansion) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}

	var expanded []string
	var expansions []ServiceGroupExpansion
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if group, exists := serviceGroups[item]; exists {
			services := append([]string(nil), group...)
			expansions = append(expansions, ServiceGroupExpansion{Name: item, Services: services})
			expanded = append(expanded, services...)
			continue
		}
		expanded = append(expanded, item)
	}

	seen := make(map[string]struct{}, len(expanded))
	result := make([]string, 0, len(expanded))
	for _, service := range expanded {
		if _, exists := seen[service]; exists {
			continue
		}
		seen[service] = struct{}{}
		result = append(result, service)
	}
	return result, expansions
}

// ServiceGroups returns a defensive copy for adapters that display group help.
func ServiceGroups() map[string][]string {
	result := make(map[string][]string, len(serviceGroups))
	for name, services := range serviceGroups {
		result[name] = append([]string(nil), services...)
	}
	return result
}

// ServiceGroupNames returns stable display order for service-group adapters.
func ServiceGroupNames() []string {
	names := make([]string, 0, len(serviceGroups))
	for name := range serviceGroups {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func splitList(value string) []string {
	var result []string
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item != "" {
			result = append(result, item)
		}
	}
	return result
}
