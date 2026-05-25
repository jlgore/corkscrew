RUSTFLAGS := -C target-cpu=native
CRATE := corkscrew_graph
BUILD_DIR := build
TARGET_DIR := target/release
EXT := $(BUILD_DIR)/$(CRATE).duckdb_extension
DUCKDB ?= duckdb
CARGO_DUCKDB_EXT ?= cargo duckdb-ext
DUCKDB_EXT_VERSION ?= v1.5.2
FIXTURE_DB := __test_graph_info.db
MULTI_PROVIDER_DB := __test_graph_info_multi.db

.PHONY: all build clean test test-host

all: build

build:
	@echo "Building corkscrew-graph extension (release)"
	$(CARGO_DUCKDB_EXT) build --duckdb-version $(DUCKDB_EXT_VERSION) -- --release
	mkdir -p $(BUILD_DIR)
	cp $(TARGET_DIR)/$(CRATE).duckdb_extension $(EXT)

# Milestone 1 verification combines a real DuckDB CLI load smoke test with the
# sibling bundled-host test crate that exercises the SQL surface in-process.
test: build
	@echo "Running DuckDB CLI smoke tests against $(EXT)"
	@rm -f $(FIXTURE_DB) $(FIXTURE_DB).wal $(MULTI_PROVIDER_DB) $(MULTI_PROVIDER_DB).wal
	$(DUCKDB) $(FIXTURE_DB) -batch -c "CREATE TABLE aws_resources (id VARCHAR, type VARCHAR, name VARCHAR, region VARCHAR, account_id VARCHAR, arn VARCHAR, tags VARCHAR); CREATE TABLE aws_relationships (from_id VARCHAR, to_id VARCHAR, relationship_type VARCHAR, properties VARCHAR); INSERT INTO aws_resources VALUES ('r1','ec2','i-1','us-east-1','123',NULL,NULL), ('r2','ec2','i-2','us-east-1','123',NULL,NULL), ('r3','s3','b-1','us-east-1','123',NULL,NULL); INSERT INTO aws_relationships VALUES ('r1','r2','peer',NULL), ('r1','r3','reads',NULL);"
	@test "$$($(DUCKDB) :memory: -unsigned -batch -csv -noheader -c "LOAD '$(abspath $(EXT))'; SELECT provider, nodes, edges FROM graph_info('$(FIXTURE_DB)') ORDER BY provider;")" = "aws,3,2"
	@test "$$($(DUCKDB) :memory: -unsigned -batch -csv -noheader -c "LOAD '$(abspath $(EXT))'; SELECT pattern_name FROM graph_list_patterns() ORDER BY pattern_name;")" = "$$(printf 'cross_account_trust\ninternet_to_database\nk8s_privileged_to_cloud\nlateral_movement_risk\noverprivileged_lambda\npublic_lb_to_private_db\npublic_s3_via_instance\nunencrypted_data_path')"
	@test "$$($(DUCKDB) :memory: -unsigned -batch -csv -noheader -c "LOAD '$(abspath $(EXT))'; SELECT pattern_name FROM attack_patterns() ORDER BY pattern_name;")" = "$$(printf 'cross_account_trust\ninternet_to_database\nk8s_privileged_to_cloud\nlateral_movement_risk\noverprivileged_lambda\npublic_lb_to_private_db\npublic_s3_via_instance\nunencrypted_data_path')"
	@test "$$($(DUCKDB) :memory: -unsigned -batch -csv -noheader -c "LOAD '$(abspath $(EXT))'; SELECT node_id, hop_count FROM graph_traverse('$(FIXTURE_DB)', 'r1', 3, 'outbound', 'NULL') ORDER BY hop_count, node_id;")" = "$$(printf 'r2,1\nr3,1')"
	@test "$$($(DUCKDB) :memory: -unsigned -batch -csv -noheader -c "LOAD '$(abspath $(EXT))'; SELECT hop_number, resource_id FROM cloud_path('$(FIXTURE_DB)', 'r1', 'r3') ORDER BY hop_number;")" = "$$(printf '0,r1\n1,r3')"
	@test "$$($(DUCKDB) :memory: -unsigned -batch -csv -noheader -c "LOAD '$(abspath $(EXT))'; SELECT hop_number, resource_id, incoming_rel_type, total_hops FROM graph_shortest_path('$(FIXTURE_DB)', 'r1', 'r3', false) ORDER BY hop_number;")" = "$$(printf '0,r1,NULL,1\n1,r3,reads,1')"
	@test "$$($(DUCKDB) :memory: -unsigned -batch -csv -noheader -c "LOAD '$(abspath $(EXT))'; SELECT resource_type, reachable_count, max_hop_distance FROM graph_blast_radius('$(FIXTURE_DB)', 'r1', 3) ORDER BY resource_type;")" = "$$(printf 'ec2,1,1\ns3,1,1')"
	@test "$$($(DUCKDB) :memory: -unsigned -batch -csv -noheader -c "LOAD '$(abspath $(EXT))'; SELECT resource_type, reachable_count FROM blast_radius('$(FIXTURE_DB)', 'r1', 3) ORDER BY resource_type;")" = "$$(printf 'ec2,1\ns3,1')"
	@test "$$($(DUCKDB) :memory: -unsigned -batch -csv -noheader -c "LOAD '$(abspath $(EXT))'; SELECT is_reachable, match_count, closest_hop, example_id FROM graph_reachable('$(FIXTURE_DB)', 'r1', 's3', 3);")" = "true,1,1,r3"
	@test "$$($(DUCKDB) :memory: -unsigned -batch -csv -noheader -c "LOAD '$(abspath $(EXT))'; SELECT match_id, pattern_node, resource_id FROM graph_match_pattern('$(FIXTURE_DB)', 'public_s3_via_instance') ORDER BY match_id, pattern_node;")" = "$$(printf '0,bucket,r3\n0,instance,r1')"
	@test "$$($(DUCKDB) :memory: -unsigned -batch -csv -noheader -c "LOAD '$(abspath $(EXT))'; SELECT invalidated FROM graph_cache_invalidate('$(FIXTURE_DB)');")" = "1"
	$(DUCKDB) $(MULTI_PROVIDER_DB) -batch -c "CREATE TABLE aws_resources (id VARCHAR, type VARCHAR, name VARCHAR, region VARCHAR, account_id VARCHAR, arn VARCHAR, tags VARCHAR); CREATE TABLE aws_relationships (from_id VARCHAR, to_id VARCHAR, relationship_type VARCHAR, properties VARCHAR); CREATE TABLE azure_resources (id VARCHAR, type VARCHAR, name VARCHAR, region VARCHAR, account_id VARCHAR, arn VARCHAR, tags VARCHAR); CREATE TABLE azure_relationships (from_id VARCHAR, to_id VARCHAR, relationship_type VARCHAR, properties VARCHAR); INSERT INTO aws_resources VALUES ('aws-r1','ec2','i-1','us-east-1','123',NULL,NULL), ('aws-r2','s3','b-1','us-east-1','123',NULL,NULL); INSERT INTO aws_relationships VALUES ('aws-r1','aws-r2','reads',NULL); INSERT INTO azure_resources VALUES ('az-r1','vm','vm-1','eastus','sub-1',NULL,NULL); INSERT INTO azure_relationships VALUES ('az-r1','az-r1','self',NULL);"
	@out="$$($(DUCKDB) :memory: -unsigned -batch -csv -noheader -c "LOAD '$(abspath $(EXT))'; SELECT provider, nodes, edges FROM graph_info('$(MULTI_PROVIDER_DB)') ORDER BY provider;")"; expected="$$(printf 'aws,2,1\nazure,1,1')"; test "$$out" = "$$expected"
	@echo "Running bundled-host graph tests"
	$(MAKE) test-host
	@rm -f $(FIXTURE_DB) $(FIXTURE_DB).wal $(MULTI_PROVIDER_DB) $(MULTI_PROVIDER_DB).wal

# Pure-Rust unit tests for graph::schema, graph::loader, graph::cache.
# These run in a sibling crate that links DuckDB statically (bundled),
# so they do NOT require a running DuckDB host process.
test-host:
	cargo test --manifest-path ../corkscrew-graph-tests/Cargo.toml

clean:
	cargo clean
	rm -rf $(BUILD_DIR)
	rm -f $(FIXTURE_DB) $(FIXTURE_DB).wal $(MULTI_PROVIDER_DB) $(MULTI_PROVIDER_DB).wal
