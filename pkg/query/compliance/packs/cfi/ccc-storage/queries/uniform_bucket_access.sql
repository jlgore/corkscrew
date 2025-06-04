-- CCC.ObjStor.C02: Enforce Uniform Bucket-level Access to Prevent Inconsistent Permissions
-- This query identifies S3 buckets that do not have uniform bucket-level access enabled

WITH bucket_public_access AS (
    SELECT 
        r.id,
        r.name,
        r.arn,
        r.region,
        r.account_id,
        json_extract_string(r.raw_data, '$.PublicAccessBlockConfiguration.BlockPublicAcls') AS block_public_acls,
        json_extract_string(r.raw_data, '$.PublicAccessBlockConfiguration.BlockPublicPolicy') AS block_public_policy,
        json_extract_string(r.raw_data, '$.PublicAccessBlockConfiguration.IgnorePublicAcls') AS ignore_public_acls,
        json_extract_string(r.raw_data, '$.PublicAccessBlockConfiguration.RestrictPublicBuckets') AS restrict_public_buckets,
        COALESCE(json_extract_string(r.raw_data, '$.PublicAccessBlockConfiguration.BlockPublicAcls'), 'false') = 'true' AS has_block_public_acls,
        COALESCE(json_extract_string(r.raw_data, '$.PublicAccessBlockConfiguration.BlockPublicPolicy'), 'false') = 'true' AS has_block_public_policy,
        COALESCE(json_extract_string(r.raw_data, '$.PublicAccessBlockConfiguration.IgnorePublicAcls'), 'false') = 'true' AS has_ignore_public_acls,
        COALESCE(json_extract_string(r.raw_data, '$.PublicAccessBlockConfiguration.RestrictPublicBuckets'), 'false') = 'true' AS has_restrict_public_buckets,
        r.tags,
        r.scanned_at
    FROM aws_resources r
    WHERE r.type = 'AWS::S3::Bucket'
),
non_compliant_buckets AS (
    SELECT *,
        CASE 
            WHEN NOT (has_block_public_acls AND has_block_public_policy AND has_ignore_public_acls AND has_restrict_public_buckets)
            THEN true
            ELSE false
        END AS is_non_compliant,
        ARRAY[
            CASE WHEN NOT has_block_public_acls THEN 'BlockPublicAcls disabled' END,
            CASE WHEN NOT has_block_public_policy THEN 'BlockPublicPolicy disabled' END,
            CASE WHEN NOT has_ignore_public_acls THEN 'IgnorePublicAcls disabled' END,
            CASE WHEN NOT has_restrict_public_buckets THEN 'RestrictPublicBuckets disabled' END
        ] AS missing_controls
    FROM bucket_public_access
)

SELECT 
    CASE WHEN is_non_compliant THEN 'FAIL' ELSE 'PASS' END AS status,
    id AS resource_id,
    name AS bucket_name,
    arn AS bucket_arn,
    region,
    account_id,
    CASE 
        WHEN is_non_compliant THEN 'Bucket does not have uniform bucket-level access controls enabled'
        ELSE 'Bucket has proper uniform bucket-level access controls'
    END AS issue_description,
    CASE WHEN is_non_compliant THEN 'MEDIUM' ELSE 'INFO' END AS severity,
    json_object(
        'bucket_name', name,
        'region', region,
        'account_id', account_id,
        'block_public_acls', block_public_acls,
        'block_public_policy', block_public_policy,
        'ignore_public_acls', ignore_public_acls,
        'restrict_public_buckets', restrict_public_buckets,
        'missing_controls', array_to_string(array_filter(missing_controls, x -> x IS NOT NULL), ', '),
        'tags', tags
    ) AS details,
    scanned_at
FROM non_compliant_buckets
ORDER BY is_non_compliant DESC, bucket_name;