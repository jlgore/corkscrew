-- CF.SEC.03: a Secrets Store secret was last modified more than max_secret_age_days before
-- as_of_date (today when empty). Reads the provider's modified_on attribute, the secret's own
-- last-modified time (also stored as modified_at). Deleted secrets aren't evaluated.
WITH params AS (
    SELECT coalesce(TRY_CAST(nullif(:as_of_date, '') AS DATE), current_date) AS as_of
),
evaluated AS (
    SELECT
        r.id,
        r.name,
        r.account_id,
        json_extract_string(r.attributes, '$.store_name') AS store_name,
        json_extract_string(r.attributes, '$.modified_on') AS modified_on,
        date_diff('day', CAST(TRY_CAST(json_extract_string(r.attributes, '$.modified_on') AS TIMESTAMP) AS DATE), p.as_of) AS age_days
    FROM cloudflare_resources r, params p
    WHERE r.type = 'secret_store_secret'
        AND coalesce(json_extract_string(r.attributes, '$.status'), '') <> 'deleted'
        AND TRY_CAST(json_extract_string(r.attributes, '$.modified_on') AS TIMESTAMP) IS NOT NULL
)
SELECT
    CASE WHEN age_days > :max_secret_age_days THEN 'FAIL' ELSE 'PASS' END AS status,
    id AS resource_id,
    store_name || '/' || name AS resource_name,
    'secret_store_secret' AS resource_type,
    'CF.SEC.03' AS control_id,
    'Secrets Store secret hasn''t been rotated' AS control_name,
    CASE WHEN age_days > :max_secret_age_days THEN 'MEDIUM' ELSE 'INFO' END AS severity,
    CASE
        WHEN age_days > :max_secret_age_days THEN 'Secret last modified ' || age_days || ' days ago'
        ELSE 'Secret rotated within ' || :max_secret_age_days || ' days'
    END AS issue_description,
    json_object('account_id', account_id, 'store', store_name, 'modified_on', modified_on, 'age_days', age_days) AS details
FROM evaluated
ORDER BY age_days DESC, name
