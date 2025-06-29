# Corkscrew TUI Architecture Plan

## Overview

This plan outlines the implementation of enhanced Terminal User Interface (TUI) components for Corkscrew using Bubble Tea, Lip Gloss, and the broader Charm ecosystem. The goal is to create an intuitive, interactive experience that simplifies cloud resource scanning and analysis while maintaining power-user capabilities.

## Current State Analysis

Corkscrew already has:
- ✅ **Bubble Tea v1.1.0** - Latest stable version
- ✅ **Lip Gloss v0.13.0** - Current styling library
- ✅ **Bubbles v0.20.0** - Pre-built components
- ✅ **Existing diagram viewer** - Sophisticated TUI implementation in `pkg/diagrams/pkg/ui/model.go`

## Architecture Overview

### Core TUI Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                    Corkscrew TUI Application                │
├─────────────────────────────────────────────────────────────┤
│  Main Application Controller (MVU Pattern)                 │
├─────────────────────────────────────────────────────────────┤
│  View Router & State Management                            │
├─────────────────────────────────────────────────────────────┤
│ ┌─────────────┐ ┌─────────────┐ ┌─────────────┐ ┌─────────┐ │
│ │ Scan View   │ │ Results     │ │ Config      │ │ Diagram │ │
│ │ - Progress  │ │ - Tables    │ │ - Editor    │ │ - Viewer│ │
│ │ - Status    │ │ - Filters   │ │ - Wizard    │ │ - ASCII │ │
│ │ - Controls  │ │ - Details   │ │ - Validator │ │ - Graphs│ │
│ └─────────────┘ └─────────────┘ └─────────────┘ └─────────┘ │
├─────────────────────────────────────────────────────────────┤
│ Shared Components (Status Bar, Navigation, Help)           │
├─────────────────────────────────────────────────────────────┤
│ Bubble Tea Core (Event Loop, Rendering, Commands)          │
└─────────────────────────────────────────────────────────────┘
```

## Implementation Plan

### Phase 1: Foundation Components (Week 1-2)

#### 1.1 Core Application Structure

**File: `internal/tui/app.go`**
```go
type CorkscrewApp struct {
    // Core components
    currentView ViewType
    database    *db.GraphLoader
    scanner     *orchestrator.Orchestrator
    config      *config.Config
    
    // View models
    mainMenu    *MainMenuModel
    scanView    *ScanViewModel
    resultsView *ResultsViewModel
    configView  *ConfigViewModel
    diagramView *DiagramViewModel
    
    // Shared components
    statusBar   *StatusBarModel
    navigation  *NavigationModel
    help        help.Model
    
    // State
    width, height int
    scanning      bool
    lastError     error
}
```

#### 1.2 View Router

**File: `internal/tui/router.go`**
```go
type ViewType int

const (
    ViewMain ViewType = iota
    ViewScan
    ViewResults
    ViewConfig
    ViewDiagrams
    ViewCompliance
    ViewQuery
)

func (app *CorkscrewApp) switchView(view ViewType) tea.Cmd
func (app *CorkscrewApp) routeUpdate(msg tea.Msg) tea.Cmd
```

#### 1.3 Shared Components

**Status Bar Component**
```go
// File: internal/tui/components/statusbar.go
type StatusBarModel struct {
    scanning     bool
    scanProgress float64
    resourceCount int
    errors       []error
    currentTime  time.Time
}

func (m StatusBarModel) View() string {
    leftSection := fmt.Sprintf("Resources: %d", m.resourceCount)
    centerSection := m.scanStatusView()
    rightSection := m.currentTime.Format("15:04:05")
    
    return lipgloss.JoinHorizontal(
        lipgloss.Top,
        lipgloss.NewStyle().Width(20).Render(leftSection),
        lipgloss.NewStyle().Width(40).Align(lipgloss.Center).Render(centerSection),
        lipgloss.NewStyle().Width(20).Align(lipgloss.Right).Render(rightSection),
    )
}
```

#### 1.4 Color Scheme & Styling

**File: `internal/tui/styles/theme.go`**
```go
var (
    // Color palette
    PrimaryColor    = lipgloss.Color("#7C3AED")  // Purple
    SecondaryColor  = lipgloss.Color("#06B6D4")  // Cyan  
    AccentColor     = lipgloss.Color("#F59E0B")  // Amber
    SuccessColor    = lipgloss.Color("#10B981")  // Green
    ErrorColor      = lipgloss.Color("#EF4444")  // Red
    WarningColor    = lipgloss.Color("#F59E0B")  // Amber
    
    // Base styles
    BaseStyle = lipgloss.NewStyle().
        Foreground(lipgloss.Color("#FAFAFA")).
        Background(lipgloss.Color("#1A1A1A"))
        
    TitleStyle = lipgloss.NewStyle().
        Foreground(PrimaryColor).
        Bold(true).
        MarginBottom(1)
        
    // Component styles
    TableHeaderStyle = lipgloss.NewStyle().
        Foreground(lipgloss.Color("#FFFFFF")).
        Background(PrimaryColor).
        Bold(true).
        Padding(0, 1)
        
    SelectedRowStyle = lipgloss.NewStyle().
        Foreground(lipgloss.Color("#FFFFFF")).
        Background(SecondaryColor).
        Bold(true)
)
```

### Phase 2: Main Menu & Navigation (Week 2-3)

#### 2.1 Interactive Main Menu

**File: `internal/tui/views/main_menu.go`**
```go
type MainMenuModel struct {
    list         list.Model
    selectedItem string
    database     *db.GraphLoader
    config       *config.Config
}

