package verification

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// RelationshipVerifier provides methods for verifying relationships in DuckDB
type RelationshipVerifier struct {
	db *sql.DB
}

// NewRelationshipVerifier creates a new relationship verifier instance
func NewRelationshipVerifier(db *sql.DB) *RelationshipVerifier {
	return &RelationshipVerifier{db: db}
}

// Relationship represents a relationship between two AWS resources
type Relationship struct {
	ID             string
	FromResourceID string
	ToResourceID   string
	Type           string
	Properties     map[string]interface{}
}

// QueryRelationships retrieves all relationships from the aws_relationships table
func (rv *RelationshipVerifier) QueryRelationships(ctx context.Context) ([]Relationship, error) {
	query := `
		SELECT 
			id,
			from_resource_id,
			to_resource_id,
			type,
			properties
		FROM aws_relationships
	`

	rows, err := rv.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query relationships: %w", err)
	}
	defer rows.Close()

	var relationships []Relationship
	for rows.Next() {
		var rel Relationship
		var propertiesJSON sql.NullString

		err := rows.Scan(
			&rel.ID,
			&rel.FromResourceID,
			&rel.ToResourceID,
			&rel.Type,
			&propertiesJSON,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan relationship row: %w", err)
		}

		// Parse properties JSON if present
		if propertiesJSON.Valid {
			rel.Properties = make(map[string]interface{})
			// In a real implementation, you would parse the JSON here
			// For now, we'll store the raw JSON string
			rel.Properties["raw"] = propertiesJSON.String
		}

		relationships = append(relationships, rel)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating relationships: %w", err)
	}

	return relationships, nil
}

// VerifyRelationshipExists checks if a specific relationship exists between two resources
func (rv *RelationshipVerifier) VerifyRelationshipExists(ctx context.Context, fromResourceID, toResourceID, relType string) (bool, error) {
	query := `
		SELECT COUNT(*) 
		FROM aws_relationships 
		WHERE from_resource_id = ? 
		  AND to_resource_id = ? 
		  AND type = ?
	`

	var count int
	err := rv.db.QueryRowContext(ctx, query, fromResourceID, toResourceID, relType).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check relationship existence: %w", err)
	}

	return count > 0, nil
}

// GetRelationshipsByResource retrieves all relationships for a specific resource
func (rv *RelationshipVerifier) GetRelationshipsByResource(ctx context.Context, resourceID string) ([]Relationship, error) {
	query := `
		SELECT 
			id,
			from_resource_id,
			to_resource_id,
			type,
			properties
		FROM aws_relationships
		WHERE from_resource_id = ? OR to_resource_id = ?
	`

	rows, err := rv.db.QueryContext(ctx, query, resourceID, resourceID)
	if err != nil {
		return nil, fmt.Errorf("failed to query relationships by resource: %w", err)
	}
	defer rows.Close()

	var relationships []Relationship
	for rows.Next() {
		var rel Relationship
		var propertiesJSON sql.NullString

		err := rows.Scan(
			&rel.ID,
			&rel.FromResourceID,
			&rel.ToResourceID,
			&rel.Type,
			&propertiesJSON,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan relationship row: %w", err)
		}

		if propertiesJSON.Valid {
			rel.Properties = make(map[string]interface{})
			rel.Properties["raw"] = propertiesJSON.String
		}

		relationships = append(relationships, rel)
	}

	return relationships, nil
}

// GetRelationshipsByType retrieves all relationships of a specific type
func (rv *RelationshipVerifier) GetRelationshipsByType(ctx context.Context, relType string) ([]Relationship, error) {
	query := `
		SELECT 
			id,
			from_resource_id,
			to_resource_id,
			type,
			properties
		FROM aws_relationships
		WHERE type = ?
	`

	rows, err := rv.db.QueryContext(ctx, query, relType)
	if err != nil {
		return nil, fmt.Errorf("failed to query relationships by type: %w", err)
	}
	defer rows.Close()

	var relationships []Relationship
	for rows.Next() {
		var rel Relationship
		var propertiesJSON sql.NullString

		err := rows.Scan(
			&rel.ID,
			&rel.FromResourceID,
			&rel.ToResourceID,
			&rel.Type,
			&propertiesJSON,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan relationship row: %w", err)
		}

		if propertiesJSON.Valid {
			rel.Properties = make(map[string]interface{})
			rel.Properties["raw"] = propertiesJSON.String
		}

		relationships = append(relationships, rel)
	}

	return relationships, nil
}

// CheckRelationshipProperty verifies a specific property value in a relationship
func (rv *RelationshipVerifier) CheckRelationshipProperty(ctx context.Context, relationshipID, propertyPath string, expectedValue interface{}) (bool, error) {
	query := `
		SELECT properties
		FROM aws_relationships
		WHERE id = ?
	`

	var propertiesJSON sql.NullString
	err := rv.db.QueryRowContext(ctx, query, relationshipID).Scan(&propertiesJSON)
	if err != nil {
		if err == sql.ErrNoRows {
			return false, fmt.Errorf("relationship not found: %s", relationshipID)
		}
		return false, fmt.Errorf("failed to query relationship properties: %w", err)
	}

	if !propertiesJSON.Valid {
		return false, fmt.Errorf("relationship has no properties")
	}

	// In a real implementation, you would parse the JSON and check the property path
	// For now, we'll do a simple string contains check
	return strings.Contains(propertiesJSON.String, fmt.Sprintf("%v", expectedValue)), nil
}

