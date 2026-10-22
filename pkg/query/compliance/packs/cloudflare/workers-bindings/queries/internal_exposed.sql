-- CF.BND.05: a Worker with no route or custom domain, called through service bindings, is on
-- workers.dev. Workers whose exposure is unknown aren't evaluated.
WITH service_targets AS (
    SELECT DISTINCT json_extract_string(attributes, '$.target_id') AS script_id
    FROM cloudflare_resources
    WHERE type = 'worker_binding'
        AND json_extract_string(attributes, '$.kind') = 'service'
        AND json_extract_string(attributes, '$.target_resolved') = 'true'
),
evaluated AS (
    SELECT
        r.id,
        r.name,
        r.account_id,
        json_extract_string(r.attributes, '$.workers_dev_enabled') AS workers_dev_enabled,
        json_extract_string(r.attributes, '$.previews_enabled') AS previews_enabled
    FROM cloudflare_resources r
    JOIN service_targets s ON s.script_id = r.id
    WHERE r.type = 'worker_script'
        AND json_extract_string(r.attributes, '$.has_route') = 'false'
        AND json_extract_string(r.attributes, '$.has_custom_domain') = 'false'
        AND json_extract_string(r.attributes, '$.workers_dev_enabled') IS NOT NULL
)
SELECT
    CASE WHEN workers_dev_enabled = 'true' THEN 'FAIL' ELSE 'PASS' END AS status,
    id AS resource_id,
    name AS resource_name,
    'worker_script' AS resource_type,
    'CF.BND.05' AS control_id,
    'Internal Worker is exposed on workers.dev' AS control_name,
    CASE WHEN workers_dev_enabled = 'true' THEN 'HIGH' ELSE 'INFO' END AS severity,
    CASE
        WHEN workers_dev_enabled = 'true' THEN 'Internal Worker is callable directly from the internet on workers.dev'
        ELSE 'Internal Worker is reachable only through service bindings'
    END AS issue_description,
    json_object('account_id', account_id, 'workers_dev_enabled', workers_dev_enabled, 'previews_enabled', previews_enabled) AS details
FROM evaluated
ORDER BY status, name
