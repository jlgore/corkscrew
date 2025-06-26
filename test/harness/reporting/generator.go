package reporting

import (
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jlgore/corkscrew/test/harness"
)

// ReportGenerator creates comprehensive test reports
type ReportGenerator struct {
	outputDir string
}

// NewReportGenerator creates a new report generator
func NewReportGenerator(outputDir string) *ReportGenerator {
	return &ReportGenerator{
		outputDir: outputDir,
	}
}

// GenerateReport creates HTML, JSON, and markdown reports
func (r *ReportGenerator) GenerateReport(result *harness.TestResult) error {
	// Ensure output directory exists
	if err := os.MkdirAll(r.outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	timestamp := time.Now().Format("20060102_150405")
	baseFilename := fmt.Sprintf("test_%s_%s_%s", result.Provider, result.ScenarioName, timestamp)

	// Generate JSON report
	if err := r.generateJSONReport(result, filepath.Join(r.outputDir, baseFilename+".json")); err != nil {
		return fmt.Errorf("failed to generate JSON report: %w", err)
	}

	// Generate HTML report
	if err := r.generateHTMLReport(result, filepath.Join(r.outputDir, baseFilename+".html")); err != nil {
		return fmt.Errorf("failed to generate HTML report: %w", err)
	}

	// Generate Markdown report
	if err := r.generateMarkdownReport(result, filepath.Join(r.outputDir, baseFilename+".md")); err != nil {
		return fmt.Errorf("failed to generate Markdown report: %w", err)
	}

	// Generate metrics for CloudWatch
	if err := r.generateMetricsReport(result, filepath.Join(r.outputDir, baseFilename+"_metrics.json")); err != nil {
		return fmt.Errorf("failed to generate metrics report: %w", err)
	}

	fmt.Printf("📊 Reports generated in: %s\n", r.outputDir)
	fmt.Printf("   - HTML: %s.html\n", baseFilename)
	fmt.Printf("   - JSON: %s.json\n", baseFilename)
	fmt.Printf("   - Markdown: %s.md\n", baseFilename)
	fmt.Printf("   - Metrics: %s_metrics.json\n", baseFilename)

	return nil
}

// generateJSONReport creates a detailed JSON report
func (r *ReportGenerator) generateJSONReport(result *harness.TestResult, filename string) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}

// generateHTMLReport creates a comprehensive HTML report
func (r *ReportGenerator) generateHTMLReport(result *harness.TestResult, filename string) error {
	tmpl := template.Must(template.New("report").Funcs(template.FuncMap{
		"formatDuration": func(d time.Duration) string {
			return d.Round(time.Millisecond).String()
		},
		"formatBytes": func(bytes int64) string {
			const unit = 1024
			if bytes < unit {
				return fmt.Sprintf("%d B", bytes)
			}
			div, exp := int64(unit), 0
			for n := bytes / unit; n >= unit; n /= unit {
				div *= unit
				exp++
			}
			return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
		},
		"formatFloat": func(f float64) string {
			return fmt.Sprintf("%.1f", f)
		},
		"statusIcon": func(success bool) string {
			if success {
				return "✅"
			}
			return "❌"
		},
		"phaseClass": func(phase string, currentPhase string) string {
			if phase == currentPhase {
				return "current"
			}
			return "completed"
		},
	}).Parse(htmlTemplate))

	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	return tmpl.Execute(file, result)
}

