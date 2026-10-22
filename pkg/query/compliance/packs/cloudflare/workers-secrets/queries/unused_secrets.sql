-- CF.SEC.04: a Secrets Store secret scoped to Workers that no Worker binds. Evaluated only for
-- accounts whose Workers were scanned, so a scan without the workers service can't make every
-- secret look unused. A binding counts whether or not its target resolved.
WITH secrets AS (
    SELECT
        id,
        name,
        account_id,
        json_extract_string(attributes, '$.store_id') AS store_id,
        json_extract_string(attributes, '$.store_name') AS store_name
    FROM cloudflare_resources
    WHERE type = 'secret_store_secret'
        AND coalesce(json_extract_string(attributes, '$.status'), '') <> 'deleted'
        AND list_contains(string_split(coalesce(json_extract_string(attributes, '$.scopes'), ''), ','), 'workers')
),
scanned_accounts AS (
    SELECT DISTINCT account_id FROM cloudflare_resources WHERE type = 'worker_script'
),
secret_refs AS (
    SELECT
        json_extract_string(attributes, '$.target_id') AS target_id,
        json_extract_string(attributes, '$.target_ref') AS target_ref,
        json_extract_string(attributes, '$.script') AS script
    FROM cloudflare_resources
    WHERE type = 'worker_binding'
        AND json_extract_string(attributes, '$.kind') = 'secrets_store_secret'
),
evaluated AS (
    SELECT
        s.*,
        -- list() over no rows is NULL, not [], so an unbound secret needs the coalesce.
        coalesce(list_sort(list_distinct(list(r.script) FILTER (WHERE r.script IS NOT NULL))), []) AS scripts
    FROM secrets s
    JOIN scanned_accounts a ON a.account_id = s.account_id
    LEFT JOIN secret_refs r ON r.target_id = s.id OR r.target_ref = s.store_id || '/' || s.name
    GROUP BY ALL
)
SELECT
    CASE WHEN len(scripts) = 0 THEN 'FAIL' ELSE 'PASS' END AS status,
    id AS resource_id,
    store_name || '/' || name AS resource_name,
    'secret_store_secret' AS resource_type,
    'CF.SEC.04' AS control_id,
    'Secrets Store secret is unused' AS control_name,
    CASE WHEN len(scripts) = 0 THEN 'LOW' ELSE 'INFO' END AS severity,
    CASE
        WHEN len(scripts) = 0 THEN 'Secret is scoped to Workers but no Worker binds it'
        ELSE 'Secret is bound by ' || len(scripts) || ' Worker(s)'
    END AS issue_description,
    json_object('account_id', account_id, 'store', store_name, 'scripts', scripts) AS details
FROM evaluated
ORDER BY len(scripts), name
