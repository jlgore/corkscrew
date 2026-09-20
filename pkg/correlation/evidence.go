// Package correlation defines the provider-neutral correlation evidence
// contract shared by provider plugins and Corkscrew's materializer.
package correlation

import (
	"encoding/json"
	"fmt"
	"net"
	"sort"
	"strings"
)

const AttributeKey = "corkscrew_correlation"
const Version = "1"

type Kind string

const (
	KindIP           Kind = "ip"
	KindDNS          Kind = "dns"
	KindNetwork      Kind = "network"
	KindLoadBalancer Kind = "load-balancer"
	KindConnectivity Kind = "connectivity"
	KindSecurity     Kind = "security"
	KindDomain       Kind = "domain"
	KindIdentity     Kind = "identity"
	KindPolicy       Kind = "policy"
	KindSecret       Kind = "secret"
)

func AllKinds() []Kind {
	return []Kind{KindIP, KindDNS, KindNetwork, KindLoadBalancer, KindConnectivity,
		KindSecurity, KindDomain, KindIdentity, KindPolicy, KindSecret}
}

type Envelope struct {
	Version  string     `json:"version"`
	Evidence []Evidence `json:"evidence"`
}

// Evidence values use the column names of the corresponding core-owned
// correlation table. Context fields such as provider and resource_id are
// inherited from the containing resource when omitted.
type Evidence struct {
	ID         string         `json:"id"`
	Kind       Kind           `json:"kind"`
	Subtype    string         `json:"subtype,omitempty"`
	Confidence float64        `json:"confidence,omitempty"`
	Method     string         `json:"method,omitempty"`
	Values     map[string]any `json:"values"`
}

func ParseAttributes(attributes string) (Envelope, error) {
	if strings.TrimSpace(attributes) == "" {
		return Envelope{Version: Version}, nil
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal([]byte(attributes), &root); err != nil {
		return Envelope{}, fmt.Errorf("parse resource attributes: %w", err)
	}
	raw, ok := root[AttributeKey]
	if !ok {
		return Envelope{Version: Version}, nil
	}
	var envelope Envelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return Envelope{}, fmt.Errorf("parse %s envelope: %w", AttributeKey, err)
	}
	if envelope.Version != Version {
		return Envelope{}, fmt.Errorf("unsupported correlation evidence version %q", envelope.Version)
	}
	for index := range envelope.Evidence {
		if err := envelope.Evidence[index].Validate(); err != nil {
			return Envelope{}, fmt.Errorf("evidence %d: %w", index, err)
		}
	}
	return envelope, nil
}

func Attach(attributes string, evidence ...Evidence) (string, error) {
	root := map[string]any{}
	existing := []Evidence{}
	if strings.TrimSpace(attributes) != "" {
		if err := json.Unmarshal([]byte(attributes), &root); err != nil {
			return "", fmt.Errorf("parse resource attributes: %w", err)
		}
		if raw, ok := root[AttributeKey]; ok {
			encoded, _ := json.Marshal(raw)
			var envelope Envelope
			if json.Unmarshal(encoded, &envelope) == nil && envelope.Version == Version {
				existing = envelope.Evidence
			}
		}
	}
	evidence = append(existing, evidence...)
	envelope := Envelope{Version: Version, Evidence: evidence}
	for index := range evidence {
		if err := evidence[index].Validate(); err != nil {
			return "", fmt.Errorf("evidence %d: %w", index, err)
		}
	}
	root[AttributeKey] = envelope
	encoded, err := json.Marshal(root)
	return string(encoded), err
}

// InferNetworkEvidence extracts conservative IP and DNS evidence from a
// provider resource's configuration. Providers opt into this helper at their
// scan boundary; core storage never parses provider-specific raw data.
func InferNetworkEvidence(rawData string) []Evidence {
	var root any
	if json.Unmarshal([]byte(rawData), &root) != nil {
		return nil
	}
	var result []Evidence
	seen := map[string]bool{}
	var walk func(any, string)
	walk = func(value any, path string) {
		switch typed := value.(type) {
		case map[string]any:
			keys := make([]string, 0, len(typed))
			for key := range typed {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				childPath := strings.Trim(path+"."+key, ".")
				if text, ok := typed[key].(string); ok {
					lower := strings.ToLower(key)
					if ip := net.ParseIP(strings.TrimSpace(text)); ip != nil && containsAny(lower, "ip", "address") {
						version := "ipv6"
						if ip.To4() != nil {
							version = "ipv4"
						}
						ipType := "public"
						if ip.IsPrivate() {
							ipType = "private"
						}
						id := "ip:" + childPath + ":" + text
						if !seen[id] {
							seen[id] = true
							result = append(result, Evidence{ID: id, Kind: KindIP, Confidence: .8, Method: "provider_common_fields", Values: map[string]any{"ip_address": text, "ip_version": version, "ip_type": ipType}})
						}
					}
					if strings.Contains(text, ".") && containsAny(lower, "dns", "hostname", "fqdn", "domain") {
						name := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(text)), ".")
						id := "dns:" + childPath + ":" + name
						if !seen[id] {
							seen[id] = true
							result = append(result, Evidence{ID: id, Kind: KindDNS, Confidence: .7, Method: "provider_common_fields", Values: map[string]any{"dns_name": name, "record_type": "REFERENCE", "record_values": []string{name}}})
						}
					}
				}
				walk(typed[key], childPath)
			}
		case []any:
			for index, item := range typed {
				walk(item, fmt.Sprintf("%s[%d]", path, index))
			}
		}
	}
	walk(root, "")
	return result
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

func (e Evidence) Validate() error {
	if strings.TrimSpace(e.ID) == "" {
		return fmt.Errorf("id is required")
	}
	valid := false
	for _, kind := range AllKinds() {
		if e.Kind == kind {
			valid = true
			break
		}
	}
	if !valid {
		return fmt.Errorf("unsupported kind %q", e.Kind)
	}
	if e.Confidence < 0 || e.Confidence > 1 {
		return fmt.Errorf("confidence must be between 0 and 1")
	}
	if len(e.Values) == 0 {
		return fmt.Errorf("values are required")
	}
	if e.Kind == KindSecret {
		if _, forbidden := e.Values["secret_value"]; forbidden {
			return fmt.Errorf("secret_value is forbidden; emit only a non-sensitive fingerprint")
		}
	}
	return nil
}
