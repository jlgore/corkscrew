// Schema detection for corkscrew provider tables.
use anyhow::Result;
use duckdb::Connection;
use std::collections::{BTreeMap, HashSet};

#[derive(Debug, Clone)]
pub struct ProviderTables {
    pub provider: String,
    pub resources_table: String,
    pub relationships_table: String,
}

/// Prefixes that follow `*_resources` / `*_relationships` naming but are not
/// provider scan output. `cloud` is the unified cross-provider view; keep that
/// invisible to the graph layer so we don't double-count.
const RESERVED_PREFIXES: &[&str] = &["cloud"];

/// Detect provider tables by convention: any prefix `P` that has both
/// `{P}_resources` and `{P}_relationships` tables in the connected DuckDB
/// instance is treated as a provider. The previous hardcoded list (aws, azure,
/// gcp, kubernetes) meant new providers added on the Go side were silently
/// invisible until this file was edited.
pub fn detect_providers(conn: &Connection) -> Result<Vec<ProviderTables>> {
    let sql = "SELECT table_name FROM information_schema.tables";
    let mut stmt = conn.prepare(sql)?;
    let mut rows = stmt.query([])?;

    // For each prefix, track which of (resources, relationships) we've seen.
    let mut buckets: BTreeMap<String, (bool, bool)> = BTreeMap::new();
    let mut reserved: HashSet<&str> = RESERVED_PREFIXES.iter().copied().collect();
    // Also reserve any other infra/metadata table prefixes we discover.
    let _ = &mut reserved; // currently only the const set; reserved for future.

    while let Some(row) = rows.next()? {
        let name: String = row.get(0)?;
        let name = name.to_lowercase();
        if let Some(prefix) = name.strip_suffix("_resources") {
            if !reserved.contains(prefix) {
                buckets
                    .entry(prefix.to_string())
                    .or_insert((false, false))
                    .0 = true;
            }
        } else if let Some(prefix) = name.strip_suffix("_relationships") {
            if !reserved.contains(prefix) {
                buckets
                    .entry(prefix.to_string())
                    .or_insert((false, false))
                    .1 = true;
            }
        }
    }

    Ok(buckets
        .into_iter()
        .filter_map(|(prefix, (has_res, has_rel))| {
            (has_res && has_rel).then(|| ProviderTables {
                resources_table: format!("{prefix}_resources"),
                relationships_table: format!("{prefix}_relationships"),
                provider: prefix,
            })
        })
        .collect())
}
