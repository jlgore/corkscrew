package main

import (
	"context"
	"fmt"
	"strings"

	cloudflare "github.com/cloudflare/cloudflare-go/v6"
	"github.com/cloudflare/cloudflare-go/v6/d1"
	"github.com/cloudflare/cloudflare-go/v6/durable_objects"
	"github.com/cloudflare/cloudflare-go/v6/secrets_store"
	pb "github.com/jlgore/corkscrew/internal/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (p *CloudflareProvider) scanData(ctx context.Context) ([]*pb.Resource, []string) {
	accountIDs, err := p.accountIDsForWorkers(ctx)
	if err != nil {
		return nil, []string{fmt.Sprintf("accounts list failed: %v", err)}
	}

	resources := make([]*pb.Resource, 0)
	var errs []string

	for _, accountID := range accountIDs {
		databases, err := p.listD1Databases(ctx, accountID)
		if err != nil {
			errs = append(errs, fmt.Sprintf("d1 databases list failed for account %s: %v", accountID, err))
		} else {
			for _, database := range databases {
				resources = append(resources, p.d1DatabaseResource(accountID, database))
			}
		}

		namespaces, err := p.listDurableObjectNamespaces(ctx, accountID)
		if err != nil {
			errs = append(errs, fmt.Sprintf("durable object namespaces list failed for account %s: %v", accountID, err))
		} else {
			for _, namespace := range namespaces {
				resources = append(resources, p.durableObjectNamespaceResource(accountID, namespace))
				objects, err := p.listDurableObjects(ctx, accountID, namespace.ID)
				if err != nil {
					errs = append(errs, fmt.Sprintf("durable objects list failed for namespace %s: %v", namespace.ID, err))
					continue
				}
				for _, object := range objects {
					resources = append(resources, p.durableObjectResource(accountID, namespace.ID, namespace.Name, object))
				}
			}
		}

		stores, err := p.listSecretStores(ctx, accountID)
		if err != nil {
			errs = append(errs, fmt.Sprintf("secret stores list failed for account %s: %v", accountID, err))
		} else {
			for _, store := range stores {
				resources = append(resources, p.secretStoreResource(accountID, store))
				secrets, err := p.listSecretStoreSecrets(ctx, accountID, store.ID)
				if err != nil {
					errs = append(errs, fmt.Sprintf("secret store secrets list failed for store %s: %v", store.ID, err))
					continue
				}
				for _, secret := range secrets {
					resources = append(resources, p.secretStoreSecretListResource(accountID, store.ID, store.Name, secret))
				}
			}
		}
	}

	return resources, errs
}

