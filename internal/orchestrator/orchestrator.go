package orchestrator

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Orchestrator coordinates resource discovery across any cloud provider
type Orchestrator interface {
	// Discover initiates the discovery process for a provider
	Discover(ctx context.Context, provider string, options DiscoveryOptions) (*DiscoveryResult, error)
	
	// Analyze processes discovered information through provider-specific analyzer
	Analyze(ctx context.Context, provider string, discovered *DiscoveryResult) (*AnalysisResult, error)
	
	// Generate triggers provider-specific code generation
	Generate(ctx context.Context, provider string, analysis *AnalysisResult) (*GenerationResult, error)
	
	// GetPipeline returns the complete pipeline status
	GetPipeline(provider string) (*PipelineStatus, error)
}

// DiscoveryOptions configures the discovery process
type DiscoveryOptions struct {
	Sources         []DiscoverySource      `json:"sources"`          // GitHub, API, Local
	ForceRefresh    bool                   `json:"force_refresh"`
	ConcurrentLimit int                    `json:"concurrent_limit"`
	CacheStrategy   CacheStrategy          `json:"cache_strategy"`
	Filters         map[string]interface{} `json:"filters"` // Provider-specific filters
	ProgressHandler ProgressHandler        `json:"-"`       // Progress callback
}

// ProgressHandler receives progress updates during discovery
type ProgressHandler func(phase string, progress float64, message string)

// ProviderAnalyzer is implemented by provider plugins to analyze discovered data
type ProviderAnalyzer interface {
	// Analyze processes raw discovery data into structured analysis
	Analyze(ctx context.Context, discovered *DiscoveryResult) (*AnalysisResult, error)
	
	// Validate checks if the analysis results are valid and complete
	Validate(analysis *AnalysisResult) []string // returns warnings
}

// ProviderGenerator is implemented by provider plugins to generate code
type ProviderGenerator interface {
	// Generate creates code from analysis results
	Generate(ctx context.Context, analysis *AnalysisResult, options GenerationOptions) (*GenerationResult, error)
	
	// GetTemplates returns available generation templates
	GetTemplates() []GenerationTemplate
}

// GenerationOptions configures code generation
type GenerationOptions struct {
	OutputDir     string                 `json:"output_dir"`
	Templates     []string               `json:"templates"`      // Template names to use
	Overwrite     bool                   `json:"overwrite"`
	DryRun        bool                   `json:"dry_run"`
	CustomOptions map[string]interface{} `json:"custom_options"` // Provider-specific options
}

// GenerationTemplate describes an available code generation template
type GenerationTemplate struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	FilePattern string `json:"file_pattern"` // e.g., "{{.Service}}_scanner.go"
	Type        string `json:"type"`         // scanner, client, types, etc.
}

// defaultOrchestrator implements the Orchestrator interface
type defaultOrchestrator struct {
	sources    map[string]DiscoverySource
	analyzers  map[string]ProviderAnalyzer
	generators map[string]ProviderGenerator
	cache      Cache
	pipelines  map[string]*PipelineStatus
	mu         sync.RWMutex
}

// NewOrchestrator creates a new orchestrator instance
func NewOrchestrator(cache Cache) Orchestrator {
	return &defaultOrchestrator{
		sources:    make(map[string]DiscoverySource),
		analyzers:  make(map[string]ProviderAnalyzer),
		generators: make(map[string]ProviderGenerator),
		cache:      cache,
		pipelines:  make(map[string]*PipelineStatus),
	}
}

// RegisterSource registers a discovery source
func (o *defaultOrchestrator) RegisterSource(name string, source DiscoverySource) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.sources[name] = source
}

// RegisterAnalyzer registers a provider-specific analyzer
func (o *defaultOrchestrator) RegisterAnalyzer(provider string, analyzer ProviderAnalyzer) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.analyzers[provider] = analyzer
}

// RegisterGenerator registers a provider-specific generator
func (o *defaultOrchestrator) RegisterGenerator(provider string, generator ProviderGenerator) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.generators[provider] = generator
}

