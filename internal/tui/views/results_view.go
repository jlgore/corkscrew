package views

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/jlgore/corkscrew/internal/tui/styles"
	"github.com/jlgore/corkscrew/internal/tui/types"
)

const resultsPageSize = 200

// ResultsViewModel renders a read-only inventory table backed by the scan DB.
type ResultsViewModel struct {
	state     ViewState
	database  interface{}
	resources []types.Resource
	total     int
	selected  int
	offset    int
	mode      string
	loadError error
}

type resourceTableSource struct {
	table          string
	provider       string
	locationColumn string
}

var inventoryTableSources = []resourceTableSource{
	{table: "aws_resources", provider: "aws", locationColumn: "region"},
	{table: "azure_resources", provider: "azure", locationColumn: "location"},
	{table: "gcp_resources", provider: "gcp", locationColumn: "location"},
	{table: "kubernetes_resources", provider: "kubernetes", locationColumn: "region"},
	{table: "cloudflare_resources", provider: "cloudflare", locationColumn: "region"},
	{table: "github_resources", provider: "github", locationColumn: "org"},
}

// NewResultsViewModel creates a results inventory view.
func NewResultsViewModel() *ResultsViewModel {
	return &ResultsViewModel{
		state: NewViewState(types.ViewResults, "Results"),
		mode:  "inventory",
	}
}

// Init loads initial resources.
func (m *ResultsViewModel) Init() tea.Cmd {
	return m.loadResources()
}

// Update handles result table navigation and reloads.
func (m *ResultsViewModel) Update(msg tea.Msg) (BaseView, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.Resize(msg.Width, msg.Height)
	case types.ResourcesLoadedMsg:
		m.resources = msg.Resources
		m.total = msg.Total
		m.loadError = msg.Error
		m.selected = clampInt(m.selected, 0, len(m.resources)-1)
		m.ensureSelectionVisible()
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			m.moveSelection(-1)
		case "down", "j":
			m.moveSelection(1)
		case "pgup", "ctrl+u":
			m.moveSelection(-m.visibleRows())
		case "pgdown", "ctrl+d":
			m.moveSelection(m.visibleRows())
		case "home", "g":
			m.selected = 0
			m.ensureSelectionVisible()
		case "end", "G":
			m.selected = len(m.resources) - 1
			m.ensureSelectionVisible()
		case "r":
			return m, m.loadResources()
		}
	}
	return m, nil
}

// View renders the results table.
func (m *ResultsViewModel) View() string {
	width := m.state.Width - 4
	height := m.state.Height - 2
	if width < 60 {
		width = 60
	}
	if height < 10 {
		height = 10
	}

	content := []string{
		styles.TitleStyle.Render("Results"),
		styles.SubtitleStyle.Render(m.subtitle()),
		"",
	}

	if m.loadError != nil {
		content = append(content, styles.ErrorStyle.Render(m.loadError.Error()), "")
	}

	content = append(content, m.renderTable(width)...)
	if selected := m.selectedResource(); selected != nil {
		content = append(content, "", m.renderSelected(*selected, width))
	}

	border := styles.BorderStyle
	if m.state.Focused {
		border = styles.FocusedBorderStyle
	}
	return border.
		Width(width).
		Height(height).
		Render(lipgloss.JoinVertical(lipgloss.Left, content...))
}

// Title returns the view title.
func (m *ResultsViewModel) Title() string {
	return m.state.Title
}

// ShortHelp returns compact key help.
func (m *ResultsViewModel) ShortHelp() []string {
	return []string{"up/down select", "r refresh", "esc back", "q quit"}
}

// FullHelp returns detailed key help.
func (m *ResultsViewModel) FullHelp() [][]string {
	return [][]string{
		{"up/down", "select resource"},
		{"pgup/pgdown", "page"},
		{"home/end", "jump"},
		{"r", "refresh"},
		{"esc", "back"},
	}
}