type menuItem struct {
    title       string
    description string
    action      string
    icon        string
    enabled     bool
}

func NewMainMenuModel() MainMenuModel {
    items := []list.Item{
        menuItem{
            title:       "🔍 Quick Scan",
            description: "Scan default providers with current configuration",
            action:      "quick-scan",
            enabled:     true,
        },
        menuItem{
            title:       "⚙️  Configure Scan",
            description: "Set up providers, regions, and services",
            action:      "configure",
            enabled:     true,
        },
        menuItem{
            title:       "📊 View Results",
            description: "Browse and analyze previous scan results",
            action:      "results",
            enabled:     true,
        },
        menuItem{
            title:       "🔗 Cross-Cloud Analysis",
            description: "Find correlations between cloud providers",
            action:      "correlate",
            enabled:     true,
        },
        menuItem{
            title:       "📋 Compliance Check",
            description: "Run compliance packs and security audits",
            action:      "compliance",
            enabled:     true,
        },
        menuItem{
            title:       "🗃️  Query Builder",
            description: "Interactive SQL query builder",
            action:      "query",
            enabled:     true,
        },
        menuItem{
            title:       "📈 Diagrams",
            description: "Generate architecture diagrams",
            action:      "diagrams",
            enabled:     true,
        },
        menuItem{
            title:       "⚙️  Settings",
            description: "Configure Corkscrew settings",
            action:      "settings",
            enabled:     true,
        },
    }
    
    l := list.New(items, menuItemDelegate{}, defaultWidth, listHeight)
    l.Title = "Corkscrew - Cloud Resource Scanner"
    l.SetShowStatusBar(false)
    l.SetFilteringEnabled(false)
    
    return MainMenuModel{list: l}
}
```

#### 2.2 Breadcrumb Navigation

**File: `internal/tui/components/breadcrumb.go`**
```go
type BreadcrumbModel struct {
    path []string
}

func (m BreadcrumbModel) View() string {
    if len(m.path) == 0 {
        return ""
    }
    
    var segments []string
    for i, segment := range m.path {
        style := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
        if i == len(m.path)-1 {
            style = style.Foreground(PrimaryColor).Bold(true)
        }
        segments = append(segments, style.Render(segment))
    }
    
    return lipgloss.JoinHorizontal(
        lipgloss.Left,
        strings.Join(segments, " > "),
    )
}
```

### Phase 3: Scan Management TUI (Week 3-4)

#### 3.1 Interactive Scan Configuration

**File: `internal/tui/views/scan_config.go`**
```go
type ScanConfigModel struct {
    form            FormModel
    selectedTab     int
    tabs            []string
    providers       map[string]ProviderConfig
    presets         []ScanPreset
    validation      ValidationResult
}

type ScanPreset struct {
    Name        string
    Description string
    Providers   []string
    Services    map[string][]string
    Estimated   time.Duration
}

