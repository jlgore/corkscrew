package automation

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/jlgore/corkscrew/test/harness/aws"
	"github.com/pulumi/pulumi/sdk/v3/go/auto"
	"github.com/pulumi/pulumi/sdk/v3/go/auto/optdestroy"
	"github.com/pulumi/pulumi/sdk/v3/go/auto/optup"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// Scenario defines the interface for test scenarios
type Scenario interface {
	DefineResources(ctx *pulumi.Context, testID string) error
	GetExpectedResources() map[string]interface{}
	GetName() string
	GetServices() []string
}

// ScanResult contains results from running Corkscrew
type ScanResult struct {
	Output   string
	ExitCode int
	Duration time.Duration
	Error    error
}

// TestHarness manages Pulumi automation for integration tests
type TestHarness struct {
	ctx           context.Context
	projectName   string
	stackName     string
	stack         auto.Stack
	outputs       auto.OutputMap
	testID        string
	startTime     time.Time
	scenario      Scenario
	config        HarnessConfig
	cleanupResult *aws.CleanupResult
}

// HarnessConfig contains configuration for the test harness
type HarnessConfig struct {
	Provider      string
	Region        string
	Scenario      string
	TestID        string
	KeepOnFail    bool
	Timeout       time.Duration
	CorkscrewPath string
	DBPath        string
}

// NewTestHarness creates a new test harness instance
func NewTestHarness(ctx context.Context, cfg HarnessConfig, scenario Scenario) (*TestHarness, error) {
	if cfg.Timeout == 0 {
		cfg.Timeout = 10 * time.Minute
	}
	
	if cfg.CorkscrewPath == "" {
		cfg.CorkscrewPath = "../../corkscrew"
	}
	
	if cfg.DBPath == "" {
		cfg.DBPath = fmt.Sprintf("test_%s-%s.db", scenario.GetName(), cfg.TestID)
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	projectName := fmt.Sprintf("corkscrew-test-%s", scenario.GetName())
	stackName := fmt.Sprintf("%s-%s-%d", cfg.TestID, cfg.Region, time.Now().Unix())

	// Create workspace with inline program
	workspace, err := auto.NewLocalWorkspace(timeoutCtx,
		auto.Program(createProgramFunc(scenario)),
		auto.WorkDir("."),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create workspace: %w", err)
	}

	// Create or select stack
	stack, err := auto.UpsertStack(timeoutCtx, stackName, workspace)
	if err != nil {
		return nil, fmt.Errorf("failed to create stack: %w", err)
	}

	// Set configuration
	if err := configureStack(timeoutCtx, stack, cfg); err != nil {
		return nil, fmt.Errorf("failed to configure stack: %w", err)
	}

	return &TestHarness{
		ctx:         ctx,
		projectName: projectName,
		stackName:   stackName,
		stack:       stack,
		testID:      cfg.TestID,
		startTime:   time.Now(),
		scenario:    scenario,
		config:      cfg,
	}, nil
}

// Deploy deploys the test infrastructure
func (h *TestHarness) Deploy() error {
	fmt.Printf("🚀 Deploying test infrastructure for %s...\n", h.stackName)

	upRes, err := h.stack.Up(h.ctx, optup.ProgressStreams(os.Stdout))
	if err != nil {
		return fmt.Errorf("deployment failed: %w", err)
	}

	h.outputs = upRes.Outputs
	fmt.Printf("✅ Deployment complete in %s\n", time.Since(h.startTime))

	return nil
}

// GetOutputs returns the deployment outputs as a map
func (h *TestHarness) GetOutputs() map[string]interface{} {
	result := make(map[string]interface{})
	for k, v := range h.outputs {
		result[k] = v.Value
	}
	return result
}

// GetStackName returns the stack name for debugging
func (h *TestHarness) GetStackName() string {
	return h.stackName
}

// Scan runs Corkscrew against the deployed infrastructure
func (h *TestHarness) Scan() (*ScanResult, error) {
	fmt.Printf("🔍 Running Corkscrew scan for %s...\n", h.scenario.GetName())
	
	start := time.Now()
	
	args := []string{
		"scan",
		"--provider", h.config.Provider,
		"--services", strings.Join(h.scenario.GetServices(), ","),
		"--region", h.config.Region,
		"--output", h.config.DBPath,
	}
	
	cmd := exec.CommandContext(h.ctx, h.config.CorkscrewPath, args...)
	output, err := cmd.CombinedOutput()
	
	result := &ScanResult{
		Output:   string(output),
		Duration: time.Since(start),
	}
	
	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
	}
	
	if err != nil {
		result.Error = err
		return result, fmt.Errorf("corkscrew scan failed: %w", err)
	}
	
	fmt.Printf("✅ Scan complete in %s\n", result.Duration)
	return result, nil
}

