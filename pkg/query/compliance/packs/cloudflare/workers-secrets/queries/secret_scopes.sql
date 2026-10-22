-- CF.SEC.06: a Secrets Store secret has a scope outside allowed_secret_scopes. Secrets with no
-- recorded scopes aren't evaluated.
WITH evaluated AS (
    SELECT
        id,
        name,
        account_id,
        json_extract_string(attributes, '$.store_name') AS store_name,
        string_split(json_extract_string(attributes, '$.scopes'), ',') AS scopes,
        list_filter(
            string_split(json_extract_string(attributes, '$.scopes'), ','),
            scope -> NOT list_contains(string_split(:allowed_secret_scopes, ','), scope)
        ) AS extra_scopes
    FROM cloudflare_resources
    WHERE type = 'secret_store_secret'
        AND coalesce(json_extract_string(attributes, '$.scopes'), '') <> ''
        AND coalesce(json_extract_string(attributes, '$.status'), '') <> 'deleted'
)
SELECT
    CASE WHEN len(extra_scopes) > 0 THEN 'FAIL' ELSE 'PASS' END AS status,
    id AS resource_id,
    store_name || '/' || name AS resource_name,
    'secret_store_secret' AS resource_type,
    'CF.SEC.06' AS control_id,
    'Secrets Store secret is scoped beyond what''s allowed' AS control_name,
    CASE WHEN len(extra_scopes) > 0 THEN 'MEDIUM' ELSE 'INFO' END AS severity,
    CASE
        WHEN len(extra_scopes) > 0 THEN 'Secret is also available to ' || array_to_string(extra_scopes, ', ')
        ELSE 'Secret''s scopes are all allowed'
    END AS issue_description,
    json_object('account_id', account_id, 'store', store_name, 'scopes', scopes, 'extra_scopes', extra_scopes) AS details
FROM evaluated
ORDER BY len(extra_scopes) DESC, name