// RelationshipPath represents a path between two resources through relationships
type RelationshipPath struct {
	StartResource string
	EndResource   string
	Path          []Relationship
	TotalHops     int
}

// TraceRelationshipPath finds paths between two resources through relationships
func (rv *RelationshipVerifier) TraceRelationshipPath(ctx context.Context, startResourceID, endResourceID string, maxDepth int) ([]RelationshipPath, error) {
	// This is a simplified implementation
	// In a real implementation, you would use a graph traversal algorithm

	// First, check if there's a direct relationship
	directQuery := `
		SELECT 
			id,
			from_resource_id,
			to_resource_id,
			type,
			properties
		FROM aws_relationships
		WHERE (from_resource_id = ? AND to_resource_id = ?)
		   OR (from_resource_id = ? AND to_resource_id = ?)
	`

	rows, err := rv.db.QueryContext(ctx, directQuery, startResourceID, endResourceID, endResourceID, startResourceID)
	if err != nil {
		return nil, fmt.Errorf("failed to trace relationship path: %w", err)
	}
	defer rows.Close()

	var paths []RelationshipPath
	for rows.Next() {
		var rel Relationship
		var propertiesJSON sql.NullString

		err := rows.Scan(
			&rel.ID,
			&rel.FromResourceID,
			&rel.ToResourceID,
			&rel.Type,
			&propertiesJSON,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan relationship: %w", err)
		}

		if propertiesJSON.Valid {
			rel.Properties = make(map[string]interface{})
			rel.Properties["raw"] = propertiesJSON.String
		}

		path := RelationshipPath{
			StartResource: startResourceID,
			EndResource:   endResourceID,
			Path:          []Relationship{rel},
			TotalHops:     1,
		}
		paths = append(paths, path)
	}

	// For more complex paths, you would implement BFS or DFS here
	// This is a placeholder for demonstration

	return paths, nil
}

// GetRelationshipCounts returns counts of relationships by type
func (rv *RelationshipVerifier) GetRelationshipCounts(ctx context.Context) (map[string]int, error) {
	query := `
		SELECT type, COUNT(*) as count
		FROM aws_relationships
		GROUP BY type
		ORDER BY count DESC
	`

	rows, err := rv.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to get relationship counts: %w", err)
	}
	defer rows.Close()

	counts := make(map[string]int)
	for rows.Next() {
		var relType string
		var count int

		err := rows.Scan(&relType, &count)
		if err != nil {
			return nil, fmt.Errorf("failed to scan count row: %w", err)
		}

		counts[relType] = count
	}

	return counts, nil
}

// ValidateRelationshipIntegrity checks for orphaned relationships
func (rv *RelationshipVerifier) ValidateRelationshipIntegrity(ctx context.Context) ([]string, error) {
	query := `
		SELECT DISTINCT r.id, r.from_resource_id, r.to_resource_id
		FROM aws_relationships r
		LEFT JOIN aws_resources fr ON r.from_resource_id = fr.id
		LEFT JOIN aws_resources tr ON r.to_resource_id = tr.id
		WHERE fr.id IS NULL OR tr.id IS NULL
	`

	rows, err := rv.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to validate relationship integrity: %w", err)
	}
	defer rows.Close()

	var orphanedRelationships []string
	for rows.Next() {
		var relID, fromID, toID string
		err := rows.Scan(&relID, &fromID, &toID)
		if err != nil {
			return nil, fmt.Errorf("failed to scan orphaned relationship: %w", err)
		}

		orphanedRelationships = append(orphanedRelationships,
			fmt.Sprintf("Relationship %s: from=%s, to=%s", relID, fromID, toID))
	}

	return orphanedRelationships, nil
}

// GetRelationshipGraph returns a graph representation of relationships for visualization
func (rv *RelationshipVerifier) GetRelationshipGraph(ctx context.Context, resourceTypes []string) (map[string][]string, error) {
	var query string
	var args []interface{}

	if len(resourceTypes) > 0 {
		placeholders := make([]string, len(resourceTypes))
		for i := range resourceTypes {
			placeholders[i] = "?"
			args = append(args, resourceTypes[i])
		}

		query = fmt.Sprintf(`
			SELECT DISTINCT r.from_resource_id, r.to_resource_id
			FROM aws_relationships r
			JOIN aws_resources fr ON r.from_resource_id = fr.id
			JOIN aws_resources tr ON r.to_resource_id = tr.id
			WHERE fr.type IN (%s) OR tr.type IN (%s)
		`, strings.Join(placeholders, ","), strings.Join(placeholders, ","))

		// Duplicate args for the second IN clause
		args = append(args, args...)
	} else {
		query = `
			SELECT DISTINCT from_resource_id, to_resource_id
			FROM aws_relationships
		`
	}

	rows, err := rv.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to get relationship graph: %w", err)
	}
	defer rows.Close()

	graph := make(map[string][]string)
	for rows.Next() {
		var fromID, toID string
		err := rows.Scan(&fromID, &toID)
		if err != nil {
			return nil, fmt.Errorf("failed to scan graph edge: %w", err)
		}

		graph[fromID] = append(graph[fromID], toID)
	}

	return graph, nil
}
