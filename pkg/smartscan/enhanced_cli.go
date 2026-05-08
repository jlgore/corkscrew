package smartscan

import (
    "context"
    "encoding/csv"
    "encoding/json"
    "fmt"
    "os"
    "sort"
    "strings"
    "text/tabwriter"
    "time"

    "github.com/jlgore/corkscrew/internal/client"
    "github.com/jlgore/corkscrew/internal/db"
    pb "github.com/jlgore/corkscrew/internal/proto"
)

type EnhancedScanOptions struct {
    Provider       string
    Regions        []string
    Services       []string
    OutputFormat   string
    SaveToFile     bool
    ShowEmpty      bool
    ConfigPath     string
    MaxConcurrency int
    DatabasePath   string
    DBProviderTableOverride string
    // Filters and provider-specific init options
    Namespace      string
    LabelSelector  string
    FieldSelector  string
    KubeconfigPath string
    KubeContext    string
    IncludeRelationships bool
}

func RunEnhancedScan(ctx context.Context, options EnhancedScanOptions) error {
	// Load configuration
	config, err := LoadSmartScanConfig(options.ConfigPath)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// Validate provider
	if err := config.ValidateProvider(options.Provider); err != nil {
		return err
	}

	if !config.IsProviderEnabled(options.Provider) {
		return fmt.Errorf("provider %s is disabled in configuration", options.Provider)
	}

	// Get regions from config if not specified in command line
	regions := options.Regions
	if len(regions) == 0 {
		regions, err = config.GetRegionsForProvider(options.Provider)
		if err != nil {
			return fmt.Errorf("failed to get regions from config: %w", err)
		}
	}

	// Get services from config if not specified in command line
	services := options.Services
	if len(services) == 0 {
		services, err = config.GetServicesForProvider(options.Provider)
		if err != nil {
			return fmt.Errorf("failed to get services from config: %w", err)
		}
	}

	// Initialize plugin client
	pc, err := client.NewPluginClient(options.Provider)
	if err != nil {
		return fmt.Errorf("failed to initialize plugin client: %w", err)
	}
	defer pc.Close()

	provider, err := pc.GetProvider()
	if err != nil {
		return fmt.Errorf("failed to get provider: %w", err)
	}

	// Initialize provider
    // Build provider initialization config
    initCfg := map[string]string{
        "region": func() string {
            if len(regions) > 0 && regions[0] != "all" {
                return regions[0]
            }
            return ""
        }(),
    }
    if options.KubeconfigPath != "" {
        initCfg["kubeconfig_path"] = options.KubeconfigPath
    }
    if options.KubeContext != "" {
        initCfg["contexts"] = options.KubeContext
    }
    initReq := &pb.InitializeRequest{ Config: initCfg }
	_, err = provider.Initialize(ctx, initReq)
	if err != nil {
		return fmt.Errorf("failed to initialize provider: %w", err)
	}

	// Create smart scan config
	smartConfig := config.GetSmartScanConfig(options.Provider)
	if options.MaxConcurrency > 0 {
		smartConfig.MaxConcurrency = options.MaxConcurrency
	}

	// Override hiding empty regions/services if explicitly requested
	if options.ShowEmpty {
		smartConfig.HideEmptyRegions = false
		smartConfig.HideEmptyServices = false
	}

	// Create multi-region scanner
    // Pass include-relationships and filters via config and scanner
    smartConfig.IncludeRelationships = options.IncludeRelationships
    scanner := NewMultiRegionScanner(provider, options.Provider, smartConfig)
    scanner.SetFilters(map[string]string{
        "namespace":      options.Namespace,
        "label_selector": options.LabelSelector,
        "field_selector": options.FieldSelector,
    })

	// Print scan information
	fmt.Printf("🔍 Enhanced scan starting:\n")
	fmt.Printf("   Provider: %s\n", options.Provider)
	if len(regions) == 1 && regions[0] == "all" {
		fmt.Printf("   Regions: all available regions\n")
	} else {
		fmt.Printf("   Regions: %s (%d)\n", strings.Join(regions, ", "), len(regions))
	}
	if len(services) > 0 {
		fmt.Printf("   Services: %s (%d)\n", strings.Join(services, ", "), len(services))
	} else {
		fmt.Printf("   Services: all configured services\n")
	}
	fmt.Printf("   Concurrency: %d regions\n", smartConfig.MaxConcurrency)
	fmt.Printf("   Empty filtering: regions=%t, services=%t\n", 
		smartConfig.HideEmptyRegions, smartConfig.HideEmptyServices)

	// Execute multi-region scan
	results, err := scanner.ScanMultipleRegions(ctx, regions, services)
	if err != nil {
		return fmt.Errorf("multi-region scan failed: %w", err)
	}

	// Apply service filtering
	scanner.FilterEmptyServices(results)

	// Store results to database if database path is specified
    if options.DatabasePath != "" {
        if err := storeResultsToDatabase(results, options.DatabasePath, options.DBProviderTableOverride); err != nil {
            fmt.Printf("⚠️ Warning: Failed to store results to database: %v\n", err)
        } else {
            fmt.Printf("💾 Results stored to database: %s\n", options.DatabasePath)
        }
    } else {
        // Try to store to config database or default location
        dbPath := getDefaultDatabasePath(config)
        if dbPath != "" {
            if err := storeResultsToDatabase(results, dbPath, options.DBProviderTableOverride); err != nil {
                fmt.Printf("⚠️ Warning: Failed to store results to database: %v\n", err)
            } else {
                fmt.Printf("💾 Results stored to database: %s\n", dbPath)
            }
        }
    }

	// Print results based on output format
	switch options.OutputFormat {
	case "json":
		return printJSONResults(results)
	case "csv":
		return printCSVResults(results)
	default:
		return printTableResults(results, smartConfig)
	}
}

