package main

import (
	"context"
	"fmt"

	cloudflare "github.com/cloudflare/cloudflare-go/v6"
	"github.com/cloudflare/cloudflare-go/v6/accounts"
	"github.com/cloudflare/cloudflare-go/v6/d1"
	dns "github.com/cloudflare/cloudflare-go/v6/dns"
	"github.com/cloudflare/cloudflare-go/v6/kv"
	"github.com/cloudflare/cloudflare-go/v6/queues"
	"github.com/cloudflare/cloudflare-go/v6/r2"
	"github.com/cloudflare/cloudflare-go/v6/secrets_store"
	"github.com/cloudflare/cloudflare-go/v6/workers"
	"github.com/cloudflare/cloudflare-go/v6/zones"
	pb "github.com/jlgore/corkscrew/internal/proto"
)

func (p *CloudflareProvider) describeByType(ctx context.Context, ref *pb.ResourceRef) (*pb.Resource, error) {
	switch ref.GetType() {
	case "account":
		account, err := p.client.Accounts.Get(ctx, accounts.AccountGetParams{AccountID: cloudflare.F(ref.GetId())})
		if err != nil {
			return nil, fmt.Errorf("get account %s: %w", ref.GetId(), err)
		}
		return p.accountResource(*account), nil
	case "zone":
		zone, err := p.client.Zones.Get(ctx, zones.ZoneGetParams{ZoneID: cloudflare.F(ref.GetId())})
		if err != nil {
			return nil, fmt.Errorf("get zone %s: %w", ref.GetId(), err)
		}
		return p.zoneResource(*zone), nil
	case "dns_record":
		zoneID := firstNonEmpty(ref.GetBasicAttributes()["zone_id"], ref.GetAccountId())
		if zoneID == "" {
			return nil, fmt.Errorf("dns_record %s is missing zone_id context", ref.GetId())
		}
		record, err := p.client.DNS.Records.Get(ctx, ref.GetId(), dns.RecordGetParams{ZoneID: cloudflare.F(zoneID)})
		if err != nil {
			return nil, fmt.Errorf("get dns record %s: %w", ref.GetId(), err)
		}
		zoneName := ref.GetBasicAttributes()["zone_name"]
		zone := zones.Zone{ID: zoneID, Name: zoneName, Account: zones.ZoneAccount{ID: ref.GetBasicAttributes()["account_id"], Name: ref.GetBasicAttributes()["account_name"]}}
		return p.dnsRecordResource(zone, *record), nil
	case "d1_database":
		accountID := firstNonEmpty(ref.GetBasicAttributes()["account_id"], ref.GetAccountId())
		if accountID == "" {
			return nil, fmt.Errorf("d1_database %s is missing account_id context", ref.GetId())
		}
		database, err := p.client.D1.Database.Get(ctx, ref.GetId(), d1.DatabaseGetParams{AccountID: cloudflare.F(accountID)})
		if err != nil {
			return nil, fmt.Errorf("get d1 database %s: %w", ref.GetId(), err)
		}
		return p.d1DatabaseDetailResource(accountID, *database), nil
	case "durable_object_namespace":
		accountID := firstNonEmpty(ref.GetBasicAttributes()["account_id"], ref.GetAccountId())
		if accountID == "" {
			return nil, fmt.Errorf("durable_object_namespace %s is missing account_id context", ref.GetId())
		}
		namespaces, err := p.listDurableObjectNamespaces(ctx, accountID)
		if err != nil {
			return nil, fmt.Errorf("list durable object namespaces for account %s: %w", accountID, err)
		}
		for _, namespace := range namespaces {
			if namespace.ID == ref.GetId() {
				return p.durableObjectNamespaceResource(accountID, namespace), nil
			}
		}
		return nil, fmt.Errorf("durable_object_namespace %s not found in account %s", ref.GetId(), accountID)
	case "durable_object":
		accountID := firstNonEmpty(ref.GetBasicAttributes()["account_id"], ref.GetAccountId())
		namespaceID := ref.GetBasicAttributes()["namespace_id"]
		if accountID == "" || namespaceID == "" {
			return nil, fmt.Errorf("durable_object %s is missing account_id or namespace_id context", ref.GetId())
		}
		objects, err := p.listDurableObjects(ctx, accountID, namespaceID)
		if err != nil {
			return nil, fmt.Errorf("list durable objects for namespace %s: %w", namespaceID, err)
		}
		for _, object := range objects {
			if object.ID == ref.GetId() {
				return p.durableObjectResource(accountID, namespaceID, ref.GetBasicAttributes()["namespace_name"], object), nil
			}
		}
		return nil, fmt.Errorf("durable_object %s not found in namespace %s", ref.GetId(), namespaceID)
	case "r2_bucket":
		accountID := firstNonEmpty(ref.GetBasicAttributes()["account_id"], ref.GetAccountId())
		bucketName := firstNonEmpty(ref.GetBasicAttributes()["bucket_name"], ref.GetName())
		if accountID == "" || bucketName == "" {
			return nil, fmt.Errorf("r2_bucket %s is missing account_id or bucket_name context", ref.GetId())
		}
		bucket, err := p.client.R2.Buckets.Get(ctx, bucketName, r2.BucketGetParams{AccountID: cloudflare.F(accountID)})
		if err != nil {
			return nil, fmt.Errorf("get r2 bucket %s: %w", bucketName, err)
		}
		return p.r2BucketResource(accountID, *bucket), nil
	case "kv_namespace":
		accountID := firstNonEmpty(ref.GetBasicAttributes()["account_id"], ref.GetAccountId())
		if accountID == "" {
			return nil, fmt.Errorf("kv_namespace %s is missing account_id context", ref.GetId())
		}
		namespace, err := p.client.KV.Namespaces.Get(ctx, ref.GetId(), kv.NamespaceGetParams{AccountID: cloudflare.F(accountID)})
		if err != nil {
			return nil, fmt.Errorf("get kv namespace %s: %w", ref.GetId(), err)
		}
		return p.kvNamespaceResource(accountID, *namespace), nil
	case "queue":
		accountID := firstNonEmpty(ref.GetBasicAttributes()["account_id"], ref.GetAccountId())
		if accountID == "" {
			return nil, fmt.Errorf("queue %s is missing account_id context", ref.GetId())
		}
		queue, err := p.client.Queues.Get(ctx, ref.GetId(), queues.QueueGetParams{AccountID: cloudflare.F(accountID)})
		if err != nil {
			return nil, fmt.Errorf("get queue %s: %w", ref.GetId(), err)
		}
		return p.queueResource(accountID, *queue), nil
	case "secret_store":
		accountID := firstNonEmpty(ref.GetBasicAttributes()["account_id"], ref.GetAccountId())
		if accountID == "" {
			return nil, fmt.Errorf("secret_store %s is missing account_id context", ref.GetId())
		}
		stores, err := p.listSecretStores(ctx, accountID)
		if err != nil {
			return nil, fmt.Errorf("list secret stores for account %s: %w", accountID, err)
		}
		for _, store := range stores {
			if store.ID == ref.GetId() {
				return p.secretStoreResource(accountID, store), nil
			}
		}
		return nil, fmt.Errorf("secret_store %s not found in account %s", ref.GetId(), accountID)
	case "secret_store_secret":
		accountID := firstNonEmpty(ref.GetBasicAttributes()["account_id"], ref.GetAccountId())
		storeID := ref.GetBasicAttributes()["store_id"]
		if accountID == "" || storeID == "" {
			return nil, fmt.Errorf("secret_store_secret %s is missing account_id or store_id context", ref.GetId())
		}
		secret, err := p.client.SecretsStore.Stores.Secrets.Get(ctx, storeID, ref.GetId(), secrets_store.StoreSecretGetParams{AccountID: cloudflare.F(accountID)})
		if err != nil {
			return nil, fmt.Errorf("get secret_store_secret %s: %w", ref.GetId(), err)
		}
		return p.secretStoreSecretResource(accountID, storeID, ref.GetBasicAttributes()["store_name"], *secret), nil
	case "worker_script":
		accountID := firstNonEmpty(ref.GetBasicAttributes()["account_id"], ref.GetAccountId())
		if accountID == "" {
			return nil, fmt.Errorf("worker_script %s is missing account_id context", ref.GetId())
		}
		scripts, err := p.listWorkerScripts(ctx, accountID)
		if err != nil {
			return nil, fmt.Errorf("list worker scripts for account %s: %w", accountID, err)
		}
		for _, script := range scripts {
			if script.ID == ref.GetId() || script.ID == ref.GetName() {
				return p.workerScriptResource(accountID, script), nil
			}
		}
		return nil, fmt.Errorf("worker_script %s not found in account %s", ref.GetId(), accountID)
	case "worker_route":
		zoneID := firstNonEmpty(ref.GetBasicAttributes()["zone_id"], ref.GetAccountId())
		if zoneID == "" {
			return nil, fmt.Errorf("worker_route %s is missing zone_id context", ref.GetId())
		}
		route, err := p.client.Workers.Routes.Get(ctx, ref.GetId(), workers.RouteGetParams{ZoneID: cloudflare.F(zoneID)})
		if err != nil {
			return nil, fmt.Errorf("get worker route %s: %w", ref.GetId(), err)
		}
		return p.workerRouteResource(zoneID, ref.GetBasicAttributes()["zone_name"], route.ID, route.Pattern, route.Script, *route), nil
	case "worker_domain":
		accountID := firstNonEmpty(ref.GetBasicAttributes()["account_id"], ref.GetAccountId())
		if accountID == "" {
			return nil, fmt.Errorf("worker_domain %s is missing account_id context", ref.GetId())
		}
		domain, err := p.client.Workers.Domains.Get(ctx, ref.GetId(), workers.DomainGetParams{AccountID: cloudflare.F(accountID)})
		if err != nil {
			return nil, fmt.Errorf("get worker domain %s: %w", ref.GetId(), err)
		}
		return p.workerDomainResource(accountID, domain.ID, domain.Hostname, domain.Service, domain.ZoneID, domain.ZoneName, domain.Environment, domain.CERTID, *domain), nil
	default:
		return nil, fmt.Errorf("describe not implemented for type %q", ref.GetType())
	}
}
