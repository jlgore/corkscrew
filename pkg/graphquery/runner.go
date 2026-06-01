package graphquery

import (
	"bytes"
	"encoding/csv"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/jlgore/corkscrew/internal/db"
)

type Runner struct {
	Stdout io.Writer
	Stderr io.Writer
}

type traverseRow struct {
	NodeID            string
	NodeType          string
	NodeName          string
	HopCount          string
	PathIDs           []string
	RelationshipTypes []string
}

func NewRunner(stdout, stderr io.Writer) *Runner {
	if stdout == nil {
		stdout = os.Stdout
	}
	if stderr == nil {
		stderr = os.Stderr
	}
	return &Runner{Stdout: stdout, Stderr: stderr}
}

func (r *Runner) Run(args []string) int {
	if len(args) == 0 {
		r.printGraphUsage()
		return 1
	}

	var err error
	switch args[0] {
	case "traverse":
		err = r.runTraverse(args[1:])
	case "path":
		err = r.runPath(args[1:])
	case "blast-radius":
		err = r.runBlastRadius(args[1:])
	case "match":
		err = r.runMatch(args[1:])
	case "patterns":
		err = r.runPatterns(args[1:])
	case "cache":
		err = r.runCache(args[1:])
	case "help", "--help", "-h":
		r.printGraphUsage()
		return 0
	default:
		_, _ = fmt.Fprintf(r.Stderr, "Unknown graph subcommand: %s\n\n", args[0])
		r.printGraphUsage()
		return 1
	}

	if err != nil {
		_, _ = fmt.Fprintf(r.Stderr, "graph command failed: %v\n", err)
		return 1
	}
	return 0
}

func (r *Runner) printGraphUsage() {
	_, _ = fmt.Fprintln(r.Stdout, "Usage: corkscrew graph <command> [options]")
	_, _ = fmt.Fprintln(r.Stdout)
	_, _ = fmt.Fprintln(r.Stdout, "Commands:")
	_, _ = fmt.Fprintln(r.Stdout, "  traverse <resource-id>                Traverse graph outward/inward from a resource")
	_, _ = fmt.Fprintln(r.Stdout, "  path <from-id> <to-id>                Find shortest path between two resources")
	_, _ = fmt.Fprintln(r.Stdout, "  blast-radius <resource-id>            Summarize reachable resources by type")
	_, _ = fmt.Fprintln(r.Stdout, "  match <pattern-name>|--pattern-file   Run a builtin or inline JSON attack pattern")
	_, _ = fmt.Fprintln(r.Stdout, "  patterns list                         List builtin graph patterns")
	_, _ = fmt.Fprintln(r.Stdout, "  cache invalidate                      Invalidate graph cache for a database")
	_, _ = fmt.Fprintln(r.Stdout)
	_, _ = fmt.Fprintln(r.Stdout, "Common options:")
	_, _ = fmt.Fprintln(r.Stdout, "  --db <path>                           DuckDB database path")
	_, _ = fmt.Fprintln(r.Stdout, "  --extension <path>                    Packaged corkscrew graph extension path")
	_, _ = fmt.Fprintln(r.Stdout, "  --output table|json|csv|dot           Output format")
}

