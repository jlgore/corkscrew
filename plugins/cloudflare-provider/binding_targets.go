package main

import (
	"context"
	"fmt"
	"sort"

	"github.com/cloudflare/cloudflare-go/v6/workers"
	pb "github.com/jlgore/corkscrew/internal/proto"
	"github.com/jlgore/corkscrew/internal/shared"
)

// targetStatus is what resolving a binding's target found.
type targetStatus int

const (
	// targetUnknown: the target couldn't be checked (its listing failed, or Corkscrew doesn't
	// inventory it). The binding gets no target_resolved attribute and no edge.
	targetUnknown targetStatus = iota
	// targetFound: the target is in the account's inventory.
	targetFound
	// targetMissing: the listing succeeded and the target isn't in it: a dangling binding.
	targetMissing
)

// bindingTarget is a resolved target's Corkscrew ID.
type bindingTarget struct {
	id string
}

// bindingTargets resolves one account's bindings to Corkscrew resources. It lists each kind of
// target the first time a binding needs it (through the scan's list memo), and remembers a
// failed listing so it is reported once.
type bindingTargets struct {
	p         *CloudflareProvider
	ctx       context.Context
	accountID string

	scripts   map[string]bool // nil when the script listing failed
	r2        map[string]bool
	kv        map[string]bool
	d1        map[string]bool
	queues    map[string]string // name -> queue ID
	doByClass map[string]string // script#class -> namespace ID
	doIDs     map[string]bool
	stores    map[string]bool
	secrets   map[string]map[string]string // store ID -> secret name -> secret ID
	loaded    map[string]bool
	failures  map[string]error // listing -> why it failed
	failOrder []string
}

func (p *CloudflareProvider) newBindingTargets(ctx context.Context, accountID string, scripts []workers.ScriptListResponse, scriptsErr error) *bindingTargets {
	t := &bindingTargets{
		p: p, ctx: ctx, accountID: accountID,
		secrets:  map[string]map[string]string{},
		loaded:   map[string]bool{},
		failures: map[string]error{},
	}
	// A failed script listing is already a scan error; service bindings just stay unknown.
	if scriptsErr == nil {
		t.scripts = make(map[string]bool, len(scripts))
		for _, script := range scripts {
			t.scripts[script.ID] = true
		}
	}
	return t
}

// load runs fetch the first time listing is needed. It reports whether the listing is usable.
func (t *bindingTargets) load(listing string, fetch func() error) bool {
	if !t.loaded[listing] {
		t.loaded[listing] = true
		if err := fetch(); err != nil {
			t.failures[listing] = err
			t.failOrder = append(t.failOrder, listing)
		}
	}
	return t.failures[listing] == nil
}

// resolve finds the resource a binding of kind reaches, from the binding's fields.
func (t *bindingTargets) resolve(kind string, f bindingFields, scriptName string) (bindingTarget, targetStatus) {
	switch kind {
	case "r2_bucket":
		// Buckets outside the default jurisdiction aren't in the default listing.
		if j := f.str("jurisdiction"); j != "" && j != "default" {
			return bindingTarget{}, targetUnknown
		}
		return t.lookup(t.loadR2(), t.r2[f.str("bucket_name")],
			bindingTarget{t.accountID + "/r2/" + f.str("bucket_name")})
	case "kv_namespace":
		id := f.str("namespace_id")
		return t.lookup(t.loadKV(), t.kv[id], bindingTarget{id})
	case "d1":
		id := f.str("database_id")
		return t.lookup(t.loadD1(), t.d1[id], bindingTarget{id})
	case "queue":
		ok := t.loadQueues()
		id, found := t.queues[f.str("queue_name")]
		return t.lookup(ok, found, bindingTarget{id})
	case "durable_object_namespace":
		return t.resolveDurableObject(f, scriptName)
	case "service":
		name := f.str("service")
		return t.lookup(t.scripts != nil, t.scripts[name],
			bindingTarget{workerScriptID(t.accountID, name)})
	case "secrets_store_secret":
		return t.resolveSecret(f.str("store_id"), f.str("secret_name"))
	}
	return bindingTarget{}, targetUnknown
}

func (t *bindingTargets) lookup(listed, found bool, target bindingTarget) (bindingTarget, targetStatus) {
	switch {
	case !listed:
		return bindingTarget{}, targetUnknown
	case !found:
		return bindingTarget{}, targetMissing
	}
	return target, targetFound
}

func (t *bindingTargets) resolveDurableObject(f bindingFields, scriptName string) (bindingTarget, targetStatus) {
	ok := t.loadDurableObjects()
	if id := f.str("namespace_id"); id != "" {
		return t.lookup(ok, t.doIDs[id], bindingTarget{id})
	}
	// A class in another environment or a dispatch namespace isn't in the account's listing.
	if f.str("environment") != "" || f.str("dispatch_namespace") != "" {
		return bindingTarget{}, targetUnknown
	}
	key := firstNonEmpty(f.str("script_name"), scriptName) + "#" + f.str("class_name")
	id, found := t.doByClass[key]
	return t.lookup(ok, found, bindingTarget{id})
}

