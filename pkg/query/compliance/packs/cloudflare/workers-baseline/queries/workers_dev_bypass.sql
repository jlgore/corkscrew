-- CF.WRK.01: a Worker served through a zone (route or custom domain) is also on workers.dev.
-- Scripts whose exposure or subdomain is unknown (a failed request) aren't evaluated.
WITH scripts AS (
    SELECT
        id,
        name,
        account_id,
        json_extract_string(attributes, '$.workers_dev_enabled') AS workers_dev_enabled,
        json_extract_string(attributes, '$.has_route') AS has_route,
        json_extract_string(attributes, '$.has_custom_domain') AS has_custom_domain
    FROM cloudflare_resources
    WHERE type = 'worker_script'
),
evaluated AS (
    SELECT
        *,
        list_contains(string_split(:workers_dev_allowlist, ','), name)
            OR list_contains(string_split(:workers_dev_allowlist, ','), id) AS allowlisted
    FROM scripts
    WHERE (has_route = 'true' OR has_custom_domain = 'true')
        AND workers_dev_enabled IS NOT NULL
),
judged AS (
    SELECT *, workers_dev_enabled = 'true' AND NOT allowlisted AS failing
    FROM evaluated
)
SELECT
    CASE WHEN failing THEN 'FAIL' ELSE 'PASS' END AS status,
    id AS resource_id,
    name AS resource_name,
    'worker_script' AS resource_type,
    'CF.WRK.01' AS control_id,
    'Routed Worker is also reachable on workers.dev' AS control_name,
    CASE WHEN failing THEN 'HIGH' ELSE 'INFO' END AS severity,
    CASE
        WHEN failing THEN 'Worker is served through its zone and also on workers.dev, which skips the zone''s security settings'
        WHEN allowlisted AND workers_dev_enabled = 'true' THEN 'Worker is on workers.dev and allowlisted'
        ELSE 'Worker is reachable only through its zone'
    END AS issue_description,
    json_object(
        'account_id', account_id,
        'workers_dev_enabled', workers_dev_enabled,
        'has_route', has_route,
        'has_custom_domain', has_custom_domain,
        'allowlisted', allowlisted
    ) AS details
FROM judged
ORDER BY failing DESC, name
