package main

import (
	"fmt"
	"strings"
)

// AzureDuckDBSchemas contains all the SQL DDL statements for Azure resources
type AzureDuckDBSchemas struct {
	// Main resources table schema
	AzureResourcesTable string
	
	// Relationships table schema
	AzureRelationshipsTable string
	
	// Scan metadata table schema
	ScanMetadataTable string
	
	// API action metadata table schema
	APIActionMetadataTable string
	
	// Service-specific table generation
	ServiceTables map[string]string
}

// GenerateAzureDuckDBSchemas generates all the DuckDB schemas for Azure resources
func GenerateAzureDuckDBSchemas() *AzureDuckDBSchemas {
	schemas := &AzureDuckDBSchemas{
		ServiceTables: make(map[string]string),
	}
	
	// Main Azure resources table - unified storage for all Azure resources
	schemas.AzureResourcesTable = `
CREATE TABLE IF NOT EXISTS azure_resources (
    -- Primary identifiers
    id VARCHAR PRIMARY KEY,                    -- Azure Resource ID (full path)
    name VARCHAR NOT NULL,                     -- Resource name
    type VARCHAR NOT NULL,                     -- Resource type (e.g., Microsoft.Storage/storageAccounts)
    
    -- Azure-specific identifiers
    resource_id VARCHAR,                       -- Short resource ID
    subscription_id VARCHAR NOT NULL,          -- Azure subscription ID
    resource_group VARCHAR NOT NULL,           -- Resource group name
    
    -- Location and hierarchy
    location VARCHAR NOT NULL,                 -- Azure region (e.g., centralus)
    parent_id VARCHAR,                         -- Parent resource ID for hierarchical resources
    managed_by VARCHAR,                        -- ID of resource managing this resource
    
    -- Service information
    service VARCHAR,                           -- Service name (e.g., storage, compute)
    kind VARCHAR,                              -- Resource kind (e.g., StorageV2)
    
    -- SKU information
    sku_name VARCHAR,                          -- SKU name (e.g., Standard_LRS)
    sku_tier VARCHAR,                          -- SKU tier (e.g., Standard)
    sku_size VARCHAR,                          -- SKU size
    sku_family VARCHAR,                        -- SKU family
    sku_capacity INTEGER,                      -- SKU capacity
    
    -- Metadata
    tags JSON,                                 -- Resource tags
    properties JSON,                           -- Resource-specific properties
    raw_data JSON,                             -- Complete raw resource data
    
    -- State information
    provisioning_state VARCHAR,                -- Current provisioning state
    power_state VARCHAR,                       -- Power state (for VMs)
    
    -- Timestamps
    created_time TIMESTAMP,                    -- Resource creation time
    changed_time TIMESTAMP,                    -- Last modification time
    scanned_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,  -- When we discovered this resource
    
    -- Additional metadata
    etag VARCHAR,                              -- Entity tag for optimistic concurrency
    api_version VARCHAR,                       -- API version used to fetch this resource
    
    -- Indexes for performance
    INDEX idx_type (type),
    INDEX idx_service (service),
    INDEX idx_resource_group (resource_group),
    INDEX idx_location (location),
    INDEX idx_subscription_id (subscription_id),
    INDEX idx_parent_id (parent_id),
    INDEX idx_provisioning_state (provisioning_state),
    INDEX idx_scanned_at (scanned_at)
);`

	// Azure relationships table for resource dependencies and associations
	schemas.AzureRelationshipsTable = `
CREATE TABLE IF NOT EXISTS azure_relationships (
    -- Relationship identifiers
    from_id VARCHAR NOT NULL,                  -- Source resource ID
    to_id VARCHAR NOT NULL,                    -- Target resource ID
    relationship_type VARCHAR NOT NULL,        -- Type of relationship
    
    -- Relationship metadata
    relationship_subtype VARCHAR,              -- More specific relationship type
    properties JSON,                           -- Additional relationship properties
    
    -- Azure-specific relationship data
    from_resource_type VARCHAR,                -- Source resource type
    to_resource_type VARCHAR,                  -- Target resource type
    direction VARCHAR DEFAULT 'outbound',      -- Relationship direction
    
    -- Timestamps
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    discovered_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    
    -- Constraints
    PRIMARY KEY (from_id, to_id, relationship_type),
    
    -- Indexes for graph traversal
    INDEX idx_from_id (from_id),
    INDEX idx_to_id (to_id),
    INDEX idx_relationship_type (relationship_type),
    INDEX idx_from_type (from_resource_type),
    INDEX idx_to_type (to_resource_type)
);`

	// Scan metadata table for tracking scan operations
	schemas.ScanMetadataTable = `
CREATE TABLE IF NOT EXISTS azure_scan_metadata (
    -- Scan identifiers
    id VARCHAR PRIMARY KEY,                    -- Unique scan ID
    scan_type VARCHAR NOT NULL,                -- Type of scan (full, incremental, service)
    
    -- Scan scope
    services LIST<VARCHAR>,                    -- List of services scanned
    resource_groups LIST<VARCHAR>,             -- Resource groups scanned
    locations LIST<VARCHAR>,                   -- Locations scanned
    subscription_id VARCHAR NOT NULL,          -- Subscription scanned
    
    -- Scan results
    total_resources INTEGER DEFAULT 0,         -- Total resources found
    new_resources INTEGER DEFAULT 0,           -- New resources discovered
    updated_resources INTEGER DEFAULT 0,       -- Updated resources
    deleted_resources INTEGER DEFAULT 0,       -- Resources no longer found
    failed_resources INTEGER DEFAULT 0,        -- Resources that failed to scan
    
    -- Performance metrics
    scan_start_time TIMESTAMP NOT NULL,        -- When scan started
    scan_end_time TIMESTAMP,                   -- When scan completed
    duration_ms BIGINT,                        -- Total duration in milliseconds
    
    -- Scan metadata
    initiated_by VARCHAR,                      -- User or system that initiated scan
    scan_reason VARCHAR,                       -- Reason for scan
    error_messages JSON,                       -- Any errors encountered
    warnings JSON,                             -- Any warnings
    metadata JSON,                             -- Additional scan metadata
    
    -- Status
    status VARCHAR DEFAULT 'running',          -- Scan status (running, completed, failed)
    
    -- Indexes
    INDEX idx_scan_type (scan_type),
    INDEX idx_subscription_id (subscription_id),
    INDEX idx_scan_start_time (scan_start_time),
    INDEX idx_status (status)
);`

	// API action metadata for tracking Azure API calls
	schemas.APIActionMetadataTable = `
CREATE TABLE IF NOT EXISTS azure_api_action_metadata (
    -- Action identifiers
    id VARCHAR PRIMARY KEY,                    -- Unique action ID
    correlation_id VARCHAR,                    -- Correlation ID for request tracking
    
    -- API details
    service VARCHAR NOT NULL,                  -- Azure service (e.g., compute, storage)
    resource_provider VARCHAR NOT NULL,        -- Resource provider namespace
    operation_name VARCHAR NOT NULL,           -- API operation name
    operation_type VARCHAR,                    -- Operation type (List, Get, etc.)
    api_version VARCHAR,                       -- API version used
    
    -- Execution details
    execution_time TIMESTAMP NOT NULL,         -- When the API call was made
    location VARCHAR,                          -- Azure region
    subscription_id VARCHAR,                   -- Subscription ID
    
    -- Results
    success BOOLEAN NOT NULL,                  -- Whether the operation succeeded
    status_code INTEGER,                       -- HTTP status code
    duration_ms BIGINT,                        -- Duration in milliseconds
    resource_count INTEGER DEFAULT 0,          -- Number of resources returned
    
    -- Request details
    request_method VARCHAR,                    -- HTTP method (GET, POST, etc.)
    request_path VARCHAR,                      -- API path
    request_headers JSON,                      -- Request headers (sanitized)
    request_body_size INTEGER,                 -- Size of request body
    
    -- Response details
    response_headers JSON,                     -- Response headers (sanitized)
    response_body_size INTEGER,                -- Size of response body
    
    -- Error information
    error_code VARCHAR,                        -- Azure error code
    error_message VARCHAR,                     -- Error message
    error_details JSON,                        -- Detailed error information
    
    -- Rate limiting
    rate_limit_remaining INTEGER,              -- Remaining API calls
    rate_limit_reset_time TIMESTAMP,           -- When rate limit resets
    
    -- Additional metadata
    client_request_id VARCHAR,                 -- Client request ID
    request_charge DOUBLE,                     -- Request units consumed (for Cosmos DB)
    metadata JSON,                             -- Additional metadata
    
    -- Timestamps
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    
    -- Indexes
    INDEX idx_service (service),
    INDEX idx_operation_name (operation_name),
    INDEX idx_execution_time (execution_time),
    INDEX idx_success (success),
    INDEX idx_subscription_id (subscription_id),
    INDEX idx_correlation_id (correlation_id)
);`

	// Generate service-specific tables for common Azure services
	schemas.generateServiceSpecificTables()
	
	return schemas
}

