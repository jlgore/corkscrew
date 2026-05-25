use corkscrew_graph_tests::functions::info::GraphInfoVTab;
use corkscrew_graph_tests::functions::info::GraphCacheInvalidateVTab;
use corkscrew_graph_tests::functions::blast::GraphBlastRadiusVTab;
use corkscrew_graph_tests::functions::list_patterns::GraphListPatternsVTab;
use corkscrew_graph_tests::functions::match_pattern::GraphMatchPatternVTab;
use corkscrew_graph_tests::functions::paths::GraphShortestPathVTab;
use corkscrew_graph_tests::functions::reachable::GraphReachableVTab;
use corkscrew_graph_tests::functions::traverse::GraphTraverseVTab;
use corkscrew_graph_tests::graph::{cache, loader, schema};
use corkscrew_graph_tests::sql_macros;
use duckdb::Connection;
use std::sync::{Arc, Barrier};
use std::thread;
use tempfile::TempDir;

fn make_fixture(conn: &Connection) {
    conn.execute_batch(
        "CREATE TABLE aws_resources (
            id VARCHAR, type VARCHAR, name VARCHAR, region VARCHAR,
            account_id VARCHAR, arn VARCHAR, tags VARCHAR
        );
        CREATE TABLE aws_relationships (
            from_id VARCHAR, to_id VARCHAR,
            relationship_type VARCHAR, properties VARCHAR
        );
        INSERT INTO aws_resources VALUES
            ('r1','ec2','i-1','us-east-1','123',NULL,NULL),
            ('r2','ec2','i-2','us-east-1','123',NULL,NULL),
            ('r3','s3', 'b-1','us-east-1','123',NULL,NULL);
        INSERT INTO aws_relationships VALUES
            ('r1','r2','peer',NULL),
            ('r1','r3','reads',NULL);",
    )
    .unwrap();
}

fn make_pattern_fixture(conn: &Connection) {
    conn.execute_batch(
        "CREATE TABLE aws_resources (
            id VARCHAR, type VARCHAR, name VARCHAR, region VARCHAR,
            account_id VARCHAR, arn VARCHAR, tags VARCHAR
        );
        CREATE TABLE aws_relationships (
            from_id VARCHAR, to_id VARCHAR,
            relationship_type VARCHAR, properties VARCHAR
        );
        INSERT INTO aws_resources VALUES
            ('igw1','aws::ec2::InternetGateway','igw-1','us-east-1','123',NULL,NULL),
            ('sg1','aws::ec2::SecurityGroup','sg-1','us-east-1','123',NULL,NULL),
            ('inst1','aws::ec2::Instance','app-1','us-east-1','123',NULL,NULL),
            ('db1','aws::rds::DBInstance','db-1','us-east-1','123',NULL,NULL),
            ('lb1','aws::elasticloadbalancing::LoadBalancer','lb-1','us-east-1','123',NULL,NULL),
            ('bucket1','aws::s3::Bucket','bucket-1','us-east-1','123',NULL,NULL),
            ('pod1','kubernetes::Pod','pod-1','us-east-1','123',NULL,NULL),
            ('sa1','kubernetes::ServiceAccount','sa-1','us-east-1','123',NULL,NULL),
            ('role1','aws::iam::Role','role-1','us-east-1','123',NULL,NULL),
            ('lambda1','aws::lambda::Function','lambda-1','us-east-1','123',NULL,NULL),
            ('policy1','aws::iam::Policy','AdministratorAccess','us-east-1','123',NULL,NULL),
            ('role2','aws::iam::Role','external-role','us-east-1','999',NULL,NULL),
            ('inst2','aws::ec2::Instance','app-2','us-east-1','123',NULL,NULL),
            ('bucket2','aws::s3::Bucket','bucket-2','us-east-1','123',NULL,NULL);
        INSERT INTO aws_relationships VALUES
            ('igw1','sg1','allows',NULL),
            ('sg1','inst1','member_of',NULL),
            ('inst1','db1','connects_to',NULL),
            ('lb1','inst1','routes_to',NULL),
            ('igw1','inst1','exposes',NULL),
            ('inst1','bucket1','writes',NULL),
            ('pod1','sa1','uses',NULL),
            ('sa1','role1','assumes',NULL),
            ('lambda1','policy1','administratoraccess',NULL),
            ('role1','role2','assume_role',NULL),
            ('inst1','inst2','peer',NULL),
            ('inst2','bucket2','reads',NULL);",
    )
    .unwrap();
}

fn make_many_public_s3_matches_fixture(conn: &Connection) {
    conn.execute_batch(
        "CREATE TABLE aws_resources (
            id VARCHAR, type VARCHAR, name VARCHAR, region VARCHAR,
            account_id VARCHAR, arn VARCHAR, tags VARCHAR
        );
        CREATE TABLE aws_relationships (
            from_id VARCHAR, to_id VARCHAR,
            relationship_type VARCHAR, properties VARCHAR
        );",
    )
    .unwrap();

    for index in 0..300 {
        let sql = format!(
            "INSERT INTO aws_resources VALUES
                ('inst{index}','aws::ec2::Instance','inst{index}','us-east-1','123',NULL,NULL),
                ('bucket{index}','aws::s3::Bucket','bucket{index}','us-east-1','123',NULL,NULL);
             INSERT INTO aws_relationships VALUES
                ('inst{index}','bucket{index}','reads',NULL);"
        );
        conn.execute_batch(&sql).unwrap();
    }
}

fn make_shortest_path_edge_fixture(conn: &Connection) {
    conn.execute_batch(
        "CREATE TABLE aws_resources (
            id VARCHAR, type VARCHAR, name VARCHAR, region VARCHAR,
            account_id VARCHAR, arn VARCHAR, tags VARCHAR
        );
        CREATE TABLE aws_relationships (
            from_id VARCHAR, to_id VARCHAR,
            relationship_type VARCHAR, properties VARCHAR
        );
        INSERT INTO aws_resources VALUES
            ('a','ec2','a','us-east-1','123',NULL,NULL),
            ('b','ec2','b','us-east-1','123',NULL,NULL),
            ('c','ec2','c','us-east-1','123',NULL,NULL),
            ('isolated','ec2','isolated','us-east-1','123',NULL,NULL);
        INSERT INTO aws_relationships VALUES
            ('a','b','peer',NULL),
            ('b','c','peer',NULL);",
    )
    .unwrap();
}