var defaultPresets = []ScanPreset{
    {
        Name:        "Security Audit",
        Description: "Scan security-related services across all providers",
        Providers:   []string{"aws", "azure", "gcp"},
        Services: map[string][]string{
            "aws":   {"iam", "kms", "s3", "ec2", "securityhub"},
            "azure": {"keyvault", "security", "storage"},
            "gcp":   {"iam", "kms", "storage"},
        },
        Estimated: 5 * time.Minute,
    },
    {
        Name:        "Cost Optimization",
        Description: "Find unused and underutilized resources",
        Providers:   []string{"aws", "azure", "gcp"},
        Services: map[string][]string{
            "aws":   {"ec2", "rds", "s3", "ebs", "elb"},
            "azure": {"compute", "storage", "sql"},
            "gcp":   {"compute", "storage", "sql"},
        },
        Estimated: 8 * time.Minute,
    },
    {
        Name:        "Network Discovery",
        Description: "Map network topology and connections",
        Providers:   []string{"aws", "azure", "gcp"},
        Services: map[string][]string{
            "aws":   {"vpc", "ec2", "elb", "route53"},
            "azure": {"network", "compute"},
            "gcp":   {"compute", "network"},
        },
        Estimated: 6 * time.Minute,
    },
}
```

#### 3.2 Real-time Scan Progress

**File: `internal/tui/views/scan_progress.go`**
```go
type ScanProgressModel struct {
    scanner          *orchestrator.Orchestrator
    overallProgress  progress.Model
    regionProgress   map[string]progress.Model
    serviceProgress  map[string]progress.Model
    logs             []LogEntry
    viewport         viewport.Model
    ticker           *time.Ticker
    
    // Stats
    startTime        time.Time
    resourcesFound   int
    errorsCount      int
    currentOperation string
}

type LogEntry struct {
    Timestamp time.Time
    Level     string
    Message   string
    Provider  string
    Service   string
}

func (m ScanProgressModel) View() string {
    header := m.renderHeader()
    overallProgress := m.renderOverallProgress()
    regionDetails := m.renderRegionProgress()
    serviceDetails := m.renderServiceProgress()
    logsSection := m.renderLogs()
    
    return lipgloss.JoinVertical(
        lipgloss.Left,
        header,
        overallProgress,
        lipgloss.JoinHorizontal(
            lipgloss.Top,
            regionDetails,
            serviceDetails,
        ),
        logsSection,
    )
}

func (m ScanProgressModel) renderOverallProgress() string {
    elapsed := time.Since(m.startTime)
    eta := m.calculateETA()
    
    progressBar := m.overallProgress.View()
    stats := fmt.Sprintf("Resources: %d | Errors: %d | Elapsed: %v | ETA: %v",
        m.resourcesFound, m.errorsCount, elapsed.Round(time.Second), eta.Round(time.Second))
    
    return lipgloss.JoinVertical(
        lipgloss.Left,
        progressBar,
        lipgloss.NewStyle().Foreground(lipgloss.Color("#888888")).Render(stats),
    )
}
```

#### 3.3 Service & Region Selection UI

**File: `internal/tui/components/service_selector.go`**
```go
type ServiceSelectorModel struct {
    providers       []ProviderModel
    selectedProvider int
    selectedService  int
    tree            TreeModel
    searchFilter    textinput.Model
    showDescriptions bool
}

type ProviderModel struct {
    Name         string
    Enabled      bool
    Services     []ServiceModel
    Regions      []RegionModel
    EstimatedTime time.Duration
}

type ServiceModel struct {
    Name        string
    Enabled     bool
    Description string
    Category    string
    Estimated   time.Duration
    Resources   int // from previous scans
}

func (m ServiceSelectorModel) View() string {
    providersList := m.renderProvidersList()
    servicesList := m.renderServicesList()
    regionsSelector := m.renderRegionsSelector()
    summary := m.renderSelectionSummary()
    
    return lipgloss.JoinHorizontal(
        lipgloss.Top,
        lipgloss.NewStyle().Width(25).Render(providersList),
        lipgloss.NewStyle().Width(40).Render(servicesList),
        lipgloss.NewStyle().Width(25).Render(regionsSelector),
        lipgloss.NewStyle().Width(30).Render(summary),
    )
}
```

### Phase 4: Results Browser (Week 4-5)

#### 4.1 Resource Table with Advanced Filtering

**File: `internal/tui/views/results_browser.go`**
```go
type ResultsBrowserModel struct {
    table           table.Model
    filters         FiltersModel
    searchInput     textinput.Model
    sortColumn      string
    sortDirection   SortDirection
    selectedResource *Resource
    detailsViewport viewport.Model
    
    // Data
    resources       []Resource
    filteredResources []Resource
    totalCount      int
    currentPage     int
    pageSize        int
}

type FiltersModel struct {
    providerFilter   []string
    typeFilter       []string
    regionFilter     []string
    serviceFilter    []string
    tagFilters       map[string]string
    dateRange        DateRange
    customFilters    []CustomFilter
}

func (m ResultsBrowserModel) View() string {
    header := m.renderHeader()
    filtersBar := m.filters.View()
    searchBar := m.renderSearchBar()
    
    mainContent := lipgloss.JoinHorizontal(
        lipgloss.Top,
        lipgloss.NewStyle().Width(60).Render(m.table.View()),
        lipgloss.NewStyle().Width(40).Render(m.renderDetails()),
    )
    
    pagination := m.renderPagination()
    
    return lipgloss.JoinVertical(
        lipgloss.Left,
        header,
        filtersBar,
        searchBar,
        mainContent,
        pagination,
    )
}

