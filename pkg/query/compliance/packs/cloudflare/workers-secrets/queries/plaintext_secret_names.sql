-- CF.SEC.02: a plain_text binding is named like a secret.
WITH evaluated AS (
    SELECT
        id,
        name,
        account_id,
        json_extract_string(attributes, '$.script') AS script,
        regexp_matches(name, :secret_name_pattern) AS secret_name
    FROM cloudflare_resources
    WHERE type = 'worker_binding'
        AND json_extract_string(attributes, '$.kind') = 'plain_text'
)
SELECT
    CASE WHEN secret_name THEN 'FAIL' ELSE 'PASS' END AS status,
    id AS resource_id,
    script || '.' || name AS resource_name,
    'worker_binding' AS resource_type,
    'CF.SEC.02' AS control_id,
    'Plaintext binding is named like a secret' AS control_name,
    CASE WHEN secret_name THEN 'HIGH' ELSE 'INFO' END AS severity,
    CASE
        WHEN secret_name THEN script || '.' || name || ' is a plaintext variable named like a secret'
        ELSE 'Variable name doesn''t suggest a secret'
    END AS issue_description,
    json_object('account_id', account_id, 'script', script, 'binding', name) AS details
FROM evaluated
ORDER BY secret_name DESC, script, name