// generateServiceSpecificTables creates service-specific table schemas
func (s *AzureDuckDBSchemas) generateServiceSpecificTables() {
	// Storage Accounts table
	s.ServiceTables["storage_accounts"] = `
CREATE TABLE IF NOT EXISTS azure_storage_accounts (
    -- Identifiers
    id VARCHAR PRIMARY KEY,
    name VARCHAR NOT NULL UNIQUE,
    resource_group VARCHAR NOT NULL,
    location VARCHAR NOT NULL,
    
    -- Storage configuration
    account_kind VARCHAR,                      -- StorageV2, BlobStorage, etc.
    account_tier VARCHAR,                      -- Standard, Premium
    replication_type VARCHAR,                  -- LRS, GRS, ZRS, etc.
    access_tier VARCHAR,                       -- Hot, Cool, Archive
    
    -- Encryption settings
    encryption_key_source VARCHAR,             -- Microsoft.Storage, Microsoft.Keyvault
    encryption_enabled BOOLEAN DEFAULT true,
    
    -- Network settings
    https_traffic_only BOOLEAN DEFAULT true,
    minimum_tls_version VARCHAR,
    public_network_access VARCHAR,
    
    -- Endpoints
    primary_blob_endpoint VARCHAR,
    primary_file_endpoint VARCHAR,
    primary_queue_endpoint VARCHAR,
    primary_table_endpoint VARCHAR,
    
    -- Features
    blob_public_access BOOLEAN,
    hierarchical_namespace BOOLEAN,            -- Data Lake Gen2
    nfs_v3_enabled BOOLEAN,
    large_file_shares_enabled BOOLEAN,
    
    -- Metadata
    tags JSON,
    properties JSON,
    created_time TIMESTAMP,
    last_modified TIMESTAMP,
    
    -- Indexes
    INDEX idx_account_kind (account_kind),
    INDEX idx_replication_type (replication_type),
    INDEX idx_access_tier (access_tier)
);`

	// Virtual Machines table
	s.ServiceTables["virtual_machines"] = `
CREATE TABLE IF NOT EXISTS azure_virtual_machines (
    -- Identifiers
    id VARCHAR PRIMARY KEY,
    name VARCHAR NOT NULL,
    resource_group VARCHAR NOT NULL,
    location VARCHAR NOT NULL,
    
    -- VM configuration
    vm_size VARCHAR NOT NULL,                  -- Standard_D2s_v3, etc.
    os_type VARCHAR,                           -- Linux, Windows
    os_publisher VARCHAR,
    os_offer VARCHAR,
    os_sku VARCHAR,
    os_version VARCHAR,
    
    -- Hardware
    cpu_cores INTEGER,
    memory_gb DOUBLE,
    
    -- Storage
    os_disk_id VARCHAR,
    os_disk_type VARCHAR,                      -- Premium_LRS, Standard_LRS
    os_disk_size_gb INTEGER,
    data_disk_count INTEGER DEFAULT 0,
    
    -- Network
    primary_nic_id VARCHAR,
    network_interface_ids JSON,                -- Array of NIC IDs
    private_ip_address VARCHAR,
    public_ip_address VARCHAR,
    
    -- Availability
    availability_set_id VARCHAR,
    availability_zone VARCHAR,
    proximity_placement_group_id VARCHAR,
    
    -- State
    power_state VARCHAR,                       -- running, stopped, deallocated
    provisioning_state VARCHAR,
    
    -- Security
    security_profile JSON,
    boot_diagnostics_enabled BOOLEAN,
    
    -- Metadata
    tags JSON,
    properties JSON,
    created_time TIMESTAMP,
    last_modified TIMESTAMP,
    
    -- Indexes
    INDEX idx_vm_size (vm_size),
    INDEX idx_os_type (os_type),
    INDEX idx_power_state (power_state),
    INDEX idx_availability_set_id (availability_set_id)
);`

	// Virtual Networks table
	s.ServiceTables["virtual_networks"] = `
CREATE TABLE IF NOT EXISTS azure_virtual_networks (
    -- Identifiers
    id VARCHAR PRIMARY KEY,
    name VARCHAR NOT NULL,
    resource_group VARCHAR NOT NULL,
    location VARCHAR NOT NULL,
    
    -- Network configuration
    address_space JSON,                        -- Array of address prefixes
    dns_servers JSON,                          -- Custom DNS servers
    
    -- Subnets
    subnet_count INTEGER DEFAULT 0,
    subnets JSON,                              -- Array of subnet configurations
    
    -- Peering
    peering_count INTEGER DEFAULT 0,
    peerings JSON,                             -- Array of peering configurations
    
    -- DDoS protection
    ddos_protection_enabled BOOLEAN DEFAULT false,
    ddos_protection_plan_id VARCHAR,
    
    -- Service endpoints
    service_endpoints JSON,
    
    -- Metadata
    tags JSON,
    properties JSON,
    created_time TIMESTAMP,
    last_modified TIMESTAMP,
    
    -- Indexes
    INDEX idx_subnet_count (subnet_count),
    INDEX idx_peering_count (peering_count)
);`

	// Key Vaults table
	s.ServiceTables["key_vaults"] = `
CREATE TABLE IF NOT EXISTS azure_key_vaults (
    -- Identifiers
    id VARCHAR PRIMARY KEY,
    name VARCHAR NOT NULL UNIQUE,
    resource_group VARCHAR NOT NULL,
    location VARCHAR NOT NULL,
    
    -- Vault configuration
    vault_uri VARCHAR NOT NULL,
    tenant_id VARCHAR NOT NULL,
    sku_name VARCHAR,                          -- standard, premium
    sku_family VARCHAR DEFAULT 'A',
    
    -- Access policies
    access_policy_count INTEGER DEFAULT 0,
    access_policies JSON,                      -- Array of access policies
    enable_rbac_authorization BOOLEAN DEFAULT false,
    
    -- Features
    enabled_for_deployment BOOLEAN DEFAULT false,
    enabled_for_disk_encryption BOOLEAN DEFAULT false,
    enabled_for_template_deployment BOOLEAN DEFAULT false,
    
    -- Soft delete settings
    soft_delete_enabled BOOLEAN DEFAULT true,
    soft_delete_retention_days INTEGER DEFAULT 90,
    purge_protection_enabled BOOLEAN DEFAULT false,
    
    -- Network settings
    public_network_access VARCHAR,
    network_acls JSON,
    
    -- Metadata
    tags JSON,
    properties JSON,
    created_time TIMESTAMP,
    last_modified TIMESTAMP,
    
    -- Indexes
    INDEX idx_tenant_id (tenant_id),
    INDEX idx_sku_name (sku_name),
    INDEX idx_vault_uri (vault_uri)
);`
}