func (m *ResultsBrowserModel) applyFilters() {
    filtered := make([]Resource, 0)
    
    for _, resource := range m.resources {
        if m.matchesFilters(resource) {
            filtered = append(filtered, resource)
        }
    }
    
    // Apply search
    if m.searchInput.Value() != "" {
        filtered = m.applySearch(filtered, m.searchInput.Value())
    }
    
    // Apply sorting
    filtered = m.applySorting(filtered)
    
    m.filteredResources = filtered
    m.updateTable()
}
```

#### 4.2 Resource Details Panel

**File: `internal/tui/components/resource_details.go`**
```go
type ResourceDetailsModel struct {
    resource     *Resource
    viewport     viewport.Model
    tabs         []string
    selectedTab  int
    relationships []Relationship
    compliance   []ComplianceResult
    costs        *CostAnalysis
}

func (m ResourceDetailsModel) View() string {
    if m.resource == nil {
        return lipgloss.NewStyle().
            Foreground(lipgloss.Color("#888888")).
            Italic(true).
            Render("Select a resource to view details")
    }
    
    header := m.renderResourceHeader()
    tabs := m.renderTabs()
    content := m.renderTabContent()
    
    return lipgloss.JoinVertical(
        lipgloss.Left,
        header,
        tabs,
        content,
    )
}

func (m ResourceDetailsModel) renderTabContent() string {
    switch m.tabs[m.selectedTab] {
    case "Overview":
        return m.renderOverview()
    case "Attributes":
        return m.renderAttributes()
    case "Relationships":
        return m.renderRelationships()
    case "Compliance":
        return m.renderCompliance()
    case "Cost":
        return m.renderCostAnalysis()
    case "Raw JSON":
        return m.renderRawData()
    default:
        return ""
    }
}
```

### Phase 5: Query Builder TUI (Week 5-6)

#### 5.1 Interactive SQL Builder

**File: `internal/tui/views/query_builder.go`**
```go
type QueryBuilderModel struct {
    mode            QueryMode
    schemaExplorer  SchemaExplorerModel
    queryEditor     textarea.Model
    resultTable     table.Model
    queryHistory    []HistoryEntry
    templates       []QueryTemplate
    
    // Current query state
    currentQuery    string
    queryResult     QueryResult
    executing       bool
    executionTime   time.Duration
}

type QueryMode int
const (
    ModeBuilder QueryMode = iota
    ModeSQL
    ModeTemplate
)

type QueryTemplate struct {
    Name        string
    Description string
    Category    string
    Query       string
    Parameters  []Parameter
}

var builtinTemplates = []QueryTemplate{
    {
        Name:        "Security Overview",
        Description: "Show security-related resources and their status",
        Category:    "Security",
        Query: `
SELECT 
    provider,
    type,
    COUNT(*) as count,
    COUNT(CASE WHEN json_extract_string(attributes, '$.Public') = 'true' THEN 1 END) as public_count
FROM all_resources 
WHERE type IN ('AWS::S3::Bucket', 'AWS::RDS::DBInstance', 'AWS::EC2::Instance')
GROUP BY provider, type
ORDER BY public_count DESC`,
    },
    {
        Name:        "Cost Analysis by Service",
        Description: "Analyze resource distribution by service for cost estimation",
        Category:    "Cost",
        Query: `
SELECT 
    provider,
    service,
    COUNT(*) as resource_count,
    COUNT(DISTINCT region) as regions
FROM all_resources
GROUP BY provider, service
ORDER BY resource_count DESC`,
    },
}

func (m QueryBuilderModel) View() string {
    var content string
    
    switch m.mode {
    case ModeBuilder:
        content = m.renderBuilderMode()
    case ModeSQL:
        content = m.renderSQLMode()
    case ModeTemplate:
        content = m.renderTemplateMode()
    }
    
    return lipgloss.JoinVertical(
        lipgloss.Left,
        m.renderHeader(),
        m.renderModeSelector(),
        content,
        m.renderResults(),
    )
}
```

#### 5.2 Schema Explorer

**File: `internal/tui/components/schema_explorer.go`**
```go
type SchemaExplorerModel struct {
    tree            TreeModel
    selectedTable   string
    selectedColumn  string
    tableInfo       TableInfo
    sampleData      []map[string]interface{}
    searchFilter    textinput.Model
}

