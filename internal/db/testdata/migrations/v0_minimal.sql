-- A valid version-0 database could contain unrelated user tables and none of
-- Corkscrew's optional persistence tables. Migrations must retain them.
CREATE TABLE legacy_fixture_marker (
    id INTEGER PRIMARY KEY,
    note VARCHAR NOT NULL
);

INSERT INTO legacy_fixture_marker VALUES (1, 'retain unknown user tables');