fn register_graph_functions(conn: &Connection) {
    conn.register_table_function::<GraphInfoVTab>("graph_info").unwrap();
    conn.register_table_function::<GraphCacheInvalidateVTab>("graph_cache_invalidate").unwrap();
    conn.register_table_function::<GraphListPatternsVTab>("graph_list_patterns").unwrap();
    conn.register_table_function::<GraphTraverseVTab>("graph_traverse").unwrap();
    conn.register_table_function::<GraphShortestPathVTab>("graph_shortest_path").unwrap();
    conn.register_table_function::<GraphBlastRadiusVTab>("graph_blast_radius").unwrap();
    conn.register_table_function::<GraphReachableVTab>("graph_reachable").unwrap();
    conn.register_table_function::<GraphMatchPatternVTab>("graph_match_pattern").unwrap();
    sql_macros::register_macros(conn).unwrap();
}

#[test]
fn detect_providers_finds_aws() {
    let conn = Connection::open_in_memory().unwrap();
    make_fixture(&conn);
    let providers = schema::detect_providers(&conn).unwrap();
    assert_eq!(providers.len(), 1);
    assert_eq!(providers[0].provider, "aws");
    assert_eq!(providers[0].resources_table, "aws_resources");
    assert_eq!(providers[0].relationships_table, "aws_relationships");
}

#[test]
fn load_graph_counts_nodes_and_edges() {
    let conn = Connection::open_in_memory().unwrap();
    make_fixture(&conn);
    let providers = schema::detect_providers(&conn).unwrap();
    let loaded = loader::load_graph(&conn, &providers).unwrap();
    assert_eq!(loaded.node_count, 3);
    assert_eq!(loaded.edge_count, 2);
    assert_eq!(loaded.graph.node_weights().filter(|node| node.provider == "aws").count(), 3);
    assert_eq!(loaded.graph.edge_weights().filter(|edge| edge.provider == "aws").count(), 2);
}

#[test]
fn cache_get_or_load_then_invalidate() {
    let conn = Connection::open_in_memory().unwrap();
    make_fixture(&conn);

    let path = "test::cache_get_or_load";
    let g1 = cache::get_or_load(&conn, path).unwrap();
    let g2 = cache::get_or_load(&conn, path).unwrap();
    // Same Arc => cache hit
    assert!(std::sync::Arc::ptr_eq(&g1, &g2));

    cache::invalidate(path);
    let g3 = cache::get_or_load(&conn, path).unwrap();
    assert!(!std::sync::Arc::ptr_eq(&g1, &g3));
}

#[test]
fn cache_ttl_setting_zero_forces_reload() {
    let conn = Connection::open_in_memory().unwrap();
    make_fixture(&conn);

    let path = "test::cache_ttl_setting_zero_forces_reload";
    let g1 = cache::get_or_load(&conn, path).unwrap();
    conn.execute("SET VARIABLE corkscrew_graph_cache_ttl = 0", []).unwrap();
    let g2 = cache::get_or_load(&conn, path).unwrap();

    assert!(!std::sync::Arc::ptr_eq(&g1, &g2));
}

#[test]
fn cache_get_or_load_is_shared_across_threads() {
    let temp_dir = TempDir::new().unwrap();
    let fixture_path = temp_dir.path().join("fixture.duckdb").to_string_lossy().into_owned();

    let fixture_conn = Connection::open(&fixture_path).unwrap();
    make_fixture(&fixture_conn);
    drop(fixture_conn);

    let barrier = Arc::new(Barrier::new(8));
    let mut handles = Vec::new();
    for _ in 0..8 {
        let fixture_path = fixture_path.clone();
        let barrier = Arc::clone(&barrier);
        handles.push(thread::spawn(move || {
            let conn = Connection::open(&fixture_path).unwrap();
            barrier.wait();
            cache::get_or_load(&conn, &fixture_path).unwrap()
        }));
    }

    let graphs = handles
        .into_iter()
        .map(|handle| handle.join().unwrap())
        .collect::<Vec<_>>();

    for graph in &graphs[1..] {
        assert!(std::sync::Arc::ptr_eq(&graphs[0], graph));
    }
}

#[test]
fn cache_readers_and_invalidator_can_race_safely() {
    let temp_dir = TempDir::new().unwrap();
    let fixture_path = temp_dir.path().join("fixture.duckdb").to_string_lossy().into_owned();

    let fixture_conn = Connection::open(&fixture_path).unwrap();
    make_fixture(&fixture_conn);
    drop(fixture_conn);

    let path_for_invalidator = fixture_path.clone();
    let invalidator = thread::spawn(move || {
        for _ in 0..50 {
            cache::invalidate(&path_for_invalidator);
        }
    });

    let mut handles = Vec::new();
    for _ in 0..6 {
        let fixture_path = fixture_path.clone();
        handles.push(thread::spawn(move || {
            let conn = Connection::open(&fixture_path).unwrap();
            for _ in 0..25 {
                let loaded = cache::get_or_load(&conn, &fixture_path).unwrap();
                assert_eq!(loaded.node_count, 3);
                assert_eq!(loaded.edge_count, 2);
            }
        }));
    }

    invalidator.join().unwrap();
    for handle in handles {
        handle.join().unwrap();
    }
}

#[test]
fn detect_providers_empty_when_no_tables() {
    let conn = Connection::open_in_memory().unwrap();
    let providers = schema::detect_providers(&conn).unwrap();
    assert!(providers.is_empty());
}

#[test]
fn graph_info_table_function_returns_provider_counts() {
    let temp_dir = TempDir::new().unwrap();
    let fixture_path = temp_dir.path().join("fixture.duckdb").to_string_lossy().into_owned();

    let fixture_conn = Connection::open(&fixture_path).unwrap();
    make_fixture(&fixture_conn);
    drop(fixture_conn);

    let conn = Connection::open_in_memory().unwrap();
    register_graph_functions(&conn);

    let mut stmt = conn
        .prepare("SELECT provider, nodes, edges FROM graph_info(?) ORDER BY provider")
        .unwrap();
    let mut rows = stmt.query([fixture_path.as_str()]).unwrap();

    let row = rows.next().unwrap().unwrap();
    let provider: String = row.get(0).unwrap();
    let nodes: i64 = row.get(1).unwrap();
    let edges: i64 = row.get(2).unwrap();

    assert_eq!(provider, "aws");
    assert_eq!(nodes, 3);
    assert_eq!(edges, 2);
    assert!(rows.next().unwrap().is_none());
}