// Focus marks the view as active.
func (m *ResultsViewModel) Focus() {
	m.state.Focused = true
}

// Blur marks the view as inactive.
func (m *ResultsViewModel) Blur() {
	m.state.Focused = false
}

// Resize updates the view dimensions.
func (m *ResultsViewModel) Resize(width, height int) {
	m.state.Width = width
	m.state.Height = height
	m.ensureSelectionVisible()
}

// ViewType returns the routed view type.
func (m *ResultsViewModel) ViewType() types.ViewType {
	return m.state.ViewType
}

// SetDatabase updates the database dependency.
func (m *ResultsViewModel) SetDatabase(database interface{}) {
	m.database = database
}

// SetMode updates the result workspace mode.
func (m *ResultsViewModel) SetMode(mode string) {
	if strings.TrimSpace(mode) == "" {
		mode = "inventory"
	}
	m.mode = mode
}

func (m *ResultsViewModel) loadResources() tea.Cmd {
	return func() tea.Msg {
		resources, err := queryInventoryResources(m.database, resultsPageSize)
		return types.ResourcesLoadedMsg{
			Resources: resources,
			Total:     len(resources),
			Page:      1,
			Error:     err,
		}
	}
}

func (m *ResultsViewModel) subtitle() string {
	if m.mode == "correlate" {
		return fmt.Sprintf("Cross-cloud analysis inventory - %d resources", m.total)
	}
	return fmt.Sprintf("Resource inventory - %d resources", m.total)
}

func (m *ResultsViewModel) renderTable(width int) []string {
	if len(m.resources) == 0 {
		return []string{styles.SubtitleStyle.Render("No resources found")}
	}

	cols := resultColumns(width)
	header := fmt.Sprintf("%-*s  %-*s  %-*s  %-*s  %-*s",
		cols.provider, "Provider",
		cols.service, "Service",
		cols.resourceType, "Type",
		cols.name, "Name",
		cols.location, "Location")

	lines := []string{styles.TableHeaderStyle.Render(header)}
	visible := m.visibleRows()
	end := m.offset + visible
	if end > len(m.resources) {
		end = len(m.resources)
	}

	for i := m.offset; i < end; i++ {
		resource := m.resources[i]
		line := fmt.Sprintf("%-*s  %-*s  %-*s  %-*s  %-*s",
			cols.provider, truncateCell(resource.Provider, cols.provider),
			cols.service, truncateCell(resource.Service, cols.service),
			cols.resourceType, truncateCell(resource.Type, cols.resourceType),
			cols.name, truncateCell(resource.Name, cols.name),
			cols.location, truncateCell(resource.Region, cols.location))
		if i == m.selected {
			line = styles.SelectedRowStyle.Render(line)
		}
		lines = append(lines, line)
	}

	if len(m.resources) > visible {
		lines = append(lines, styles.SubtitleStyle.Render(fmt.Sprintf("%d-%d of %d", m.offset+1, end, len(m.resources))))
	}
	return lines
}

