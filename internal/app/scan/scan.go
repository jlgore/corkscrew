// Package scan owns the complete provider scan application workflow. Adapters
// supply syntax and render the returned transport-neutral outcome.
package scan

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	appconfig "github.com/jlgore/corkscrew/internal/config"
	dataaccess "github.com/jlgore/corkscrew/internal/data"
	"github.com/jlgore/corkscrew/internal/db"
	pb "github.com/jlgore/corkscrew/internal/proto"
	providerRuntime "github.com/jlgore/corkscrew/internal/provider"
	"github.com/jlgore/corkscrew/internal/scanexec"
)

type Request struct {
	Provider                string
	Services                string
	Regions                 string
	OutputFormat            string
	ConfigPath              string
	MaxConcurrency          int
	SaveToFile              bool
	DatabasePath            string
	DatabaseExplicit        bool
	QuackToken              string
	DBProviderTableOverride string
	Namespace               string
	LabelSelector           string
	FieldSelector           string
	KubeconfigPath          string
	KubeContext             string
	IncludeRelationships    bool
}

type PreparedRequest struct {
	Request
	ProviderName string
	ServiceList  []string
	ScopeList    []string
}

type Dependencies struct {
	Getenv         func(string) string
	RuntimeFactory func(io.Writer) (*providerRuntime.Runtime, error)
	OpenSession    func(context.Context, string, ...dataaccess.SessionOption) (*dataaccess.Session, error)
}

type WarningKind string

const (
	WarningPersistence WarningKind = "persistence"
	WarningRuntime     WarningKind = "runtime"
)

type Warning struct {
	Kind    WarningKind
	Message string
}

type ServiceGroupExpansion struct {
	Name     string
	Services []string
}

type ScopeResult struct {
	Scope     string
	Resources []*pb.Resource
	Stats     *pb.ScanStats
	Errors    []string
	Duration  time.Duration
}

