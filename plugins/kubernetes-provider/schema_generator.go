package main

import (
	"fmt"
	"strings"

	pb "github.com/jlgore/corkscrew/internal/proto"
)

// K8sSchemaGenerator generates DuckDB schemas for Kubernetes resources
type K8sSchemaGenerator struct{}

// NewK8sSchemaGenerator creates a new schema generator
func NewK8sSchemaGenerator() *K8sSchemaGenerator {
	return &K8sSchemaGenerator{}
}

// GenerateTableSchema generates a DuckDB table schema for a Kubernetes resource
func (g *K8sSchemaGenerator) GenerateTableSchema(resource *ResourceDefinition) *pb.Schema {
	tableName := g.getTableName(resource)
	
	// Generate CREATE TABLE SQL
	sql := g.generateCreateTableSQL(tableName, resource)
	
	return &pb.Schema{
		Name:         tableName,
		Service:      "kubernetes",
		ResourceType: resource.Kind,
		Sql:          sql,
		Description:  fmt.Sprintf("Kubernetes %s resources", resource.Kind),
		Metadata: map[string]string{
			"api_version": fmt.Sprintf("%s/%s", resource.Group, resource.Version),
			"namespaced":  fmt.Sprintf("%t", resource.Namespaced),
			"crd":         fmt.Sprintf("%t", resource.CRD),
		},
	}
}

// getTableName generates a table name for the resource
func (g *K8sSchemaGenerator) getTableName(resource *ResourceDefinition) string {
	group := resource.Group
	if group == "" {
		group = "core"
	}
	
	// Replace dots with underscores and convert to lowercase
	group = strings.ReplaceAll(strings.ToLower(group), ".", "_")
	name := strings.ToLower(resource.Name)
	
	return fmt.Sprintf("k8s_%s_%s", group, name)
}

// generateCreateTableSQL generates the CREATE TABLE SQL for a Kubernetes resource
func (g *K8sSchemaGenerator) generateCreateTableSQL(tableName string, resource *ResourceDefinition) string {
	var sql strings.Builder
	
	sql.WriteString(fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (\n", tableName))
	
	// Standard columns for all Kubernetes resources
	sql.WriteString("    uid VARCHAR PRIMARY KEY,\n")
	sql.WriteString("    name VARCHAR NOT NULL,\n")
	sql.WriteString("    namespace VARCHAR,\n")
	sql.WriteString("    kind VARCHAR NOT NULL,\n")
	sql.WriteString("    api_version VARCHAR NOT NULL,\n")
	sql.WriteString("    resource_version VARCHAR,\n")
	sql.WriteString("    creation_timestamp TIMESTAMP,\n")
	sql.WriteString("    deletion_timestamp TIMESTAMP,\n")
	sql.WriteString("    labels JSON,\n")
	sql.WriteString("    annotations JSON,\n")
	sql.WriteString("    owner_references JSON,\n")
	sql.WriteString("    finalizers JSON,\n")
	sql.WriteString("    generation BIGINT,\n")
	sql.WriteString("    spec JSON,\n")
	sql.WriteString("    status JSON,\n")
	sql.WriteString("    raw_manifest JSON,\n")
	sql.WriteString("    discovered_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,\n")
	
	// Resource-specific columns based on resource type
	sql.WriteString(g.getResourceSpecificColumns(resource))
	
	// Remove trailing comma and newline
	sqlStr := sql.String()
	if strings.HasSuffix(sqlStr, ",\n") {
		sqlStr = strings.TrimSuffix(sqlStr, ",\n") + "\n"
	}
	
	sqlStr += ");\n"
	
	// Add indexes
	sqlStr += g.generateIndexes(tableName)
	
	return sqlStr
}

// getResourceSpecificColumns returns additional column definitions for specific resource types
func (g *K8sSchemaGenerator) getResourceSpecificColumns(resource *ResourceDefinition) string {
	var columns strings.Builder
	
	switch resource.Kind {
	case "Pod":
		columns.WriteString("    node_name VARCHAR,\n")
		columns.WriteString("    phase VARCHAR,\n")
		columns.WriteString("    pod_ip VARCHAR,\n")
		columns.WriteString("    host_ip VARCHAR,\n")
		
	case "Service":
		columns.WriteString("    cluster_ip VARCHAR,\n")
		columns.WriteString("    service_type VARCHAR,\n")
		columns.WriteString("    external_ips JSON,\n")
		columns.WriteString("    load_balancer_ip VARCHAR,\n")
		
	case "Node":
		columns.WriteString("    provider_id VARCHAR,\n")
		columns.WriteString("    pod_cidr VARCHAR,\n")
		columns.WriteString("    unschedulable BOOLEAN,\n")
		
	case "PersistentVolume":
		columns.WriteString("    storage_class VARCHAR,\n")
		columns.WriteString("    capacity VARCHAR,\n")
		columns.WriteString("    access_modes JSON,\n")
		columns.WriteString("    reclaim_policy VARCHAR,\n")
	}
	
	return columns.String()
}

// generateIndexes returns index creation SQL for the table
func (g *K8sSchemaGenerator) generateIndexes(tableName string) string {
	var indexes strings.Builder
	
	// Standard indexes
	indexes.WriteString(fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%s_name ON %s(name);\n", tableName, tableName))
	indexes.WriteString(fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%s_namespace ON %s(namespace);\n", tableName, tableName))
	indexes.WriteString(fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%s_created ON %s(creation_timestamp);\n", tableName, tableName))
	indexes.WriteString(fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%s_kind ON %s(kind);\n", tableName, tableName))
	
	return indexes.String()
}

// GenerateRelationshipTableSchema generates the schema for the relationships table
func (g *K8sSchemaGenerator) GenerateRelationshipTableSchema() *pb.Schema {
	sql := `CREATE TABLE IF NOT EXISTS k8s_relationships (
    id VARCHAR PRIMARY KEY,
    source_uid VARCHAR NOT NULL,
    source_kind VARCHAR NOT NULL,
    source_name VARCHAR NOT NULL,
    source_namespace VARCHAR,
    target_uid VARCHAR NOT NULL,
    target_kind VARCHAR NOT NULL,
    target_name VARCHAR NOT NULL,
    target_namespace VARCHAR,
    relationship_type VARCHAR NOT NULL,
    properties JSON,
    cluster_name VARCHAR NOT NULL,
    discovered_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_k8s_rel_source ON k8s_relationships(source_uid);
CREATE INDEX IF NOT EXISTS idx_k8s_rel_target ON k8s_relationships(target_uid);
CREATE INDEX IF NOT EXISTS idx_k8s_rel_type ON k8s_relationships(relationship_type);
CREATE INDEX IF NOT EXISTS idx_k8s_rel_cluster ON k8s_relationships(cluster_name);`

	return &pb.Schema{
		Name:         "k8s_relationships",
		Service:      "kubernetes",
		ResourceType: "Relationship",
		Sql:          sql,
		Description:  "Relationships between Kubernetes resources",
		Metadata: map[string]string{
			"table_type": "relationships",
		},
	}
}

// GenerateSchemas generates all schemas for the given resources
func (g *K8sSchemaGenerator) GenerateSchemas(resources []*ResourceDefinition) []*pb.Schema {
	var schemas []*pb.Schema
	
	// Generate schema for each resource type
	for _, resource := range resources {
		schema := g.GenerateTableSchema(resource)
		schemas = append(schemas, schema)
	}
	
	// Add relationships table
	schemas = append(schemas, g.GenerateRelationshipTableSchema())
	
	return schemas
}