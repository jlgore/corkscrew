use duckdb::Connection;

mod graph;
mod functions;
mod patterns;
mod sql_macros;
pub use functions::info::GraphInfoVTab;
pub use functions::blast::GraphBlastRadiusVTab;
pub use functions::match_pattern::GraphMatchPatternVTab;
pub use functions::list_patterns::GraphListPatternsVTab;
pub use functions::paths::GraphShortestPathVTab;
pub use functions::reachable::GraphReachableVTab;
pub use functions::traverse::GraphTraverseVTab;
pub use functions::info::GraphCacheInvalidateVTab;

// Register the table function when the extension entrypoint is invoked via the duckdb macro
#[duckdb::duckdb_entrypoint_c_api(ext_name = "corkscrew_graph", min_duckdb_version = "v1.5.3")]
pub fn extension_entrypoint(con: Connection) -> Result<(), Box<dyn std::error::Error>> {
    // Register GraphInfoVTab with the connection. The function takes a single
    // VARCHAR parameter: the path to the DuckDB database to inspect.
    con.register_table_function::<GraphInfoVTab>("graph_info")?;
    con.register_table_function::<GraphTraverseVTab>("graph_traverse")?;
    con.register_table_function::<GraphShortestPathVTab>("graph_shortest_path")?;
    con.register_table_function::<GraphBlastRadiusVTab>("graph_blast_radius")?;
    con.register_table_function::<GraphReachableVTab>("graph_reachable")?;
    con.register_table_function::<GraphMatchPatternVTab>("graph_match_pattern")?;
    con.register_table_function::<GraphListPatternsVTab>("graph_list_patterns")?;
    con.register_table_function::<GraphCacheInvalidateVTab>("graph_cache_invalidate")?;
    sql_macros::register_macros(&con)?;
    // No scalar or invalidation table function registered here; use the
    // Rust API functions in tests or implement a proper scalar when needed.
    Ok(())
}