// GetCreateTableSQL returns the CREATE TABLE SQL for a specific table
func (s *AzureDuckDBSchemas) GetCreateTableSQL(tableName string) string {
	switch tableName {
	case "azure_resources":
		return s.AzureResourcesTable
	case "azure_relationships":
		return s.AzureRelationshipsTable
	case "azure_scan_metadata":
		return s.ScanMetadataTable
	case "azure_api_action_metadata":
		return s.APIActionMetadataTable
	default:
		// Check service-specific tables
		if sql, exists := s.ServiceTables[tableName]; exists {
			return sql
		}
		return ""
	}
}

// GetAllCreateTableSQL returns all CREATE TABLE statements
func (s *AzureDuckDBSchemas) GetAllCreateTableSQL() []string {
	sqls := []string{
		s.AzureResourcesTable,
		s.AzureRelationshipsTable,
		s.ScanMetadataTable,
		s.APIActionMetadataTable,
	}
	
	// Add service-specific tables
	for _, sql := range s.ServiceTables {
		sqls = append(sqls, sql)
	}
	
	return sqls
}

// GenerateResourceInsertSQL generates an INSERT statement for azure_resources table
func GenerateResourceInsertSQL(resource map[string]interface{}) (string, []interface{}) {
	columns := []string{
		"id", "name", "type", "resource_id", "subscription_id", "resource_group",
		"location", "parent_id", "managed_by", "service", "kind",
		"sku_name", "sku_tier", "sku_size", "sku_family", "sku_capacity",
		"tags", "properties", "raw_data", "provisioning_state", "power_state",
		"created_time", "changed_time", "etag", "api_version",
	}
	
	placeholders := make([]string, len(columns))
	values := make([]interface{}, len(columns))
	
	for i := range columns {
		placeholders[i] = "?"
		if val, exists := resource[columns[i]]; exists {
			values[i] = val
		} else {
			values[i] = nil
		}
	}
	
	sql := fmt.Sprintf(
		"INSERT INTO azure_resources (%s) VALUES (%s)",
		strings.Join(columns, ", "),
		strings.Join(placeholders, ", "),
	)
	
	return sql, values
}