type TableInfo struct {
    Name        string
    Description string
    Columns     []ColumnInfo
    RowCount    int64
    SampleQuery string
}

type ColumnInfo struct {
    Name        string
    Type        string
    Description string
    Nullable    bool
    Examples    []string
}

func (m SchemaExplorerModel) View() string {
    treeView := m.renderSchemaTree()
    detailsView := m.renderTableDetails()
    samplesView := m.renderSampleData()
    
    return lipgloss.JoinHorizontal(
        lipgloss.Top,
        lipgloss.NewStyle().Width(30).Render(treeView),
        lipgloss.NewStyle().Width(35).Render(detailsView),
        lipgloss.NewStyle().Width(35).Render(samplesView),
    )
}
```

### Phase 6: Configuration Editor (Week 6-7)

#### 6.1 Interactive Configuration Wizard

**File: `internal/tui/views/config_wizard.go`**
```go
type ConfigWizardModel struct {
    currentStep   int
    steps         []WizardStep
    config        *config.Config
    validation    ValidationResult
    testResults   map[string]TestResult
    
    // Step-specific models
    providerStep  ProviderStepModel
    authStep      AuthStepModel
    servicesStep  ServicesStepModel
    advancedStep  AdvancedStepModel
}

type WizardStep struct {
    Name        string
    Title       string
    Description string
    Required    bool
    Validator   func(*config.Config) ValidationResult
}

type ProviderStepModel struct {
    providers     []ProviderOption
    selectedIndex int
    testingConn   bool
    testResults   map[string]ConnectionTestResult
}

type ProviderOption struct {
    Name        string
    DisplayName string
    Description string
    Enabled     bool
    Available   bool
    AuthMethod  string
    RequiredEnv []string
}

func (m ConfigWizardModel) View() string {
    header := m.renderWizardHeader()
    progress := m.renderStepProgress()
    stepContent := m.renderCurrentStep()
    navigation := m.renderWizardNavigation()
    
    return lipgloss.JoinVertical(
        lipgloss.Left,
        header,
        progress,
        stepContent,
        navigation,
    )
}
```

#### 6.2 YAML Configuration Editor

**File: `internal/tui/components/config_editor.go`**
```go
type ConfigEditorModel struct {
    editor          textarea.Model
    originalConfig  string
    currentConfig   string
    validation      ValidationResult
    syntaxHighlight bool
    lineNumbers     bool
    modified        bool
    
    // Side panel
    outline         OutlineModel
    help           help.Model
}

type OutlineModel struct {
    sections       []ConfigSection
    selectedIndex  int
    expandedSections map[string]bool
}

type ConfigSection struct {
    Name        string
    Line        int
    Level       int
    Description string
    Children    []ConfigSection
}

func (m ConfigEditorModel) View() string {
    editorView := m.renderEditor()
    outlineView := m.outline.View()
    validationView := m.renderValidation()
    
    return lipgloss.JoinHorizontal(
        lipgloss.Top,
        lipgloss.NewStyle().Width(70).Render(
            lipgloss.JoinVertical(
                lipgloss.Left,
                editorView,
                validationView,
            ),
        ),
        lipgloss.NewStyle().Width(30).Render(outlineView),
    )
}
```

### Phase 7: Advanced Visualizations (Week 7-8)

#### 7.1 Enhanced Diagram Viewer (Extend Existing)

**File: `internal/tui/views/diagram_viewer_enhanced.go`**
```go
// Extend the existing pkg/diagrams/pkg/ui/model.go
type EnhancedDiagramModel struct {
    // Inherit from existing DiagramModel
    baseDiagramModel.Model
    
    // Additional features
    minimap         MinimapModel
    searchOverlay   SearchOverlayModel
    layerSelector   LayerSelectorModel
    exportOptions   ExportOptionsModel
    
    // Interaction state
    multiSelect     bool
    selectedNodes   []string
    highlightPath   []string
    animationState  AnimationState
}

type MinimapModel struct {
    viewport     Rectangle
    fullDiagram  Rectangle
    visible      bool
    scale        float64
}

type LayerSelectorModel struct {
    layers          []DiagramLayer
    visibleLayers   map[string]bool
    selectedLayer   string
}

type DiagramLayer struct {
    Name        string
    Description string
    NodeType    string
    Color       lipgloss.Color
    Enabled     bool
}