func printTableResults(results *AggregatedResults, config *SmartScanConfig) error {
	// Print summary
	fmt.Printf("\n✅ Enhanced scan completed!\n")
	
	if results.Summary != nil {
		fmt.Printf("📊 Summary:\n")
		fmt.Printf("   Total resources: %d\n", results.Summary.TotalResources)
		fmt.Printf("   Active regions: %d/%d\n", results.Summary.ActiveRegions, results.Summary.TotalRegions)
		fmt.Printf("   Duration: %s\n", results.Summary.TotalDuration.Round(time.Millisecond))

		if len(results.Summary.EmptyRegions) > 0 && !config.HideEmptyRegions {
			fmt.Printf("   Empty regions: %s\n", strings.Join(results.Summary.EmptyRegions, ", "))
		}
	}

	// Print region breakdown
	if len(results.RegionResults) > 0 {
		fmt.Printf("\n📍 Region breakdown:\n")
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
		fmt.Fprintln(w, "Region\tResources\tDuration\tStatus")
		fmt.Fprintln(w, "------\t---------\t--------\t------")

		// Sort regions by resource count (descending)
		type regionStat struct {
			name      string
			count     int
			duration  time.Duration
			hasErrors bool
		}

		var stats []regionStat
		for region, result := range results.RegionResults {
			stats = append(stats, regionStat{
				name:      region,
				count:     len(result.Resources),
				duration:  result.Duration,
				hasErrors: len(result.Errors) > 0,
			})
		}

		sort.Slice(stats, func(i, j int) bool {
			return stats[i].count > stats[j].count
		})

		for _, stat := range stats {
			status := "✅"
			if stat.hasErrors {
				status = "⚠️"
			}
			if stat.count == 0 {
				status = "📭"
			}

			fmt.Fprintf(w, "%s\t%d\t%s\t%s\n",
				stat.name,
				stat.count,
				stat.duration.Round(time.Millisecond),
				status)
		}
		w.Flush()
	}

	// Print service breakdown
	if results.Summary != nil && len(results.Summary.ServiceCounts) > 0 {
		fmt.Printf("\n📋 Service breakdown:\n")
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
		fmt.Fprintln(w, "Service\tResource Count\tActive Regions")
		fmt.Fprintln(w, "-------\t--------------\t--------------")

		// Sort services by resource count (descending)
		type serviceStat struct {
			name           string
			count          int32
			activeRegions  int
		}

		var serviceStats []serviceStat
		for service, count := range results.Summary.ServiceCounts {
			if count == 0 && config.HideEmptyServices {
				continue
			}

			// Count regions where this service has resources
			activeRegions := 0
			for _, regionResult := range results.RegionResults {
				if regionResult.Stats != nil {
					if regionCount, exists := regionResult.Stats.ServiceCounts[service]; exists && regionCount > 0 {
						activeRegions++
					}
				}
			}

			serviceStats = append(serviceStats, serviceStat{
				name:          service,
				count:         count,
				activeRegions: activeRegions,
			})
		}

		sort.Slice(serviceStats, func(i, j int) bool {
			return serviceStats[i].count > serviceStats[j].count
		})

		for _, stat := range serviceStats {
			fmt.Fprintf(w, "%s\t%d\t%d\n",
				stat.name,
				stat.count,
				stat.activeRegions)
		}
		w.Flush()
	}

	// Print errors if any
	if len(results.Errors) > 0 {
		fmt.Printf("\n⚠️  Issues encountered:\n")
		for _, err := range results.Errors {
			fmt.Printf("   - %s\n", err)
		}
	}

	return nil
}