// GenerateRelationshipInsertSQL generates an INSERT statement for azure_relationships table
func GenerateRelationshipInsertSQL(fromID, toID, relationType string, properties map[string]interface{}) (string, []interface{}) {
	sql := `INSERT INTO azure_relationships 
		(from_id, to_id, relationship_type, relationship_subtype, properties, 
		 from_resource_type, to_resource_type, direction) 
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
	
	values := []interface{}{
		fromID,
		toID,
		relationType,
		properties["subtype"],
		properties,
		properties["from_type"],
		properties["to_type"],
		properties["direction"],
	}
	
	return sql, values
}

// GenerateScanMetadataInsertSQL generates an INSERT statement for scan metadata
func GenerateScanMetadataInsertSQL(scanID string, scanData map[string]interface{}) (string, []interface{}) {
	sql := `INSERT INTO azure_scan_metadata 
		(id, scan_type, services, resource_groups, locations, subscription_id,
		 total_resources, new_resources, updated_resources, deleted_resources, failed_resources,
		 scan_start_time, scan_end_time, duration_ms, initiated_by, scan_reason,
		 error_messages, warnings, metadata, status) 
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	
	values := []interface{}{
		scanID,
		scanData["scan_type"],
		scanData["services"],
		scanData["resource_groups"],
		scanData["locations"],
		scanData["subscription_id"],
		scanData["total_resources"],
		scanData["new_resources"],
		scanData["updated_resources"],
		scanData["deleted_resources"],
		scanData["failed_resources"],
		scanData["scan_start_time"],
		scanData["scan_end_time"],
		scanData["duration_ms"],
		scanData["initiated_by"],
		scanData["scan_reason"],
		scanData["error_messages"],
		scanData["warnings"],
		scanData["metadata"],
		scanData["status"],
	}
	
	return sql, values
}

