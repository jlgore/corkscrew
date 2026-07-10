use duckdb::Connection;

mod functions;
mod graph;
mod patterns;
mod sql_macros;
pub use functions::blast::GraphBlastRadiusVTab;
pub use functions::correlate_connectivity::GraphCorrelateConnectivityVTab;
pub use functions::correlate_dns::GraphCorrelateDNSVTab;
pub use functions::correlate_domains::GraphCorrelateDomainsVTab;
pub use functions::correlate_identity::GraphCorrelateIdentityVTab;
pub use functions::correlate_ips::GraphCorrelateIPsVTab;
pub use functions::correlate_load_balancers::GraphCorrelateLoadBalancersVTab;
pub use functions::correlate_networks::GraphCorrelateNetworksVTab;
pub use functions::correlate_policies::GraphCorrelatePoliciesVTab;
pub use functions::correlate_secrets::GraphCorrelateSecretsVTab;
pub use functions::correlate_security::GraphCorrelateSecurityVTab;
pub use functions::info::GraphCacheInvalidateVTab;
pub use functions::info::GraphInfoVTab;
pub use functions::list_patterns::GraphListPatternsVTab;
pub use functions::match_pattern::GraphMatchPatternVTab;
pub use functions::paths::GraphShortestPathVTab;
pub use functions::reachable::GraphReachableVTab;
pub use functions::traverse::GraphTraverseVTab;

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
    con.register_table_function::<GraphCorrelateIPsVTab>("graph_correlate_ips")?;
    con.register_table_function::<GraphCorrelateDNSVTab>("graph_correlate_dns")?;
    con.register_table_function::<GraphCorrelateNetworksVTab>("graph_correlate_networks")?;
    con.register_table_function::<GraphCorrelateLoadBalancersVTab>(
        "graph_correlate_load_balancers",
    )?;
    con.register_table_function::<GraphCorrelateConnectivityVTab>("graph_correlate_connectivity")?;
    con.register_table_function::<GraphCorrelateSecurityVTab>("graph_correlate_security")?;
    con.register_table_function::<GraphCorrelateDomainsVTab>("graph_correlate_domains")?;
    con.register_table_function::<GraphCorrelateIdentityVTab>("graph_correlate_identity")?;
    con.register_table_function::<GraphCorrelatePoliciesVTab>("graph_correlate_policies")?;
    con.register_table_function::<GraphCorrelateSecretsVTab>("graph_correlate_secrets")?;
    sql_macros::register_macros(&con)?;
    // No scalar or invalidation table function registered here; use the
    // Rust API functions in tests or implement a proper scalar when needed.
    Ok(())
}