// generateMarkdownReport creates a markdown summary
func (r *ReportGenerator) generateMarkdownReport(result *harness.TestResult, filename string) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	md := &strings.Builder{}

	// Header
	md.WriteString(fmt.Sprintf("# Corkscrew Integration Test Report\n\n"))
	md.WriteString(fmt.Sprintf("**Scenario:** %s  \n", result.ScenarioName))
	md.WriteString(fmt.Sprintf("**Provider:** %s  \n", result.Provider))
	md.WriteString(fmt.Sprintf("**Region:** %s  \n", result.Region))
	md.WriteString(fmt.Sprintf("**Test ID:** %s  \n", result.TestID))
	md.WriteString(fmt.Sprintf("**Status:** %s  \n", statusIcon(result.Success)))
	md.WriteString(fmt.Sprintf("**Duration:** %v  \n", result.Duration.Round(time.Millisecond)))
	md.WriteString(fmt.Sprintf("**Timestamp:** %s  \n\n", result.StartTime.Format(time.RFC3339)))

	// Metrics section
	md.WriteString("## 📊 Test Metrics\n\n")
	md.WriteString(fmt.Sprintf("- **Deployment Time:** %v\n", result.DeploymentDuration.Round(time.Millisecond)))
	md.WriteString(fmt.Sprintf("- **Scan Time:** %v\n", result.Metrics.ScanDuration.Round(time.Millisecond)))
	md.WriteString(fmt.Sprintf("- **Verification Time:** %v\n", result.Metrics.VerificationDuration.Round(time.Millisecond)))
	md.WriteString(fmt.Sprintf("- **Resources Deployed:** %d\n", result.Metrics.ResourcesDeployed))
	md.WriteString(fmt.Sprintf("- **Resources Scanned:** %d\n", result.Metrics.ResourcesScanned))
	md.WriteString(fmt.Sprintf("- **Resources Verified:** %d\n", result.Metrics.ResourcesVerified))
	if result.Metrics.DatabaseSize > 0 {
		md.WriteString(fmt.Sprintf("- **Database Size:** %.2f KB\n", float64(result.Metrics.DatabaseSize)/1024))
	}

	// Verification results
	if result.VerificationResult != nil {
		vr := result.VerificationResult
		md.WriteString("\n## 🔍 Verification Results\n\n")
		md.WriteString(fmt.Sprintf("- **Success Rate:** %.1f%% (%d/%d)\n", vr.GetSuccessRate(), vr.TotalFound, vr.TotalExpected))
		md.WriteString(fmt.Sprintf("- **Resources Found:** %d\n", vr.TotalFound))
		md.WriteString(fmt.Sprintf("- **Resources Missing:** %d\n", vr.TotalMissing))
		md.WriteString(fmt.Sprintf("- **Attribute Checks:** %d\n", len(vr.AttributeChecks)))
		md.WriteString(fmt.Sprintf("- **Relationship Checks:** %d\n", len(vr.RelationshipChecks)))

		// Missing resources details
		if len(vr.Missing) > 0 {
			md.WriteString("\n### Missing Resources\n\n")
			md.WriteString("| Type | Name | Expected ARN |\n")
			md.WriteString("|------|------|-------------|\n")
			for _, missing := range vr.Missing {
				md.WriteString(fmt.Sprintf("| %s | %s | %s |\n", missing.Type, missing.Name, missing.ARN))
			}
		}

		// Attribute check details
		if len(vr.AttributeChecks) > 0 {
			md.WriteString("\n### Attribute Verification\n\n")
			md.WriteString("| Resource | Attribute | Expected | Actual | Status |\n")
			md.WriteString("|----------|-----------|----------|--------|--------|\n")
			for _, check := range vr.AttributeChecks {
				status := "✅"
				if !check.Match {
					status = "❌"
				}
				md.WriteString(fmt.Sprintf("| %s | %s | %v | %v | %s |\n", 
					check.ResourceID, check.Attribute, check.Expected, check.Actual, status))
			}
		}
	}

	// Cleanup results
	if result.CleanupResult != nil {
		cr := result.CleanupResult
		md.WriteString("\n## 🧹 Cleanup Results\n\n")
		md.WriteString(fmt.Sprintf("- **Pulumi Success:** %s\n", statusIcon(cr.PulumiSuccess)))
		md.WriteString(fmt.Sprintf("- **AWS Verified:** %s\n", statusIcon(cr.AWSVerified)))
		md.WriteString(fmt.Sprintf("- **Cleanup Duration:** %v\n", cr.CleanupDuration.Round(time.Millisecond)))
		
		if len(cr.ResourcesFound) > 0 {
			md.WriteString(fmt.Sprintf("- **⚠️ Remaining Resources:** %d\n", len(cr.ResourcesFound)))
		}
		
		if len(cr.ManualCleanup) > 0 {
			successful := 0
			for _, action := range cr.ManualCleanup {
				if action.Success {
					successful++
				}
			}
			md.WriteString(fmt.Sprintf("- **Manual Cleanup Actions:** %d/%d successful\n", successful, len(cr.ManualCleanup)))
		}
	}

	// Error details
	if result.Error != nil {
		md.WriteString("\n## ❌ Error Details\n\n")
		md.WriteString(fmt.Sprintf("**Phase:** %s  \n", result.Phase))
		md.WriteString(fmt.Sprintf("**Error:** %v  \n", result.Error))
	}

	// Scan output
	if result.ScanResult != nil && result.ScanResult.Output != "" {
		md.WriteString("\n## 📋 Corkscrew Scan Output\n\n")
		md.WriteString("```\n")
		md.WriteString(result.ScanResult.Output)
		md.WriteString("\n```\n")
	}

	_, err = file.WriteString(md.String())
	return err
}