#[test]
fn graph_info_table_function_handles_multiple_providers() {
    let temp_dir = TempDir::new().unwrap();
    let fixture_path = temp_dir.path().join("fixture.duckdb").to_string_lossy().into_owned();

    let fixture_conn = Connection::open(&fixture_path).unwrap();
    fixture_conn
        .execute_batch(
            "CREATE TABLE aws_resources (
                id VARCHAR, type VARCHAR, name VARCHAR, region VARCHAR,
                account_id VARCHAR, arn VARCHAR, tags VARCHAR
            );
            CREATE TABLE aws_relationships (
                from_id VARCHAR, to_id VARCHAR,
                relationship_type VARCHAR, properties VARCHAR
            );
            CREATE TABLE azure_resources (
                id VARCHAR, type VARCHAR, name VARCHAR, region VARCHAR,
                account_id VARCHAR, arn VARCHAR, tags VARCHAR
            );
            CREATE TABLE azure_relationships (
                from_id VARCHAR, to_id VARCHAR,
                relationship_type VARCHAR, properties VARCHAR
            );
            INSERT INTO aws_resources VALUES
                ('aws-r1','ec2','i-1','us-east-1','123',NULL,NULL),
                ('aws-r2','s3','b-1','us-east-1','123',NULL,NULL);
            INSERT INTO aws_relationships VALUES
                ('aws-r1','aws-r2','reads',NULL);
            INSERT INTO azure_resources VALUES
                ('az-r1','vm','vm-1','eastus','sub-1',NULL,NULL);
            INSERT INTO azure_relationships VALUES
                ('az-r1','az-r1','self',NULL);",
        )
        .unwrap();
    drop(fixture_conn);

    let conn = Connection::open_in_memory().unwrap();
    register_graph_functions(&conn);

    let mut stmt = conn
        .prepare("SELECT provider, nodes, edges FROM graph_info(?) ORDER BY provider")
        .unwrap();
    let mut rows = stmt.query([fixture_path.as_str()]).unwrap();

    let aws = rows.next().unwrap().unwrap();
    assert_eq!(aws.get::<_, String>(0).unwrap(), "aws");
    assert_eq!(aws.get::<_, i64>(1).unwrap(), 2);
    assert_eq!(aws.get::<_, i64>(2).unwrap(), 1);

    let azure = rows.next().unwrap().unwrap();
    assert_eq!(azure.get::<_, String>(0).unwrap(), "azure");
    assert_eq!(azure.get::<_, i64>(1).unwrap(), 1);
    assert_eq!(azure.get::<_, i64>(2).unwrap(), 1);

    assert!(rows.next().unwrap().is_none());
}

#[test]
fn graph_traverse_returns_paths_and_hops() {
    let temp_dir = TempDir::new().unwrap();
    let fixture_path = temp_dir.path().join("fixture.duckdb").to_string_lossy().into_owned();

    let fixture_conn = Connection::open(&fixture_path).unwrap();
    make_fixture(&fixture_conn);
    drop(fixture_conn);

    let conn = Connection::open_in_memory().unwrap();
    register_graph_functions(&conn);

    let mut stmt = conn
        .prepare("SELECT node_id, hop_count, CAST(path_ids AS VARCHAR), CAST(relationship_types AS VARCHAR) FROM graph_traverse(?, ?, CAST(? AS INTEGER), ?, ?) ORDER BY hop_count, node_id")
        .unwrap();
    let rows = stmt
        .query_map([fixture_path.as_str(), "r1", "3", "outbound", "NULL"], |row| {
            Ok((
                row.get::<_, String>(0)?,
                row.get::<_, i32>(1)?,
                row.get::<_, String>(2)?,
                row.get::<_, String>(3)?,
            ))
        })
        .unwrap()
        .collect::<Result<Vec<_>, _>>()
        .unwrap();

    assert_eq!(rows.len(), 2);
    assert_eq!(rows[0].0, "r2");
    assert_eq!(rows[0].1, 1);
    assert_eq!(rows[0].2, "[r1, r2]".to_string());
    assert_eq!(rows[0].3, "[peer]".to_string());
    assert_eq!(rows[1].0, "r3");
}

#[test]
fn graph_traverse_filter_type_uses_case_insensitive_contains() {
    let temp_dir = TempDir::new().unwrap();
    let fixture_path = temp_dir.path().join("fixture.duckdb").to_string_lossy().into_owned();

    let fixture_conn = Connection::open(&fixture_path).unwrap();
    fixture_conn
        .execute_batch(
            "CREATE TABLE aws_resources (
                id VARCHAR, type VARCHAR, name VARCHAR, region VARCHAR,
                account_id VARCHAR, arn VARCHAR, tags VARCHAR
            );
            CREATE TABLE aws_relationships (
                from_id VARCHAR, to_id VARCHAR,
                relationship_type VARCHAR, properties VARCHAR
            );
            INSERT INTO aws_resources VALUES
                ('r1','aws::ec2::Instance','i-1','us-east-1','123',NULL,NULL),
                ('r2','aws::ec2::Instance','i-2','us-east-1','123',NULL,NULL),
                ('r3','aws::s3::Bucket','b-1','us-east-1','123',NULL,NULL);
            INSERT INTO aws_relationships VALUES
                ('r1','r2','peer',NULL),
                ('r1','r3','reads',NULL);",
        )
        .unwrap();
    drop(fixture_conn);

    let conn = Connection::open_in_memory().unwrap();
    register_graph_functions(&conn);

    let rows = conn
        .prepare("SELECT node_id FROM graph_traverse(?, ?, CAST(? AS INTEGER), ?, ?) ORDER BY node_id")
        .unwrap()
        .query_map([fixture_path.as_str(), "r1", "2", "outbound", "instance"], |row| {
            row.get::<_, String>(0)
        })
        .unwrap()
        .collect::<Result<Vec<_>, _>>()
        .unwrap();

    assert_eq!(rows, vec!["r2".to_string()]);
}

