// Package data provides normalized read access to Corkscrew storage.
package data

import (
	"context"
	"database/sql"
	"fmt"
)

type Queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

type Resource struct {
	Provider string
	ID       string
	Name     string
	Type     string
	Service  string
	Location string
	Account  string
	ARN      string
	ParentID string
}

type InventoryFilter struct {
	Provider string
	Service  string
	Type     string
	Location string
}

type Page struct {
	Limit  int
	Offset int
}

type Inventory struct{ queryer Queryer }

func NewInventory(queryer Queryer) *Inventory { return &Inventory{queryer: queryer} }

func (i *Inventory) Count(ctx context.Context, filter InventoryFilter) (int, error) {
	if i == nil || i.queryer == nil {
		return 0, fmt.Errorf("inventory queryer is required")
	}
	rows, err := i.queryer.QueryContext(ctx, `
SELECT COUNT(*) FROM all_cloud_resources
WHERE (? = '' OR provider = ?)
  AND (? = '' OR service = ?)
  AND (? = '' OR type = ?)
  AND (? = '' OR location = ?)
`, filter.Provider, filter.Provider, filter.Service, filter.Service,
		filter.Type, filter.Type, filter.Location, filter.Location)
	if err != nil {
		return 0, fmt.Errorf("count normalized inventory: %w", err)
	}
	defer rows.Close()
	var count int
	if !rows.Next() {
		return 0, fmt.Errorf("count normalized inventory returned no row")
	}
	if err := rows.Scan(&count); err != nil {
		return 0, fmt.Errorf("scan normalized inventory count: %w", err)
	}
	return count, rows.Err()
}

func (i *Inventory) List(ctx context.Context, filter InventoryFilter, page Page) ([]Resource, error) {
	if i == nil || i.queryer == nil {
		return nil, fmt.Errorf("inventory queryer is required")
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
	rows, err := i.queryer.QueryContext(ctx, `
SELECT provider, id, COALESCE(name, ''), COALESCE(type, ''),
       COALESCE(service, ''), COALESCE(location, ''),
       COALESCE(account_id, ''), COALESCE(resource_identifier, ''),
       COALESCE(parent_id, '')
FROM all_cloud_resources
WHERE (? = '' OR provider = ?)
  AND (? = '' OR service = ?)
  AND (? = '' OR type = ?)
  AND (? = '' OR location = ?)
ORDER BY provider, id
LIMIT ? OFFSET ?
`, filter.Provider, filter.Provider, filter.Service, filter.Service,
		filter.Type, filter.Type, filter.Location, filter.Location, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("query normalized inventory: %w", err)
	}
	defer rows.Close()
	var resources []Resource
	for rows.Next() {
		var resource Resource
		if err := rows.Scan(&resource.Provider, &resource.ID, &resource.Name, &resource.Type,
			&resource.Service, &resource.Location, &resource.Account, &resource.ARN, &resource.ParentID); err != nil {
			return nil, fmt.Errorf("scan normalized inventory: %w", err)
		}
		resources = append(resources, resource)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate normalized inventory: %w", err)
	}
	return resources, nil
}