// Discover initiates the discovery process for a provider
func (o *defaultOrchestrator) Discover(ctx context.Context, provider string, options DiscoveryOptions) (*DiscoveryResult, error) {
	// Initialize pipeline status
	pipeline := &PipelineStatus{
		Provider:     provider,
		CurrentPhase: "discovering",
		Progress:     0.0,
		StartedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		PhaseDetails: make(map[string]PhaseInfo),
	}
	
	o.mu.Lock()
	o.pipelines[provider] = pipeline
	o.mu.Unlock()
	
	// Update phase info
	o.updatePhase(provider, "discovering", "running", 0.0, nil)
	
	// Check cache first if not forcing refresh
	if !options.ForceRefresh && options.CacheStrategy.Enabled {
		cacheKey := fmt.Sprintf("discovery:%s", provider)
		if cached, err := o.cache.Get(ctx, cacheKey); err == nil && cached != nil {
			if result, ok := cached.(*DiscoveryResult); ok {
				o.updatePhase(provider, "discovering", "completed", 1.0, nil)
				o.updateProgress(provider, "discovering", 1.0, "Loaded from cache")
				return result, nil
			}
		}
	}
	
	// Execute discovery from all sources
	result := &DiscoveryResult{
		Provider:     provider,
		DiscoveredAt: time.Now(),
		Sources:      make(map[string]interface{}),
		Metadata:     make(map[string]interface{}),
		Errors:       []DiscoveryError{},
	}
	
	// Use semaphore for concurrency control
	sem := make(chan struct{}, options.ConcurrentLimit)
	var wg sync.WaitGroup
	var mu sync.Mutex
	
	totalSources := len(options.Sources)
	completed := 0
	
	for _, source := range options.Sources {
		wg.Add(1)
		go func(src DiscoverySource) {
			defer wg.Done()
			
			// Acquire semaphore
			sem <- struct{}{}
			defer func() { <-sem }()
			
			// Validate source
			if err := src.Validate(); err != nil {
				mu.Lock()
				result.Errors = append(result.Errors, DiscoveryError{
					Source:    src.Name(),
					Message:   fmt.Sprintf("validation failed: %v", err),
					Timestamp: time.Now(),
					Retryable: false,
				})
				mu.Unlock()
				return
			}
			
			// Get source config from filters
			var config SourceConfig
			if cfg, ok := options.Filters[src.Name()]; ok {
				config, _ = cfg.(SourceConfig)
			}
			
			// Execute discovery
			data, err := src.Discover(ctx, config)
			if err != nil {
				mu.Lock()
				result.Errors = append(result.Errors, DiscoveryError{
					Source:    src.Name(),
					Message:   err.Error(),
					Timestamp: time.Now(),
					Retryable: true,
				})
				mu.Unlock()
			} else {
				mu.Lock()
				result.Sources[src.Name()] = data
				mu.Unlock()
			}
			
			// Update progress
			mu.Lock()
			completed++
			progress := float64(completed) / float64(totalSources)
			mu.Unlock()
			
			o.updateProgress(provider, "discovering", progress, 
				fmt.Sprintf("Completed %s source", src.Name()))
		}(source)
	}
	
	wg.Wait()
	
	// Cache the result
	if options.CacheStrategy.Enabled {
		cacheKey := fmt.Sprintf("discovery:%s", provider)
		_ = o.cache.Set(ctx, cacheKey, result, options.CacheStrategy.TTL)
	}
	
	// Update phase completion
	o.updatePhase(provider, "discovering", "completed", 1.0, nil)
	
	return result, nil
}

// Analyze processes discovered information through provider-specific analyzer
func (o *defaultOrchestrator) Analyze(ctx context.Context, provider string, discovered *DiscoveryResult) (*AnalysisResult, error) {
	o.mu.RLock()
	analyzer, exists := o.analyzers[provider]
	o.mu.RUnlock()
	
	if !exists {
		return nil, fmt.Errorf("no analyzer registered for provider: %s", provider)
	}
	
	// Update pipeline status
	o.updatePhase(provider, "analyzing", "running", 0.0, nil)
	o.mu.Lock()
	if pipeline, ok := o.pipelines[provider]; ok {
		pipeline.CurrentPhase = "analyzing"
		pipeline.UpdatedAt = time.Now()
	}
	o.mu.Unlock()
	
	// Perform analysis
	result, err := analyzer.Analyze(ctx, discovered)
	if err != nil {
		o.updatePhase(provider, "analyzing", "failed", 0.0, err)
		return nil, fmt.Errorf("analysis failed: %w", err)
	}
	
	// Validate results
	warnings := analyzer.Validate(result)
	if len(warnings) > 0 {
		result.Warnings = warnings
	}
	
	// Update phase completion
	o.updatePhase(provider, "analyzing", "completed", 1.0, nil)
	
	return result, nil
}