func (r *Runner) runTraverse(args []string) error {
	fs := flag.NewFlagSet("graph traverse", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dbPath := fs.String("db", defaultDatabasePath(), "Path to DuckDB database file")
	extensionPath := fs.String("extension", "", "Path to corkscrew_graph.duckdb_extension")
	maxHops := fs.Int("hops", 5, "Maximum traversal depth")
	direction := fs.String("direction", "outbound", "Traversal direction")
	filterType := fs.String("filter-type", "", "Optional resource type filter")
	outputFormat := fs.String("output", "table", "Output format")
	noHeader := fs.Bool("no-header", false, "Omit header in table output")
	if err := fs.Parse(normalizeArgs(args, map[string]bool{"-no-header": true})); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return fmt.Errorf("usage: corkscrew graph traverse <resource-id> [options]")
	}

	resourceID := fs.Arg(0)
	filterValue := "NULL"
	if strings.TrimSpace(*filterType) != "" {
		filterValue = strings.TrimSpace(*filterType)
	}
	query := fmt.Sprintf(
		"SELECT * FROM graph_traverse(%s, %s, %d, %s, %s) ORDER BY hop_count, node_id",
		sqlStringLiteral(*dbPath),
		sqlStringLiteral(resourceID),
		*maxHops,
		sqlStringLiteral(*direction),
		sqlStringLiteral(filterValue),
	)

	if *outputFormat == "dot" {
		rows, columns, err := executeGraphQuery(query, *extensionPath)
		if err != nil {
			return err
		}
		traverseRows, err := decodeTraverseRows(rows, columns)
		if err != nil {
			return err
		}
		_, _ = io.WriteString(r.Stdout, RenderTraverseDOT(resourceID, traverseRows))
		return nil
	}

	return r.runSQL(*dbPath, *extensionPath, query, *outputFormat, *noHeader)
}

