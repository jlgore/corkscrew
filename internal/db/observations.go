package db

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	pb "github.com/jlgore/corkscrew/internal/proto"
)

func (gs *GraphStore) storeScanObservations(ctx context.Context, resources []*pb.Resource, metadata ScanOutcomeMetadata) error {
	if strings.TrimSpace(metadata.ID) == "" {
		return fmt.Errorf("scan ID is required")
	}
	seenResources := map[string]struct{}{}
	seenRelationships := map[string]struct{}{}
	for _, resource := range resources {
		if resource == nil || strings.TrimSpace(resource.Id) == "" {
			continue
		}
		provider := strings.ToLower(strings.TrimSpace(resource.Provider))
		if provider == "" {
			provider = strings.ToLower(strings.TrimSpace(metadata.Provider))
		}
		key := provider + "|" + resource.Id
		if _, ok := seenResources[key]; !ok {
			seenResources[key] = struct{}{}
			tags, _ := json.Marshal(resource.Tags)
			if _, err := gs.scanExecContext(ctx, `
INSERT INTO resource_observations (
  scan_id, provider, resource_id, name, type, service, location,
  account_id, arn, parent_id, tags, attributes, raw_data, semantic_hash, observed_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, try_cast(? AS JSON),
          try_cast(? AS JSON), try_cast(? AS JSON), ?, ?)
ON CONFLICT(scan_id, provider, resource_id) DO UPDATE SET
  name = excluded.name, type = excluded.type, service = excluded.service,
  location = excluded.location, account_id = excluded.account_id,
  arn = excluded.arn, parent_id = excluded.parent_id, tags = excluded.tags,
  attributes = excluded.attributes, raw_data = excluded.raw_data,
  semantic_hash = excluded.semantic_hash, observed_at = excluded.observed_at
`, metadata.ID, provider, resource.Id, resource.Name, resource.Type, resource.Service,
				resource.Region, resource.AccountId, resource.Arn, resource.ParentId,
				string(tags), jsonOrNull(resource.Attributes), jsonOrNull(resource.RawData),
				resourceSemanticHash(resource), metadata.EndedAt); err != nil {
				return err
			}
		}

		for _, relationship := range resource.Relationships {
			if relationship == nil || relationship.TargetId == "" || relationship.RelationshipType == "" {
				continue
			}
			relKey := key + "|" + relationship.TargetId + "|" + relationship.RelationshipType
			if _, ok := seenRelationships[relKey]; ok {
				continue
			}
			seenRelationships[relKey] = struct{}{}
			properties, _ := json.Marshal(relationship.Properties)
			if _, err := gs.scanExecContext(ctx, `
INSERT INTO relationship_observations (
  scan_id, provider, from_id, to_id, relationship_type, relationship_subtype,
  properties, from_resource_type, to_resource_type, direction, observed_at
) VALUES (?, ?, ?, ?, ?, '', try_cast(? AS JSON), ?, ?, 'outbound', ?)
ON CONFLICT(scan_id, provider, from_id, to_id, relationship_type) DO UPDATE SET
  properties = excluded.properties, from_resource_type = excluded.from_resource_type,
  to_resource_type = excluded.to_resource_type, observed_at = excluded.observed_at
`, metadata.ID, provider, resource.Id, relationship.TargetId, relationship.RelationshipType,
				string(properties), resource.Type, relationship.TargetType, metadata.EndedAt); err != nil {
				return err
			}
		}
	}
	return nil
}

func resourceSemanticHash(resource *pb.Resource) string {
	payload := map[string]any{
		"provider": strings.ToLower(strings.TrimSpace(resource.Provider)),
		"id":       resource.Id, "name": resource.Name, "type": resource.Type,
		"service": resource.Service, "location": resource.Region,
		"account_id": resource.AccountId, "arn": resource.Arn,
		"parent_id": resource.ParentId, "tags": resource.Tags,
		"attributes": canonicalJSONValue(resource.Attributes),
		"raw_data":   canonicalJSONValue(resource.RawData),
	}
	encoded, _ := json.Marshal(payload)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func canonicalJSONValue(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	var decoded any
	if json.Unmarshal([]byte(value), &decoded) == nil {
		return decoded
	}
	return value
}

func jsonOrNull(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}