type Outcome struct {
	ID         string
	Provider   string
	Status     scanexec.Status
	Resources  []*pb.Resource
	Scopes     []ScopeResult
	Stats      *pb.ScanStats
	Duration   time.Duration
	Events     []scanexec.Event
	Warnings   []Warning
	Expansions []ServiceGroupExpansion
	Persisted  bool
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

func Run(ctx context.Context, request Request, dependencies Dependencies) (Outcome, error) {
	getenv := dependencies.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	prepared, expansions := Prepare(request, getenv)
	outcome := Outcome{ID: uuid.NewString(), Provider: prepared.ProviderName, Expansions: expansions}

	config, err := appconfig.LoadCorkscrewConfig(prepared.ConfigPath)
	if err != nil {
		return outcome, fmt.Errorf("failed to load configuration: %w", err)
	}
	if !config.IsProviderEnabled(prepared.ProviderName) {
		return outcome, fmt.Errorf("provider %s is not configured and enabled", prepared.ProviderName)
	}

	var runtimeWarnings bytes.Buffer
	runtimeFactory := dependencies.RuntimeFactory
	if runtimeFactory == nil {
		runtimeFactory = providerRuntime.NewDefaultRuntime
	}
	runtime, err := runtimeFactory(&runtimeWarnings)
	if err != nil {
		return outcome, fmt.Errorf("failed to load provider runtime: %w", err)
	}
	defer runtime.Close()

	descriptor, found := installedDescriptor(runtime.List(), prepared.ProviderName)
	if !found {
		return outcome, fmt.Errorf("provider %s is not installed", prepared.ProviderName)
	}

	scopes, err := resolveScopes(config, descriptor, prepared)
	if err != nil {
		return outcome, err
	}
	services, err := resolveServices(config, prepared)
	if err != nil {
		return outcome, err
	}

	providerSession, err := runtime.Open(ctx, prepared.ProviderName, initializationConfig(config, prepared, scopes))
	if err != nil {
		return outcome, err
	}
	if err := providerSession.Require(providerRuntime.CapabilityBatchScan); err != nil {
		return outcome, err
	}

	concurrency := prepared.MaxConcurrency
	if concurrency <= 0 {
		concurrency = 3
	}
	var eventMu sync.Mutex
	execution, executionErr := scanexec.Execute(ctx, providerSession.Provider(), scanexec.Plan{
		Provider: prepared.ProviderName, Scopes: scopes, Services: services,
		Filters: map[string]string{
			"namespace": prepared.Namespace, "label_selector": prepared.LabelSelector,
			"field_selector": prepared.FieldSelector,
		},
		IncludeRelationships: prepared.IncludeRelationships,
		MaxConcurrency:       concurrency, ScopeTimeout: 5 * time.Minute,
	}, func(event scanexec.Event) {
		eventMu.Lock()
		outcome.Events = append(outcome.Events, event)
		eventMu.Unlock()
	})
	outcome.Status = execution.Status
	outcome.Resources = execution.Resources
	outcome.Stats = execution.Stats
	outcome.Duration = execution.Duration
	outcome.Scopes = make([]ScopeResult, len(execution.Scopes))
	for index, scope := range execution.Scopes {
		outcome.Scopes[index] = ScopeResult{
			Scope: scope.Scope, Resources: scope.Resources, Stats: scope.Stats,
			Errors: append([]string(nil), scope.Errors...), Duration: scope.Duration,
		}
	}
	if message := strings.TrimSpace(runtimeWarnings.String()); message != "" {
		outcome.Warnings = append(outcome.Warnings, Warning{Kind: WarningRuntime, Message: message})
	}

	persistenceTarget := strings.TrimSpace(prepared.DatabasePath)
	if persistenceTarget == "" {
		persistenceTarget = strings.TrimSpace(config.Database.Path)
	}
	if persistenceTarget != "" {
		persistErr := persist(ctx, dependencies, outcome.ID, prepared, execution, services, scopes, persistenceTarget, descriptor.ResourceTable)
		if persistErr != nil {
			if prepared.DatabaseExplicit {
				return outcome, fmt.Errorf("store scan results in explicitly requested database: %w", persistErr)
			}
			outcome.Warnings = append(outcome.Warnings, Warning{Kind: WarningPersistence, Message: persistErr.Error()})
		} else {
			outcome.Persisted = true
		}
	}

	return outcome, executionErr
}

func persist(ctx context.Context, dependencies Dependencies, scanID string, request PreparedRequest, execution scanexec.Outcome, services, scopes []string, target, manifestTable string) error {
	var databaseOptions []db.Option
	if request.QuackToken != "" && db.IsRemoteTarget(target) {
		databaseOptions = append(databaseOptions, db.WithToken(request.QuackToken))
	}
	opener := dependencies.OpenSession
	if opener == nil {
		opener = dataaccess.OpenSession
	}
	session, err := opener(ctx, target, dataaccess.WithDatabaseOptions(databaseOptions...))
	if err != nil {
		return err
	}
	defer session.Close()

	failedScopes := make([]string, 0)
	for _, scope := range execution.Scopes {
		if len(scope.Errors) > 0 {
			failedScopes = append(failedScopes, scope.Scope)
		}
	}
	status := dataaccess.ScanStatusCompleted
	if execution.Status == scanexec.StatusPartial {
		status = dataaccess.ScanStatusPartial
	} else if execution.Status == scanexec.StatusFailed {
		status = dataaccess.ScanStatusFailed
	}
	endedAt := time.Now()
	table := strings.TrimSpace(request.DBProviderTableOverride)
	if table == "" {
		table = manifestTable
	}
	return session.PersistScanOutcome(ctx, dataaccess.ScanOutcome{
		ID: scanID, Provider: request.ProviderName, Services: services, Scopes: scopes,
		FailedScopes: failedScopes, Status: status,
		StartedAt: endedAt.Add(-execution.Duration), EndedAt: endedAt,
		Resources: execution.Resources,
	}, dataaccess.PersistScanOptions{ProviderTableOverride: table})
}

func Prepare(request Request, getenv func(string) string) (PreparedRequest, []ServiceGroupExpansion) {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	services, expansions := ExpandServices(request.Services)
	request.Provider = strings.TrimSpace(request.Provider)
	request.ConfigPath = strings.TrimSpace(request.ConfigPath)
	request.DatabasePath = strings.TrimSpace(request.DatabasePath)
	if request.DatabasePath == "" {
		request.DatabasePath = strings.TrimSpace(getenv("CORKSCREW_QUACK_URL"))
	}
	request.QuackToken = strings.TrimSpace(request.QuackToken)
	if request.QuackToken == "" && db.IsRemoteTarget(request.DatabasePath) {
		request.QuackToken = strings.TrimSpace(getenv("CORKSCREW_QUACK_TOKEN"))
	}
	request.DBProviderTableOverride = strings.TrimSpace(request.DBProviderTableOverride)
	request.Namespace = strings.TrimSpace(request.Namespace)
	request.LabelSelector = strings.TrimSpace(request.LabelSelector)
	request.FieldSelector = strings.TrimSpace(request.FieldSelector)
	request.KubeconfigPath = strings.TrimSpace(request.KubeconfigPath)
	request.KubeContext = strings.TrimSpace(request.KubeContext)
	return PreparedRequest{
		Request: request, ProviderName: request.Provider,
		ServiceList: services, ScopeList: splitList(request.Regions),
	}, expansions
}

func resolveScopes(config *appconfig.CorkscrewConfig, descriptor providerRuntime.Descriptor, request PreparedRequest) ([]string, error) {
	scopes := append([]string(nil), request.ScopeList...)
	if len(scopes) == 0 {
		var err error
		scopes, err = config.GetRegionsForProvider(request.ProviderName)
		if err != nil {
			return nil, fmt.Errorf("failed to get scopes from config: %w", err)
		}
	}
	if len(scopes) == 0 || (len(scopes) == 1 && scopes[0] == "all") {
		scopes = append([]string(nil), descriptor.DefaultScopes...)
	}
	if len(scopes) == 0 {
		return nil, fmt.Errorf("provider %s has no configured or manifest default scopes", request.ProviderName)
	}
	return scopes, nil
}

func resolveServices(config *appconfig.CorkscrewConfig, request PreparedRequest) ([]string, error) {
	if len(request.ServiceList) > 0 {
		return append([]string(nil), request.ServiceList...), nil
	}
	services, err := config.GetServicesForProvider(request.ProviderName)
	if err != nil {
		return nil, fmt.Errorf("failed to get services from config: %w", err)
	}
	return services, nil
}

func initializationConfig(config *appconfig.CorkscrewConfig, request PreparedRequest, scopes []string) map[string]string {
	result := config.ProviderInitializationConfig(request.ProviderName)
	if len(scopes) > 0 {
		result["region"] = scopes[0]
	}
	if request.KubeconfigPath != "" {
		result["kubeconfig_path"] = request.KubeconfigPath
	}
	if request.KubeContext != "" {
		result["contexts"] = request.KubeContext
	}
	return result
}

func installedDescriptor(descriptors []providerRuntime.Descriptor, name string) (providerRuntime.Descriptor, bool) {
	for _, descriptor := range descriptors {
		if descriptor.Name == name && descriptor.Installed {
			return descriptor, true
		}
	}
	return providerRuntime.Descriptor{}, false
}

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

func ServiceGroups() map[string][]string {
	result := make(map[string][]string, len(serviceGroups))
	for name, services := range serviceGroups {
		result[name] = append([]string(nil), services...)
	}
	return result
}

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
		if item = strings.TrimSpace(item); item != "" {
			result = append(result, item)
		}
	}
	return result
}