func (r *Runner) runPath(args []string) error {
	fs := flag.NewFlagSet("graph path", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dbPath := fs.String("db", defaultDatabasePath(), "Path to DuckDB database file")
	extensionPath := fs.String("extension", "", "Path to corkscrew_graph.duckdb_extension")
	weighted := fs.Bool("weighted", false, "Use weighted path")
	outputFormat := fs.String("output", "table", "Output format")
	noHeader := fs.Bool("no-header", false, "Omit header in table output")
	if err := fs.Parse(normalizeArgs(args, map[string]bool{"-weighted": true, "-no-header": true})); err != nil {
		return err
	}
	if fs.NArg() < 2 {
		return fmt.Errorf("usage: corkscrew graph path <from-id> <to-id> [options]")
	}
	fromID := fs.Arg(0)
	toID := fs.Arg(1)
	query := fmt.Sprintf("SELECT * FROM cloud_path(%s, %s, %s)", sqlStringLiteral(*dbPath), sqlStringLiteral(fromID), sqlStringLiteral(toID))
	if *weighted {
		query = fmt.Sprintf("SELECT * FROM graph_shortest_path(%s, %s, %s, true)", sqlStringLiteral(*dbPath), sqlStringLiteral(fromID), sqlStringLiteral(toID))
	}
	return r.runSQL(*dbPath, *extensionPath, query, *outputFormat, *noHeader)
}

func (r *Runner) runBlastRadius(args []string) error {
	fs := flag.NewFlagSet("graph blast-radius", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dbPath := fs.String("db", defaultDatabasePath(), "Path to DuckDB database file")
	extensionPath := fs.String("extension", "", "Path to corkscrew_graph.duckdb_extension")
	maxHops := fs.Int("hops", 8, "Maximum traversal depth")
	outputFormat := fs.String("output", "table", "Output format")
	noHeader := fs.Bool("no-header", false, "Omit header in table output")
	if err := fs.Parse(normalizeArgs(args, map[string]bool{"-no-header": true})); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return fmt.Errorf("usage: corkscrew graph blast-radius <resource-id> [options]")
	}
	resourceID := fs.Arg(0)
	query := fmt.Sprintf("SELECT * FROM blast_radius(%s, %s, %d) ORDER BY reachable_count DESC, resource_type", sqlStringLiteral(*dbPath), sqlStringLiteral(resourceID), *maxHops)
	return r.runSQL(*dbPath, *extensionPath, query, *outputFormat, *noHeader)
}

func (r *Runner) runMatch(args []string) error {
	fs := flag.NewFlagSet("graph match", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dbPath := fs.String("db", defaultDatabasePath(), "Path to DuckDB database file")
	extensionPath := fs.String("extension", "", "Path to corkscrew_graph.duckdb_extension")
	patternFile := fs.String("pattern-file", "", "Path to pattern file")
	outputFormat := fs.String("output", "table", "Output format")
	noHeader := fs.Bool("no-header", false, "Omit header in table output")
	if err := fs.Parse(normalizeArgs(args, map[string]bool{"-no-header": true})); err != nil {
		return err
	}
	patternSpec := ""
	if strings.TrimSpace(*patternFile) != "" {
		content, err := os.ReadFile(*patternFile)
		if err != nil {
			return fmt.Errorf("failed to read pattern file: %w", err)
		}
		patternSpec = string(content)
	} else if fs.NArg() >= 1 {
		patternSpec = fs.Arg(0)
	}
	if strings.TrimSpace(patternSpec) == "" {
		return fmt.Errorf("usage: corkscrew graph match <pattern-name> | --pattern-file pattern.json")
	}
	query := fmt.Sprintf("SELECT * FROM graph_match_pattern(%s, %s) ORDER BY match_id, pattern_node", sqlStringLiteral(*dbPath), sqlStringLiteral(patternSpec))
	return r.runSQL(*dbPath, *extensionPath, query, *outputFormat, *noHeader)
}

func (r *Runner) runPatterns(args []string) error {
	if len(args) == 0 || args[0] != "list" {
		return fmt.Errorf("usage: corkscrew graph patterns list [options]")
	}
	fs := flag.NewFlagSet("graph patterns list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dbPath := fs.String("db", defaultDatabasePath(), "Path to DuckDB database file")
	extensionPath := fs.String("extension", "", "Path to corkscrew_graph.duckdb_extension")
	outputFormat := fs.String("output", "table", "Output format")
	noHeader := fs.Bool("no-header", false, "Omit header in table output")
	if err := fs.Parse(normalizeArgs(args[1:], map[string]bool{"-no-header": true})); err != nil {
		return err
	}
	return r.runSQL(*dbPath, *extensionPath, "SELECT * FROM attack_patterns() ORDER BY pattern_name", *outputFormat, *noHeader)
}

func (r *Runner) runCache(args []string) error {
	if len(args) == 0 || args[0] != "invalidate" {
		return fmt.Errorf("usage: corkscrew graph cache invalidate [options]")
	}
	fs := flag.NewFlagSet("graph cache invalidate", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dbPath := fs.String("db", defaultDatabasePath(), "Path to DuckDB database file")
	extensionPath := fs.String("extension", "", "Path to corkscrew_graph.duckdb_extension")
	outputFormat := fs.String("output", "table", "Output format")
	noHeader := fs.Bool("no-header", false, "Omit header in table output")
	if err := fs.Parse(normalizeArgs(args[1:], map[string]bool{"-no-header": true})); err != nil {
		return err
	}
	query := fmt.Sprintf("SELECT * FROM graph_cache_invalidate(%s)", sqlStringLiteral(*dbPath))
	return r.runSQL(*dbPath, *extensionPath, query, *outputFormat, *noHeader)
}

func (r *Runner) runSQL(dbPath, extensionPath, sqlQuery, outputFormat string, noHeader bool) error {
	resolvedExtensionPath, err := resolveExtensionPath(extensionPath)
	if err != nil {
		return err
	}

	switch outputFormat {
	case "json":
		output, err := runDuckDBQuery(sqlQuery, resolvedExtensionPath, []string{"-json"})
		if err != nil {
			return err
		}
		_, _ = io.WriteString(r.Stdout, output)
	case "csv":
		flags := []string{"-csv"}
		if noHeader {
			flags = append(flags, "-noheader")
		}
		output, err := runDuckDBQuery(sqlQuery, resolvedExtensionPath, flags)
		if err != nil {
			return err
		}
		_, _ = io.WriteString(r.Stdout, output)
	case "table":
		rows, columns, err := executeGraphQuery(sqlQuery, resolvedExtensionPath)
		if err != nil {
			return err
		}
		printTable(r.Stdout, rows, columns, noHeader)
	default:
		return fmt.Errorf("unsupported output format %q", outputFormat)
	}
	_ = dbPath
	return nil
}

func defaultDatabasePath() string {
	path, err := db.GetUnifiedDatabasePath()
	if err == nil {
		return path
	}
	return ".corkscrew.duckdb"
}

func executeGraphQuery(sqlQuery, extensionPath string) ([][]interface{}, []string, error) {
	output, err := runDuckDBQuery(sqlQuery, extensionPath, []string{"-csv"})
	if err != nil {
		return nil, nil, err
	}
	reader := csv.NewReader(strings.NewReader(output))
	records, err := reader.ReadAll()
	if err != nil {
		return nil, nil, err
	}
	if len(records) == 0 {
		return nil, nil, nil
	}
	columns := records[0]
	rows := make([][]interface{}, 0, max(len(records)-1, 0))
	for _, record := range records[1:] {
		row := make([]interface{}, len(record))
		for i, value := range record {
			if value == "NULL" {
				row[i] = nil
			} else {
				row[i] = value
			}
		}
		rows = append(rows, row)
	}
	return rows, columns, nil
}

func runDuckDBQuery(sqlQuery, extensionPath string, formatFlags []string) (string, error) {
	duckdbPath, err := exec.LookPath("duckdb")
	if err != nil {
		return "", fmt.Errorf("duckdb CLI not found on PATH")
	}
	loadSQL := fmt.Sprintf("LOAD '%s'; %s", strings.ReplaceAll(extensionPath, "'", "''"), sqlQuery)
	args := []string{":memory:", "-unsigned", "-batch"}
	args = append(args, formatFlags...)
	args = append(args, "-c", loadSQL)
	cmd := exec.Command(duckdbPath, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if stderr.Len() > 0 {
			return "", errors.New(strings.TrimSpace(stderr.String()))
		}
		return "", err
	}
	return stdout.String(), nil
}

func decodeTraverseRows(rows [][]interface{}, columns []string) ([]traverseRow, error) {
	indices := make(map[string]int, len(columns))
	for i, col := range columns {
		indices[col] = i
	}
	required := []string{"node_id", "node_type", "node_name", "hop_count", "path_ids", "relationship_types"}
	for _, col := range required {
		if _, ok := indices[col]; !ok {
			return nil, fmt.Errorf("missing traverse column %q", col)
		}
	}

	decoded := make([]traverseRow, 0, len(rows))
	for _, row := range rows {
		decoded = append(decoded, traverseRow{
			NodeID:            stringValue(row[indices["node_id"]]),
			NodeType:          stringValue(row[indices["node_type"]]),
			NodeName:          stringValue(row[indices["node_name"]]),
			HopCount:          stringValue(row[indices["hop_count"]]),
			PathIDs:           parseDuckDBList(stringValue(row[indices["path_ids"]])),
			RelationshipTypes: parseDuckDBList(stringValue(row[indices["relationship_types"]])),
		})
	}
	return decoded, nil
}

func RenderTraverseDOT(sourceID string, rows []traverseRow) string {
	type nodeMeta struct {
		Name  string
		Type  string
		Start bool
	}
	nodes := map[string]nodeMeta{
		sourceID: {Name: sourceID, Start: true},
	}
	edges := map[string][3]string{}

	for _, row := range rows {
		nodes[row.NodeID] = nodeMeta{Name: row.NodeName, Type: row.NodeType}
		for i := 0; i+1 < len(row.PathIDs); i++ {
			rel := ""
			if i < len(row.RelationshipTypes) {
				rel = row.RelationshipTypes[i]
			}
			key := row.PathIDs[i] + "\x00" + row.PathIDs[i+1] + "\x00" + rel
			edges[key] = [3]string{row.PathIDs[i], row.PathIDs[i+1], rel}
		}
	}

	var b strings.Builder
	b.WriteString("digraph corkscrew_graph {\n")
	b.WriteString("  rankdir=LR;\n")
	b.WriteString("  node [shape=box, style=rounded];\n")

	nodeIDs := make([]string, 0, len(nodes))
	for id := range nodes {
		nodeIDs = append(nodeIDs, id)
	}
	sort.Strings(nodeIDs)
	for _, id := range nodeIDs {
		meta := nodes[id]
		label := id
		if strings.TrimSpace(meta.Name) != "" && meta.Name != id {
			label = meta.Name + "\n" + id
		}
		if strings.TrimSpace(meta.Type) != "" {
			label += "\n" + meta.Type
		}
		attrs := []string{fmt.Sprintf("label=%q", label)}
		if meta.Start {
			attrs = append(attrs, `style="rounded,filled"`, `fillcolor="#dbeafe"`)
		}
		b.WriteString(fmt.Sprintf("  %q [%s];\n", id, strings.Join(attrs, ", ")))
	}

	edgeKeys := make([]string, 0, len(edges))
	for key := range edges {
		edgeKeys = append(edgeKeys, key)
	}
	sort.Strings(edgeKeys)
	for _, key := range edgeKeys {
		edge := edges[key]
		if edge[2] == "" {
			b.WriteString(fmt.Sprintf("  %q -> %q;\n", edge[0], edge[1]))
			continue
		}
		b.WriteString(fmt.Sprintf("  %q -> %q [label=%q];\n", edge[0], edge[1], edge[2]))
	}
	b.WriteString("}\n")
	return b.String()
}

func parseDuckDBList(value string) []string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || trimmed == "[]" {
		return nil
	}
	trimmed = strings.TrimPrefix(trimmed, "[")
	trimmed = strings.TrimSuffix(trimmed, "]")
	parts := strings.Split(trimmed, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		item := strings.TrimSpace(strings.Trim(part, `"`))
		if item != "" {
			values = append(values, item)
		}
	}
	return values
}

func normalizeArgs(args []string, boolFlags map[string]bool) []string {
	flags := make([]string, 0, len(args))
	positionals := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "-") {
			positionals = append(positionals, arg)
			continue
		}
		flags = append(flags, arg)
		if strings.Contains(arg, "=") || boolFlags[arg] {
			continue
		}
		if i+1 < len(args) {
			flags = append(flags, args[i+1])
			i++
		}
	}
	return append(flags, positionals...)
}

func resolveExtensionPath(explicitPath string) (string, error) {
	if strings.TrimSpace(explicitPath) != "" {
		return filepath.Abs(strings.TrimSpace(explicitPath))
	}
	if envPath := strings.TrimSpace(os.Getenv("CORKSCREW_GRAPH_EXTENSION")); envPath != "" {
		return filepath.Abs(envPath)
	}
	candidates := []string{
		"extensions/corkscrew-graph/build/corkscrew_graph.duckdb_extension",
		"extensions/corkscrew-graph/target/release/corkscrew_graph.duckdb_extension",
		"build/corkscrew_graph.duckdb_extension",
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, ".corkscrew", "extensions", "corkscrew_graph.duckdb_extension"))
	}
	for _, candidate := range candidates {
		abs, err := filepath.Abs(candidate)
		if err == nil {
			if _, statErr := os.Stat(abs); statErr == nil {
				return abs, nil
			}
		}
	}
	return "", fmt.Errorf("could not find corkscrew_graph.duckdb_extension; use --extension or CORKSCREW_GRAPH_EXTENSION")
}

func printTable(w io.Writer, rows [][]interface{}, columns []string, noHeader bool) {
	if len(rows) == 0 {
		_, _ = fmt.Fprintln(w, "No results found.")
		return
	}
	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	if !noHeader {
		_, _ = fmt.Fprintln(tw, strings.Join(columns, "\t"))
		separators := make([]string, len(columns))
		for i, col := range columns {
			separators[i] = strings.Repeat("-", len(col))
		}
		_, _ = fmt.Fprintln(tw, strings.Join(separators, "\t"))
	}
	for _, row := range rows {
		values := make([]string, len(row))
		for i, val := range row {
			values[i] = stringValue(val)
		}
		_, _ = fmt.Fprintln(tw, strings.Join(values, "\t"))
	}
	_ = tw.Flush()
	_, _ = fmt.Fprintf(w, "\n(%d rows)\n", len(rows))
}

func sqlStringLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func stringValue(val interface{}) string {
	if val == nil {
		return "NULL"
	}
	switch v := val.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
