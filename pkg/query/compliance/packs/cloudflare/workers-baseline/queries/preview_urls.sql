-- CF.WRK.02: a Worker served through a zone has Preview URLs enabled.
WITH evaluated AS (
    SELECT
        id,
        name,
        account_id,
        json_extract_string(attributes, '$.previews_enabled') AS previews_enabled,
        json_extract_string(attributes, '$.has_route') AS has_route,
        json_extract_string(attributes, '$.has_custom_domain') AS has_custom_domain
    FROM cloudflare_resources
    WHERE type = 'worker_script'
        AND (json_extract_string(attributes, '$.has_route') = 'true'
            OR json_extract_string(attributes, '$.has_custom_domain') = 'true')
        AND json_extract_string(attributes, '$.previews_enabled') IS NOT NULL
)
SELECT
    CASE WHEN previews_enabled = 'true' THEN 'FAIL' ELSE 'PASS' END AS status,
    id AS resource_id,
    name AS resource_name,
    'worker_script' AS resource_type,
    'CF.WRK.02' AS control_id,
    'Routed Worker has Preview URLs enabled' AS control_name,
    CASE WHEN previews_enabled = 'true' THEN 'MEDIUM' ELSE 'INFO' END AS severity,
    CASE
        WHEN previews_enabled = 'true' THEN 'Every uploaded version of this Worker is public on workers.dev, outside the zone''s security settings'
        ELSE 'Preview URLs are off'
    END AS issue_description,
    json_object(
        'account_id', account_id,
        'previews_enabled', previews_enabled,
        'has_route', has_route,
        'has_custom_domain', has_custom_domain
    ) AS details
FROM evaluated
ORDER BY status, name
