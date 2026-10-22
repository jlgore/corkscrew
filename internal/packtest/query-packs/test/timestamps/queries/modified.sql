SELECT
    CASE WHEN modified_at < TIMESTAMP '2024-01-01' THEN 'FAIL' ELSE 'PASS' END AS status,
    id AS resource_id,
    name AS resource_name,
    type AS resource_type,
    'TIMES.01' AS control_id,
    'Secret modified since 2024' AS control_name,
    'LOW' AS severity,
    CAST(modified_at AS VARCHAR) AS details
FROM cloudflare_resources
WHERE type = 'secret_store_secret'
