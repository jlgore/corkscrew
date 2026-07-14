-- The archive name already exists. The migration must fail atomically without
-- renaming or deleting the original table or recording a schema version.
CREATE TABLE scan_metadata (
    id VARCHAR PRIMARY KEY,
    service VARCHAR,
    region VARCHAR,
    scan_time TIMESTAMP,
    total_resources INTEGER,
    failed_resources INTEGER,
    duration_ms BIGINT,
    metadata JSON
);

INSERT INTO scan_metadata VALUES (
    'must-survive-rollback', 's3', 'us-east-1', TIMESTAMP '2025-01-03 01:02:03',
    1, 0, 1, '{}'
);

CREATE TABLE scan_metadata_legacy_v0 (id VARCHAR);