// generateMetricsReport creates CloudWatch-compatible metrics
func (r *ReportGenerator) generateMetricsReport(result *harness.TestResult, filename string) error {
	metrics := CloudWatchMetrics{
		MetricData: []MetricData{},
		Namespace:  "Corkscrew/IntegrationTests",
		Timestamp:  result.StartTime,
	}

	// Add dimension for all metrics
	dimensions := []Dimension{
		{Name: "Provider", Value: result.Provider},
		{Name: "Scenario", Value: result.ScenarioName},
		{Name: "Region", Value: result.Region},
	}

	// Test success/failure
	successValue := 0.0
	if result.Success {
		successValue = 1.0
	}
	metrics.MetricData = append(metrics.MetricData, MetricData{
		MetricName: "TestSuccess",
		Value:      successValue,
		Unit:       "None",
		Dimensions: dimensions,
	})

	// Duration metrics
	metrics.MetricData = append(metrics.MetricData, MetricData{
		MetricName: "TestDuration",
		Value:      result.Duration.Seconds(),
		Unit:       "Seconds",
		Dimensions: dimensions,
	})

	metrics.MetricData = append(metrics.MetricData, MetricData{
		MetricName: "DeploymentDuration",
		Value:      result.DeploymentDuration.Seconds(),
		Unit:       "Seconds",
		Dimensions: dimensions,
	})

	metrics.MetricData = append(metrics.MetricData, MetricData{
		MetricName: "ScanDuration",
		Value:      result.Metrics.ScanDuration.Seconds(),
		Unit:       "Seconds",
		Dimensions: dimensions,
	})

	// Resource metrics
	metrics.MetricData = append(metrics.MetricData, MetricData{
		MetricName: "ResourcesDeployed",
		Value:      float64(result.Metrics.ResourcesDeployed),
		Unit:       "Count",
		Dimensions: dimensions,
	})

	metrics.MetricData = append(metrics.MetricData, MetricData{
		MetricName: "ResourcesScanned",
		Value:      float64(result.Metrics.ResourcesScanned),
		Unit:       "Count",
		Dimensions: dimensions,
	})

	// Database size
	if result.Metrics.DatabaseSize > 0 {
		metrics.MetricData = append(metrics.MetricData, MetricData{
			MetricName: "DatabaseSize",
			Value:      float64(result.Metrics.DatabaseSize),
			Unit:       "Bytes",
			Dimensions: dimensions,
		})
	}

	// Verification metrics
	if result.VerificationResult != nil {
		metrics.MetricData = append(metrics.MetricData, MetricData{
			MetricName: "VerificationSuccessRate",
			Value:      result.VerificationResult.GetSuccessRate(),
			Unit:       "Percent",
			Dimensions: dimensions,
		})
	}

	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(metrics)
}

