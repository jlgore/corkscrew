package main

import (
	"context"
	"fmt"
	"strings"

	cloudflare "github.com/cloudflare/cloudflare-go/v6"
	"github.com/cloudflare/cloudflare-go/v6/accounts"
	dns "github.com/cloudflare/cloudflare-go/v6/dns"
	"github.com/cloudflare/cloudflare-go/v6/zones"
	pb "github.com/jlgore/corkscrew/internal/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (p *CloudflareProvider) scanAccounts(ctx context.Context) ([]*pb.Resource, []string) {
	accountsList, err := p.listAccounts(ctx)
	if err != nil {
		return nil, []string{fmt.Sprintf("accounts list failed: %v", err)}
	}
	resources := make([]*pb.Resource, 0, len(accountsList))
	for _, account := range accountsList {
		resources = append(resources, p.accountResource(account))
	}
	return resources, nil
}

func (p *CloudflareProvider) scanZones(ctx context.Context) ([]*pb.Resource, []string) {
	zonesList, err := p.listZones(ctx)
	if err != nil {
		return nil, []string{fmt.Sprintf("zones list failed: %v", err)}
	}
	resources := make([]*pb.Resource, 0, len(zonesList))
	for _, zone := range zonesList {
		resources = append(resources, p.zoneResource(zone))
	}
	return resources, nil
}

func (p *CloudflareProvider) scanDNS(ctx context.Context) ([]*pb.Resource, []string) {
	zonesList, err := p.listZones(ctx)
	if err != nil {
		return nil, []string{fmt.Sprintf("zones list failed: %v", err)}
	}
	resources := make([]*pb.Resource, 0)
	var errs []string
	for _, zone := range zonesList {
		records, err := p.listDNSRecords(ctx, zone.ID)
		if err != nil {
			errs = append(errs, fmt.Sprintf("dns records list failed for zone %s: %v", zone.Name, err))
			continue
		}
		for _, record := range records {
			resources = append(resources, p.dnsRecordResource(zone, record))
		}
	}
	return resources, errs
}

