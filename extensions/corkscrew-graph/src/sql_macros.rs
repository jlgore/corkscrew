use duckdb::Connection;

pub fn register_macros(conn: &Connection) -> Result<(), Box<dyn std::error::Error>> {
    conn.execute_batch(
        "CREATE OR REPLACE MACRO cloud_path(db_path, from_id, to_id) AS TABLE
            SELECT * FROM graph_shortest_path(db_path, from_id, to_id, false);

        CREATE OR REPLACE MACRO blast_radius(db_path, resource_id, hops := 8) AS TABLE
            SELECT * FROM graph_blast_radius(db_path, resource_id, hops);

        CREATE OR REPLACE MACRO attack_patterns() AS TABLE
            SELECT pattern_name, description, node_count, edge_count
            FROM graph_list_patterns();",
    )?;
    Ok(())
}
