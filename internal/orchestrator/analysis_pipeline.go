package orchestrator

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// AnalysisPipeline represents a generic analysis pipeline
type AnalysisPipeline interface {
	// AddStage adds an analysis stage to the pipeline
	AddStage(name string, stage AnalysisStage)
	
	// Execute runs the pipeline with the given input
	Execute(ctx context.Context, input interface{}) (interface{}, error)
	
	// GetStages returns all registered stages
	GetStages() []string
}

// AnalysisStage represents a single stage in the analysis pipeline
type AnalysisStage interface {
	// Name returns the stage name
	Name() string
	
	// Process executes the stage logic
	Process(ctx context.Context, input interface{}) (interface{}, error)
	
	// Validate checks if the input is valid for this stage
	Validate(input interface{}) error
}

// PipelineConfig configures the analysis pipeline
type PipelineConfig struct {
	MaxConcurrentStages int           // Max stages to run concurrently
	StageTimeout        time.Duration // Timeout for each stage
	ContinueOnError     bool          // Continue pipeline on stage errors
	ProgressHandler     func(stage string, progress float64)
}

// defaultPipeline implements AnalysisPipeline
type defaultPipeline struct {
	stages  map[string]AnalysisStage
	order   []string
	config  PipelineConfig
	mu      sync.RWMutex
}

// NewAnalysisPipeline creates a new analysis pipeline
func NewAnalysisPipeline(config PipelineConfig) AnalysisPipeline {
	if config.MaxConcurrentStages <= 0 {
		config.MaxConcurrentStages = 1
	}
	if config.StageTimeout <= 0 {
		config.StageTimeout = 5 * time.Minute
	}
	
	return &defaultPipeline{
		stages: make(map[string]AnalysisStage),
		order:  []string{},
		config: config,
	}
}

// AddStage adds an analysis stage to the pipeline
func (p *defaultPipeline) AddStage(name string, stage AnalysisStage) {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	p.stages[name] = stage
	p.order = append(p.order, name)
}

// Execute runs the pipeline with the given input
func (p *defaultPipeline) Execute(ctx context.Context, input interface{}) (interface{}, error) {
	p.mu.RLock()
	stageOrder := make([]string, len(p.order))
	copy(stageOrder, p.order)
	p.mu.RUnlock()
	
	// Execute stages in order
	current := input
	for i, stageName := range stageOrder {
		p.mu.RLock()
		stage, exists := p.stages[stageName]
		p.mu.RUnlock()
		
		if !exists {
			continue
		}
		
		// Report progress
		if p.config.ProgressHandler != nil {
			progress := float64(i) / float64(len(stageOrder))
			p.config.ProgressHandler(stageName, progress)
		}
		
		// Validate input
		if err := stage.Validate(current); err != nil {
			if p.config.ContinueOnError {
				continue
			}
			return nil, fmt.Errorf("stage %s validation failed: %w", stageName, err)
		}
		
		// Create stage context with timeout
		stageCtx, cancel := context.WithTimeout(ctx, p.config.StageTimeout)
		defer cancel()
		
		// Process stage
		output, err := stage.Process(stageCtx, current)
		if err != nil {
			if p.config.ContinueOnError {
				continue
			}
			return nil, fmt.Errorf("stage %s failed: %w", stageName, err)
		}
		
		current = output
	}
	
	// Report completion
	if p.config.ProgressHandler != nil {
		p.config.ProgressHandler("complete", 1.0)
	}
	
	return current, nil
}

// GetStages returns all registered stages
func (p *defaultPipeline) GetStages() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	
	stages := make([]string, len(p.order))
	copy(stages, p.order)
	return stages
}

// CommonAnalysisStages provides reusable analysis stages

// ValidationStage validates discovery results
type ValidationStage struct {
	validators []func(interface{}) error
}

// NewValidationStage creates a new validation stage
func NewValidationStage(validators ...func(interface{}) error) *ValidationStage {
	return &ValidationStage{
		validators: validators,
	}
}

// Name returns the stage name
func (v *ValidationStage) Name() string {
	return "validation"
}

// Process executes validation
func (v *ValidationStage) Process(ctx context.Context, input interface{}) (interface{}, error) {
	for _, validator := range v.validators {
		if err := validator(input); err != nil {
			return nil, err
		}
	}
	return input, nil
}