// ExtractRelationshipsFromResource extracts relationships from an Azure resource
func ExtractRelationshipsFromResource(resource map[string]interface{}) []map[string]interface{} {
	relationships := []map[string]interface{}{}
	
	resourceID := resource["id"].(string)
	resourceType := resource["type"].(string)
	
	// Extract parent relationship
	if parentID, exists := resource["parent_id"]; exists && parentID != nil && parentID != "" {
		relationships = append(relationships, map[string]interface{}{
			"from_id":           resourceID,
			"to_id":             parentID.(string),
			"relationship_type": "child_of",
			"properties": map[string]interface{}{
				"subtype":   "parent_child",
				"from_type": resourceType,
				"to_type":   extractResourceType(parentID.(string)),
				"direction": "outbound",
			},
		})
	}
	
	// Extract managed_by relationship
	if managedBy, exists := resource["managed_by"]; exists && managedBy != nil && managedBy != "" {
		relationships = append(relationships, map[string]interface{}{
			"from_id":           resourceID,
			"to_id":             managedBy.(string),
			"relationship_type": "managed_by",
			"properties": map[string]interface{}{
				"subtype":   "management",
				"from_type": resourceType,
				"to_type":   extractResourceType(managedBy.(string)),
				"direction": "outbound",
			},
		})
	}
	
	// Extract resource-specific relationships
	if properties, exists := resource["properties"].(map[string]interface{}); exists {
		// Virtual Machine relationships
		if strings.Contains(resourceType, "Microsoft.Compute/virtualMachines") {
			// Network interfaces
			if nics, exists := properties["networkProfile"].(map[string]interface{}); exists {
				if interfaces, exists := nics["networkInterfaces"].([]interface{}); exists {
					for _, nic := range interfaces {
						if nicMap, ok := nic.(map[string]interface{}); ok {
							if nicID, exists := nicMap["id"].(string); exists {
								relationships = append(relationships, map[string]interface{}{
									"from_id":           resourceID,
									"to_id":             nicID,
									"relationship_type": "uses",
									"properties": map[string]interface{}{
										"subtype":   "network_interface",
										"from_type": resourceType,
										"to_type":   "Microsoft.Network/networkInterfaces",
										"direction": "outbound",
									},
								})
							}
						}
					}
				}
			}
			
			// Availability Set
			if availSet, exists := properties["availabilitySet"].(map[string]interface{}); exists {
				if availSetID, exists := availSet["id"].(string); exists {
					relationships = append(relationships, map[string]interface{}{
						"from_id":           resourceID,
						"to_id":             availSetID,
						"relationship_type": "member_of",
						"properties": map[string]interface{}{
							"subtype":   "availability_set",
							"from_type": resourceType,
							"to_type":   "Microsoft.Compute/availabilitySets",
							"direction": "outbound",
						},
					})
				}
			}
		}
		
		// Storage Account relationships
		if strings.Contains(resourceType, "Microsoft.Storage/storageAccounts") {
			// Private endpoints
			if privateEndpoints, exists := properties["privateEndpointConnections"].([]interface{}); exists {
				for _, pe := range privateEndpoints {
					if peMap, ok := pe.(map[string]interface{}); ok {
						if peProps, exists := peMap["properties"].(map[string]interface{}); exists {
							if privateEndpoint, exists := peProps["privateEndpoint"].(map[string]interface{}); exists {
								if peID, exists := privateEndpoint["id"].(string); exists {
									relationships = append(relationships, map[string]interface{}{
										"from_id":           resourceID,
										"to_id":             peID,
										"relationship_type": "connected_to",
										"properties": map[string]interface{}{
											"subtype":   "private_endpoint",
											"from_type": resourceType,
											"to_type":   "Microsoft.Network/privateEndpoints",
											"direction": "outbound",
										},
									})
								}
							}
						}
					}
				}
			}
		}
		
		// Virtual Network relationships
		if strings.Contains(resourceType, "Microsoft.Network/virtualNetworks") {
			// Peerings
			if peerings, exists := properties["virtualNetworkPeerings"].([]interface{}); exists {
				for _, peering := range peerings {
					if peerMap, ok := peering.(map[string]interface{}); ok {
						if peerProps, exists := peerMap["properties"].(map[string]interface{}); exists {
							if remoteVnet, exists := peerProps["remoteVirtualNetwork"].(map[string]interface{}); exists {
								if vnetID, exists := remoteVnet["id"].(string); exists {
									relationships = append(relationships, map[string]interface{}{
										"from_id":           resourceID,
										"to_id":             vnetID,
										"relationship_type": "peered_with",
										"properties": map[string]interface{}{
											"subtype":   "vnet_peering",
											"from_type": resourceType,
											"to_type":   "Microsoft.Network/virtualNetworks",
											"direction": "bidirectional",
										},
									})
								}
							}
						}
					}
				}
			}
		}
	}
	
	return relationships
}

// extractResourceType extracts the resource type from an Azure resource ID
func extractResourceType(resourceID string) string {
	parts := strings.Split(resourceID, "/")
	if len(parts) >= 8 {
		// Format: /subscriptions/{sub}/resourceGroups/{rg}/providers/{provider}/{type}/{name}
		return fmt.Sprintf("%s/%s", parts[6], parts[7])
	}
	return "Unknown"
}