func (m EnhancedDiagramModel) renderDiagramWithLayers() string {
    diagram := m.baseDiagramModel.View()
    
    if m.minimap.visible {
        diagram = m.overlayMinimap(diagram)
    }
    
    if m.searchOverlay.active {
        diagram = m.overlaySearch(diagram)
    }
    
    return diagram
}
```

#### 7.2 Network Topology Viewer

**File: `internal/tui/views/network_topology.go`**
```go
type NetworkTopologyModel struct {
    topology        NetworkGraph
    layout          LayoutAlgorithm
    renderMode      RenderMode
    viewport        ViewportModel
    filters         TopologyFilters
    selectedNode    string
    highlightPath   []string
    
    // Interaction
    panOffset       Point
    zoomLevel       float64
    following       string // Follow a specific node
}

type NetworkGraph struct {
    Nodes []NetworkNode
    Edges []NetworkEdge
}

type NetworkNode struct {
    ID           string
    Type         string
    Provider     string
    Name         string
    Properties   map[string]interface{}
    Position     Point
    Size         Size
    Color        lipgloss.Color
    Icon         string
}

type RenderMode int
const (
    RenderASCII RenderMode = iota
    RenderUnicode
    RenderBraille
)

func (m NetworkTopologyModel) View() string {
    header := m.renderTopologyHeader()
    
    mainView := lipgloss.JoinHorizontal(
        lipgloss.Top,
        lipgloss.NewStyle().Width(60).Render(m.renderTopology()),
        lipgloss.NewStyle().Width(40).Render(m.renderDetails()),
    )
    
    controls := m.renderControls()
    
    return lipgloss.JoinVertical(
        lipgloss.Left,
        header,
        mainView,
        controls,
    )
}
```

#### 7.3 Compliance Dashboard

**File: `internal/tui/views/compliance_dashboard.go`**
```go
type ComplianceDashboardModel struct {
    overviewChart   ChartModel
    controlsList    table.Model
    failuresList    table.Model
    trendsChart     TrendChartModel
    selectedPack    string
    selectedControl string
    
    // Data
    complianceData  ComplianceData
    filters         ComplianceFilters
    refreshing      bool
}

type ComplianceData struct {
    OverallScore    float64
    PackResults     map[string]PackResult
    TrendData       []TrendPoint
    FailedResources []FailedResource
}

type ChartModel struct {
    chartType   ChartType
    data        []ChartDataPoint
    width       int
    height      int
    colors      []lipgloss.Color
}

type ChartType int
const (
    ChartBar ChartType = iota
    ChartPie
    ChartLine
    ChartGauge
)

func (m ComplianceDashboardModel) View() string {
    overview := m.renderOverviewSection()
    charts := m.renderChartsSection()
    details := m.renderDetailsSection()
    
    return lipgloss.JoinVertical(
        lipgloss.Left,
        overview,
        lipgloss.JoinHorizontal(
            lipgloss.Top,
            lipgloss.NewStyle().Width(60).Render(charts),
            lipgloss.NewStyle().Width(40).Render(details),
        ),
    )
}

func (m ComplianceDashboardModel) renderOverviewSection() string {
    scoreGauge := m.renderComplianceGauge()
    summary := m.renderComplianceSummary()
    
    return lipgloss.JoinHorizontal(
        lipgloss.Top,
        scoreGauge,
        summary,
    )
}
```

### Phase 8: Integration & Polish (Week 8-9)

#### 8.1 Command Integration

**File: `cmd/corkscrew/tui.go`**
```go
func runTUIMode(args []string) {
    // Initialize TUI application
    app := tui.NewCorkscrewApp()
    
    // Configure based on CLI args
    if len(args) > 0 {
        switch args[0] {
        case "scan":
            app.StartWithScanView()
        case "results":
            app.StartWithResultsView()
        case "config":
            app.StartWithConfigView()
        default:
            app.StartWithMainMenu()
        }
    }
    
    // Start the TUI
    program := tea.NewProgram(
        app,
        tea.WithAltScreen(),
        tea.WithMouseCellMotion(),
    )
    
    if err := program.Start(); err != nil {
        fmt.Printf("Error starting TUI: %v\n", err)
        os.Exit(1)
    }
}

// Add TUI flags to existing commands
func init() {
    scanCmd.Flags().BoolP("tui", "t", false, "Launch interactive TUI")
    queryCmd.Flags().BoolP("tui", "t", false, "Launch query builder TUI")
    configCmd.Flags().BoolP("tui", "t", false, "Launch configuration wizard TUI")
}
```

#### 8.2 Keyboard Shortcuts System

**File: `internal/tui/keybindings.go`**
```go
type KeyBindings struct {
    Quit           key.Binding
    Help           key.Binding
    Back           key.Binding
    Refresh        key.Binding
    Search         key.Binding
    Filter         key.Binding
    
    // Navigation
    Up             key.Binding
    Down           key.Binding
    Left           key.Binding
    Right          key.Binding
    PageUp         key.Binding
    PageDown       key.Binding
    Home           key.Binding
    End            key.Binding
    
    // Actions
    Select         key.Binding
    Edit           key.Binding
    Delete         key.Binding
    Export         key.Binding
    
    // View switching
    MainMenu       key.Binding
    ScanView       key.Binding
    ResultsView    key.Binding
    ConfigView     key.Binding
    DiagramView    key.Binding
}