// GetExpectedResources returns the expected resources from the scenario
func (h *TestHarness) GetExpectedResources() map[string]interface{} {
	return h.scenario.GetExpectedResources()
}

// GetDBPath returns the database path for verification
func (h *TestHarness) GetDBPath() string {
	return h.config.DBPath
}

// GetCleanupResult returns the cleanup verification result
func (h *TestHarness) GetCleanupResult() *aws.CleanupResult {
	return h.cleanupResult
}

// GetBucketName returns the created bucket name from outputs (backward compatibility)
func (h *TestHarness) GetBucketName() (string, error) {
	bucketName, exists := h.outputs["bucketName"]
	if !exists {
		return "", fmt.Errorf("bucketName not found in outputs")
	}
	
	name, ok := bucketName.Value.(string)
	if !ok {
		return "", fmt.Errorf("bucketName is not a string")
	}
	
	return name, nil
}

// GetBucketArn returns the created bucket ARN from outputs (backward compatibility)
func (h *TestHarness) GetBucketArn() (string, error) {
	bucketArn, exists := h.outputs["bucketArn"]
	if !exists {
		return "", fmt.Errorf("bucketArn not found in outputs")
	}
	
	arn, ok := bucketArn.Value.(string)
	if !ok {
		return "", fmt.Errorf("bucketArn is not a string")
	}
	
	return arn, nil
}

// Destroy cleans up the test infrastructure with verification
func (h *TestHarness) Destroy() error {
	return h.DestroyWithVerification()
}

// DestroyWithVerification cleans up infrastructure and verifies actual AWS cleanup
func (h *TestHarness) DestroyWithVerification() error {
	fmt.Printf("🧹 Destroying test infrastructure %s...\n", h.stackName)

	// Get expected resources before destroying
	expectedResources := h.scenario.GetExpectedResources()

	// Attempt Pulumi destroy
	_, pulumiErr := h.stack.Destroy(h.ctx, optdestroy.ProgressStreams(os.Stdout))
	pulumiSuccess := pulumiErr == nil

	if pulumiErr != nil {
		fmt.Printf("⚠️ Pulumi destroy failed: %v\n", pulumiErr)
	}

	// Create cleanup verifier
	verifier, err := aws.NewCleanupVerifier(h.ctx, h.config.Region, h.config.TestID)
	if err != nil {
		fmt.Printf("⚠️ Failed to create cleanup verifier: %v\n", err)
		// Continue with basic cleanup
		return h.basicCleanup(pulumiErr)
	}

	// Verify and perform manual cleanup if needed
	result, err := verifier.VerifyAndCleanup(h.ctx, expectedResources, pulumiSuccess)
	if err != nil {
		fmt.Printf("⚠️ Cleanup verification failed: %v\n", err)
		return h.basicCleanup(pulumiErr)
	}

	// Store cleanup result for later retrieval
	h.cleanupResult = result

	// Print cleanup summary
	fmt.Print(verifier.GetCleanupSummary(result))

	// Remove the stack if Pulumi destroy succeeded
	if pulumiSuccess {
		if err := h.stack.Workspace().RemoveStack(h.ctx, h.stackName); err != nil {
			fmt.Printf("Warning: failed to remove stack: %v\n", err)
		}
	}

	// Return error if cleanup wasn't fully successful
	if !result.AWSVerified {
		return fmt.Errorf("cleanup incomplete: %d resources remain in AWS", len(result.ResourcesFound))
	}

	if pulumiErr != nil {
		return fmt.Errorf("pulumi destroy failed but AWS cleanup succeeded: %w", pulumiErr)
	}

	fmt.Println("✅ Complete cleanup verified")
	return nil
}

// basicCleanup performs basic Pulumi cleanup without AWS verification
func (h *TestHarness) basicCleanup(pulumiErr error) error {
	if pulumiErr == nil {
		if err := h.stack.Workspace().RemoveStack(h.ctx, h.stackName); err != nil {
			fmt.Printf("Warning: failed to remove stack: %v\n", err)
		}
	}
	return pulumiErr
}

// configureStack sets up the Pulumi stack configuration
func configureStack(ctx context.Context, stack auto.Stack, cfg HarnessConfig) error {
	return stack.SetAllConfig(ctx, auto.ConfigMap{
		"aws:region": auto.ConfigValue{Value: cfg.Region},
	})
}

// createProgramFunc creates a Pulumi program function from a scenario
func createProgramFunc(scenario Scenario) pulumi.RunFunc {
	return func(ctx *pulumi.Context) error {
		testID := ctx.Stack()
		return scenario.DefineResources(ctx, testID)
	}
}