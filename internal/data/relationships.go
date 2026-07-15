package data

import (
	"context"
	"database/sql"
	"fmt"
)

type Direction string

const (
	DirectionAny      Direction = ""
	DirectionOutbound Direction = "outbound"
	DirectionInbound  Direction = "inbound"
)

type Relationship struct {
	Provider   string
	FromID     string
	ToID       string
	Type       string
	Subtype    string
	Direction  string
	FromType   string
	ToType     string
	Properties string
}

type RelationshipFilter struct {
	Provider   string
	Type       string
	ResourceID string
	Direction  Direction
}

type Relationships struct{ queryer Queryer }

func NewRelationships(queryer Queryer) *Relationships { return &Relationships{queryer: queryer} }

func (r *Relationships) List(ctx context.Context, filter RelationshipFilter, page Page) ([]Relationship, error) {
	if r == nil || r.queryer == nil {
		return nil, fmt.Errorf("relationship queryer is required")
	}
	if filter.Direction != DirectionAny && filter.Direction != DirectionOutbound && filter.Direction != DirectionInbound {
		return nil, fmt.Errorf("unknown relationship direction %q", filter.Direction)
	}
	limit := page.Limit
	if limit <= 0 {
		limit = 200
	}
	if limit > 1000 {
		limit = 1000
	}
	offset := page.Offset
	if offset < 0 {
		offset = 0
	}
	rows, err := r.queryer.QueryContext(ctx, `
SELECT provider, from_id, to_id, relationship_type,
       COALESCE(relationship_subtype, ''), COALESCE(direction, ''),
       COALESCE(from_resource_type, ''), COALESCE(to_resource_type, ''),
       COALESCE(CAST(properties AS VARCHAR), '')
FROM cloud_relationships
WHERE (? = '' OR provider = ?)
  AND (? = '' OR relationship_type = ?)
  AND (
    ? = '' OR
    (? = 'outbound' AND from_id = ?) OR
    (? = 'inbound' AND to_id = ?) OR
    (? = '' AND (from_id = ? OR to_id = ?))
  )
ORDER BY provider, from_id, to_id, relationship_type
LIMIT ? OFFSET ?
`, filter.Provider, filter.Provider, filter.Type, filter.Type,
		filter.ResourceID,
		string(filter.Direction), filter.ResourceID,
		string(filter.Direction), filter.ResourceID,
		string(filter.Direction), filter.ResourceID, filter.ResourceID,
		limit, offset)
	if err != nil {
		return nil, fmt.Errorf("query normalized relationships: %w", err)
	}
	defer rows.Close()
	var relationships []Relationship
	for rows.Next() {
		var relationship Relationship
		var properties sql.NullString
		if err := rows.Scan(&relationship.Provider, &relationship.FromID, &relationship.ToID,
			&relationship.Type, &relationship.Subtype, &relationship.Direction,
			&relationship.FromType, &relationship.ToType, &properties); err != nil {
			return nil, fmt.Errorf("scan normalized relationship: %w", err)
		}
		if properties.Valid {
			relationship.Properties = properties.String
		}
		relationships = append(relationships, relationship)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate normalized relationships: %w", err)
	}
	return relationships, nil
}
