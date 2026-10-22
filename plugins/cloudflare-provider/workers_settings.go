package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"

	cloudflare "github.com/cloudflare/cloudflare-go/v6"
	"github.com/cloudflare/cloudflare-go/v6/workers"
	"github.com/jlgore/corkscrew/internal/assert"
	"github.com/jlgore/corkscrew/internal/shared"
)

// scriptDetailConcurrency bounds the per-script settings and subdomain requests in flight for
// one account. Each script costs two requests, and Cloudflare allows 1,200 per five minutes per
// account, so a large account is paced by the SDK's 429 backoff rather than by this bound.
const scriptDetailConcurrency = 4

// scriptExposure is how a script is reachable through a zone. known is false when a route or
// domain listing failed, so a false route or customDomain might just be unobserved.
type scriptExposure struct {
	route        bool
	customDomain bool
	known        bool
}

// scriptDetail is what the per-script requests found. A nil field means its request failed;
// the scan's errors say why. settings carries the bindings, which hold plain_text values, so it
// never goes into a resource's raw data.
type scriptDetail struct {
	settings  *workers.ScriptScriptAndVersionSettingGetResponse
	subdomain *workers.ScriptSubdomainGetResponse
	exposure  scriptExposure
}

// scriptDetails fetches settings and subdomain for every script, at most
// scriptDetailConcurrency scripts at a time. details[i] belongs to scripts[i]. A failed request
// leaves its field nil and adds an entry to errs: a 403 is a coverage notice, anything else an
// error, so the scan is partial.
func (p *CloudflareProvider) scriptDetails(ctx context.Context, accountID string, scripts []workers.ScriptListResponse) ([]scriptDetail, []string) {
	details := make([]scriptDetail, len(scripts))
	scriptErrs := make([][]string, len(scripts))
	sem := make(chan struct{}, scriptDetailConcurrency)
	var wg sync.WaitGroup
	for i := range scripts {
		sem <- struct{}{}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			details[i], scriptErrs[i] = p.scriptDetail(ctx, accountID, scripts[i].ID)
		}(i)
	}
	wg.Wait()
	assert.Thatf(len(sem) == 0, "cloudflare: every script detail slot is released: %d held", len(sem))

	var errs []string
	for _, entries := range scriptErrs {
		errs = append(errs, entries...)
	}
	return details, errs
}

// scriptDetail fetches one script's settings and subdomain.
func (p *CloudflareProvider) scriptDetail(ctx context.Context, accountID, scriptName string) (scriptDetail, []string) {
	var detail scriptDetail
	var errs []string
	settings, err := p.client.Workers.Scripts.ScriptAndVersionSettings.Get(ctx, scriptName,
		workers.ScriptScriptAndVersionSettingGetParams{AccountID: cloudflare.F(accountID)})
	if err != nil {
		errs = append(errs, scriptDetailError("settings", accountID, scriptName, err))
	} else {
		detail.settings = settings
	}
	subdomain, err := p.client.Workers.Scripts.Subdomain.Get(ctx, scriptName,
		workers.ScriptSubdomainGetParams{AccountID: cloudflare.F(accountID)})
	if err != nil {
		errs = append(errs, scriptDetailError("subdomain", accountID, scriptName, err))
	} else {
		detail.subdomain = subdomain
	}
	return detail, errs
}

func scriptDetailError(what, accountID, scriptName string, err error) string {
	if isForbidden(err) {
		return shared.AccessDeniedNotice(fmt.Sprintf("workers script %s for %s/%s", what, accountID, scriptName), err)
	}
	return fmt.Sprintf("workers script %s failed for %s/%s: %v", what, accountID, scriptName, err)
}

// isForbidden reports whether err is the API refusing the credential (HTTP 403).
func isForbidden(err error) bool {
	var apiErr *cloudflare.Error
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusForbidden
}

// addAttributes sets the attributes the detail requests found. An attribute whose request
// failed is left out, so a pack can tell "unknown" from "false".
func (d scriptDetail) addAttributes(attrs map[string]string) {
	if s := d.settings; s != nil {
		attrs["logpush"] = strconv.FormatBool(s.Logpush)
		attrs["observability_enabled"] = strconv.FormatBool(s.Observability.Enabled)
		attrs["observability_logs_enabled"] = strconv.FormatBool(s.Observability.Logs.Enabled)
		attrs["placement_mode"] = string(s.Placement.Mode)
		attrs["binding_count"] = strconv.Itoa(len(s.Bindings))
		consumers := make([]string, 0, len(s.TailConsumers))
		for _, consumer := range s.TailConsumers {
			consumers = append(consumers, consumer.Service)
		}
		attrs["tail_consumers"] = strings.Join(consumers, ",")
	}
	if d.subdomain != nil {
		attrs["workers_dev_enabled"] = strconv.FormatBool(d.subdomain.Enabled)
		attrs["previews_enabled"] = strconv.FormatBool(d.subdomain.PreviewsEnabled)
	}
	if d.exposure.known {
		attrs["has_route"] = strconv.FormatBool(d.exposure.route)
		attrs["has_custom_domain"] = strconv.FormatBool(d.exposure.customDomain)
	}
}