#[test]
fn graph_traverse_accepts_actual_sql_null_filter() {
    let temp_dir = TempDir::new().unwrap();
    let fixture_path = temp_dir.path().join("fixture.duckdb").to_string_lossy().into_owned();

    let fixture_conn = Connection::open(&fixture_path).unwrap();
    make_fixture(&fixture_conn);
    drop(fixture_conn);

    let conn = Connection::open_in_memory().unwrap();
    register_graph_functions(&conn);

    let rows = conn
        .prepare("SELECT node_id FROM graph_traverse(?, ?, CAST(? AS INTEGER), ?, NULL) ORDER BY node_id")
        .unwrap()
        .query_map([fixture_path.as_str(), "r1", "3", "outbound"], |row| {
            row.get::<_, String>(0)
        })
        .unwrap()
        .collect::<Result<Vec<_>, _>>()
        .unwrap();

    assert_eq!(rows, vec!["r2".to_string(), "r3".to_string()]);
}

#[test]
fn graph_traverse_both_direction_deduplicates_bidirectional_neighbors() {
    let temp_dir = TempDir::new().unwrap();
    let fixture_path = temp_dir.path().join("fixture.duckdb").to_string_lossy().into_owned();

    let fixture_conn = Connection::open(&fixture_path).unwrap();
    fixture_conn
        .execute_batch(
            "CREATE TABLE aws_resources (
                id VARCHAR, type VARCHAR, name VARCHAR, region VARCHAR,
                account_id VARCHAR, arn VARCHAR, tags VARCHAR
            );
            CREATE TABLE aws_relationships (
                from_id VARCHAR, to_id VARCHAR,
                relationship_type VARCHAR, properties VARCHAR
            );
            INSERT INTO aws_resources VALUES
                ('a','ec2','a','us-east-1','123',NULL,NULL),
                ('b','ec2','b','us-east-1','123',NULL,NULL);
            INSERT INTO aws_relationships VALUES
                ('a','b','outbound_rel',NULL),
                ('b','a','inbound_rel',NULL);",
        )
        .unwrap();
    drop(fixture_conn);

    let conn = Connection::open_in_memory().unwrap();
    register_graph_functions(&conn);

    let rows = conn
        .prepare("SELECT node_id, CAST(relationship_types AS VARCHAR) FROM graph_traverse(?, ?, CAST(? AS INTEGER), ?, ?) ORDER BY node_id")
        .unwrap()
        .query_map([fixture_path.as_str(), "a", "1", "both", "NULL"], |row| {
            Ok((row.get::<_, String>(0)?, row.get::<_, String>(1)?))
        })
        .unwrap()
        .collect::<Result<Vec<_>, _>>()
        .unwrap();

    assert_eq!(rows, vec![("b".to_string(), "[outbound_rel]".to_string())]);
}

#[test]
fn graph_shortest_path_returns_ordered_route() {
    let temp_dir = TempDir::new().unwrap();
    let fixture_path = temp_dir.path().join("fixture.duckdb").to_string_lossy().into_owned();

    let fixture_conn = Connection::open(&fixture_path).unwrap();
    make_fixture(&fixture_conn);
    drop(fixture_conn);

    let conn = Connection::open_in_memory().unwrap();
    register_graph_functions(&conn);

    let rows = conn
        .prepare("SELECT hop_number, resource_id, incoming_rel_type, total_hops FROM graph_shortest_path(?, ?, ?, CAST(? AS BOOLEAN)) ORDER BY hop_number")
        .unwrap()
        .query_map([fixture_path.as_str(), "r1", "r3", "false"], |row| {
            Ok((
                row.get::<_, i32>(0)?,
                row.get::<_, String>(1)?,
                row.get::<_, Option<String>>(2)?,
                row.get::<_, i32>(3)?,
            ))
        })
        .unwrap()
        .collect::<Result<Vec<_>, _>>()
        .unwrap();

    assert_eq!(rows.len(), 2);
    assert_eq!(rows[0], (0, "r1".to_string(), None, 1));
    assert_eq!(rows[1], (1, "r3".to_string(), Some("reads".to_string()), 1));
}

#[test]
fn graph_shortest_path_returns_empty_when_source_missing() {
    let temp_dir = TempDir::new().unwrap();
    let fixture_path = temp_dir.path().join("fixture.duckdb").to_string_lossy().into_owned();

    let fixture_conn = Connection::open(&fixture_path).unwrap();
    make_fixture(&fixture_conn);
    drop(fixture_conn);

    let conn = Connection::open_in_memory().unwrap();
    register_graph_functions(&conn);

    let count = conn
        .query_row(
            "SELECT COUNT(*) FROM graph_shortest_path(?, ?, ?, CAST(? AS BOOLEAN))",
            [fixture_path.as_str(), "missing", "r3", "false"],
            |row| row.get::<_, i64>(0),
        )
        .unwrap();

    assert_eq!(count, 0);
}

#[test]
fn graph_shortest_path_returns_empty_when_target_missing() {
    let temp_dir = TempDir::new().unwrap();
    let fixture_path = temp_dir.path().join("fixture.duckdb").to_string_lossy().into_owned();

    let fixture_conn = Connection::open(&fixture_path).unwrap();
    make_fixture(&fixture_conn);
    drop(fixture_conn);

    let conn = Connection::open_in_memory().unwrap();
    register_graph_functions(&conn);

    let count = conn
        .query_row(
            "SELECT COUNT(*) FROM graph_shortest_path(?, ?, ?, CAST(? AS BOOLEAN))",
            [fixture_path.as_str(), "r1", "missing", "false"],
            |row| row.get::<_, i64>(0),
        )
        .unwrap();

    assert_eq!(count, 0);
}

#[test]
fn graph_shortest_path_returns_empty_when_no_path_exists() {
    let temp_dir = TempDir::new().unwrap();
    let fixture_path = temp_dir.path().join("fixture.duckdb").to_string_lossy().into_owned();

    let fixture_conn = Connection::open(&fixture_path).unwrap();
    make_shortest_path_edge_fixture(&fixture_conn);
    drop(fixture_conn);

    let conn = Connection::open_in_memory().unwrap();
    register_graph_functions(&conn);

    let count = conn
        .query_row(
            "SELECT COUNT(*) FROM graph_shortest_path(?, ?, ?, CAST(? AS BOOLEAN))",
            [fixture_path.as_str(), "a", "isolated", "false"],
            |row| row.get::<_, i64>(0),
        )
        .unwrap();

    assert_eq!(count, 0);
}

