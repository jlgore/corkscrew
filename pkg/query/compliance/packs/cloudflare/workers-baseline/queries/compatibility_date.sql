-- CF.WRK.04: a Worker's compatibility_date is older than max_compat_age_days before as_of_date
-- (today when empty).
WITH params AS (
    SELECT coalesce(TRY_CAST(nullif(:as_of_date, '') AS DATE), current_date) AS as_of
),
evaluated AS (
    SELECT
        r.id,
        r.name,
        r.account_id,
        json_extract_string(r.attributes, '$.compatibility_date') AS compatibility_date,
        date_diff('day', TRY_CAST(json_extract_string(r.attributes, '$.compatibility_date') AS DATE), p.as_of) AS age_days
    FROM cloudflare_resources r, params p
    WHERE r.type = 'worker_script'
        AND TRY_CAST(json_extract_string(r.attributes, '$.compatibility_date') AS DATE) IS NOT NULL
)
SELECT
    CASE WHEN age_days > :max_compat_age_days THEN 'FAIL' ELSE 'PASS' END AS status,
    id AS resource_id,
    name AS resource_name,
    'worker_script' AS resource_type,
    'CF.WRK.04' AS control_id,
    'Worker compatibility date is out of date' AS control_name,
    CASE WHEN age_days > :max_compat_age_days THEN 'LOW' ELSE 'INFO' END AS severity,
    CASE
        WHEN age_days > :max_compat_age_days THEN 'Compatibility date is ' || age_days || ' days old'
        ELSE 'Compatibility date is current'
    END AS issue_description,
    json_object(
        'account_id', account_id,
        'compatibility_date', compatibility_date,
        'age_days', age_days,
        'max_compat_age_days', :max_compat_age_days
    ) AS details
FROM evaluated
ORDER BY age_days DESC, name
