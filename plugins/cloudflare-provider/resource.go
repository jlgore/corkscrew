package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	pb "github.com/jlgore/corkscrew/internal/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (p *CloudflareProvider) resource(service, typ, id, name, accountID string, raw interface{}, attrs map[string]string) *pb.Resource {
	if id == "" {
		id = name
	}
	rawJSON := ""
	if raw != nil {
		rawJSON = mustJSON(raw)
	}
	if attrs == nil {
		attrs = map[string]string{}
	}
	return &pb.Resource{
		Provider:     "cloudflare",
		Service:      service,
		Type:         typ,
		Id:           id,
		Name:         name,
		AccountId:    accountID,
		RawData:      rawJSON,
		Attributes:   mustJSON(attrs),
		DiscoveredAt: timestamppb.Now(),
	}
}

func mustJSON(value interface{}) string {
	data, err := json.Marshal(value)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[cloudflare-provider] mustJSON encode failure: %v (value type=%T)\n", err, value)
		return "{}"
	}
	return string(data)
}

func decodeAttrs(raw string) map[string]string {
	if raw == "" {
		return nil
	}
	out, _ := parseStringMap(raw)
	return out
}

func parseStringMap(raw string) (map[string]string, error) {
	out := map[string]string{}
	if raw == "" || raw == "null" {
		return out, nil
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