func printJSONResults(results *AggregatedResults) error {
	output := struct {
		Summary       *ScanSummary                     `json:"summary"`
		RegionResults map[string]*RegionScanResult     `json:"region_results"`
		AllResources  []*pb.Resource                   `json:"all_resources"`
		Errors        []string                         `json:"errors"`
	}{
		Summary:       results.Summary,
		RegionResults: results.RegionResults,
		AllResources:  results.AllResources,
		Errors:        results.Errors,
	}

	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}

	fmt.Println(string(data))
	return nil
}

func printCSVResults(results *AggregatedResults) error {
	w := csv.NewWriter(os.Stdout)
	defer w.Flush()

	// Write header
	w.Write([]string{"Region", "Service", "ResourceType", "ResourceID", "ResourceName", "ARN"})

	// Write data
	for _, resource := range results.AllResources {
		w.Write([]string{
			resource.Region,
			resource.Service,
			resource.Type,
			resource.Id,
			resource.Name,
			resource.Arn,
		})
	}

	return nil
}

func SaveResultsToFile(results *AggregatedResults, filename string) error {
	data, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal results: %w", err)
	}

	if err := os.WriteFile(filename, data, 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

func GenerateTimestampedFilename(provider string) string {
	timestamp := time.Now().Format("20060102-150405")
	return fmt.Sprintf("enhanced-scan-%s-%s.json", provider, timestamp)
}

// storeResultsToDatabase stores scan results to the unified database
func storeResultsToDatabase(results *AggregatedResults, dbPath string, overrideTable string) error {
    // Initialize the unified database with custom path
    dbConfig, err := db.InitializeUnifiedDatabase(dbPath)
    if err != nil {
        return fmt.Errorf("failed to initialize database: %w", err)
    }
    defer dbConfig.DB.Close()

    // De-duplicate by (table,id) to avoid duplicate inserts
    seen := make(map[string]struct{})
    for _, resource := range results.AllResources {
        // Compute target table for dedupe key
        table := tableOverrideFromResource(resource)
        if strings.TrimSpace(overrideTable) != "" {
            table = strings.TrimSpace(overrideTable)
        }
        if table == "" {
            table = tableForProvider(resource.Provider)
        }
        if table == "" {
            table = "aws_resources"
        }
        key := table + "|" + resource.Id
        if _, ok := seen[key]; ok {
            continue
        }
        seen[key] = struct{}{}

        if err := storeResourceToDatabase(dbConfig, resource, overrideTable); err != nil {
            return fmt.Errorf("failed to store resource %s: %w", resource.Id, err)
        }
        if len(resource.Relationships) > 0 {
            if err := storeRelationshipsToDatabase(dbConfig, resource); err != nil {
                return fmt.Errorf("failed to store relationships for %s: %w", resource.Id, err)
            }
        }
    }

    return nil
}

// storeResourceToDatabase stores a single resource to the database
func storeResourceToDatabase(dbConfig *db.UnifiedDatabaseConfig, resource *pb.Resource, overrideTable string) error {
    // Determine target table (CLI override > attributes override > provider default)
    table := strings.TrimSpace(overrideTable)
    if table == "" {
        table = tableOverrideFromResource(resource)
    }
    if table == "" {
        table = tableForProvider(resource.Provider)
    }
    if table == "" {
        table = "aws_resources"
    }

    // Convert tags map to JSON string (nullable)
    tagsJSON := ""
    if len(resource.Tags) > 0 {
        if b, err := json.Marshal(resource.Tags); err == nil {
            tagsJSON = string(b)
        }
    }
    // Prepare JSON parameters (use NULL when empty to avoid conversion errors)
    var tagsParam interface{}
    var attrsParam interface{}
    var rawParam interface{}
    if strings.TrimSpace(tagsJSON) != "" {
        tagsParam = tagsJSON
    } else {
        tagsParam = nil
    }
    if strings.TrimSpace(resource.Attributes) != "" {
        attrsParam = resource.Attributes
    } else {
        attrsParam = nil
    }
    if strings.TrimSpace(resource.RawData) != "" {
        rawParam = resource.RawData
    } else {
        rawParam = nil
    }

    // The arn column has a UNIQUE constraint in the resource tables.
    // Some Cloud Control resource types (and a few SDK ones too) don't
    // populate an ARN — in that case every row would share arn="" and
    // collide. Fall back to the resource Id, which is already unique.
    arnValue := resource.Arn
    if strings.TrimSpace(arnValue) == "" {
        arnValue = resource.Id
    }

    // Upsert via DELETE + INSERT
    tx, txErr := dbConfig.DB.Begin()
    if txErr != nil {
        return fmt.Errorf("begin tx: %w", txErr)
    }
    if _, delErr := tx.Exec(fmt.Sprintf("DELETE FROM %s WHERE id = ?", table), resource.Id); delErr != nil {
        _ = tx.Rollback()
        return fmt.Errorf("delete existing: %w", delErr)
    }
    insertStmt := fmt.Sprintf(`
        INSERT INTO %s (
            id, arn, name, type, service, region, account_id,
            tags, attributes, raw_data, state,
            created_at, modified_at, scanned_at
        ) VALUES (
            ?, ?, ?, ?, ?, ?, ?,
            try_cast(? AS JSON), try_cast(? AS JSON), try_cast(? AS JSON), ?,
            ?, ?, CURRENT_TIMESTAMP
        )
    `, table)
    if _, insErr := tx.Exec(insertStmt,
        resource.Id,
        arnValue,
        resource.Name,
        resource.Type,
        resource.Service,
        resource.Region,
        resource.AccountId,
        tagsParam,
        attrsParam,
        rawParam,
        "active",
        resource.DiscoveredAt.AsTime(),
        resource.DiscoveredAt.AsTime(),
    ); insErr != nil {
        _ = tx.Rollback()
        return fmt.Errorf("insert: %w", insErr)
    }
    if commitErr := tx.Commit(); commitErr != nil {
        return fmt.Errorf("commit: %w", commitErr)
    }
    return nil
}

func tableForProvider(provider string) string {
    switch strings.ToLower(provider) {
    case "aws":
        return "aws_resources"
    case "azure":
        return "azure_resources"
    case "kubernetes":
        return "kubernetes_resources"
    case "gcp":
        return "gcp_resources"
    default:
        return ""
    }
}

// storeRelationshipsToDatabase persists resource relationships into cloud_relationships
func storeRelationshipsToDatabase(dbConfig *db.UnifiedDatabaseConfig, resource *pb.Resource) error {
    if len(resource.Relationships) == 0 {
        return nil
    }
    tx, err := dbConfig.DB.Begin()
    if err != nil {
        return fmt.Errorf("begin tx: %w", err)
    }
    // De-duplicate relationships per resource to avoid PK conflicts
    seen := make(map[string]struct{})
    for _, rel := range resource.Relationships {
        if rel == nil || rel.TargetId == "" || rel.RelationshipType == "" {
            continue
        }
        key := resource.Id + "|" + rel.TargetId + "|" + rel.RelationshipType
        if _, ok := seen[key]; ok {
            continue
        }
        seen[key] = struct{}{}
        // Infer target type from target ID if possible: cluster/namespace/Kind/name
        toType := ""
        parts := strings.Split(rel.TargetId, "/")
        if len(parts) >= 3 {
            toType = parts[2]
        }
        if _, insErr := tx.Exec(
            `INSERT INTO cloud_relationships (
                from_id, to_id, relationship_type, provider,
                relationship_subtype, properties,
                from_resource_type, to_resource_type, direction
            ) VALUES (
                ?, ?, ?, ?, ?, try_cast(? AS JSON), ?, ?, ?
            )`,
            resource.Id, rel.TargetId, rel.RelationshipType, strings.ToLower(resource.Provider),
            "", jsonOrNull(rel.Properties), resource.Type, toType, "outbound",
        ); insErr != nil {
            _ = tx.Rollback()
            return fmt.Errorf("insert rel: %w", insErr)
        }
    }
    if err := tx.Commit(); err != nil {
        return fmt.Errorf("commit rels: %w", err)
    }
    return nil
}

func jsonOrNull(m map[string]string) interface{} {
    if len(m) == 0 {
        return nil
    }
    b, err := json.Marshal(m)
    if err != nil {
        return nil
    }
    return string(b)
}

func coalesceStr(v string, d string) string {
    if strings.TrimSpace(v) == "" {
        return d
    }
    return v
}

// tableOverrideFromResource allows a provider to choose a custom table name by
// embedding a special key in the Attributes JSON: {"_table": "my_table"}
func tableOverrideFromResource(resource *pb.Resource) string {
    if strings.TrimSpace(resource.Attributes) == "" {
        return ""
    }
    var m map[string]interface{}
    if err := json.Unmarshal([]byte(resource.Attributes), &m); err != nil {
        return ""
    }
    if v, ok := m["_table"]; ok {
        if s, ok2 := v.(string); ok2 && s != "" {
            return s
        }
    }
    return ""
}

// getDefaultDatabasePath gets database path from config or returns default
func getDefaultDatabasePath(config *SmartScanConfiguration) string {
	if config.Database.Path != "" {
		return config.Database.Path
	}
	
	// Try to get default path
	if defaultPath, err := db.GetUnifiedDatabasePath(); err == nil {
		return defaultPath
	}
	
	return ""
}