// Generate triggers provider-specific code generation
func (o *defaultOrchestrator) Generate(ctx context.Context, provider string, analysis *AnalysisResult) (*GenerationResult, error) {
	o.mu.RLock()
	generator, exists := o.generators[provider]
	o.mu.RUnlock()
	
	if !exists {
		return nil, fmt.Errorf("no generator registered for provider: %s", provider)
	}
	
	// Update pipeline status
	o.updatePhase(provider, "generating", "running", 0.0, nil)
	o.mu.Lock()
	if pipeline, ok := o.pipelines[provider]; ok {
		pipeline.CurrentPhase = "generating"
		pipeline.UpdatedAt = time.Now()
	}
	o.mu.Unlock()
	
	// Default generation options
	options := GenerationOptions{
		OutputDir: fmt.Sprintf("generated/%s", provider),
		Templates: []string{"scanner", "types", "client"},
		Overwrite: true,
		DryRun:    false,
	}
	
	// Perform generation
	result, err := generator.Generate(ctx, analysis, options)
	if err != nil {
		o.updatePhase(provider, "generating", "failed", 0.0, err)
		return nil, fmt.Errorf("generation failed: %w", err)
	}
	
	// Update phase completion
	o.updatePhase(provider, "generating", "completed", 1.0, nil)
	
	// Mark pipeline as complete
	o.mu.Lock()
	if pipeline, ok := o.pipelines[provider]; ok {
		pipeline.CurrentPhase = "complete"
		now := time.Now()
		pipeline.CompletedAt = &now
		pipeline.UpdatedAt = now
		pipeline.Progress = 1.0
	}
	o.mu.Unlock()
	
	return result, nil
}

// GetPipeline returns the complete pipeline status
func (o *defaultOrchestrator) GetPipeline(provider string) (*PipelineStatus, error) {
	o.mu.RLock()
	defer o.mu.RUnlock()
	
	pipeline, exists := o.pipelines[provider]
	if !exists {
		return nil, fmt.Errorf("no pipeline found for provider: %s", provider)
	}
	
	// Return a copy to avoid race conditions
	copy := *pipeline
	copy.PhaseDetails = make(map[string]PhaseInfo)
	for k, v := range pipeline.PhaseDetails {
		copy.PhaseDetails[k] = v
	}
	
	return &copy, nil
}

// updatePhase updates the status of a pipeline phase
func (o *defaultOrchestrator) updatePhase(provider, phase, status string, progress float64, err error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	
	pipeline, exists := o.pipelines[provider]
	if !exists {
		return
	}
	
	info := pipeline.PhaseDetails[phase]
	info.Name = phase
	info.Status = status
	info.Progress = progress
	
	now := time.Now()
	if status == "running" && info.StartedAt == nil {
		info.StartedAt = &now
	}
	if status == "completed" || status == "failed" {
		info.CompletedAt = &now
	}
	if err != nil {
		info.Error = err.Error()
	}
	
	pipeline.PhaseDetails[phase] = info
	pipeline.UpdatedAt = now
	
	// Update overall progress
	totalPhases := 3.0 // discovering, analyzing, generating
	completedPhases := 0.0
	for _, phase := range pipeline.PhaseDetails {
		if phase.Status == "completed" {
			completedPhases++
		} else if phase.Status == "running" {
			completedPhases += phase.Progress
		}
	}
	pipeline.Progress = completedPhases / totalPhases
}

// updateProgress sends progress updates if handler is configured
func (o *defaultOrchestrator) updateProgress(provider, phase string, progress float64, message string) {
	// This would call the progress handler if configured in options
	// For now, just update the pipeline status
	o.mu.Lock()
	defer o.mu.Unlock()
	
	if pipeline, exists := o.pipelines[provider]; exists {
		if info, ok := pipeline.PhaseDetails[phase]; ok {
			info.Progress = progress
			pipeline.PhaseDetails[phase] = info
			pipeline.UpdatedAt = time.Now()
		}
	}
}