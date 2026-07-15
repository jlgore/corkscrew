// Package crosscloud owns cross-cloud correlation workflows. Adapters parse
// flags into a Request and render the correlations, which are produced by the
// packaged graph extension over the normalized DuckDB correlation tables.
package crosscloud

import (
	"fmt"
	"io"
	"strings"

	"github.com/jlgore/corkscrew/pkg/graphquery"
)

// Request describes one cross-cloud correlation run.
type Request struct {
	DBPath        string
	Kinds         []string // correlation kinds; empty means every kind
	MinConfidence float64
	OutputFormat  string
}

// Correlate runs the requested correlation kinds against the target database,
// rendering results to out and diagnostics to errOut. "all" expands to every
// kind; duplicate kinds are collapsed.
func Correlate(request Request, out, errOut io.Writer) error {
	kinds := request.Kinds
	if len(kinds) == 0 {
		kinds = AllCorrelationKinds()
	}
	resolved, err := resolveKinds(kinds)
	if err != nil {
		return err
	}

	multi := len(resolved) > 1
	for _, kind := range resolved {
		if multi && normalizeOutputFormat(request.OutputFormat) == "table" {
			fmt.Fprintf(out, "\n=== %s correlations ===\n", kind)
		}
		if err := runCorrelation(request.DBPath, kind, request.MinConfidence, request.OutputFormat, out, errOut); err != nil {
			return err
		}
	}
	return nil
}

func resolveKinds(kinds []string) ([]string, error) {
	var resolved []string
	seen := make(map[string]struct{}, len(kinds))
	for _, raw := range kinds {
		kind, err := CorrelationKind(raw)
		if err != nil {
			return nil, err
		}
		if kind == "all" {
			return AllCorrelationKinds(), nil
		}
		if _, ok := seen[kind]; ok {
			continue
		}
		seen[kind] = struct{}{}
		resolved = append(resolved, kind)
	}
	return resolved, nil
}

func runCorrelation(dbPath, kind string, minConfidence float64, outputFormat string, out, errOut io.Writer) error {
	args := []string{"correlate", kind}
	if dbPath = strings.TrimSpace(dbPath); dbPath != "" {
		args = append(args, "--db", dbPath)
	}
	if minConfidence > 0 {
		args = append(args, "--confidence", fmt.Sprintf("%.6f", minConfidence))
	}
	if output := normalizeOutputFormat(outputFormat); output != "" {
		args = append(args, "--output", output)
	}

	if exitCode := graphquery.NewRunner(out, errOut).Run(args); exitCode != 0 {
		return fmt.Errorf("graph correlate %s failed with exit code %d", kind, exitCode)
	}
	return nil
}

// CorrelationKind normalizes a user-supplied correlation type to its canonical
// name, or returns an error for an unknown type. "" and "all" map to "all".
func CorrelationKind(kind string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "", "all":
		return "all", nil
	case "ip", "ips":
		return "ip", nil
	case "dns":
		return "dns", nil
	case "network", "networks", "topology":
		return "network", nil
	case "loadbalancer", "load-balancer", "lb":
		return "load-balancer", nil
	case "connectivity", "vpn", "peering", "direct", "directconnect":
		return "connectivity", nil
	case "security", "securitygroup", "security-group":
		return "security", nil
	case "domain", "domains", "certificate", "certificates":
		return "domain", nil
	case "identity", "federation":
		return "identity", nil
	case "policy", "policies":
		return "policy", nil
	case "secret", "secrets":
		return "secret", nil
	default:
		return "", fmt.Errorf("unsupported graph correlation type %q", kind)
	}
}

// ParseCorrelationTypes parses a comma-separated list of correlation types into
// canonical kinds, dropping unknown or duplicate entries. Empty or "all" yields
// every kind.
func ParseCorrelationTypes(types string) []string {
	if strings.TrimSpace(types) == "" || strings.EqualFold(strings.TrimSpace(types), "all") {
		return AllCorrelationKinds()
	}

	parts := strings.Split(types, ",")
	kinds := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		kind, err := CorrelationKind(part)
		if err != nil || kind == "all" {
			continue
		}
		if _, ok := seen[kind]; ok {
			continue
		}
		seen[kind] = struct{}{}
		kinds = append(kinds, kind)
	}
	return kinds
}

// AllCorrelationKinds returns every canonical correlation kind.
func AllCorrelationKinds() []string {
	return []string{
		"ip",
		"dns",
		"network",
		"load-balancer",
		"connectivity",
		"security",
		"domain",
		"identity",
		"policy",
		"secret",
	}
}

// DefaultNetworkKinds is the correlation set for the comprehensive network
// analysis command.
func DefaultNetworkKinds() []string {
	return []string{"network", "connectivity", "dns", "load-balancer", "security"}
}

func normalizeOutputFormat(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "table", "ascii", "graph", "mermaid", "dot":
		return "table"
	case "json":
		return "json"
	case "csv":
		return "csv"
	default:
		return format
	}
}