#[test]
fn graph_shortest_path_returns_single_row_for_self_path() {
    let temp_dir = TempDir::new().unwrap();
    let fixture_path = temp_dir.path().join("fixture.duckdb").to_string_lossy().into_owned();

    let fixture_conn = Connection::open(&fixture_path).unwrap();
    make_shortest_path_edge_fixture(&fixture_conn);
    drop(fixture_conn);

    let conn = Connection::open_in_memory().unwrap();
    register_graph_functions(&conn);

    let rows = conn
        .prepare("SELECT hop_number, resource_id, incoming_rel_type, total_hops FROM graph_shortest_path(?, ?, ?, CAST(? AS BOOLEAN))")
        .unwrap()
        .query_map([fixture_path.as_str(), "a", "a", "false"], |row| {
            Ok((
                row.get::<_, i32>(0)?,
                row.get::<_, String>(1)?,
                row.get::<_, Option<String>>(2)?,
                row.get::<_, i32>(3)?,
            ))
        })
        .unwrap()
        .collect::<Result<Vec<_>, _>>()
        .unwrap();

    assert_eq!(rows, vec![(0, "a".to_string(), None, 0)]);
}

#[test]
fn graph_reachable_reports_matches() {
    let temp_dir = TempDir::new().unwrap();
    let fixture_path = temp_dir.path().join("fixture.duckdb").to_string_lossy().into_owned();

    let fixture_conn = Connection::open(&fixture_path).unwrap();
    make_fixture(&fixture_conn);
    drop(fixture_conn);

    let conn = Connection::open_in_memory().unwrap();
    register_graph_functions(&conn);

    let result = conn
        .query_row(
            "SELECT is_reachable, match_count, closest_hop, example_id FROM graph_reachable(?, ?, ?, CAST(? AS INTEGER))",
            [fixture_path.as_str(), "r1", "s3", "3"],
            |row| {
                Ok((
                    row.get::<_, bool>(0)?,
                    row.get::<_, i64>(1)?,
                    row.get::<_, i32>(2)?,
                    row.get::<_, String>(3)?,
                ))
            },
        )
        .unwrap();

    assert_eq!(result, (true, 1, 1, "r3".to_string()));
}

#[test]
fn graph_blast_radius_aggregates_by_type() {
    let temp_dir = TempDir::new().unwrap();
    let fixture_path = temp_dir.path().join("fixture.duckdb").to_string_lossy().into_owned();

    let fixture_conn = Connection::open(&fixture_path).unwrap();
    make_fixture(&fixture_conn);
    drop(fixture_conn);

    let conn = Connection::open_in_memory().unwrap();
    register_graph_functions(&conn);

    let rows = conn
        .prepare("SELECT resource_type, reachable_count, max_hop_distance, CAST(sample_ids AS VARCHAR) FROM graph_blast_radius(?, ?, CAST(? AS INTEGER)) ORDER BY resource_type")
        .unwrap()
        .query_map([fixture_path.as_str(), "r1", "3"], |row| {
            Ok((
                row.get::<_, String>(0)?,
                row.get::<_, i32>(1)?,
                row.get::<_, i32>(2)?,
                row.get::<_, String>(3)?,
            ))
        })
        .unwrap()
        .collect::<Result<Vec<_>, _>>()
        .unwrap();

    assert_eq!(rows.len(), 2);
    assert_eq!(rows[0], ("ec2".to_string(), 1, 1, "[r2]".to_string()));
    assert_eq!(rows[1], ("s3".to_string(), 1, 1, "[r3]".to_string()));
}

#[test]
fn graph_match_pattern_matches_builtin_pattern() {
    let temp_dir = TempDir::new().unwrap();
    let fixture_path = temp_dir.path().join("fixture.duckdb").to_string_lossy().into_owned();

    let fixture_conn = Connection::open(&fixture_path).unwrap();
    make_fixture(&fixture_conn);
    drop(fixture_conn);

    let conn = Connection::open_in_memory().unwrap();
    register_graph_functions(&conn);

    let rows = conn
        .prepare("SELECT match_id, pattern_node, resource_id FROM graph_match_pattern(?, ?) ORDER BY match_id, pattern_node")
        .unwrap()
        .query_map([fixture_path.as_str(), "public_s3_via_instance"], |row| {
            Ok((
                row.get::<_, i32>(0)?,
                row.get::<_, String>(1)?,
                row.get::<_, String>(2)?,
            ))
        })
        .unwrap()
        .collect::<Result<Vec<_>, _>>()
        .unwrap();

    assert_eq!(
        rows,
        vec![
            (0, "bucket".to_string(), "r3".to_string()),
            (0, "instance".to_string(), "r1".to_string()),
        ]
    );
}

#[test]
fn graph_match_pattern_matches_inline_json_pattern() {
    let temp_dir = TempDir::new().unwrap();
    let fixture_path = temp_dir.path().join("fixture.duckdb").to_string_lossy().into_owned();

    let fixture_conn = Connection::open(&fixture_path).unwrap();
    make_fixture(&fixture_conn);
    drop(fixture_conn);

    let conn = Connection::open_in_memory().unwrap();
    register_graph_functions(&conn);

    let pattern_json = r#"{
        "name": "peer_link",
        "description": "EC2 peer relationship",
        "nodes": [
            { "label": "left", "type_filter": "ec2" },
            { "label": "right", "type_filter": "ec2" }
        ],
        "edges": [
            { "from": "left", "to": "right", "rel_filter": "peer" }
        ]
    }"#;

    let rows = conn
        .prepare("SELECT match_id, pattern_node, resource_id FROM graph_match_pattern(?, ?) ORDER BY match_id, pattern_node")
        .unwrap()
        .query_map([fixture_path.as_str(), pattern_json], |row| {
            Ok((
                row.get::<_, i32>(0)?,
                row.get::<_, String>(1)?,
                row.get::<_, String>(2)?,
            ))
        })
        .unwrap()
        .collect::<Result<Vec<_>, _>>()
        .unwrap();

    assert_eq!(
        rows,
        vec![
            (0, "left".to_string(), "r1".to_string()),
            (0, "right".to_string(), "r2".to_string()),
        ]
    );
}

#[test]
fn graph_match_pattern_matches_multihop_builtin_patterns() {
    let temp_dir = TempDir::new().unwrap();
    let fixture_path = temp_dir.path().join("fixture.duckdb").to_string_lossy().into_owned();

    let fixture_conn = Connection::open(&fixture_path).unwrap();
    make_pattern_fixture(&fixture_conn);
    drop(fixture_conn);

    let conn = Connection::open_in_memory().unwrap();
    register_graph_functions(&conn);

    let expectations = [
        ("internet_to_database", 4_i64),
        ("public_lb_to_private_db", 3_i64),
        ("unencrypted_data_path", 3_i64),
        ("k8s_privileged_to_cloud", 3_i64),
    ];

    for (pattern_name, expected_rows) in expectations {
        let row_count = conn
            .query_row(
                "SELECT COUNT(*) FROM graph_match_pattern(?, ?)",
                [fixture_path.as_str(), pattern_name],
                |row| row.get::<_, i64>(0),
            )
            .unwrap();
        assert_eq!(row_count, expected_rows, "unexpected row count for {pattern_name}");
    }
}

