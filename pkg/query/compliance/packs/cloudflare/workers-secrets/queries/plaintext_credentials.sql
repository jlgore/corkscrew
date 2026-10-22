-- CF.SEC.01: a plain_text or json binding's value looks like a credential. Reads only the
-- provider's derived signals; Corkscrew never stores the value.
WITH evaluated AS (
    SELECT
        id,
        name,
        account_id,
        json_extract_string(attributes, '$.script') AS script,
        json_extract_string(attributes, '$.kind') AS kind,
        json_extract_string(attributes, '$.looks_like_credential') = 'true' AS looks_like_credential,
        coalesce(json_extract_string(attributes, '$.credential_reason'), '') AS credential_reason
    FROM cloudflare_resources
    WHERE type = 'worker_binding'
        AND json_extract_string(attributes, '$.kind') IN ('plain_text', 'json')
        AND json_extract_string(attributes, '$.looks_like_credential') IS NOT NULL
)
SELECT
    CASE WHEN looks_like_credential THEN 'FAIL' ELSE 'PASS' END AS status,
    id AS resource_id,
    script || '.' || name AS resource_name,
    'worker_binding' AS resource_type,
    'CF.SEC.01' AS control_id,
    'Plaintext binding holds a credential' AS control_name,
    CASE WHEN looks_like_credential THEN 'CRITICAL' ELSE 'INFO' END AS severity,
    CASE
        WHEN looks_like_credential THEN script || '.' || name || ' is a ' || kind || ' binding holding what looks like a credential (' || credential_reason || ')'
        ELSE 'Value doesn''t look like a credential'
    END AS issue_description,
    json_object('account_id', account_id, 'script', script, 'binding', name, 'kind', kind, 'credential_reason', credential_reason) AS details
FROM evaluated
ORDER BY looks_like_credential DESC, script, name