var DefaultKeyBindings = KeyBindings{
    Quit:        key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
    Help:        key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
    Back:        key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
    Refresh:     key.NewBinding(key.WithKeys("r", "f5"), key.WithHelp("r", "refresh")),
    Search:      key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search")),
    Filter:      key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "filter")),
    
    // Standard navigation
    Up:          key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
    Down:        key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
    Left:        key.NewBinding(key.WithKeys("left", "h"), key.WithHelp("←/h", "left")),
    Right:       key.NewBinding(key.WithKeys("right", "l"), key.WithHelp("→/l", "right")),
    
    // View switching (vim-like)
    MainMenu:    key.NewBinding(key.WithKeys("1", "ctrl+1"), key.WithHelp("1", "main menu")),
    ScanView:    key.NewBinding(key.WithKeys("2", "ctrl+2"), key.WithHelp("2", "scan")),
    ResultsView: key.NewBinding(key.WithKeys("3", "ctrl+3"), key.WithHelp("3", "results")),
    ConfigView:  key.NewBinding(key.WithKeys("4", "ctrl+4"), key.WithHelp("4", "config")),
    DiagramView: key.NewBinding(key.WithKeys("5", "ctrl+5"), key.WithHelp("5", "diagrams")),
}
```

#### 8.3 Context-Aware Help System

**File: `internal/tui/components/contextual_help.go`**
```go
type ContextualHelpModel struct {
    help        help.Model
    currentView ViewType
    keyBindings interface{}
    tips        []HelpTip
    expanded    bool
}

type HelpTip struct {
    Context     string
    Title       string
    Description string
    KeyBinding  string
}

var contextualTips = map[ViewType][]HelpTip{
    ViewMain: {
        {
            Context:     "navigation",
            Title:       "Quick Navigation",
            Description: "Use number keys 1-5 to quickly switch between views",
            KeyBinding:  "1-5",
        },
        {
            Context:     "scanning",
            Title:       "Quick Scan",
            Description: "Press Enter on 'Quick Scan' to start with default settings",
            KeyBinding:  "enter",
        },
    },
    ViewScan: {
        {
            Context:     "providers",
            Title:       "Provider Selection",
            Description: "Use space to toggle providers on/off",
            KeyBinding:  "space",
        },
        {
            Context:     "presets",
            Title:       "Scan Presets",
            Description: "Press 'p' to choose from predefined scan configurations",
            KeyBinding:  "p",
        },
    },
    ViewResults: {
        {
            Context:     "filtering",
            Title:       "Quick Filters",
            Description: "Press 'f' to open filter panel, '/' to search",
            KeyBinding:  "f, /",
        },
        {
            Context:     "export",
            Title:       "Export Results",
            Description: "Press 'e' to export current view to various formats",
            KeyBinding:  "e",
        },
    },
}
```

## Technical Implementation Details

### Database Integration

**Async Query Execution**
```go
// File: internal/tui/database/async_queries.go
func (db *TUIDatabase) ExecuteQueryAsync(query string) tea.Cmd {
    return func() tea.Msg {
        ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
        defer cancel()
        
        start := time.Now()
        rows, err := db.QueryContext(ctx, query)
        duration := time.Since(start)
        
        if err != nil {
            return QueryErrorMsg{Error: err, Duration: duration}
        }
        
        result, err := db.parseRows(rows)
        return QueryResultMsg{Result: result, Duration: duration, Error: err}
    }
}
```

**Progressive Loading**
```go
func (db *TUIDatabase) LoadResourcesPaged(offset, limit int, filters ResourceFilter) tea.Cmd {
    return func() tea.Msg {
        query := db.buildResourceQuery(filters, offset, limit)
        resources, totalCount, err := db.executeResourceQuery(query)
        
        return ResourcePageMsg{
            Resources:   resources,
            Offset:      offset,
            Limit:       limit,
            TotalCount:  totalCount,
            HasMore:     offset+limit < totalCount,
            Error:       err,
        }
    }
}
```

### Performance Optimizations

**Virtual Scrolling for Large Tables**
```go
// File: internal/tui/components/virtual_table.go
type VirtualTableModel struct {
    table.Model
    
    // Virtual scrolling
    totalRows      int
    visibleRows    int
    windowStart    int
    windowEnd      int
    rowHeight      int
    
    // Data loading
    dataLoader     DataLoader
    loadedRanges   []Range
    loading        bool
}

