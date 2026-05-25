use corkscrew_graph_tests::functions::common::{TraversalDirection, collect_blast_rows, collect_traverse_rows};
use corkscrew_graph_tests::graph::{loader, schema};
use criterion::{BenchmarkId, Criterion, criterion_group, criterion_main};
use duckdb::Connection;

const TRAVERSE_TARGET_TOTAL_NODES: usize = 50_000;
const TRAVERSE_FANOUT: usize = 4;
const BLAST_TARGET_TOTAL_NODES: usize = 10_000;
const BLAST_FANOUT: usize = 3;

fn synthetic_loaded_graph(instance_count: usize, fanout: usize) -> loader::LoadedGraph {
    let conn = Connection::open_in_memory().unwrap();
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

    for index in 0..instance_count {
        conn.execute_batch(&format!(
            "INSERT INTO aws_resources VALUES ('inst{index}','aws::ec2::Instance','inst{index}','us-east-1','123',NULL,NULL);"
        ))
        .unwrap();
    }

    let bucket_count = instance_count.max(1) / fanout.max(1);
    for index in 0..bucket_count {
        conn.execute_batch(&format!(
            "INSERT INTO aws_resources VALUES ('bucket{index}','aws::s3::Bucket','bucket{index}','us-east-1','123',NULL,NULL);"
        ))
        .unwrap();
    }

    for index in 0..instance_count {
        let next = index + 1;
        if next < instance_count {
            conn.execute_batch(&format!(
                "INSERT INTO aws_relationships VALUES ('inst{index}','inst{next}','peer',NULL);"
            ))
            .unwrap();
        }

        for edge in 0..fanout {
            let target = index * fanout + edge;
            if target >= bucket_count {
                break;
            }
            conn.execute_batch(&format!(
                "INSERT INTO aws_relationships VALUES ('inst{index}','bucket{target}','reads',NULL);"
            ))
            .unwrap();
        }
    }

    let providers = schema::detect_providers(&conn).unwrap();
    loader::load_graph(&conn, &providers).unwrap()
}

fn bench_traverse_depth_five(c: &mut Criterion) {
    let instance_count = TRAVERSE_TARGET_TOTAL_NODES * TRAVERSE_FANOUT / (TRAVERSE_FANOUT + 1);
    let loaded = synthetic_loaded_graph(instance_count, TRAVERSE_FANOUT);
    let mut group = c.benchmark_group("traverse_depth_five");
    group.bench_function(BenchmarkId::new("total_nodes", loaded.node_count), |b| {
        b.iter(|| {
            let rows = collect_traverse_rows(&loaded, "inst0", 5, TraversalDirection::Outbound, None);
            criterion::black_box(rows.len())
        });
    });
    group.finish();
}

fn bench_blast_radius_scan(c: &mut Criterion) {
    let instance_count = BLAST_TARGET_TOTAL_NODES * BLAST_FANOUT / (BLAST_FANOUT + 1);
    let loaded = synthetic_loaded_graph(instance_count, BLAST_FANOUT);
    let instance_ids = (0..instance_count).map(|index| format!("inst{index}")).collect::<Vec<_>>();

    let mut group = c.benchmark_group("blast_radius_scan");
    group.bench_function(BenchmarkId::new("instances_scanned", instance_ids.len()), |b| {
        b.iter(|| {
            let total = instance_ids
                .iter()
                .map(|id| collect_blast_rows(&loaded, id, 3).len())
                .sum::<usize>();
            criterion::black_box(total)
        });
    });
    group.finish();
}

criterion_group!(benches, bench_traverse_depth_five, bench_blast_radius_scan);
criterion_main!(benches);