func (p *CloudflareProvider) listAccounts(ctx context.Context) ([]accounts.Account, error) {
	iter := p.client.Accounts.ListAutoPaging(ctx, accounts.AccountListParams{})
	allowedIDs := stringSet(p.config.Scope.AccountIDs)
	results := make([]accounts.Account, 0)
	for iter.Next() {
		account := iter.Current()
		if len(allowedIDs) > 0 {
			if _, ok := allowedIDs[account.ID]; !ok {
				continue
			}
		}
		results = append(results, account)
	}
	if err := iter.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

func (p *CloudflareProvider) listZones(ctx context.Context) ([]zones.Zone, error) {
	zoneByID := make(map[string]zones.Zone)
	if len(p.config.Scope.ZoneIDs) > 0 {
		for _, zoneID := range p.config.Scope.ZoneIDs {
			zone, err := p.client.Zones.Get(ctx, zones.ZoneGetParams{ZoneID: cloudflare.F(zoneID)})
			if err != nil {
				return nil, err
			}
			if p.zoneAllowed(*zone) {
				zoneByID[zone.ID] = *zone
			}
		}
		return mapZoneValues(zoneByID), nil
	}

	if len(p.config.Scope.AccountIDs) > 0 {
		for _, accountID := range p.config.Scope.AccountIDs {
			iter := p.client.Zones.ListAutoPaging(ctx, zones.ZoneListParams{Account: cloudflare.F(zones.ZoneListParamsAccount{ID: cloudflare.F(accountID)})})
			for iter.Next() {
				zone := iter.Current()
				if p.zoneAllowed(zone) {
					zoneByID[zone.ID] = zone
				}
			}
			if err := iter.Err(); err != nil {
				return nil, err
			}
		}
		return mapZoneValues(zoneByID), nil
	}

	iter := p.client.Zones.ListAutoPaging(ctx, zones.ZoneListParams{})
	for iter.Next() {
		zone := iter.Current()
		if p.zoneAllowed(zone) {
			zoneByID[zone.ID] = zone
		}
	}
	if err := iter.Err(); err != nil {
		return nil, err
	}
	return mapZoneValues(zoneByID), nil
}

func (p *CloudflareProvider) listDNSRecords(ctx context.Context, zoneID string) ([]dns.RecordResponse, error) {
	iter := p.client.DNS.Records.ListAutoPaging(ctx, dns.RecordListParams{ZoneID: cloudflare.F(zoneID)})
	results := make([]dns.RecordResponse, 0)
	for iter.Next() {
		results = append(results, iter.Current())
	}
	if err := iter.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

func (p *CloudflareProvider) zoneAllowed(zone zones.Zone) bool {
	if len(p.config.Scope.IncludeZones) > 0 {
		allowed := false
		for _, name := range p.config.Scope.IncludeZones {
			if strings.EqualFold(name, zone.Name) {
				allowed = true
				break
			}
		}
		if !allowed {
			return false
		}
	}
	for _, name := range p.config.Scope.ExcludeZones {
		if strings.EqualFold(name, zone.Name) {
			return false
		}
	}
	return true
}

func mapZoneValues(zoneByID map[string]zones.Zone) []zones.Zone {
	values := make([]zones.Zone, 0, len(zoneByID))
	for _, zone := range zoneByID {
		values = append(values, zone)
	}
	return values
}

func (p *CloudflareProvider) accountResource(account accounts.Account) *pb.Resource {
	attrs := map[string]string{
		"name":                account.Name,
		"type":                string(account.Type),
		"managed_by_org_id":   account.ManagedBy.ParentOrgID,
		"managed_by_org":      account.ManagedBy.ParentOrgName,
		"abuse_contact_email": account.Settings.AbuseContactEmail,
		"enforce_twofactor":   fmt.Sprintf("%t", account.Settings.EnforceTwofactor),
	}
	resource := p.resource("accounts", "account", account.ID, account.Name, account.ID, account, attrs)
	if !account.CreatedOn.IsZero() {
		resource.CreatedAt = timestamppb.New(account.CreatedOn)
	}
	return resource
}

func (p *CloudflareProvider) zoneResource(zone zones.Zone) *pb.Resource {
	attrs := map[string]string{
		"account_id":               zone.Account.ID,
		"account_name":             zone.Account.Name,
		"status":                   string(zone.Status),
		"type":                     string(zone.Type),
		"paused":                   fmt.Sprintf("%t", zone.Paused),
		"development_mode":         fmt.Sprintf("%.0f", zone.DevelopmentMode),
		"cdn_only":                 fmt.Sprintf("%t", zone.Meta.CDNOnly),
		"dns_only":                 fmt.Sprintf("%t", zone.Meta.DNSOnly),
		"foundation_dns":           fmt.Sprintf("%t", zone.Meta.FoundationDNS),
		"page_rule_quota":          fmt.Sprintf("%d", zone.Meta.PageRuleQuota),
		"custom_certificate_quota": fmt.Sprintf("%d", zone.Meta.CustomCertificateQuota),
		"verification_key":         zone.VerificationKey,
	}
	if len(zone.NameServers) > 0 {
		attrs["name_servers"] = strings.Join(zone.NameServers, ",")
	}
	if len(zone.VanityNameServers) > 0 {
		attrs["vanity_name_servers"] = strings.Join(zone.VanityNameServers, ",")
	}
	resource := p.resource("zones", "zone", zone.ID, zone.Name, firstNonEmpty(zone.Account.ID, zone.ID), zone, attrs)
	if !zone.CreatedOn.IsZero() {
		resource.CreatedAt = timestamppb.New(zone.CreatedOn)
	}
	if !zone.ModifiedOn.IsZero() {
		resource.ModifiedAt = timestamppb.New(zone.ModifiedOn)
	}
	return resource
}

func (p *CloudflareProvider) dnsRecordResource(zone zones.Zone, record dns.RecordResponse) *pb.Resource {
	attrs := map[string]string{
		"zone_id":         zone.ID,
		"zone_name":       zone.Name,
		"record_name":     record.Name,
		"record_type":     string(record.Type),
		"content":         record.Content,
		"ttl":             fmt.Sprintf("%.0f", record.TTL),
		"proxied":         fmt.Sprintf("%t", record.Proxied),
		"proxiable":       fmt.Sprintf("%t", record.Proxiable),
		"priority":        fmt.Sprintf("%.0f", record.Priority),
		"comment":         record.Comment,
		"private_routing": fmt.Sprintf("%t", record.PrivateRouting),
		"account_id":      zone.Account.ID,
		"account_name":    zone.Account.Name,
	}
	resource := p.resource("dns", "dns_record", record.ID, record.Name, firstNonEmpty(zone.Account.ID, zone.ID), record, attrs)
	resource.ParentId = zone.ID
	if !record.CreatedOn.IsZero() {
		resource.CreatedAt = timestamppb.New(record.CreatedOn)
	}
	if !record.ModifiedOn.IsZero() {
		resource.ModifiedAt = timestamppb.New(record.ModifiedOn)
	}
	return resource
}
