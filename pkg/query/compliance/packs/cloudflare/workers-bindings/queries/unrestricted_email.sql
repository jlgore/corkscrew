-- CF.BND.02: a send_email binding with no destination restriction.
WITH evaluated AS (
    SELECT
        id,
        name,
        account_id,
        json_extract_string(attributes, '$.script') AS script,
        json_extract_string(attributes, '$.destination_restricted') = 'true' AS restricted
    FROM cloudflare_resources
    WHERE type = 'worker_binding'
        AND json_extract_string(attributes, '$.kind') = 'send_email'
        AND json_extract_string(attributes, '$.destination_restricted') IS NOT NULL
)
SELECT
    CASE WHEN restricted THEN 'PASS' ELSE 'FAIL' END AS status,
    id AS resource_id,
    script || '.' || name AS resource_name,
    'worker_binding' AS resource_type,
    'CF.BND.02' AS control_id,
    'Email binding can send to any verified address' AS control_name,
    CASE WHEN restricted THEN 'INFO' ELSE 'MEDIUM' END AS severity,
    CASE
        WHEN restricted THEN 'Email binding is limited to specific destinations'
        ELSE 'Email binding can send to every verified destination address on the account'
    END AS issue_description,
    json_object('account_id', account_id, 'script', script, 'binding', name) AS details
FROM evaluated
ORDER BY restricted, script, name
