-- CF.SEC.07: a Worker binds a Secrets Store secret whose status isn't active. Only bindings
-- whose secret resolved are evaluated.
WITH bindings AS (
    SELECT
        id,
        name,
        account_id,
        json_extract_string(attributes, '$.script') AS script,
        json_extract_string(attributes, '$.target_id') AS target_id
    FROM cloudflare_resources
    WHERE type = 'worker_binding'
        AND json_extract_string(attributes, '$.kind') = 'secrets_store_secret'
        AND json_extract_string(attributes, '$.target_resolved') = 'true'
),
evaluated AS (
    SELECT
        b.*,
        s.name AS secret_name,
        json_extract_string(s.attributes, '$.store_name') AS store_name,
        coalesce(json_extract_string(s.attributes, '$.status'), '') AS secret_status
    FROM bindings b
    JOIN cloudflare_resources s ON s.id = b.target_id AND s.type = 'secret_store_secret'
)
SELECT
    CASE WHEN secret_status = 'active' THEN 'PASS' ELSE 'FAIL' END AS status,
    id AS resource_id,
    script || '.' || name AS resource_name,
    'worker_binding' AS resource_type,
    'CF.SEC.07' AS control_id,
    'Worker binds a secret that isn''t active' AS control_name,
    CASE WHEN secret_status = 'active' THEN 'INFO' ELSE 'HIGH' END AS severity,
    CASE
        WHEN secret_status = 'active' THEN 'Bound secret is active'
        ELSE script || '.' || name || ' binds ' || store_name || '/' || secret_name || ', which is ' || secret_status
    END AS issue_description,
    json_object('account_id', account_id, 'script', script, 'binding', name, 'secret', store_name || '/' || secret_name, 'secret_status', secret_status) AS details
FROM evaluated
ORDER BY secret_status = 'active', script, name
