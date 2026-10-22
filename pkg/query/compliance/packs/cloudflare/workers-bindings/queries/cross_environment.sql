-- CF.BND.03: a Worker named as non-production binds a resource named as production. A Secrets
-- Store secret is named "<store name>/<secret name>". Only resolved bindings are evaluated.
WITH bindings AS (
    SELECT
        id,
        name,
        account_id,
        json_extract_string(attributes, '$.script') AS script,
        json_extract_string(attributes, '$.kind') AS kind,
        json_extract_string(attributes, '$.target_id') AS target_id
    FROM cloudflare_resources
    WHERE type = 'worker_binding'
        AND json_extract_string(attributes, '$.target_resolved') = 'true'
),
targets AS (
    SELECT
        id,
        type,
        CASE
            WHEN type = 'secret_store_secret' THEN coalesce(json_extract_string(attributes, '$.store_name'), '') || '/' || name
            ELSE name
        END AS label
    FROM cloudflare_resources
),
judged AS (
    SELECT
        b.*,
        t.label AS target_label,
        t.type AS target_type,
        regexp_matches(b.script, :nonprod_name_pattern) AS nonprod_worker,
        regexp_matches(t.label, :prod_name_pattern) AS prod_target
    FROM bindings b
    JOIN targets t ON t.id = b.target_id
)
SELECT
    CASE WHEN nonprod_worker AND prod_target THEN 'FAIL' ELSE 'PASS' END AS status,
    id AS resource_id,
    script || '.' || name AS resource_name,
    'worker_binding' AS resource_type,
    'CF.BND.03' AS control_id,
    'Non-production Worker binds a production resource' AS control_name,
    CASE WHEN nonprod_worker AND prod_target THEN 'HIGH' ELSE 'INFO' END AS severity,
    CASE
        WHEN nonprod_worker AND prod_target THEN 'Non-production Worker ' || script || ' binds production ' || target_type || ' ' || target_label
        ELSE 'Binding stays within its environment'
    END AS issue_description,
    json_object(
        'account_id', account_id, 'script', script, 'binding', name, 'kind', kind,
        'target_id', target_id, 'target', target_label,
        'nonprod_worker', nonprod_worker, 'prod_target', prod_target
    ) AS details
FROM judged
ORDER BY status, script, name
