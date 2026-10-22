-- CF.WRK.05: a route names a script its account doesn't have. Routes that name no script
-- (deliberately disabled routes) aren't evaluated.
WITH routes AS (
    SELECT
        id,
        name AS pattern,
        json_extract_string(attributes, '$.account_id') AS route_account_id,
        json_extract_string(attributes, '$.zone_name') AS zone_name,
        json_extract_string(attributes, '$.script') AS script
    FROM cloudflare_resources
    WHERE type = 'worker_route'
        AND coalesce(json_extract_string(attributes, '$.script'), '') <> ''
        AND coalesce(json_extract_string(attributes, '$.account_id'), '') <> ''
),
scripts AS (
    SELECT account_id, name FROM cloudflare_resources WHERE type = 'worker_script'
),
evaluated AS (
    SELECT routes.*, scripts.name IS NOT NULL AS script_exists
    FROM routes
    LEFT JOIN scripts ON scripts.account_id = routes.route_account_id AND scripts.name = routes.script
)
SELECT
    CASE WHEN script_exists THEN 'PASS' ELSE 'FAIL' END AS status,
    id AS resource_id,
    pattern AS resource_name,
    'worker_route' AS resource_type,
    'CF.WRK.05' AS control_id,
    'Route points to a Worker that doesn''t exist' AS control_name,
    CASE WHEN script_exists THEN 'INFO' ELSE 'LOW' END AS severity,
    CASE
        WHEN script_exists THEN 'Route serves an existing Worker'
        ELSE 'Route sends traffic to ' || script || ', which the account doesn''t have'
    END AS issue_description,
    json_object(
        'account_id', route_account_id,
        'zone_name', zone_name,
        'script', script
    ) AS details
FROM evaluated
ORDER BY script_exists, pattern
