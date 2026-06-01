package main

import (
	"reflect"
	"testing"

	"github.com/cloudflare/cloudflare-go/v6/accounts"
	dns "github.com/cloudflare/cloudflare-go/v6/dns"
	"github.com/cloudflare/cloudflare-go/v6/queues"
	"github.com/cloudflare/cloudflare-go/v6/r2"
	"github.com/cloudflare/cloudflare-go/v6/workers"
)

func TestListCacheKeyStableForMapOrder(t *testing.T) {
	first := listCacheKey("dns", map[string]string{"b": "2", "a": "1"})
	second := listCacheKey("dns", map[string]string{"a": "1", "b": "2"})
	if first != second {
		t.Fatalf("listCacheKey order mismatch: %q != %q", first, second)
	}
}

func TestParseStringMapRoundTrip(t *testing.T) {
	out, err := parseStringMap(`{"zone_id":"z1","account_id":"a1"}`)
	if err != nil {
		t.Fatalf("parseStringMap() error = %v", err)
	}
	if out["zone_id"] != "z1" || out["account_id"] != "a1" {
		t.Fatalf("unexpected parsed attrs: %#v", out)
	}
}

func TestAccountResourceNormalization(t *testing.T) {
	p := &CloudflareProvider{}
	resource := p.accountResource(accounts.Account{
		ID:   "acc-1",
		Name: "Primary",
		Type: accounts.AccountTypeEnterprise,
		ManagedBy: accounts.AccountManagedBy{
			ParentOrgID:   "org-1",
			ParentOrgName: "Parent Org",
		},
		Settings: accounts.AccountSettings{
			AbuseContactEmail: "abuse@example.com",
			EnforceTwofactor:  true,
		},
	})
	attrs := decodeAttrs(resource.Attributes)
	if resource.Type != "account" || resource.AccountId != "acc-1" {
		t.Fatalf("unexpected account resource core fields: %#v", resource)
	}
	if attrs["managed_by_org"] != "Parent Org" || attrs["enforce_twofactor"] != "true" {
		t.Fatalf("unexpected account attrs: %#v", attrs)
	}
}

func TestDNSRecordResourceNormalization(t *testing.T) {
	p := &CloudflareProvider{}
	zone := zoneFixture()
	resource := p.dnsRecordResource(zone, dns.RecordResponse{
		ID:        "rec-1",
		Name:      "www.example.com",
		Type:      dns.RecordResponseTypeA,
		Content:   "203.0.113.10",
		TTL:       dns.TTL(300),
		Proxied:   true,
		Proxiable: true,
	})
	attrs := decodeAttrs(resource.Attributes)
	if resource.Type != "dns_record" || resource.ParentId != zone.ID {
		t.Fatalf("unexpected dns resource core fields: %#v", resource)
	}
	if attrs["zone_name"] != zone.Name || attrs["ttl"] != "300" || attrs["record_type"] != "A" {
		t.Fatalf("unexpected dns attrs: %#v", attrs)
	}
}

func TestWorkerRouteResourceNormalization(t *testing.T) {
	p := &CloudflareProvider{}
	resource := p.workerRouteResource("zone-1", "example.com", "route-1", "example.com/*", "worker-a", workers.RouteListResponse{ID: "route-1", Pattern: "example.com/*", Script: "worker-a"})
	attrs := decodeAttrs(resource.Attributes)
	if resource.Type != "worker_route" || resource.ParentId != "zone-1" {
		t.Fatalf("unexpected worker route core fields: %#v", resource)
	}
	if attrs["script"] != "worker-a" || attrs["zone_name"] != "example.com" {
		t.Fatalf("unexpected worker route attrs: %#v", attrs)
	}
}

func TestStorageResourceNormalization(t *testing.T) {
	p := &CloudflareProvider{}
	bucket := p.r2BucketResource("acc-1", r2.Bucket{Name: "bucket-a", Location: r2.BucketLocationWnam, Jurisdiction: r2.BucketJurisdictionDefault, StorageClass: r2.BucketStorageClassStandard})
	queue := p.queueResource("acc-1", queues.Queue{QueueID: "q-1", QueueName: "jobs"})
	bucketAttrs := decodeAttrs(bucket.Attributes)
	queueAttrs := decodeAttrs(queue.Attributes)
	if bucket.Type != "r2_bucket" || bucketAttrs["bucket_name"] != "bucket-a" {
		t.Fatalf("unexpected bucket resource: %#v %#v", bucket, bucketAttrs)
	}
	if queue.Type != "queue" || queueAttrs["queue_name"] != "jobs" {
		t.Fatalf("unexpected queue resource: %#v %#v", queue, queueAttrs)
	}
}

func TestSupportedResourcesForImplementedServices(t *testing.T) {
	got := supportedResourcesForService("data")
	want := []string{"d1_database", "durable_object_namespace", "durable_object", "secret_store", "secret_store_secret"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("supportedResourcesForService(data) = %#v, want %#v", got, want)
	}
}