#[test]
fn graph_match_pattern_surfaces_truncation() {
    let temp_dir = TempDir::new().unwrap();
    let fixture_path = temp_dir.path().join("fixture.duckdb").to_string_lossy().into_owned();

    let fixture_conn = Connection::open(&fixture_path).unwrap();
    make_many_public_s3_matches_fixture(&fixture_conn);
    drop(fixture_conn);

    let conn = Connection::open_in_memory().unwrap();
    register_graph_functions(&conn);

    // Truncation is detected by the caller: COUNT(DISTINCT match_id) == MAX_MATCHES.
    let distinct_matches = conn
        .query_row(
            "SELECT COUNT(DISTINCT match_id) FROM graph_match_pattern(?, ?)",
            [fixture_path.as_str(), "public_s3_via_instance"],
            |row| row.get::<_, i64>(0),
        )
        .unwrap();

    assert_eq!(distinct_matches, 256);
}

#[test]
fn graph_list_patterns_returns_builtin_registry() {
    let conn = Connection::open_in_memory().unwrap();
    register_graph_functions(&conn);

    let rows = conn
        .prepare("SELECT pattern_name, node_count, edge_count FROM graph_list_patterns() ORDER BY pattern_name")
        .unwrap()
        .query_map([], |row| {
            Ok((
                row.get::<_, String>(0)?,
                row.get::<_, i32>(1)?,
                row.get::<_, i32>(2)?,
            ))
        })
        .unwrap()
        .collect::<Result<Vec<_>, _>>()
        .unwrap();

    assert_eq!(
        rows,
        vec![
            ("cross_account_trust".to_string(), 2, 1),
            ("internet_to_database".to_string(), 4, 3),
            ("k8s_privileged_to_cloud".to_string(), 3, 2),
            ("lateral_movement_risk".to_string(), 2, 1),
            ("overprivileged_lambda".to_string(), 2, 1),
            ("public_lb_to_private_db".to_string(), 3, 2),
            ("public_s3_via_instance".to_string(), 2, 1),
            ("unencrypted_data_path".to_string(), 3, 2),
        ]
    );
}

#[test]
fn graph_cache_invalidate_table_function_returns_success() {
    let temp_dir = TempDir::new().unwrap();
    let fixture_path = temp_dir.path().join("fixture.duckdb").to_string_lossy().into_owned();

    let fixture_conn = Connection::open(&fixture_path).unwrap();
    make_fixture(&fixture_conn);
    drop(fixture_conn);

    let conn = Connection::open_in_memory().unwrap();
    register_graph_functions(&conn);

    let before = conn
        .query_row("SELECT nodes FROM graph_info(?)", [fixture_path.as_str()], |row| row.get::<_, i64>(0))
        .unwrap();
    let invalidated = conn
        .query_row(
            "SELECT invalidated FROM graph_cache_invalidate(?)",
            [fixture_path.as_str()],
            |row| row.get::<_, i64>(0),
        )
        .unwrap();
    let after = conn
        .query_row("SELECT nodes FROM graph_info(?)", [fixture_path.as_str()], |row| row.get::<_, i64>(0))
        .unwrap();

    assert_eq!(before, 3);
    assert_eq!(invalidated, 1);
    assert_eq!(after, 3);
}

#[test]
fn cloud_path_macro_wraps_shortest_path() {
    let temp_dir = TempDir::new().unwrap();
    let fixture_path = temp_dir.path().join("fixture.duckdb").to_string_lossy().into_owned();

    let fixture_conn = Connection::open(&fixture_path).unwrap();
    make_fixture(&fixture_conn);
    drop(fixture_conn);

    let conn = Connection::open_in_memory().unwrap();
    register_graph_functions(&conn);

    let rows = conn
        .prepare("SELECT hop_number, resource_id FROM cloud_path(?, ?, ?) ORDER BY hop_number")
        .unwrap()
        .query_map([fixture_path.as_str(), "r1", "r3"], |row| {
            Ok((row.get::<_, i32>(0)?, row.get::<_, String>(1)?))
        })
        .unwrap()
        .collect::<Result<Vec<_>, _>>()
        .unwrap();

    assert_eq!(rows, vec![(0, "r1".to_string()), (1, "r3".to_string())]);
}

#[test]
fn blast_radius_macro_wraps_blast_radius_function() {
    let temp_dir = TempDir::new().unwrap();
    let fixture_path = temp_dir.path().join("fixture.duckdb").to_string_lossy().into_owned();

    let fixture_conn = Connection::open(&fixture_path).unwrap();
    make_fixture(&fixture_conn);
    drop(fixture_conn);

    let conn = Connection::open_in_memory().unwrap();
    register_graph_functions(&conn);

    let rows = conn
        .prepare("SELECT resource_type, reachable_count FROM blast_radius(?, ?, 3) ORDER BY resource_type")
        .unwrap()
        .query_map([fixture_path.as_str(), "r1"], |row| {
            Ok((row.get::<_, String>(0)?, row.get::<_, i32>(1)?))
        })
        .unwrap()
        .collect::<Result<Vec<_>, _>>()
        .unwrap();

    assert_eq!(rows, vec![("ec2".to_string(), 1), ("s3".to_string(), 1)]);
}

#[test]
fn attack_patterns_macro_wraps_pattern_listing() {
    let conn = Connection::open_in_memory().unwrap();
    register_graph_functions(&conn);

    let names = conn
        .prepare("SELECT pattern_name FROM attack_patterns() ORDER BY pattern_name")
        .unwrap()
        .query_map([], |row| row.get::<_, String>(0))
        .unwrap()
        .collect::<Result<Vec<_>, _>>()
        .unwrap();

    assert_eq!(
        names,
        vec![
            "cross_account_trust".to_string(),
            "internet_to_database".to_string(),
            "k8s_privileged_to_cloud".to_string(),
            "lateral_movement_risk".to_string(),
            "overprivileged_lambda".to_string(),
            "public_lb_to_private_db".to_string(),
            "public_s3_via_instance".to_string(),
            "unencrypted_data_path".to_string(),
        ]
    );
}

