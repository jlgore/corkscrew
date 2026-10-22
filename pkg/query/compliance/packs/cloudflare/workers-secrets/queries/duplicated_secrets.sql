-- CF.SEC.05: a secret_text name is set separately in at least min_duplicate_scripts Workers of
-- one account. One row per secret_text binding.
WITH secret_texts AS (
    SELECT
        id,
        name,
        account_id,
        json_extract_string(attributes, '$.script') AS script
    FROM cloudflare_resources
    WHERE type = 'worker_binding'
        AND json_extract_string(attributes, '$.kind') = 'secret_text'
),
copies AS (
    SELECT account_id, name, count(DISTINCT script) AS workers, list_sort(list_distinct(list(script))) AS scripts
    FROM secret_texts
    GROUP BY account_id, name
),
evaluated AS (
    SELECT t.*, c.workers, c.scripts, c.workers >= :min_duplicate_scripts AS duplicated
    FROM secret_texts t
    JOIN copies c ON c.account_id = t.account_id AND c.name = t.name
)
SELECT
    CASE WHEN duplicated THEN 'FAIL' ELSE 'PASS' END AS status,
    id AS resource_id,
    script || '.' || name AS resource_name,
    'worker_binding' AS resource_type,
    'CF.SEC.05' AS control_id,
    'The same per-Worker secret is copied into many Workers' AS control_name,
    CASE WHEN duplicated THEN 'LOW' ELSE 'INFO' END AS severity,
    CASE
        WHEN duplicated THEN name || ' is set separately in ' || workers || ' Workers'
        ELSE 'Secret isn''t duplicated across Workers'
    END AS issue_description,
    json_object('account_id', account_id, 'script', script, 'secret', name, 'workers', workers, 'scripts', scripts) AS details
FROM evaluated
ORDER BY duplicated DESC, name, script