// Validate checks if the input is valid
func (v *ValidationStage) Validate(input interface{}) error {
	if input == nil {
		return fmt.Errorf("input cannot be nil")
	}
	return nil
}

// TransformationStage transforms data between formats
type TransformationStage struct {
	name        string
	transformer func(context.Context, interface{}) (interface{}, error)
}

// NewTransformationStage creates a new transformation stage
func NewTransformationStage(name string, transformer func(context.Context, interface{}) (interface{}, error)) *TransformationStage {
	return &TransformationStage{
		name:        name,
		transformer: transformer,
	}
}

// Name returns the stage name
func (t *TransformationStage) Name() string {
	return t.name
}

// Process executes the transformation
func (t *TransformationStage) Process(ctx context.Context, input interface{}) (interface{}, error) {
	return t.transformer(ctx, input)
}

// Validate checks if the input is valid
func (t *TransformationStage) Validate(input interface{}) error {
	if input == nil {
		return fmt.Errorf("input cannot be nil")
	}
	return nil
}

// AggregationStage aggregates data from multiple sources
type AggregationStage struct {
	aggregator func([]interface{}) (interface{}, error)
}

// NewAggregationStage creates a new aggregation stage
func NewAggregationStage(aggregator func([]interface{}) (interface{}, error)) *AggregationStage {
	return &AggregationStage{
		aggregator: aggregator,
	}
}

// Name returns the stage name
func (a *AggregationStage) Name() string {
	return "aggregation"
}

// Process executes the aggregation
func (a *AggregationStage) Process(ctx context.Context, input interface{}) (interface{}, error) {
	// Expect input to be a slice
	inputs, ok := input.([]interface{})
	if !ok {
		// Try to convert single input to slice
		inputs = []interface{}{input}
	}
	
	return a.aggregator(inputs)
}

// Validate checks if the input is valid
func (a *AggregationStage) Validate(input interface{}) error {
	return nil // Accept any input
}

// ParallelStage executes multiple stages in parallel
type ParallelStage struct {
	name   string
	stages []AnalysisStage
}

// NewParallelStage creates a new parallel execution stage
func NewParallelStage(name string, stages ...AnalysisStage) *ParallelStage {
	return &ParallelStage{
		name:   name,
		stages: stages,
	}
}

// Name returns the stage name
func (p *ParallelStage) Name() string {
	return p.name
}

// Process executes all stages in parallel
func (p *ParallelStage) Process(ctx context.Context, input interface{}) (interface{}, error) {
	results := make([]interface{}, len(p.stages))
	errors := make([]error, len(p.stages))
	
	var wg sync.WaitGroup
	for i, stage := range p.stages {
		wg.Add(1)
		go func(idx int, s AnalysisStage) {
			defer wg.Done()
			
			result, err := s.Process(ctx, input)
			results[idx] = result
			errors[idx] = err
		}(i, stage)
	}
	
	wg.Wait()
	
	// Check for errors
	for i, err := range errors {
		if err != nil {
			return nil, fmt.Errorf("parallel stage %s failed: %w", p.stages[i].Name(), err)
		}
	}
	
	return results, nil
}

// Validate checks if the input is valid
func (p *ParallelStage) Validate(input interface{}) error {
	// Validate for all sub-stages
	for _, stage := range p.stages {
		if err := stage.Validate(input); err != nil {
			return err
		}
	}
	return nil
}

// ConditionalStage executes a stage based on a condition
type ConditionalStage struct {
	name      string
	condition func(interface{}) bool
	stage     AnalysisStage
}

// NewConditionalStage creates a new conditional execution stage
func NewConditionalStage(name string, condition func(interface{}) bool, stage AnalysisStage) *ConditionalStage {
	return &ConditionalStage{
		name:      name,
		condition: condition,
		stage:     stage,
	}
}

// Name returns the stage name
func (c *ConditionalStage) Name() string {
	return c.name
}

// Process executes the stage if condition is met
func (c *ConditionalStage) Process(ctx context.Context, input interface{}) (interface{}, error) {
	if c.condition(input) {
		return c.stage.Process(ctx, input)
	}
	return input, nil // Pass through if condition not met
}

// Validate checks if the input is valid
func (c *ConditionalStage) Validate(input interface{}) error {
	return c.stage.Validate(input)
}