func (m *VirtualTableModel) loadVisibleData() tea.Cmd {
    startRow := m.windowStart
    endRow := m.windowEnd
    
    // Check if we need to load more data
    if !m.isRangeLoaded(startRow, endRow) {
        return m.dataLoader.LoadRange(startRow, endRow)
    }
    
    return nil
}
```

**Efficient Rendering**
```go
// File: internal/tui/rendering/optimized_renderer.go
type CachedRenderer struct {
    cache        map[string]RenderedContent
    maxCacheSize int
    cacheHits    int
    cacheMisses  int
}

func (r *CachedRenderer) RenderWithCache(key string, renderFunc func() string) string {
    if content, exists := r.cache[key]; exists {
        r.cacheHits++
        return content.Text
    }
    
    rendered := renderFunc()
    r.cache[key] = RenderedContent{
        Text:      rendered,
        Timestamp: time.Now(),
    }
    r.cacheMisses++
    
    // Cleanup old cache entries
    if len(r.cache) > r.maxCacheSize {
        r.evictOldEntries()
    }
    
    return rendered
}
```

## Testing Strategy

### Component Testing
```go
// File: internal/tui/views/main_menu_test.go
func TestMainMenuModel_Navigation(t *testing.T) {
    model := NewMainMenuModel()
    
    // Test down navigation
    model, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
    assert.Equal(t, 1, model.list.Index())
    
    // Test selection
    model, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
    assert.IsType(t, SwitchViewMsg{}, cmd())
}
```

### Integration Testing
```go
// File: internal/tui/integration_test.go
func TestTUIFlow_ScanToResults(t *testing.T) {
    app := NewCorkscrewApp()
    
    // Start scan
    app, _ = app.Update(StartScanMsg{Provider: "aws"})
    
    // Simulate scan completion
    app, _ = app.Update(ScanCompleteMsg{Resources: mockResources})
    
    // Switch to results view
    app, _ = app.Update(SwitchViewMsg{View: ViewResults})
    
    assert.Equal(t, ViewResults, app.currentView)
    assert.Equal(t, len(mockResources), app.resultsView.totalCount)
}
```

## Deployment Strategy

### 1. Incremental Rollout
- Phase 1: Core navigation and menu system
- Phase 2: Scan configuration and progress
- Phase 3: Results browser and basic queries
- Phase 4: Advanced features (diagrams, compliance)

### 2. Feature Flags
```go
// File: internal/tui/features/flags.go
type FeatureFlags struct {
    DiagramViewer    bool
    QueryBuilder     bool
    ComplianceDash   bool
    NetworkTopology  bool
    ConfigWizard     bool
}

func LoadFeatureFlags() FeatureFlags {
    return FeatureFlags{
        DiagramViewer:   true,
        QueryBuilder:    getBoolEnv("CORKSCREW_QUERY_BUILDER", false),
        ComplianceDash:  getBoolEnv("CORKSCREW_COMPLIANCE_UI", false),
        NetworkTopology: getBoolEnv("CORKSCREW_NETWORK_UI", false),
        ConfigWizard:    getBoolEnv("CORKSCREW_CONFIG_WIZARD", true),
    }
}
```

### 3. Fallback to CLI
```go
// Ensure TUI features gracefully fall back to CLI
func (app *CorkscrewApp) fallbackToCLI(err error) {
    fmt.Printf("TUI unavailable (%v), falling back to CLI mode\n", err)
    runCLIMode(os.Args[1:])
}
```

## Success Metrics

1. **User Adoption**: Percentage of users who use TUI vs CLI mode
2. **Task Completion**: Time to complete common tasks (scan setup, results analysis)
3. **Error Reduction**: Fewer configuration and usage errors
4. **Feature Discovery**: Usage of advanced features like cross-cloud correlation
5. **User Satisfaction**: Qualitative feedback on ease of use

## Future Enhancements

1. **Plugin System**: Allow third-party TUI components
2. **Themes**: Customizable color schemes and layouts
3. **Workspace Management**: Save and restore TUI sessions
4. **Remote Mode**: TUI client for remote Corkscrew instances
5. **Web Terminal**: Browser-based TUI for remote access

This comprehensive plan provides a roadmap for creating a sophisticated, user-friendly TUI that makes Corkscrew's powerful features accessible to users of all skill levels while maintaining the flexibility needed for advanced use cases.