// GeneratePRComment creates a concise comment for GitHub PRs
func (r *ReportGenerator) GeneratePRComment(result *harness.TestResult) string {
	comment := &strings.Builder{}

	// Header with status
	status := "❌ FAILED"
	if result.Success {
		status = "✅ PASSED"
	}

	comment.WriteString(fmt.Sprintf("## 🧪 Integration Test: %s/%s\n\n", result.Provider, result.ScenarioName))
	comment.WriteString(fmt.Sprintf("**Status:** %s  \n", status))
	comment.WriteString(fmt.Sprintf("**Duration:** %v  \n", result.Duration.Round(time.Millisecond)))
	comment.WriteString(fmt.Sprintf("**Region:** %s  \n", result.Region))

	// Quick metrics
	comment.WriteString("\n**Metrics:**\n")
	comment.WriteString(fmt.Sprintf("- Deployment: %v\n", result.DeploymentDuration.Round(time.Millisecond)))
	comment.WriteString(fmt.Sprintf("- Scan: %v\n", result.Metrics.ScanDuration.Round(time.Millisecond)))
	comment.WriteString(fmt.Sprintf("- Resources: %d deployed, %d scanned, %d verified\n", 
		result.Metrics.ResourcesDeployed, result.Metrics.ResourcesScanned, result.Metrics.ResourcesVerified))

	// Verification summary
	if result.VerificationResult != nil {
		vr := result.VerificationResult
		comment.WriteString(fmt.Sprintf("- Success Rate: %.1f%% (%d/%d resources found)\n", 
			vr.GetSuccessRate(), vr.TotalFound, vr.TotalExpected))
	}

	// Error details if failed
	if result.Error != nil {
		comment.WriteString(fmt.Sprintf("\n**Error in %s phase:** %v\n", result.Phase, result.Error))
	}

	comment.WriteString(fmt.Sprintf("\n*Test ID: `%s`*\n", result.TestID))

	return comment.String()
}

// Helper functions and types

func statusIcon(success bool) string {
	if success {
		return "✅ PASSED"
	}
	return "❌ FAILED"
}

// CloudWatch metrics types
type CloudWatchMetrics struct {
	MetricData []MetricData `json:"MetricData"`
	Namespace  string       `json:"Namespace"`
	Timestamp  time.Time    `json:"Timestamp"`
}

type MetricData struct {
	MetricName string      `json:"MetricName"`
	Value      float64     `json:"Value"`
	Unit       string      `json:"Unit"`
	Dimensions []Dimension `json:"Dimensions"`
}

type Dimension struct {
	Name  string `json:"Name"`
	Value string `json:"Value"`
}

