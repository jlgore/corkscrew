-- CF.BND.01: a binding of a privileged kind that privileged_binding_allowlist doesn't approve.
WITH bindings AS (
    SELECT
        id,
        name,
        account_id,
        json_extract_string(attributes, '$.script') AS script,
        json_extract_string(attributes, '$.script_id') AS script_id,
        json_extract_string(attributes, '$.kind') AS kind,
        coalesce(json_extract_string(attributes, '$.target_ref'), '') AS target_ref
    FROM cloudflare_resources
    WHERE type = 'worker_binding'
        AND list_contains(string_split(:privileged_binding_kinds, ','), json_extract_string(attributes, '$.kind'))
),
judged AS (
    SELECT
        *,
        list_contains(string_split(:privileged_binding_allowlist, ','), script || ':' || kind)
            OR list_contains(string_split(:privileged_binding_allowlist, ','), script_id || ':' || kind) AS approved
    FROM bindings
)
SELECT
    CASE WHEN approved THEN 'PASS' ELSE 'FAIL' END AS status,
    id AS resource_id,
    script || '.' || name AS resource_name,
    'worker_binding' AS resource_type,
    'CF.BND.01' AS control_id,
    'Worker holds an unreviewed privileged binding' AS control_name,
    CASE WHEN approved THEN 'INFO' ELSE 'MEDIUM' END AS severity,
    CASE
        WHEN approved THEN 'Privileged ' || kind || ' binding is approved'
        ELSE script || ' holds a ' || kind || ' binding that isn''t on the allowlist'
    END AS issue_description,
    json_object('account_id', account_id, 'script', script, 'binding', name, 'kind', kind, 'target_ref', target_ref) AS details
FROM judged
ORDER BY approved, script, name
