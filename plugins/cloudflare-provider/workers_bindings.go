package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/cloudflare/cloudflare-go/v6/workers"
	pb "github.com/jlgore/corkscrew/internal/proto"
)

// bindingReferenceKeys are the binding fields that may be stored: names, IDs, and the
// references that say what a binding reaches. Everything else is dropped, including the value
// fields (plain_text and secret_text "text", "json", secret_key "key_base64" and "key_jwk") and
// any field a new binding kind adds, so a value can't reach storage by being unanticipated.
var bindingReferenceKeys = map[string]bool{
	"name": true, "type": true, "id": true,
	"bucket_name": true, "jurisdiction": true,
	"namespace_id": true, "namespace": true, "dispatch_namespace": true,
	"database_id": true, "queue_name": true,
	"class_name": true, "script_name": true, "environment": true,
	"service": true, "entrypoint": true, "service_id": true,
	"store_id": true, "secret_name": true,
	"certificate_id": true, "index_name": true, "dataset": true,
	"destination_address": true, "allowed_destination_addresses": true,
	"allowed_sender_addresses": true,
	"pipeline":                 true, "workflow_name": true, "instance_name": true, "app_id": true,
	"network_id": true, "tunnel_id": true, "stream": true, "part": true,
	"old_name": true, "version_id": true,
	"algorithm": true, "format": true, "usages": true,
}

// bindingTargetRefs say, for each kind that reaches another resource, how to name that resource
// from the binding's fields, before it is resolved to a Corkscrew ID.
var bindingTargetRefs = map[string]func(f bindingFields, script string) string{
	"r2_bucket":    func(f bindingFields, _ string) string { return f.str("bucket_name") },
	"kv_namespace": func(f bindingFields, _ string) string { return f.str("namespace_id") },
	"d1":           func(f bindingFields, _ string) string { return f.str("database_id") },
	"queue":        func(f bindingFields, _ string) string { return f.str("queue_name") },
	"durable_object_namespace": func(f bindingFields, script string) string {
		// Without script_name, the class is exported by the binding's own script.
		return firstNonEmpty(f.str("script_name"), script) + "#" + f.str("class_name")
	},
	"workflow": func(f bindingFields, script string) string {
		return firstNonEmpty(f.str("script_name"), script) + "#" + f.str("workflow_name")
	},
	"service": func(f bindingFields, _ string) string { return f.str("service") },
	"secrets_store_secret": func(f bindingFields, _ string) string {
		return f.str("store_id") + "/" + f.str("secret_name")
	},
	"dispatch_namespace": func(f bindingFields, _ string) string { return f.str("namespace") },
	"mtls_certificate":   func(f bindingFields, _ string) string { return f.str("certificate_id") },
	"hyperdrive":         func(f bindingFields, _ string) string { return f.str("id") },
	"vectorize":          func(f bindingFields, _ string) string { return f.str("index_name") },
	"analytics_engine":   func(f bindingFields, _ string) string { return f.str("dataset") },
	"pipelines":          func(f bindingFields, _ string) string { return f.str("pipeline") },
	"vpc_service":        func(f bindingFields, _ string) string { return f.str("service_id") },
	"ratelimit":          func(f bindingFields, _ string) string { return f.str("namespace_id") },
	"ai_search":          func(f bindingFields, _ string) string { return f.str("instance_name") },
}

// bindingTargetTypes are the Corkscrew type and service of each kind's target, for the kinds
// Corkscrew inventories and so can resolve.
var bindingTargetTypes = map[string][2]string{
	"r2_bucket":                {"r2_bucket", "storage"},
	"kv_namespace":             {"kv_namespace", "storage"},
	"queue":                    {"queue", "storage"},
	"d1":                       {"d1_database", "data"},
	"durable_object_namespace": {"durable_object_namespace", "data"},
	"secrets_store_secret":     {"secret_store_secret", "data"},
	"service":                  {"worker_script", "workers"},
}

// bindingFields is one binding as the API returned it, decoded with UseNumber.
type bindingFields map[string]any

func (f bindingFields) str(key string) string {
	s, _ := f[key].(string)
	return s
}

// workerBindingID is a binding's resource ID: its script's ID and the binding's name, which is
// unique within the script.
func workerBindingID(accountID, scriptName, bindingName string) string {
	return workerScriptID(accountID, scriptName) + "/bindings/" + bindingName
}

