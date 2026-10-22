-- CF.BND.04: more Workers bind one storage resource or Secrets Store secret than allowed. One
-- row per bound resource, from resolved bindings.
WITH bound AS (
    SELECT
        json_extract_string(attributes, '$.target_id') AS target_id,
        json_extract_string(attributes, '$.target_type') AS target_type,
        json_extract_string(attributes, '$.script_id') AS script_id,
        json_extract_string(attributes, '$.script') AS script
    FROM cloudflare_resources
    WHERE type = 'worker_binding'
        AND json_extract_string(attributes, '$.target_resolved') = 'true'
        AND json_extract_string(attributes, '$.target_type')
            IN ('r2_bucket', 'kv_namespace', 'd1_database', 'queue', 'secret_store_secret')
),
fan_in AS (
    SELECT
        target_id,
        any_value(target_type) AS target_type,
        count(DISTINCT script_id) AS workers,
        list_sort(list_distinct(list(script))) AS scripts
    FROM bound
    GROUP BY target_id
),
judged AS (
    SELECT
        f.*,
        r.name AS target_name,
        r.account_id,
        CASE WHEN f.target_type = 'secret_store_secret' THEN :max_secret_fan_in ELSE :max_binding_fan_in END AS allowed
    FROM fan_in f
    JOIN cloudflare_resources r ON r.id = f.target_id
)
SELECT
    CASE WHEN workers > allowed THEN 'FAIL' ELSE 'PASS' END AS status,
    target_id AS resource_id,
    target_name AS resource_name,
    target_type AS resource_type,
    'CF.BND.04' AS control_id,
    'Too many Workers share one resource' AS control_name,
    CASE
        WHEN workers <= allowed THEN 'INFO'
        WHEN target_type = 'secret_store_secret' THEN 'MEDIUM'
        ELSE 'LOW'
    END AS severity,
    CASE
        WHEN workers > allowed THEN workers || ' Workers bind this ' || target_type || ', more than ' || allowed
        ELSE workers || ' Workers bind this ' || target_type
    END AS issue_description,
    json_object('account_id', account_id, 'workers', workers, 'allowed', allowed, 'scripts', scripts) AS details
FROM judged
ORDER BY workers > allowed DESC, workers DESC, target_name
