-- Historical graph-loader resource schema.
CREATE TABLE aws_resources (
    id VARCHAR PRIMARY KEY,
    type VARCHAR NOT NULL,
    service VARCHAR,
    arn VARCHAR,
    name VARCHAR,
    region VARCHAR,
    account_id VARCHAR,
    parent_id VARCHAR,
    raw_data JSON,
    attributes JSON,
    tags JSON,
    created_at TIMESTAMP,
    modified_at TIMESTAMP,
    scanned_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO aws_resources VALUES (
    'aws:s3:legacy-bucket', 'AWS::S3::Bucket', 's3',
    'arn:aws:s3:::legacy-bucket', 'legacy bucket', 'us-east-1', '111122223333',
    NULL, '{"name":"legacy-bucket"}', '{"versioning":true}', '{"env":"legacy"}',
    TIMESTAMP '2025-01-01 01:02:03', TIMESTAMP '2025-01-02 01:02:03',
    TIMESTAMP '2025-01-03 01:02:03'
);

CREATE TABLE aws_relationships (
    from_id VARCHAR NOT NULL,
    to_id VARCHAR NOT NULL,
    relationship_type VARCHAR NOT NULL,
    properties JSON,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (from_id, to_id, relationship_type)
);

INSERT INTO aws_relationships VALUES (
    'aws:s3:legacy-bucket', 'aws:kms:legacy-key', 'encrypted_by',
    '{"source":"v0"}', TIMESTAMP '2025-01-03 01:02:03'
);

-- Historical graph-loader scan metadata schema.
CREATE TABLE scan_metadata (
    id VARCHAR PRIMARY KEY,
    service VARCHAR NOT NULL,
    region VARCHAR NOT NULL,
    scan_time TIMESTAMP NOT NULL,
    total_resources INTEGER,
    failed_resources INTEGER,
    duration_ms BIGINT,
    metadata JSON
);

INSERT INTO scan_metadata VALUES
    ('scan-aws', 's3', 'us-east-1', TIMESTAMP '2025-01-03 01:02:03', 7, 1, 1250, '{"provider":"aws","source":"fixture"}'),
    ('scan-unknown', 'custom', 'global', TIMESTAMP '2025-01-04 01:02:03', 2, 0, 50, '{"source":"fixture"}');

-- Historical graph-loader API action metadata schema.
CREATE TABLE api_action_metadata (
    id VARCHAR PRIMARY KEY,
    service VARCHAR NOT NULL,
    operation_name VARCHAR NOT NULL,
    operation_type VARCHAR,
    execution_time TIMESTAMP NOT NULL,
    region VARCHAR,
    success BOOLEAN NOT NULL,
    duration_ms BIGINT,
    resource_count INTEGER DEFAULT 0,
    error_message VARCHAR,
    request_id VARCHAR,
    metadata JSON,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO api_action_metadata VALUES (
    'action-list-buckets', 's3', 'ListBuckets', 'List',
    TIMESTAMP '2025-01-03 01:02:04', 'us-east-1', TRUE, 12, 3, NULL,
    'request-v0', '{"provider":"aws","source":"fixture"}',
    TIMESTAMP '2025-01-03 01:02:05'
);

-- Historical generic resource store; this remains a supported compatibility
-- table, so its rows and shape must survive unchanged.
CREATE TABLE crosscloud_resources (
    id VARCHAR PRIMARY KEY,
    name VARCHAR,
    type VARCHAR,
    service VARCHAR,
    provider VARCHAR,
    region VARCHAR,
    arn VARCHAR,
    status VARCHAR,
    created_at TIMESTAMP,
    modified_at TIMESTAMP,
    scanned_at TIMESTAMP,
    tags JSON,
    attributes JSON,
    metadata JSON,
    raw_data JSON,
    cross_cloud_id VARCHAR
);

INSERT INTO crosscloud_resources VALUES (
    'legacy:aggregate:one', 'aggregate one', 'bucket', 'storage', 'aws', 'us-east-1',
    'arn:legacy:aggregate:one', 'active', TIMESTAMP '2025-01-01 00:00:00',
    TIMESTAMP '2025-01-02 00:00:00', TIMESTAMP '2025-01-03 00:00:00',
    '{"env":"legacy"}', '{"encrypted":true}', '{"source":"v0"}',
    '{"id":"legacy:aggregate:one"}', 'cross-cloud-one'
);

CREATE TABLE crosscloud_ip_addresses (
    address VARCHAR,
    type VARCHAR,
    version VARCHAR,
    provider VARCHAR,
    region VARCHAR,
    resource_id VARCHAR,
    scope VARCHAR,
    PRIMARY KEY (address, provider, resource_id)
);

INSERT INTO crosscloud_ip_addresses VALUES
    ('203.0.113.10', 'public', 'ipv4', 'aws', 'us-east-1', 'aws:lb:one', 'regional'),
    ('2001:db8::10', 'private', 'ipv6', 'azure', 'eastus', 'azure:vm:one', 'vnet');

CREATE TABLE crosscloud_dns_records (
    name VARCHAR,
    type VARCHAR,
    values JSON,
    ttl INTEGER,
    provider VARCHAR,
    zone VARCHAR,
    resource_id VARCHAR,
    PRIMARY KEY (name, type, provider, resource_id)
);

INSERT INTO crosscloud_dns_records VALUES
    ('app.example.com', 'A', '["203.0.113.10"]', 300, 'aws', 'example.com', 'aws:lb:one'),
    ('edge.example.com', 'CNAME', '["target.example.net"]', 60, 'cloudflare', 'example.com', 'cloudflare:dns:one');

CREATE TABLE crosscloud_correlations (
    id VARCHAR PRIMARY KEY,
    source_id VARCHAR,
    target_id VARCHAR,
    type VARCHAR,
    relation_type VARCHAR,
    strength DOUBLE,
    confidence DOUBLE,
    description VARCHAR,
    metadata JSON,
    discovered_at TIMESTAMP
);

INSERT INTO crosscloud_correlations VALUES (
    'correlation-v0', 'aws:lb:one', 'azure:dns:one', 'cross_cloud', 'dns_target',
    0.9, 0.8, 'legacy correlation', '{"method":"dns"}',
    TIMESTAMP '2025-01-03 01:02:03'
);

CREATE TABLE crosscloud_generic_correlations (
    id VARCHAR PRIMARY KEY,
    correlation_data JSON,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO crosscloud_generic_correlations VALUES (
    'generic-v0', '{"kind":"legacy","count":1}', TIMESTAMP '2025-01-03 01:02:03'
);
