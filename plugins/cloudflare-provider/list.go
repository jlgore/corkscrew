package main

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"time"

	pb "github.com/jlgore/corkscrew/internal/proto"
)

const listCacheTTL = 10 * time.Minute

type listEntry struct {
	refs      []*pb.ResourceRef
	expiresAt time.Time
}

func (p *CloudflareProvider) listResourcesPaged(ctx context.Context, req *pb.ListResourcesRequest) (*pb.ListResourcesResponse, error) {
	key := listCacheKey(req.GetService(), req.GetFilters())
	refs, err := p.refsForKey(ctx, key, req)
	if err != nil {
		return nil, err
	}

	offset := 0
	if req.NextToken != "" {
		if n, err := strconv.Atoi(req.NextToken); err == nil && n >= 0 {
			offset = n
		}
	}
	if offset > len(refs) {
		offset = len(refs)
	}

	page := refs[offset:]
	nextToken := ""
	if req.MaxResults > 0 && int(req.MaxResults) < len(page) {
		page = page[:req.MaxResults]
		nextToken = strconv.Itoa(offset + len(page))
	}

	return &pb.ListResourcesResponse{Resources: page, TotalCount: int32(len(refs)), NextToken: nextToken}, nil
}

func (p *CloudflareProvider) refsForKey(ctx context.Context, key string, req *pb.ListResourcesRequest) ([]*pb.ResourceRef, error) {
	p.gcListCache()
	if cached, ok := p.listCache.Load(key); ok {
		entry := cached.(*listEntry)
		if time.Now().Before(entry.expiresAt) {
			return entry.refs, nil
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
	p.listCache.Store(key, &listEntry{refs: refs, expiresAt: time.Now().Add(listCacheTTL)})
	return refs, nil
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