// HTML template for detailed reports
const htmlTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Corkscrew Integration Test Report - {{.ScenarioName}}</title>
    <style>
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
            line-height: 1.6;
            color: #333;
            max-width: 1200px;
            margin: 0 auto;
            padding: 20px;
            background-color: #f8f9fa;
        }
        .header {
            background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
            color: white;
            padding: 30px;
            border-radius: 10px;
            margin-bottom: 30px;
            box-shadow: 0 4px 6px rgba(0, 0, 0, 0.1);
        }
        .header h1 {
            margin: 0;
            font-size: 2.5em;
        }
        .header .subtitle {
            opacity: 0.9;
            font-size: 1.2em;
            margin-top: 10px;
        }
        .status-badge {
            display: inline-block;
            padding: 8px 16px;
            border-radius: 20px;
            font-weight: bold;
            margin-top: 15px;
        }
        .status-success {
            background-color: #28a745;
            color: white;
        }
        .status-failure {
            background-color: #dc3545;
            color: white;
        }
        .card {
            background: white;
            border-radius: 10px;
            padding: 25px;
            margin-bottom: 25px;
            box-shadow: 0 2px 4px rgba(0, 0, 0, 0.1);
        }
        .card h2 {
            margin-top: 0;
            color: #495057;
            border-bottom: 2px solid #e9ecef;
            padding-bottom: 10px;
        }
        .metrics-grid {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(250px, 1fr));
            gap: 20px;
            margin-bottom: 20px;
        }
        .metric-item {
            background: #f8f9fa;
            padding: 20px;
            border-radius: 8px;
            text-align: center;
            border-left: 4px solid #007bff;
        }
        .metric-value {
            font-size: 2em;
            font-weight: bold;
            color: #007bff;
            display: block;
        }
        .metric-label {
            color: #6c757d;
            font-size: 0.9em;
            margin-top: 5px;
        }
        .progress-bar {
            background-color: #e9ecef;
            border-radius: 10px;
            overflow: hidden;
            margin: 10px 0;
        }
        .progress-fill {
            height: 25px;
            background: linear-gradient(90deg, #28a745 0%, #20c997 100%);
            display: flex;
            align-items: center;
            justify-content: center;
            color: white;
            font-weight: bold;
        }
        .table {
            width: 100%;
            border-collapse: collapse;
            margin-top: 15px;
        }
        .table th, .table td {
            padding: 12px;
            text-align: left;
            border-bottom: 1px solid #dee2e6;
        }
        .table th {
            background-color: #f8f9fa;
            font-weight: 600;
            color: #495057;
        }
        .table tr:hover {
            background-color: #f8f9fa;
        }
        .status-icon {
            font-size: 1.2em;
        }
        .error-section {
            background-color: #f8d7da;
            border: 1px solid #f5c6cb;
            border-radius: 8px;
            padding: 20px;
            margin-top: 20px;
        }
        .error-section h3 {
            color: #721c24;
            margin-top: 0;
        }
        .code-block {
            background-color: #f8f9fa;
            border: 1px solid #e9ecef;
            border-radius: 6px;
            padding: 16px;
            font-family: 'Monaco', 'Menlo', 'Ubuntu Mono', monospace;
            font-size: 14px;
            overflow-x: auto;
            white-space: pre-wrap;
        }
        .timeline {
            position: relative;
            padding-left: 30px;
        }
        .timeline::before {
            content: '';
            position: absolute;
            left: 15px;
            top: 0;
            bottom: 0;
            width: 2px;
            background-color: #dee2e6;
        }
        .timeline-item {
            position: relative;
            padding-bottom: 20px;
        }
        .timeline-item::before {
            content: '';
            position: absolute;
            left: -23px;
            top: 5px;
            width: 12px;
            height: 12px;
            border-radius: 50%;
            background-color: #007bff;
        }
        .timeline-item.completed::before {
            background-color: #28a745;
        }
        .timeline-item.current::before {
            background-color: #ffc107;
        }
    </style>
</head>
<body>
    <div class="header">
        <h1>🧪 Integration Test Report</h1>
        <div class="subtitle">{{.ScenarioName}} - {{.Provider}} ({{.Region}})</div>
        <div class="status-badge {{if .Success}}status-success{{else}}status-failure{{end}}">
            {{statusIcon .Success}}
        </div>
    </div>

    <div class="card">
        <h2>📊 Test Overview</h2>
        <div class="metrics-grid">
            <div class="metric-item">
                <span class="metric-value">{{formatDuration .Duration}}</span>
                <div class="metric-label">Total Duration</div>
            </div>
            <div class="metric-item">
                <span class="metric-value">{{.Metrics.ResourcesDeployed}}</span>
                <div class="metric-label">Resources Deployed</div>
            </div>
            <div class="metric-item">
                <span class="metric-value">{{.Metrics.ResourcesScanned}}</span>
                <div class="metric-label">Resources Scanned</div>
            </div>
            <div class="metric-item">
                <span class="metric-value">{{.Metrics.ResourcesVerified}}</span>
                <div class="metric-label">Resources Verified</div>
            </div>
        </div>
        
        <p><strong>Test ID:</strong> {{.TestID}}</p>
        <p><strong>Started:</strong> {{.StartTime.Format "2006-01-02 15:04:05 MST"}}</p>
        <p><strong>Completed:</strong> {{.EndTime.Format "2006-01-02 15:04:05 MST"}}</p>
    </div>

    {{if .VerificationResult}}
    <div class="card">
        <h2>🔍 Verification Results</h2>
        <div class="progress-bar">
            <div class="progress-fill" style="width: {{.VerificationResult.GetSuccessRate}}%">
                {{formatFloat .VerificationResult.GetSuccessRate}}% Success Rate
            </div>
        </div>
        
        <div class="metrics-grid">
            <div class="metric-item">
                <span class="metric-value">{{.VerificationResult.TotalFound}}/{{.VerificationResult.TotalExpected}}</span>
                <div class="metric-label">Resources Found</div>
            </div>
            <div class="metric-item">
                <span class="metric-value">{{len .VerificationResult.AttributeChecks}}</span>
                <div class="metric-label">Attribute Checks</div>
            </div>
            <div class="metric-item">
                <span class="metric-value">{{len .VerificationResult.RelationshipChecks}}</span>
                <div class="metric-label">Relationship Checks</div>
            </div>
        </div>

        {{if .VerificationResult.Missing}}
        <h3>Missing Resources</h3>
        <table class="table">
            <thead>
                <tr>
                    <th>Type</th>
                    <th>Name</th>
                    <th>Expected ARN</th>
                </tr>
            </thead>
            <tbody>
                {{range .VerificationResult.Missing}}
                <tr>
                    <td>{{.Type}}</td>
                    <td>{{.Name}}</td>
                    <td>{{.ARN}}</td>
                </tr>
                {{end}}
            </tbody>
        </table>
        {{end}}

        {{if .VerificationResult.AttributeChecks}}
        <h3>Attribute Verification</h3>
        <table class="table">
            <thead>
                <tr>
                    <th>Resource</th>
                    <th>Attribute</th>
                    <th>Expected</th>
                    <th>Actual</th>
                    <th>Status</th>
                </tr>
            </thead>
            <tbody>
                {{range .VerificationResult.AttributeChecks}}
                <tr>
                    <td>{{.ResourceID}}</td>
                    <td>{{.Attribute}}</td>
                    <td>{{.Expected}}</td>
                    <td>{{.Actual}}</td>
                    <td class="status-icon">{{if .Match}}✅{{else}}❌{{end}}</td>
                </tr>
                {{end}}
            </tbody>
        </table>
        {{end}}
    </div>
    {{end}}

    {{if .CleanupResult}}
    <div class="card">
        <h2>🧹 Cleanup Results</h2>
        <div class="metrics-grid">
            <div class="metric-item">
                <span class="metric-value">{{if .CleanupResult.PulumiSuccess}}✅{{else}}❌{{end}}</span>
                <div class="metric-label">Pulumi Success</div>
            </div>
            <div class="metric-item">
                <span class="metric-value">{{if .CleanupResult.AWSVerified}}✅{{else}}❌{{end}}</span>
                <div class="metric-label">AWS Verified</div>
            </div>
            <div class="metric-item">
                <span class="metric-value">{{formatDuration .CleanupResult.CleanupDuration}}</span>
                <div class="metric-label">Cleanup Duration</div>
            </div>
            {{if .CleanupResult.ResourcesFound}}
            <div class="metric-item">
                <span class="metric-value">{{len .CleanupResult.ResourcesFound}}</span>
                <div class="metric-label">Remaining Resources</div>
            </div>
            {{end}}
        </div>
    </div>
    {{end}}

    {{if .Error}}
    <div class="error-section">
        <h3>❌ Error Details</h3>
        <p><strong>Phase:</strong> {{.Phase}}</p>
        <p><strong>Error:</strong> {{.Error}}</p>
    </div>
    {{end}}

    {{if and .ScanResult .ScanResult.Output}}
    <div class="card">
        <h2>📋 Corkscrew Scan Output</h2>
        <div class="code-block">{{.ScanResult.Output}}</div>
    </div>
    {{end}}

    <div class="card">
        <h2>📈 Performance Timeline</h2>
        <div class="timeline">
            <div class="timeline-item completed">
                <strong>Deployment</strong> - {{formatDuration .DeploymentDuration}}
            </div>
            <div class="timeline-item completed">
                <strong>Stabilization</strong> - 30s
            </div>
            <div class="timeline-item completed">
                <strong>Corkscrew Scan</strong> - {{formatDuration .Metrics.ScanDuration}}
            </div>
            <div class="timeline-item completed">
                <strong>Verification</strong> - {{formatDuration .Metrics.VerificationDuration}}
            </div>
            {{if .CleanupResult}}
            <div class="timeline-item completed">
                <strong>Cleanup</strong> - {{formatDuration .CleanupResult.CleanupDuration}}
            </div>
            {{end}}
        </div>
    </div>
</body>
</html>`