// workerBindingResources returns a worker_binding for each of a script's bindings. A binding
// that can't be decoded is reported in errs: it was observed but dropped.
// targets resolves each binding's target, or is nil to leave targets unresolved.
func (p *CloudflareProvider) workerBindingResources(
	accountID, scriptName string,
	settings *workers.ScriptScriptAndVersionSettingGetResponse,
	targets *bindingTargets,
) ([]*pb.Resource, []string) {
	resources := make([]*pb.Resource, 0, len(settings.Bindings))
	var errs []string
	for i, binding := range settings.Bindings {
		fields, err := decodeBinding(binding.JSON.RawJSON())
		if err != nil || fields.str("name") == "" || fields.str("type") == "" {
			errs = append(
				errs,
				fmt.Sprintf(
					"workers binding %d of %s/%s unreadable: %v",
					i,
					accountID,
					scriptName,
					err,
				),
			)
			continue
		}
		resources = append(
			resources,
			p.workerBindingResource(accountID, scriptName, fields, targets),
		)
	}
	return resources, errs
}

func decodeBinding(raw string) (bindingFields, error) {
	decoder := json.NewDecoder(bytes.NewReader([]byte(raw)))
	decoder.UseNumber()
	var fields bindingFields
	if err := decoder.Decode(&fields); err != nil {
		return nil, err
	}
	return fields, nil
}

func (p *CloudflareProvider) workerBindingResource(
	accountID, scriptName string,
	fields bindingFields,
	targets *bindingTargets,
) *pb.Resource {
	name, kind := fields.str("name"), fields.str("type")
	attrs := map[string]string{
		"account_id":   accountID,
		"script":       scriptName,
		"script_id":    workerScriptID(accountID, scriptName),
		"binding_name": name,
		"kind":         kind,
	}
	if targetRef, ok := bindingTargetRefs[kind]; ok {
		attrs["target_ref"] = targetRef(fields, scriptName)
	}
	addBindingKindAttributes(kind, fields, attrs)
	addBindingTargetAttributes(kind, fields, scriptName, targets, attrs)

	stored := make(map[string]any, len(fields))
	for key, value := range fields {
		if bindingReferenceKeys[key] {
			stored[key] = value
		}
	}
	resource := p.resource(
		"workers",
		"worker_binding",
		workerBindingID(accountID, scriptName, name),
		name,
		accountID,
		stored,
		attrs,
	)
	resource.ParentId = workerScriptID(accountID, scriptName)
	return resource
}

// addBindingTargetAttributes records the target's type and service for kinds Corkscrew
// inventories and, when the target could be checked, whether it exists (target_resolved) and
// its ID. A target that couldn't be checked gets no target_resolved, so it never reads as
// dangling.
func addBindingTargetAttributes(
	kind string,
	fields bindingFields,
	scriptName string,
	targets *bindingTargets,
	attrs map[string]string,
) {
	typeAndService, ok := bindingTargetTypes[kind]
	if !ok {
		return
	}
	attrs["target_type"], attrs["target_service"] = typeAndService[0], typeAndService[1]
	if targets == nil {
		return
	}
	target, status := targets.resolve(kind, fields, scriptName)
	switch status {
	case targetFound:
		attrs["target_id"] = target.id
		attrs["target_resolved"] = "true"
	case targetMissing:
		attrs["target_resolved"] = "false"
	case targetUnknown:
	}
}

// addBindingKindAttributes adds what one kind of binding says about its risk. For value-bearing
// kinds it records only derived signals: whether the value looks like a credential, why, and
// its length. The value itself is never recorded.
func addBindingKindAttributes(kind string, fields bindingFields, attrs map[string]string) {
	switch kind {
	case "plain_text":
		text := fields.str("text")
		looks, reason := classifyBindingValue(text)
		attrs["looks_like_credential"] = strconv.FormatBool(looks)
		attrs["credential_reason"] = reason
		attrs["value_length"] = strconv.Itoa(len(text))
	case "json":
		looks, reason := classifyJSONValue(fields["json"])
		attrs["looks_like_credential"] = strconv.FormatBool(looks)
		attrs["credential_reason"] = reason
		if encoded, err := json.Marshal(fields["json"]); err == nil {
			attrs["value_length"] = strconv.Itoa(len(encoded))
		}
	case "send_email":
		allowed, _ := fields["allowed_destination_addresses"].([]any)
		restricted := fields.str("destination_address") != "" || len(allowed) > 0
		attrs["destination_restricted"] = strconv.FormatBool(restricted)
	case "r2_bucket":
		attrs["jurisdiction"] = fields.str("jurisdiction")
	case "service":
		attrs["environment"] = fields.str("environment")
		attrs["entrypoint"] = fields.str("entrypoint")
	}
}

// parseWorkerBindingID splits a workerBindingID into its account, script, and binding name.
func parseWorkerBindingID(id string) (accountID, scriptName, bindingName string, ok bool) {
	accountID, rest, ok := strings.Cut(id, "/workers/")
	if !ok {
		return "", "", "", false
	}
	scriptName, bindingName, ok = strings.Cut(rest, "/bindings/")
	if !ok || accountID == "" || scriptName == "" || bindingName == "" {
		return "", "", "", false
	}
	return accountID, scriptName, bindingName, true
}
