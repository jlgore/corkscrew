-- CF.BND.06: a binding's target isn't in the inventory. Bindings whose target couldn't be
-- checked (target_resolved absent) aren't evaluated.
WITH evaluated AS (
    SELECT
        id,
        name,
        account_id,
        json_extract_string(attributes, '$.script') AS script,
        json_extract_string(attributes, '$.kind') AS kind,
        json_extract_string(attributes, '$.target_ref') AS target_ref,
        json_extract_string(attributes, '$.target_resolved') = 'true' AS resolved
    FROM cloudflare_resources
    WHERE type = 'worker_binding'
        AND json_extract_string(attributes, '$.target_resolved') IN ('true', 'false')
)
SELECT
    CASE WHEN resolved THEN 'PASS' ELSE 'FAIL' END AS status,
    id AS resource_id,
    script || '.' || name AS resource_name,
    'worker_binding' AS resource_type,
    'CF.BND.06' AS control_id,
    'Binding points to a resource that doesn''t exist' AS control_name,
    CASE WHEN resolved THEN 'INFO' ELSE 'MEDIUM' END AS severity,
    CASE
        WHEN resolved THEN 'Binding reaches an existing ' || kind
        ELSE script || '.' || name || ' names ' || kind || ' ' || target_ref || ', which the account doesn''t have'
    END AS issue_description,
    json_object('account_id', account_id, 'script', script, 'binding', name, 'kind', kind, 'target_ref', target_ref) AS details
FROM evaluated
ORDER BY resolved, script, name
