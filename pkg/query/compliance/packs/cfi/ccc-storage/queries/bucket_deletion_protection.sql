-- CCC.ObjStor.C03: Prevent Bucket Deletion Through Irrevocable Bucket Retention Policy
-- This query identifies S3 buckets that lack proper deletion protection mechanisms

WITH bucket_versioning AS (
    SELECT 
        r.id,
        r.name,
        r.arn,
        r.region,
        r.account_id,
        json_extract_string(r.raw_data, '$.Versioning.Status') AS versioning_status,
        json_extract_string(r.raw_data, '$.Versioning.MfaDelete') AS mfa_delete_status,
        r.tags,
        r.scanned_at
    FROM aws_resources r
    WHERE r.type = 'AWS::S3::Bucket'
),
bucket_lifecycle AS (
    SELECT 
        r.id,
        r.name,
        json_extract(r.raw_data, '$.LifecycleConfiguration.Rules') AS lifecycle_rules,
        CASE 
            WHEN json_extract(r.raw_data, '$.LifecycleConfiguration.Rules') IS NOT NULL 
            THEN true 
            ELSE false 
        END AS has_lifecycle_policy
    FROM aws_resources r
    WHERE r.type = 'AWS::S3::Bucket'
),
bucket_policy AS (
    SELECT 
        r.id,
        r.name,
        json_extract_string(r.raw_data, '$.Policy') AS bucket_policy,
        CASE 
            WHEN json_extract_string(r.raw_data, '$.Policy') LIKE '%s3:DeleteBucket%' 
                AND json_extract_string(r.raw_data, '$.Policy') LIKE '%Deny%'
            THEN true 
            ELSE false 
        END AS has_delete_protection_policy
    FROM aws_resources r
    WHERE r.type = 'AWS::S3::Bucket'
),
bucket_protection_analysis AS (
    SELECT 
        bv.*,
        bl.has_lifecycle_policy,
        bl.lifecycle_rules,
        bp.has_delete_protection_policy,
        bp.bucket_policy,
        CASE 
            WHEN bv.versioning_status = 'Enabled' THEN true 
            ELSE false 
        END AS has_versioning,
        CASE 
            WHEN bv.mfa_delete_status = 'Enabled' THEN true 
            ELSE false 
        END AS has_mfa_delete,
        CASE 
            WHEN bv.versioning_status = 'Enabled' 
                AND (bl.has_lifecycle_policy OR bp.has_delete_protection_policy)
            THEN true 
            ELSE false 
        END AS is_protected
    FROM bucket_versioning bv
    LEFT JOIN bucket_lifecycle bl ON bv.id = bl.id
    LEFT JOIN bucket_policy bp ON bv.id = bp.id
),
protection_issues AS (
    SELECT *,
        ARRAY[
            CASE WHEN NOT has_versioning THEN 'Versioning not enabled' END,
            CASE WHEN NOT has_mfa_delete THEN 'MFA delete not enabled' END,
            CASE WHEN NOT has_lifecycle_policy AND NOT has_delete_protection_policy 
                 THEN 'No lifecycle policy or bucket deletion protection' END
        ] AS protection_issues_list
    FROM bucket_protection_analysis
)

SELECT 
    CASE WHEN NOT is_protected THEN 'FAIL' ELSE 'PASS' END AS status,
    id AS resource_id,
    name AS bucket_name,
    arn AS bucket_arn,
    region,
    account_id,
    CASE 
        WHEN NOT is_protected THEN 'Bucket lacks adequate deletion protection mechanisms'
        ELSE 'Bucket has proper deletion protection'
    END AS issue_description,
    CASE WHEN NOT is_protected THEN 'CRITICAL' ELSE 'INFO' END AS severity,
    json_object(
        'bucket_name', name,
        'region', region,
        'account_id', account_id,
        'versioning_status', versioning_status,
        'mfa_delete_status', mfa_delete_status,
        'has_lifecycle_policy', has_lifecycle_policy,
        'has_delete_protection_policy', has_delete_protection_policy,
        'protection_issues', array_to_string(array_filter(protection_issues_list, x -> x IS NOT NULL), ', '),
        'retention_period_days', :retention_period_days,
        'tags', tags
    ) AS details,
    scanned_at
FROM protection_issues
ORDER BY is_protected ASC, bucket_name;