func (p *CloudflareProvider) listD1Databases(ctx context.Context, accountID string) ([]d1.DatabaseListResponse, error) {
	iter := p.client.D1.Database.ListAutoPaging(ctx, d1.DatabaseListParams{AccountID: cloudflare.F(accountID)})
	results := make([]d1.DatabaseListResponse, 0)
	for iter.Next() {
		results = append(results, iter.Current())
	}
	if err := iter.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

func (p *CloudflareProvider) listDurableObjectNamespaces(ctx context.Context, accountID string) ([]durable_objects.Namespace, error) {
	iter := p.client.DurableObjects.Namespaces.ListAutoPaging(ctx, durable_objects.NamespaceListParams{AccountID: cloudflare.F(accountID)})
	results := make([]durable_objects.Namespace, 0)
	for iter.Next() {
		results = append(results, iter.Current())
	}
	if err := iter.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

func (p *CloudflareProvider) listDurableObjects(ctx context.Context, accountID, namespaceID string) ([]durable_objects.DurableObject, error) {
	iter := p.client.DurableObjects.Namespaces.Objects.ListAutoPaging(ctx, namespaceID, durable_objects.NamespaceObjectListParams{AccountID: cloudflare.F(accountID)})
	results := make([]durable_objects.DurableObject, 0)
	for iter.Next() {
		results = append(results, iter.Current())
	}
	if err := iter.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

func (p *CloudflareProvider) listSecretStores(ctx context.Context, accountID string) ([]secrets_store.StoreListResponse, error) {
	iter := p.client.SecretsStore.Stores.ListAutoPaging(ctx, secrets_store.StoreListParams{AccountID: cloudflare.F(accountID)})
	results := make([]secrets_store.StoreListResponse, 0)
	for iter.Next() {
		results = append(results, iter.Current())
	}
	if err := iter.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

func (p *CloudflareProvider) listSecretStoreSecrets(ctx context.Context, accountID, storeID string) ([]secrets_store.StoreSecretListResponse, error) {
	iter := p.client.SecretsStore.Stores.Secrets.ListAutoPaging(ctx, storeID, secrets_store.StoreSecretListParams{AccountID: cloudflare.F(accountID)})
	results := make([]secrets_store.StoreSecretListResponse, 0)
	for iter.Next() {
		results = append(results, iter.Current())
	}
	if err := iter.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

func (p *CloudflareProvider) d1DatabaseResource(accountID string, database d1.DatabaseListResponse) *pb.Resource {
	attrs := map[string]string{
		"account_id":   accountID,
		"database_id":  database.UUID,
		"name":         database.Name,
		"jurisdiction": string(database.Jurisdiction),
		"version":      database.Version,
	}
	resource := p.resource("data", "d1_database", database.UUID, database.Name, accountID, database, attrs)
	if !database.CreatedAt.IsZero() {
		resource.CreatedAt = timestamppb.New(database.CreatedAt)
	}
	return resource
}

func (p *CloudflareProvider) d1DatabaseDetailResource(accountID string, database d1.D1) *pb.Resource {
	attrs := map[string]string{
		"account_id":            accountID,
		"database_id":           database.UUID,
		"name":                  database.Name,
		"jurisdiction":          string(database.Jurisdiction),
		"version":               database.Version,
		"file_size":             fmt.Sprintf("%.0f", database.FileSize),
		"num_tables":            fmt.Sprintf("%.0f", database.NumTables),
		"read_replication_mode": string(database.ReadReplication.Mode),
	}
	resource := p.resource("data", "d1_database", database.UUID, database.Name, accountID, database, attrs)
	if !database.CreatedAt.IsZero() {
		resource.CreatedAt = timestamppb.New(database.CreatedAt)
	}
	return resource
}

func (p *CloudflareProvider) durableObjectNamespaceResource(accountID string, namespace durable_objects.Namespace) *pb.Resource {
	attrs := map[string]string{
		"account_id": accountID,
		"class":      namespace.Class,
		"name":       namespace.Name,
		"script":     namespace.Script,
		"use_sqlite": fmt.Sprintf("%t", namespace.UseSqlite),
	}
	return p.resource("data", "durable_object_namespace", namespace.ID, namespace.Name, accountID, namespace, attrs)
}

func (p *CloudflareProvider) durableObjectResource(accountID, namespaceID, namespaceName string, object durable_objects.DurableObject) *pb.Resource {
	attrs := map[string]string{
		"account_id":      accountID,
		"namespace_id":    namespaceID,
		"namespace_name":  namespaceName,
		"has_stored_data": fmt.Sprintf("%t", object.HasStoredData),
	}
	resource := p.resource("data", "durable_object", object.ID, object.ID, accountID, object, attrs)
	resource.ParentId = namespaceID
	return resource
}

func (p *CloudflareProvider) secretStoreResource(accountID string, store secrets_store.StoreListResponse) *pb.Resource {
	attrs := map[string]string{
		"account_id": accountID,
		"store_id":   store.ID,
		"name":       store.Name,
	}
	resource := p.resource("data", "secret_store", store.ID, store.Name, accountID, store, attrs)
	if !store.Created.IsZero() {
		resource.CreatedAt = timestamppb.New(store.Created)
	}
	if !store.Modified.IsZero() {
		resource.ModifiedAt = timestamppb.New(store.Modified)
	}
	return resource
}

func (p *CloudflareProvider) secretStoreSecretListResource(accountID, storeID, storeName string, secret secrets_store.StoreSecretListResponse) *pb.Resource {
	attrs := map[string]string{
		"account_id": accountID,
		"store_id":   storeID,
		"store_name": storeName,
		"name":       secret.Name,
		"status":     string(secret.Status),
		"comment":    secret.Comment,
	}
	if len(secret.Scopes) > 0 {
		attrs["scopes"] = strings.Join(secret.Scopes, ",")
	}
	resource := p.resource("data", "secret_store_secret", secret.ID, secret.Name, accountID, secret, attrs)
	resource.ParentId = storeID
	if !secret.Created.IsZero() {
		resource.CreatedAt = timestamppb.New(secret.Created)
	}
	if !secret.Modified.IsZero() {
		resource.ModifiedAt = timestamppb.New(secret.Modified)
	}
	return resource
}

func (p *CloudflareProvider) secretStoreSecretResource(accountID, storeID, storeName string, secret secrets_store.StoreSecretGetResponse) *pb.Resource {
	attrs := map[string]string{
		"account_id": accountID,
		"store_id":   storeID,
		"store_name": storeName,
		"name":       secret.Name,
		"status":     string(secret.Status),
		"comment":    secret.Comment,
	}
	if len(secret.Scopes) > 0 {
		attrs["scopes"] = strings.Join(secret.Scopes, ",")
	}
	resource := p.resource("data", "secret_store_secret", secret.ID, secret.Name, accountID, secret, attrs)
	resource.ParentId = storeID
	if !secret.Created.IsZero() {
		resource.CreatedAt = timestamppb.New(secret.Created)
	}
	if !secret.Modified.IsZero() {
		resource.ModifiedAt = timestamppb.New(secret.Modified)
	}
	return resource
}
