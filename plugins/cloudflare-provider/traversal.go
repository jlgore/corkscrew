package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
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
	if len(p.config.Scope.AccountIDs) > 0 {
		return p.getScopedAccounts(ctx)
	}
	accountsList, err := p.listAccounts(ctx)
	var errs []string
	if err != nil {
		errs = append(errs, fmt.Sprintf("accounts list failed: %v", err))
	}
	resources := make([]*pb.Resource, 0, len(accountsList))
	for _, account := range accountsList {
		resources = append(resources, p.accountResource(account))
	}
	return resources, errs
}

// getScopedAccounts fetches the configured accounts directly, rather than listing every account
// the credential can see and filtering.
func (p *CloudflareProvider) getScopedAccounts(ctx context.Context) ([]*pb.Resource, []string) {
	resources := make([]*pb.Resource, 0, len(p.config.Scope.AccountIDs))
	var errs []string
	for _, accountID := range p.config.Scope.AccountIDs {
		params := accounts.AccountGetParams{AccountID: cloudflare.F(accountID)}
		account, err := p.client.Accounts.Get(ctx, params)
		if err != nil {
			errs = append(errs, fmt.Sprintf("account %s get failed: %v", accountID, err))
			continue
		}
		resources = append(resources, p.accountResource(*account))
	}
	return resources, errs
}

func (p *CloudflareProvider) scanZones(ctx context.Context) ([]*pb.Resource, []string) {
	zonesList, err := p.listZones(ctx)
	var errs []string
	if err != nil {
		errs = append(errs, fmt.Sprintf("zones list failed: %v", err))
	}
	resources := make([]*pb.Resource, 0, len(zonesList))
	for _, zone := range zonesList {
		resources = append(resources, p.zoneResource(zone))
	}
	return resources, errs
}

func (p *CloudflareProvider) scanDNS(ctx context.Context) ([]*pb.Resource, []string) {
	zonesList, err := p.listZones(ctx)
	resources := make([]*pb.Resource, 0)
	var errs []string
	if err != nil {
		errs = append(errs, fmt.Sprintf("zones list failed: %v", err))
	}
	for _, zone := range zonesList {
		records, err := p.listDNSRecords(ctx, zone.ID)
		if err != nil {
			errs = append(errs, fmt.Sprintf("dns records list failed for zone %s: %v", zone.Name, err))
		}
		for _, record := range records {
			resources = append(resources, p.dnsRecordResource(zone, record))
		}
	}
	return resources, errs
}

// listItemsMax caps the items collected from one ListAutoPaging listing. The SDK pagers keep
// requesting pages until one comes back empty, so a server that repeats a page would loop until
// the deadline; 100k is far beyond any real account's count for one listing. A variable so tests
// can lower it.
var listItemsMax = 100_000

// errListItemCap means a listing reached listItemsMax; the items collected are incomplete.
var errListItemCap = errors.New("listing reached item cap")

// errListRepeated means a listing returned items it had already returned, with enough items that
// later pages may exist that the server never served; the items collected may be incomplete.
var errListRepeated = errors.New("listing repeated items; later pages may be missing")

// repeatCompleteItemsMax is Cloudflare's smallest default page size. A listing that repeats
// before returning this many items fit on one page: the server is serving that page again for
// every page number (the accounts listing does this for account-owned tokens), so it is complete.
const repeatCompleteItemsMax = 20

// autoPager is the iteration surface shared by the SDK's ListAutoPaging pagers.
type autoPager[T any] interface {
	Next() bool
	Current() T
	Err() error
}

// collectAutoPaging drains iter, stopping with errListItemCap once listItemsMax items have been
// collected. The SDK pagers stop only on an empty page, so it also stops when an item comes back
// that it already returned: a server that ignores the page number would otherwise be paged until
// the cap or the deadline. A repeat within the first repeatCompleteItemsMax items ends a complete
// listing; a later one returns errListRepeated. Items gathered before an error are returned with
// it, so callers keep what they observed and report the listing as incomplete.
func collectAutoPaging[T any](iter autoPager[T]) ([]T, error) {
	itemsMax := listItemsMax
	results := make([]T, 0)
	seen := map[[sha256.Size]byte]bool{}
	for iter.Next() {
		if len(results) >= itemsMax {
			return results, fmt.Errorf("%w (%d items)", errListItemCap, itemsMax)
		}
		item := iter.Current()
		if key, ok := itemKey(item); ok {
			if seen[key] {
				if len(results) < repeatCompleteItemsMax {
					return results, nil
				}
				return results, fmt.Errorf("%w (after %d items)", errListRepeated, len(results))
			}
			seen[key] = true
		}
		results = append(results, item)
	}
	return results, iter.Err()
}

// itemKey identifies an item by its content. An item that can't be encoded isn't tracked.
func itemKey[T any](item T) ([sha256.Size]byte, bool) {
	data, err := json.Marshal(item)
	if err != nil {
		return [sha256.Size]byte{}, false
	}
	return sha256.Sum256(data), true
}

func (p *CloudflareProvider) listAccounts(ctx context.Context) ([]accounts.Account, error) {
	iter := p.client.Accounts.ListAutoPaging(ctx, accounts.AccountListParams{})
	listed, err := collectAutoPaging(iter)
	allowedIDs := stringSet(p.config.Scope.AccountIDs)
	results := make([]accounts.Account, 0, len(listed))
	for _, account := range listed {
		if len(allowedIDs) > 0 {
			if _, ok := allowedIDs[account.ID]; !ok {
				continue
			}
		}
		results = append(results, account)
	}
	return results, err
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
			listed, err := collectAutoPaging(iter)
			p.addAllowedZones(zoneByID, listed)
			if err != nil {
				return mapZoneValues(zoneByID), err
			}
		}
		return mapZoneValues(zoneByID), nil
	}

	listed, err := collectAutoPaging(p.client.Zones.ListAutoPaging(ctx, zones.ZoneListParams{}))
	p.addAllowedZones(zoneByID, listed)
	return mapZoneValues(zoneByID), err
}

func (p *CloudflareProvider) addAllowedZones(zoneByID map[string]zones.Zone, listed []zones.Zone) {
	for _, zone := range listed {
		if p.zoneAllowed(zone) {
			zoneByID[zone.ID] = zone
		}
	}
}

func (p *CloudflareProvider) listDNSRecords(ctx context.Context, zoneID string) ([]dns.RecordResponse, error) {
	iter := p.client.DNS.Records.ListAutoPaging(ctx, dns.RecordListParams{ZoneID: cloudflare.F(zoneID)})
	return collectAutoPaging(iter)
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