func (t *bindingTargets) resolveSecret(storeID, secretName string) (bindingTarget, targetStatus) {
	if !t.load("secret stores", t.fetchStores) {
		return bindingTarget{}, targetUnknown
	}
	if !t.stores[storeID] {
		return bindingTarget{}, targetMissing
	}
	listing := "secrets in store " + storeID
	ok := t.load(listing, func() error {
		secrets, err := t.p.listSecretStoreSecrets(t.ctx, t.accountID, storeID)
		names := make(map[string]string, len(secrets))
		for _, secret := range secrets {
			names[secret.Name] = secret.ID
		}
		t.secrets[storeID] = names
		return err
	})
	id, found := t.secrets[storeID][secretName]
	return t.lookup(ok, found, bindingTarget{id})
}

func (t *bindingTargets) loadR2() bool {
	return t.load("r2 buckets", func() error {
		buckets, err := t.p.listR2Buckets(t.ctx, t.accountID)
		t.r2 = make(map[string]bool, len(buckets))
		for _, bucket := range buckets {
			t.r2[bucket.Name] = true
		}
		return err
	})
}

func (t *bindingTargets) loadKV() bool {
	return t.load("kv namespaces", func() error {
		namespaces, err := t.p.listKVNamespaces(t.ctx, t.accountID)
		t.kv = make(map[string]bool, len(namespaces))
		for _, namespace := range namespaces {
			t.kv[namespace.ID] = true
		}
		return err
	})
}

func (t *bindingTargets) loadD1() bool {
	return t.load("d1 databases", func() error {
		databases, err := t.p.listD1Databases(t.ctx, t.accountID)
		t.d1 = make(map[string]bool, len(databases))
		for _, database := range databases {
			t.d1[database.UUID] = true
		}
		return err
	})
}

func (t *bindingTargets) loadQueues() bool {
	return t.load("queues", func() error {
		queuesList, err := t.p.listQueues(t.ctx, t.accountID)
		t.queues = make(map[string]string, len(queuesList))
		for _, queue := range queuesList {
			t.queues[queue.QueueName] = queue.QueueID
		}
		return err
	})
}

func (t *bindingTargets) loadDurableObjects() bool {
	return t.load("durable object namespaces", func() error {
		namespaces, err := t.p.listDurableObjectNamespaces(t.ctx, t.accountID)
		t.doByClass = make(map[string]string, len(namespaces))
		t.doIDs = make(map[string]bool, len(namespaces))
		for _, namespace := range namespaces {
			t.doByClass[namespace.Script+"#"+namespace.Class] = namespace.ID
			t.doIDs[namespace.ID] = true
		}
		return err
	})
}

func (t *bindingTargets) fetchStores() error {
	stores, err := t.p.listSecretStores(t.ctx, t.accountID)
	t.stores = make(map[string]bool, len(stores))
	for _, store := range stores {
		t.stores[store.ID] = true
	}
	return err
}

// errors reports each listing that failed, once: a 403 as a coverage notice, anything else as
// an error. Bindings behind a failed listing were emitted with their target unknown, not missing.
func (t *bindingTargets) errors() []string {
	entries := make([]string, 0, len(t.failOrder))
	for _, listing := range t.failOrder {
		err := t.failures[listing]
		target := fmt.Sprintf("workers binding targets: %s for account %s", listing, t.accountID)
		if isForbidden(err) {
			entries = append(entries, shared.AccessDeniedNotice(target, err))
		} else {
			entries = append(entries, fmt.Sprintf("%s list failed: %v", target, err))
		}
	}
	return entries
}

// bindingRelationships returns a worker script's "binds" edges, one per resolved target, from
// its bindings' attributes. Two bindings to one target make one edge (the store keys edges by
// source, target, and type), named after the first binding.
func bindingRelationships(bindings []*pb.Resource) []*pb.Relationship {
	seen := map[string]bool{}
	var relationships []*pb.Relationship
	for _, binding := range bindings {
		attrs := decodeAttrs(binding.Attributes)
		if attrs["target_resolved"] != "true" || seen[attrs["target_id"]] {
			continue
		}
		seen[attrs["target_id"]] = true
		relationships = append(relationships, &pb.Relationship{
			TargetId:         attrs["target_id"],
			TargetType:       attrs["target_type"],
			TargetService:    attrs["target_service"],
			RelationshipType: "binds",
			Properties:       map[string]string{"binding_name": attrs["binding_name"], "kind": attrs["kind"]},
		})
	}
	sort.SliceStable(relationships, func(i, j int) bool { return relationships[i].TargetId < relationships[j].TargetId })
	return relationships
}
