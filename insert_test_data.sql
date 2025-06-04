-- Insert enhanced S3 bucket data with detailed configuration
-- This simulates what our enhanced scanner would collect

-- Update existing bucket with enhanced configuration data
UPDATE aws_resources 
SET 
  raw_data = '{
    "BucketLocation": {"LocationConstraint": "us-east-1"},
    "BucketVersioning": {"Status": "Suspended"},
    "BucketEncryption": null,
    "PublicAccessBlock": null,
    "BucketPolicy": null,
    "BucketLifecycleConfiguration": null,
    "BucketLogging": null,
    "BucketNotificationConfiguration": {"Configurations": []},
    "BucketTagging": {"TagSet": [{"Key": "Environment", "Value": "Test"}]}
  }',
  attributes = '{
    "encryption_enabled": "false",
    "versioning_enabled": "false", 
    "public_access_restricted": "false",
    "logging_enabled": "false",
    "policy_exists": "false"
  }'
WHERE name = 'test-sandbox-4v2lztyv';

-- Insert additional test buckets with various configurations
INSERT INTO aws_resources (
  id, type, service, arn, name, region, account_id, 
  raw_data, attributes, tags, scanned_at
) VALUES 

-- Compliant bucket with all security features enabled
('compliant-bucket-001', 'Bucket', 's3', 
 'arn:aws:s3:::compliant-bucket-001', 
 'compliant-bucket-001', 'us-east-1', '590184030183',
 '{
   "BucketLocation": {"LocationConstraint": "us-east-1"},
   "BucketVersioning": {"Status": "Enabled", "MfaDelete": "Disabled"},
   "BucketEncryption": {
     "ServerSideEncryptionConfiguration": {
       "Rules": [{
         "ApplyServerSideEncryptionByDefault": {
           "SSEAlgorithm": "aws:kms",
           "KMSMasterKeyID": "arn:aws:kms:us-east-1:590184030183:key/trusted-key-123"
         }
       }]
     }
   },
   "PublicAccessBlock": {
     "PublicAccessBlockConfiguration": {
       "BlockPublicAcls": true,
       "BlockPublicPolicy": true,
       "IgnorePublicAcls": true,
       "RestrictPublicBuckets": true
     }
   },
   "BucketPolicy": {
     "Policy": "{\"Version\":\"2012-10-17\",\"Statement\":[{\"Effect\":\"Deny\",\"Principal\":\"*\",\"Action\":\"s3:DeleteBucket\",\"Resource\":\"arn:aws:s3:::compliant-bucket-001\"}]}"
   },
   "BucketLifecycleConfiguration": {
     "Rules": [{
       "Status": "Enabled",
       "Transition": {"Days": 30, "StorageClass": "STANDARD_IA"}
     }]
   },
   "BucketLogging": {
     "LoggingEnabled": {
       "TargetBucket": "compliant-bucket-001-logs",
       "TargetPrefix": "access-logs/"
     }
   },
   "BucketTagging": {
     "TagSet": [
       {"Key": "Environment", "Value": "Production"},
       {"Key": "Security", "Value": "High"},
       {"Key": "Compliance", "Value": "CCC"}
     ]
   }
 }',
 '{
   "encryption_enabled": "true",
   "versioning_enabled": "true",
   "public_access_restricted": "true", 
   "logging_enabled": "true",
   "policy_exists": "true",
   "mfa_delete_enabled": "false",
   "lifecycle_configured": "true",
   "kms_key": "arn:aws:kms:us-east-1:590184030183:key/trusted-key-123"
 }',
 '{"Environment": "Production", "Security": "High", "Compliance": "CCC"}',
 CURRENT_TIMESTAMP),

-- Non-compliant bucket with security issues
('insecure-bucket-002', 'Bucket', 's3',
 'arn:aws:s3:::insecure-bucket-002',
 'insecure-bucket-002', 'us-east-1', '590184030183', 
 '{
   "BucketLocation": {"LocationConstraint": "us-east-1"},
   "BucketVersioning": {"Status": "Suspended"},
   "BucketEncryption": null,
   "PublicAccessBlock": null,
   "BucketPolicy": null,
   "BucketLifecycleConfiguration": null,
   "BucketLogging": null,
   "BucketNotificationConfiguration": {"Configurations": []},
   "BucketTagging": {"TagSet": [{"Key": "Environment", "Value": "Dev"}]}
 }',
 '{
   "encryption_enabled": "false",
   "versioning_enabled": "false",
   "public_access_restricted": "false",
   "logging_enabled": "false", 
   "policy_exists": "false",
   "mfa_delete_enabled": "false",
   "lifecycle_configured": "false"
 }',
 '{"Environment": "Dev"}',
 CURRENT_TIMESTAMP),

-- Partially compliant bucket 
('partial-bucket-003', 'Bucket', 's3',
 'arn:aws:s3:::partial-bucket-003', 
 'partial-bucket-003', 'us-east-1', '590184030183',
 '{
   "BucketLocation": {"LocationConstraint": "us-east-1"},
   "BucketVersioning": {"Status": "Enabled"},
   "BucketEncryption": {
     "ServerSideEncryptionConfiguration": {
       "Rules": [{
         "ApplyServerSideEncryptionByDefault": {"SSEAlgorithm": "AES256"}
       }]
     }
   },
   "PublicAccessBlock": {
     "PublicAccessBlockConfiguration": {
       "BlockPublicAcls": true,
       "BlockPublicPolicy": true,
       "IgnorePublicAcls": true,
       "RestrictPublicBuckets": true
     }
   },
   "BucketPolicy": null,
   "BucketLifecycleConfiguration": null,
   "BucketLogging": null,
   "BucketTagging": {"TagSet": [{"Key": "Environment", "Value": "Staging"}]}
 }',
 '{
   "encryption_enabled": "true",
   "versioning_enabled": "true",
   "public_access_restricted": "true",
   "logging_enabled": "false",
   "policy_exists": "false",
   "mfa_delete_enabled": "false", 
   "lifecycle_configured": "false"
 }',
 '{"Environment": "Staging"}',
 CURRENT_TIMESTAMP);