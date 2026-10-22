-- CF.WRK.03: a Worker has no Workers Logs, no Logpush, and no tail consumer. Scripts whose
-- settings couldn't be read aren't evaluated.
WITH evaluated AS (
    SELECT
        id,
        name,
        account_id,
        json_extract_string(attributes, '$.observability_logs_enabled') AS logs_enabled,
        json_extract_string(attributes, '$.logpush') AS logpush,
        coalesce(json_extract_string(attributes, '$.tail_consumers'), '') AS tail_consumers
    FROM cloudflare_resources
    WHERE type = 'worker_script'
        AND json_extract_string(attributes, '$.observability_logs_enabled') IS NOT NULL
),
judged AS (
    SELECT *, logs_enabled <> 'true' AND coalesce(logpush, 'false') <> 'true' AND tail_consumers = '' AS failing
    FROM evaluated
)
SELECT
    CASE WHEN failing THEN 'FAIL' ELSE 'PASS' END AS status,
    id AS resource_id,
    name AS resource_name,
    'worker_script' AS resource_type,
    'CF.WRK.03' AS control_id,
    'Worker records no logs' AS control_name,
    CASE WHEN failing THEN 'MEDIUM' ELSE 'INFO' END AS severity,
    CASE
        WHEN failing THEN 'Worker has Workers Logs, Logpush, and tail consumers all off'
        ELSE 'Worker records logs'
    END AS issue_description,
    json_object(
        'account_id', account_id,
        'observability_logs_enabled', logs_enabled,
        'logpush', logpush,
        'tail_consumers', tail_consumers
    ) AS details
FROM judged
ORDER BY failing DESC, name
