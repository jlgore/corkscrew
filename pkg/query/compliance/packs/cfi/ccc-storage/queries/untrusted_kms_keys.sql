-- CCC.ObjStor.C01: Prevent Requests to Buckets or Objects with Untrusted KMS Keys
-- This query identifies S3 buckets that are encrypted with KMS keys not in the trusted list

WITH trusted_keys AS (
    SELECT unnest(split(:trusted_kms_keys, ',')) AS key_arn
),
bucket_encryption AS (
    SELECT 
        r.id,
        r.name,
        r.arn,
        r.region,
        r.account_id,
        json_extract_string(r.raw_data, '$.ServerSideEncryptionConfiguration.Rules[0].ApplyServerSideEncryptionByDefault.KMSMasterKeyID') AS kms_key_id,
        json_extract_string(r.raw_data, '$.ServerSideEncryptionConfiguration.Rules[0].ApplyServerSideEncryptionByDefault.SSEAlgorithm') AS encryption_algorithm,
        r.tags,
        r.scanned_at
    FROM aws_resources r
    WHERE r.type = 'AWS::S3::Bucket'
        AND r.raw_data IS NOT NULL
),
untrusted_buckets AS (
    SELECT 
        be.*,
        tk.key_arn IS NULL AS is_untrusted_key
    FROM bucket_encryption be
    LEFT JOIN trusted_keys tk ON (
        be.kms_key_id = tk.key_arn 
        OR be.kms_key_id LIKE '%' || split_part(tk.key_arn, '/', -1) || '%'
    )
    WHERE be.encryption_algorithm = 'aws:kms'
        AND be.kms_key_id IS NOT NULL
)

SELECT 
    'FAIL' AS status,
    ub.id AS resource_id,
    ub.name AS bucket_name,
    ub.arn AS bucket_arn,
    ub.region,
    ub.account_id,
    ub.kms_key_id,
    'Bucket encrypted with untrusted KMS key' AS issue_description,
    'HIGH' AS severity,
    json_object(
        'bucket_name', ub.name,
        'kms_key_id', ub.kms_key_id,
        'encryption_algorithm', ub.encryption_algorithm,
        'region', ub.region,
        'account_id', ub.account_id,
        'tags', ub.tags
    ) AS details,
    ub.scanned_at
FROM untrusted_buckets ub
WHERE ub.is_untrusted_key = true

UNION ALL

-- Include summary of compliant buckets
SELECT 
    'PASS' AS status,
    ub.id AS resource_id,
    ub.name AS bucket_name,
    ub.arn AS bucket_arn,
    ub.region,
    ub.account_id,
    ub.kms_key_id,
    'Bucket encrypted with trusted KMS key' AS issue_description,
    'INFO' AS severity,
    json_object(
        'bucket_name', ub.name,
        'kms_key_id', ub.kms_key_id,
        'encryption_algorithm', ub.encryption_algorithm,
        'region', ub.region,
        'account_id', ub.account_id,
        'tags', ub.tags
    ) AS details,
    ub.scanned_at
FROM untrusted_buckets ub
WHERE ub.is_untrusted_key = false

ORDER BY status DESC, bucket_name;