#[test]
fn load_graph_skips_relationships_with_missing_nodes() {
    let conn = Connection::open_in_memory().unwrap();
    make_fixture(&conn);
    conn.execute(
        "INSERT INTO aws_relationships VALUES ('missing','r1','broken',NULL)",
        [],
    )
    .unwrap();

    let providers = schema::detect_providers(&conn).unwrap();
    let loaded = loader::load_graph(&conn, &providers).unwrap();

    assert_eq!(loaded.node_count, 3);
    assert_eq!(loaded.edge_count, 2);
}

#[test]
fn detect_providers_finds_multiple_clouds() {
    let conn = Connection::open_in_memory().unwrap();
    conn.execute_batch(
        "CREATE TABLE aws_resources (
            id VARCHAR, type VARCHAR, name VARCHAR, region VARCHAR,
            account_id VARCHAR, arn VARCHAR, tags VARCHAR
        );
        CREATE TABLE aws_relationships (
            from_id VARCHAR, to_id VARCHAR,
            relationship_type VARCHAR, properties VARCHAR
        );
        CREATE TABLE azure_resources (
            id VARCHAR, type VARCHAR, name VARCHAR, region VARCHAR,
            account_id VARCHAR, arn VARCHAR, tags VARCHAR
        );
        CREATE TABLE azure_relationships (
            from_id VARCHAR, to_id VARCHAR,
            relationship_type VARCHAR, properties VARCHAR
        );",
    )
    .unwrap();

    let providers = schema::detect_providers(&conn).unwrap();
    assert_eq!(providers.len(), 2);
    assert_eq!(providers[0].provider, "aws");
    assert_eq!(providers[1].provider, "azure");
}

// --- PR4: chunking, mtime auto-invalidation, convention-based detection ---

/// Builds a star graph with one source and `leaves` leaves, all `reads`
/// relationships from the source. Produces row counts that span multiple
/// DuckDB output chunks (vector size ~2048).
fn make_star_fixture(conn: &Connection, leaves: usize) {
    conn.execute_batch(
        "CREATE TABLE aws_resources (
            id VARCHAR, type VARCHAR, name VARCHAR, region VARCHAR,
            account_id VARCHAR, arn VARCHAR, tags VARCHAR
        );
        CREATE TABLE aws_relationships (
            from_id VARCHAR, to_id VARCHAR,
            relationship_type VARCHAR, properties VARCHAR
        );
        INSERT INTO aws_resources VALUES
            ('hub','ec2','hub','us-east-1','123',NULL,NULL);",
    )
    .unwrap();

    let mut appender_res = conn.appender("aws_resources").unwrap();
    for i in 0..leaves {
        appender_res
            .append_row(duckdb::params![
                format!("leaf{i}"),
                "s3",
                format!("leaf-{i}"),
                "us-east-1",
                "123",
                Option::<String>::None,
                Option::<String>::None,
            ])
            .unwrap();
    }
    appender_res.flush().unwrap();
    drop(appender_res);

    let mut appender_rel = conn.appender("aws_relationships").unwrap();
    for i in 0..leaves {
        appender_rel
            .append_row(duckdb::params![
                "hub",
                format!("leaf{i}"),
                "reads",
                Option::<String>::None,
            ])
            .unwrap();
    }
    appender_rel.flush().unwrap();
}

#[test]
fn graph_traverse_returns_all_rows_across_multiple_chunks() {
    let temp_dir = TempDir::new().unwrap();
    let fixture_path = temp_dir.path().join("fixture.duckdb").to_string_lossy().into_owned();

    let fixture_conn = Connection::open(&fixture_path).unwrap();
    make_star_fixture(&fixture_conn, 5000);
    drop(fixture_conn);

    let conn = Connection::open_in_memory().unwrap();
    register_graph_functions(&conn);

    let row_count: i64 = conn
        .query_row(
            "SELECT COUNT(*) FROM graph_traverse(?, ?, 1, 'outbound', 'NULL')",
            [fixture_path.as_str(), "hub"],
            |row| row.get(0),
        )
        .unwrap();
    assert_eq!(row_count, 5000, "traverse must emit every leaf across chunks");

    // Sanity check that the chunked output still produces distinct rows (i.e.
    // we're not just emitting the same first 2048 over and over).
    let distinct: i64 = conn
        .query_row(
            "SELECT COUNT(DISTINCT node_id) FROM graph_traverse(?, ?, 1, 'outbound', 'NULL')",
            [fixture_path.as_str(), "hub"],
            |row| row.get(0),
        )
        .unwrap();
    assert_eq!(distinct, 5000);
}

#[test]
fn graph_blast_radius_chunks_correctly_with_many_types() {
    let temp_dir = TempDir::new().unwrap();
    let fixture_path = temp_dir.path().join("fixture.duckdb").to_string_lossy().into_owned();

    // 3000 distinct resource types means blast's per-type rollup also crosses
    // the chunk boundary.
    let fixture_conn = Connection::open(&fixture_path).unwrap();
    fixture_conn
        .execute_batch(
            "CREATE TABLE aws_resources (
                id VARCHAR, type VARCHAR, name VARCHAR, region VARCHAR,
                account_id VARCHAR, arn VARCHAR, tags VARCHAR
            );
            CREATE TABLE aws_relationships (
                from_id VARCHAR, to_id VARCHAR,
                relationship_type VARCHAR, properties VARCHAR
            );
            INSERT INTO aws_resources VALUES
                ('hub','ec2','hub','us-east-1','123',NULL,NULL);",
        )
        .unwrap();

    let mut a_res = fixture_conn.appender("aws_resources").unwrap();
    let mut a_rel = fixture_conn.appender("aws_relationships").unwrap();
    for i in 0..3000 {
        a_res
            .append_row(duckdb::params![
                format!("leaf{i}"),
                format!("type{i}"),
                format!("leaf-{i}"),
                "us-east-1",
                "123",
                Option::<String>::None,
                Option::<String>::None,
            ])
            .unwrap();
        a_rel
            .append_row(duckdb::params![
                "hub",
                format!("leaf{i}"),
                "reads",
                Option::<String>::None,
            ])
            .unwrap();
    }
    a_res.flush().unwrap();
    a_rel.flush().unwrap();
    drop(a_res);
    drop(a_rel);
    drop(fixture_conn);

    let conn = Connection::open_in_memory().unwrap();
    register_graph_functions(&conn);

    let row_count: i64 = conn
        .query_row(
            "SELECT COUNT(*) FROM graph_blast_radius(?, ?, 3)",
            [fixture_path.as_str(), "hub"],
            |row| row.get(0),
        )
        .unwrap();
    assert_eq!(row_count, 3000);
}