func (m *ResultsViewModel) renderSelected(resource types.Resource, width int) string {
	id := resource.ID
	if id == "" {
		id = "(no id)"
	}
	lines := []string{
		styles.InfoStyle.Render("Selected"),
		fmt.Sprintf("  %s", truncateCell(id, width-4)),
	}
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (m *ResultsViewModel) selectedResource() *types.Resource {
	if m.selected < 0 || m.selected >= len(m.resources) {
		return nil
	}
	return &m.resources[m.selected]
}

func (m *ResultsViewModel) moveSelection(delta int) {
	if len(m.resources) == 0 {
		m.selected = 0
		m.offset = 0
		return
	}
	m.selected = clampInt(m.selected+delta, 0, len(m.resources)-1)
	m.ensureSelectionVisible()
}

func (m *ResultsViewModel) ensureSelectionVisible() {
	if len(m.resources) == 0 {
		m.selected = 0
		m.offset = 0
		return
	}
	visible := m.visibleRows()
	if visible <= 0 {
		visible = 1
	}
	if m.selected < m.offset {
		m.offset = m.selected
	}
	if m.selected >= m.offset+visible {
		m.offset = m.selected - visible + 1
	}
	maxOffset := len(m.resources) - visible
	if maxOffset < 0 {
		maxOffset = 0
	}
	m.offset = clampInt(m.offset, 0, maxOffset)
}

func (m *ResultsViewModel) visibleRows() int {
	visible := m.state.Height - 12
	if visible < 3 {
		return 3
	}
	return visible
}

type resultColumnWidths struct {
	provider     int
	service      int
	resourceType int
	name         int
	location     int
}

func resultColumns(width int) resultColumnWidths {
	remaining := width - 10
	cols := resultColumnWidths{
		provider:     12,
		service:      14,
		resourceType: 22,
		location:     16,
	}
	cols.name = remaining - cols.provider - cols.service - cols.resourceType - cols.location
	if cols.name < 16 {
		cols.name = 16
	}
	return cols
}

func queryInventoryResources(database interface{}, limit int) ([]types.Resource, error) {
	queryer, ok := database.(databaseQueryer)
	if !ok || queryer == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = resultsPageSize
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	resources := make([]types.Resource, 0, limit)
	for _, source := range inventoryTableSources {
		if len(resources) >= limit {
			break
		}
		tableResources, err := queryInventoryTable(ctx, queryer, source, limit-len(resources))
		if err != nil {
			continue
		}
		resources = append(resources, tableResources...)
	}
	return resources, nil
}

func queryInventoryTable(ctx context.Context, queryer databaseQueryer, source resourceTableSource, limit int) ([]types.Resource, error) {
	columns, err := queryColumns(ctx, queryer, source.table)
	if err != nil {
		return nil, err
	}
	if !columns["id"] {
		return nil, fmt.Errorf("%s missing id column", source.table)
	}

	providerExpr := sqlStringLiteral(source.provider)
	if columns["provider"] {
		providerExpr = "provider"
	}

	query := fmt.Sprintf(`
		SELECT %s, %s, %s, %s, %s, id
		FROM %s
		%s
		LIMIT ?
	`,
		providerExpr,
		columnExpr(columns, "service", ""),
		columnExpr(columns, "type", ""),
		columnExpr(columns, "name", "id"),
		columnExpr(columns, source.locationColumn, ""),
		source.table,
		orderClause(columns))

	rows, err := queryer.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var resources []types.Resource
	for rows.Next() {
		var provider, service, resourceType, name, location, id sql.NullString
		if err := rows.Scan(&provider, &service, &resourceType, &name, &location, &id); err != nil {
			continue
		}
		resources = append(resources, types.Resource{
			ID:       nullString(id),
			Provider: nullString(provider),
			Service:  nullString(service),
			Type:     nullString(resourceType),
			Name:     nullString(name),
			Region:   nullString(location),
		})
	}
	return resources, rows.Err()
}

func columnExpr(columns map[string]bool, preferred, fallback string) string {
	if preferred != "" && columns[preferred] {
		return preferred
	}
	if fallback != "" && columns[fallback] {
		return fallback
	}
	return "''"
}

func orderClause(columns map[string]bool) string {
	for _, column := range []string{"scanned_at", "discovered_at", "updated_at", "created_at"} {
		if columns[column] {
			return "ORDER BY " + column + " DESC"
		}
	}
	if columns["name"] {
		return "ORDER BY name ASC"
	}
	return "ORDER BY id ASC"
}

func sqlStringLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func nullString(value sql.NullString) string {
	if !value.Valid {
		return ""
	}
	return value.String
}

func truncateCell(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(value) <= width {
		return value
	}
	runes := []rune(value)
	for len(runes) > 0 && lipgloss.Width(string(runes))+1 > width {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + "…"
}

func clampInt(value, minValue, maxValue int) int {
	if maxValue < minValue {
		return minValue
	}
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}
