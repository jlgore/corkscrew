package main

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"time"

	pb "github.com/jlgore/corkscrew/internal/proto"
	"github.com/jlgore/corkscrew/internal/shared"
)

const listCacheTTL = 10 * time.Minute

// listPageSizeDefault applies when a ListResources caller sets no MaxResults. It used to mean
// "every ref in one response", which for a large account can exceed gRPC's message size limit.
const listPageSizeDefault = 1000

// listEntry caches one listing. errors holds the scan's entries; any non-notice entry means
// refs may be incomplete.
type listEntry struct {
	refs      []*pb.ResourceRef
	errors    []string
	expiresAt time.Time
}

func (p *CloudflareProvider) listResourcesPaged(ctx context.Context, req *pb.ListResourcesRequest) (*pb.ListResourcesResponse, error) {
	key := listCacheKey(req.GetService(), req.GetFilters())
	entry, err := p.refsForKey(ctx, key, req)
	if err != nil {
		return nil, err
	}
	refs := entry.refs

	offset := 0
	if req.NextToken != "" {
		if n, err := strconv.Atoi(req.NextToken); err == nil && n >= 0 {
			offset = n
		}
	}
	if offset > len(refs) {
		offset = len(refs)
	}

	pageSize := listPageSizeDefault
	if req.MaxResults > 0 {
		pageSize = int(req.MaxResults)
	}
	page := refs[offset:]
	nextToken := ""
	if pageSize < len(page) {
		page = page[:pageSize]
		nextToken = strconv.Itoa(offset + len(page))
	}

	response := &pb.ListResourcesResponse{
		Resources:  page,
		TotalCount: int32(len(refs)),
		NextToken:  nextToken,
	}
	if scanErrors, _ := shared.SplitScanErrors(entry.errors); len(scanErrors) > 0 {
		// The listing may be missing resources; say so instead of presenting it as complete.
		response.Metadata = map[string]string{
			"partial": "true",
			"errors":  strings.Join(scanErrors, "\n"),
		}
	}
	return response, nil
}

// refsForKey returns the cached listing for key, scanning when there is none. A partial
// listing is still cached so continuation pages stay consistent, but a first-page request
// (no NextToken) rescans rather than replaying the failure for the rest of the TTL.
func (p *CloudflareProvider) refsForKey(
	ctx context.Context, key string, req *pb.ListResourcesRequest,
) (*listEntry, error) {
	p.gcListCache()
	if cached, ok := p.listCache.Load(key); ok {
		entry := cached.(*listEntry)
		scanErrors, _ := shared.SplitScanErrors(entry.errors)
		fresh := time.Now().Before(entry.expiresAt)
		if fresh && (len(scanErrors) == 0 || req.GetNextToken() != "") {
			return entry, nil
		}
		p.listCache.Delete(key)
	}

	resp, err := p.ScanService(ctx, &pb.ScanServiceRequest{Service: req.GetService(), Filters: req.GetFilters()})
	if err != nil {
		return nil, err
	}
	refs := make([]*pb.ResourceRef, 0, len(resp.Resources))
	for _, resource := range resp.Resources {
		ref := &pb.ResourceRef{
			Id:        resource.Id,
			Name:      resource.Name,
			Type:      resource.Type,
			Service:   resource.Service,
			Region:    resource.Region,
			AccountId: resource.AccountId,
			BasicAttributes: map[string]string{
				"provider": "cloudflare",
			},
		}
		if attrs := decodeAttrs(resource.Attributes); attrs != nil {
			for key, value := range attrs {
				if value != "" {
					ref.BasicAttributes[key] = value
				}
			}
		}
		refs = append(refs, ref)
	}
	entry := &listEntry{
		refs:      refs,
		errors:    append([]string(nil), resp.Errors...),
		expiresAt: time.Now().Add(listCacheTTL),
	}
	p.listCache.Store(key, entry)
	return entry, nil
}

func (p *CloudflareProvider) gcListCache() {
	now := time.Now()
	p.listCache.Range(func(key, value interface{}) bool {
		if now.After(value.(*listEntry).expiresAt) {
			p.listCache.Delete(key)
		}
		return true
	})
}

func listCacheKey(service string, filters map[string]string) string {
	keys := make([]string, 0, len(filters))
	for key := range filters {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(service)
	b.WriteByte('|')
	for _, key := range keys {
		b.WriteString(key)
		b.WriteByte('=')
		b.WriteString(filters[key])
		b.WriteByte(';')
	}
	return b.String()
}