#[test]
fn graph_reachable_handles_large_match_count() {
    let temp_dir = TempDir::new().unwrap();
    let fixture_path = temp_dir.path().join("fixture.duckdb").to_string_lossy().into_owned();

    let fixture_conn = Connection::open(&fixture_path).unwrap();
    make_star_fixture(&fixture_conn, 5000);
    drop(fixture_conn);

    let conn = Connection::open_in_memory().unwrap();
    register_graph_functions(&conn);

    let (is_reachable, match_count, closest_hop) = conn
        .query_row(
            "SELECT is_reachable, match_count, closest_hop FROM graph_reachable(?, ?, ?, 3)",
            [fixture_path.as_str(), "hub", "s3"],
            |row| Ok((row.get::<_, bool>(0)?, row.get::<_, i64>(1)?, row.get::<_, i32>(2)?)),
        )
        .unwrap();
    assert!(is_reachable);
    assert_eq!(match_count, 5000);
    assert_eq!(closest_hop, 1);
}

#[test]
fn cache_auto_invalidates_when_file_changes_on_disk() {
    let temp_dir = TempDir::new().unwrap();
    let fixture_path = temp_dir.path().join("fixture.duckdb").to_string_lossy().into_owned();

    {
        let conn1 = Connection::open(&fixture_path).unwrap();
        make_fixture(&conn1);
    }

    // Load via a connection that sees the file's tables — the cache uses this
    // conn to detect providers and read rows, and uses `fixture_path` as both
    // the cache key and the fingerprint source.
    let load_conn = Connection::open(&fixture_path).unwrap();
    let g1 = cache::get_or_load(&load_conn, &fixture_path).unwrap();
    assert_eq!(g1.node_count, 3);
    drop(load_conn);

    // Sleep past mtime resolution (1 s on most filesystems), then rewrite the
    // backing file with a different shape. No explicit graph_cache_invalidate.
    std::thread::sleep(std::time::Duration::from_millis(1100));
    std::fs::remove_file(&fixture_path).unwrap();
    {
        let conn2 = Connection::open(&fixture_path).unwrap();
        conn2
            .execute_batch(
                "CREATE TABLE aws_resources (
                    id VARCHAR, type VARCHAR, name VARCHAR, region VARCHAR,
                    account_id VARCHAR, arn VARCHAR, tags VARCHAR
                );
                CREATE TABLE aws_relationships (
                    from_id VARCHAR, to_id VARCHAR,
                    relationship_type VARCHAR, properties VARCHAR
                );
                INSERT INTO aws_resources VALUES
                    ('r1','ec2','i-1','us-east-1','123',NULL,NULL),
                    ('r2','ec2','i-2','us-east-1','123',NULL,NULL),
                    ('r3','s3','b-1','us-east-1','123',NULL,NULL),
                    ('r4','s3','b-2','us-east-1','123',NULL,NULL);
                INSERT INTO aws_relationships VALUES
                    ('r1','r2','peer',NULL),
                    ('r1','r3','reads',NULL);",
            )
            .unwrap();
    }

    let load_conn2 = Connection::open(&fixture_path).unwrap();
    let g2 = cache::get_or_load(&load_conn2, &fixture_path).unwrap();
    assert!(!std::sync::Arc::ptr_eq(&g1, &g2), "expected fresh load after file mtime change");
    assert_eq!(g2.node_count, 4);
}

#[test]
fn detect_providers_auto_picks_up_unknown_prefix() {
    let conn = Connection::open_in_memory().unwrap();
    conn.execute_batch(
        "CREATE TABLE synthetic_resources (
            id VARCHAR, type VARCHAR, name VARCHAR, region VARCHAR,
            account_id VARCHAR, arn VARCHAR, tags VARCHAR
        );
        CREATE TABLE synthetic_relationships (
            from_id VARCHAR, to_id VARCHAR,
            relationship_type VARCHAR, properties VARCHAR
        );",
    )
    .unwrap();

    let providers = schema::detect_providers(&conn).unwrap();
    assert_eq!(providers.len(), 1);
    assert_eq!(providers[0].provider, "synthetic");
    assert_eq!(providers[0].resources_table, "synthetic_resources");
    assert_eq!(providers[0].relationships_table, "synthetic_relationships");
}

#[test]
fn detect_providers_ignores_unpaired_tables() {
    let conn = Connection::open_in_memory().unwrap();
    conn.execute_batch(
        "CREATE TABLE orphan_resources (
            id VARCHAR, type VARCHAR, name VARCHAR, region VARCHAR,
            account_id VARCHAR, arn VARCHAR, tags VARCHAR
        );
        CREATE TABLE solo_relationships (
            from_id VARCHAR, to_id VARCHAR,
            relationship_type VARCHAR, properties VARCHAR
        );",
    )
    .unwrap();

    let providers = schema::detect_providers(&conn).unwrap();
    assert!(providers.is_empty(), "prefixes without both tables must be skipped");
}

#[test]
fn detect_providers_excludes_reserved_cloud_prefix() {
    let conn = Connection::open_in_memory().unwrap();
    conn.execute_batch(
        "CREATE TABLE cloud_resources (
            id VARCHAR, type VARCHAR, name VARCHAR, region VARCHAR,
            account_id VARCHAR, arn VARCHAR, tags VARCHAR
        );
        CREATE TABLE cloud_relationships (
            from_id VARCHAR, to_id VARCHAR,
            relationship_type VARCHAR, properties VARCHAR
        );
        CREATE TABLE aws_resources (
            id VARCHAR, type VARCHAR, name VARCHAR, region VARCHAR,
            account_id VARCHAR, arn VARCHAR, tags VARCHAR
        );
        CREATE TABLE aws_relationships (
            from_id VARCHAR, to_id VARCHAR,
            relationship_type VARCHAR, properties VARCHAR
        );",
    )
    .unwrap();

    let providers = schema::detect_providers(&conn).unwrap();
    assert_eq!(providers.len(), 1);
    assert_eq!(providers[0].provider, "aws");
}
