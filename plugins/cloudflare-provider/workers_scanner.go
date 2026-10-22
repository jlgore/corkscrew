package main

import (
	"context"
	"fmt"
	"strings"

	cloudflare "github.com/cloudflare/cloudflare-go/v6"
	"github.com/cloudflare/cloudflare-go/v6/workers"
	pb "github.com/jlgore/corkscrew/internal/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (p *CloudflareProvider) scanWorkers(ctx context.Context) ([]*pb.Resource, []string) {
	accountIDs, err := p.accountIDsForWorkers(ctx)
	resources := make([]*pb.Resource, 0)
	var errs []string
	if err != nil {
		errs = append(errs, fmt.Sprintf("accounts list failed: %v", err))
	}
	zonesList, zoneErr := p.listZones(ctx)
	if zoneErr != nil {
		errs = append(errs, fmt.Sprintf("zones list failed: %v", zoneErr))
	}

	for _, accountID := range accountIDs {
		scripts, err := p.listWorkerScripts(ctx, accountID)
		if err != nil {
			errs = append(errs, fmt.Sprintf("workers scripts list failed for account %s: %v", accountID, err))
		}
		for _, script := range scripts {
			resources = append(resources, p.workerScriptResource(accountID, script))
		}

		domains, err := p.listWorkerDomains(ctx, accountID)
		if err != nil {
			errs = append(errs, fmt.Sprintf("workers domains list failed for account %s: %v", accountID, err))
		}
		for _, domain := range domains {
			resources = append(resources, p.workerDomainResource(accountID, domain.ID, domain.Hostname, domain.Service, domain.ZoneID, domain.ZoneName, domain.Environment, domain.CERTID, domain))
		}
	}

	for _, zone := range zonesList {
		routes, err := p.listWorkerRoutes(ctx, zone.ID)
		if err != nil {
			errs = append(errs, fmt.Sprintf("workers routes list failed for zone %s: %v", zone.Name, err))
		}
		for _, route := range routes {
			resources = append(resources, p.workerRouteResource(zone.ID, zone.Name, route.ID, route.Pattern, route.Script, route))
		}
	}

	return resources, errs
}

func (p *CloudflareProvider) accountIDsForWorkers(ctx context.Context) ([]string, error) {
	if len(p.config.Scope.AccountIDs) > 0 {
		return append([]string(nil), p.config.Scope.AccountIDs...), nil
	}
	accountsList, err := p.listAccounts(ctx)
	ids := make([]string, 0, len(accountsList))
	for _, account := range accountsList {
		ids = append(ids, account.ID)
	}
	return ids, err
}

func (p *CloudflareProvider) listWorkerScripts(ctx context.Context, accountID string) ([]workers.ScriptListResponse, error) {
	iter := p.client.Workers.Scripts.ListAutoPaging(ctx, workers.ScriptListParams{AccountID: cloudflare.F(accountID)})
	return collectAutoPaging(iter)
}

func (p *CloudflareProvider) listWorkerDomains(ctx context.Context, accountID string) ([]workers.DomainListResponse, error) {
	iter := p.client.Workers.Domains.ListAutoPaging(ctx, workers.DomainListParams{AccountID: cloudflare.F(accountID)})
	return collectAutoPaging(iter)
}

func (p *CloudflareProvider) listWorkerRoutes(ctx context.Context, zoneID string) ([]workers.RouteListResponse, error) {
	iter := p.client.Workers.Routes.ListAutoPaging(ctx, workers.RouteListParams{ZoneID: cloudflare.F(zoneID)})
	return collectAutoPaging(iter)
}

// workerScriptID is a worker script's resource ID. Script names are unique only within an
// account, so the ID carries the account, in the same shape as an R2 bucket's.
func workerScriptID(accountID, scriptName string) string {
	return accountID + "/workers/" + scriptName
}

// scriptNameFromID returns the script name in a workerScriptID, or id unchanged when it isn't
// one (a bare script name, the ID before it was account-scoped).
func scriptNameFromID(id string) string {
	if _, name, ok := strings.Cut(id, "/workers/"); ok {
		return name
	}
	return id
}

func (p *CloudflareProvider) workerScriptResource(accountID string, script workers.ScriptListResponse) *pb.Resource {
	attrs := map[string]string{
		"account_id":            accountID,
		"script_id":             script.ID,
		"script_name":           script.ID,
		"compatibility_date":    script.CompatibilityDate,
		"etag":                  script.Etag,
		"has_assets":            fmt.Sprintf("%t", script.HasAssets),
		"has_modules":           fmt.Sprintf("%t", script.HasModules),
		"last_deployed_from":    script.LastDeployedFrom,
		"logpush":               fmt.Sprintf("%t", script.Logpush),
		"migration_tag":         script.MigrationTag,
		"observability_enabled": fmt.Sprintf("%t", script.Observability.Enabled),
		"usage_model":           string(script.UsageModel),
	}
	if len(script.CompatibilityFlags) > 0 {
		attrs["compatibility_flags"] = strings.Join(script.CompatibilityFlags, ",")
	}
	if len(script.Handlers) > 0 {
		attrs["handlers"] = strings.Join(script.Handlers, ",")
	}
	if len(script.Tags) > 0 {
		attrs["tags"] = strings.Join(script.Tags, ",")
	}
	resource := p.resource("workers", "worker_script", workerScriptID(accountID, script.ID), script.ID, accountID, script, attrs)
	if !script.CreatedOn.IsZero() {
		resource.CreatedAt = timestamppb.New(script.CreatedOn)
	}
	if !script.ModifiedOn.IsZero() {
		resource.ModifiedAt = timestamppb.New(script.ModifiedOn)
	}
	return resource
}

func (p *CloudflareProvider) workerRouteResource(zoneID, zoneName, routeID, pattern, script string, raw interface{}) *pb.Resource {
	attrs := map[string]string{
		"zone_id":   zoneID,
		"zone_name": zoneName,
		"pattern":   pattern,
		"script":    script,
	}
	resource := p.resource("workers", "worker_route", routeID, pattern, zoneID, raw, attrs)
	resource.ParentId = zoneID
	return resource
}

func (p *CloudflareProvider) workerDomainResource(accountID, domainID, hostname, service, zoneID, zoneName, environment, certID string, raw interface{}) *pb.Resource {
	attrs := map[string]string{
		"account_id":  accountID,
		"hostname":    hostname,
		"service":     service,
		"zone_id":     zoneID,
		"zone_name":   zoneName,
		"environment": environment,
		"cert_id":     certID,
	}
	resource := p.resource("workers", "worker_domain", domainID, hostname, accountID, raw, attrs)
	resource.ParentId = zoneID
	return resource
}
