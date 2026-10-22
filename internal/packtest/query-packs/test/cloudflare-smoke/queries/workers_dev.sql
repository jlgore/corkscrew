SELECT
    CASE WHEN json_extract_string(attributes, '$.workers_dev_enabled') = 'true' THEN 'FAIL' ELSE 'PASS' END AS status,
    id AS resource_id,
    name AS resource_name,
    type AS resource_type,
    'SMOKE.01' AS control_id,
    'Worker is off workers.dev' AS control_name,
    'LOW' AS severity,
    attributes AS details
FROM cloudflare_resources
WHERE type = 'worker_script'
