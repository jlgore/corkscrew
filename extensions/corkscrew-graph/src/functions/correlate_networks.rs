use duckdb::core::{DataChunkHandle, LogicalTypeHandle, LogicalTypeId};
use duckdb::vtab::{BindInfo, InitInfo, TableFunctionInfo, VTab};
use duckdb::{Connection, Result};
use serde_json::json;
use std::collections::HashSet;
use std::sync::atomic::AtomicUsize;

use crate::functions::correlate_common::{
    add_standard_columns, emit_rows, ordered_values, sort_rows, table_exists, CorrelationRow,
};
use crate::functions::params::{bind_data, guard, param_f64, param_varchar, BoxError};

#[repr(C)]
pub struct CorrelateNetworksBindData {
    pub db_path: String,
    pub min_confidence: f64,
}

pub struct CorrelateNetworksInitData {
    cursor: AtomicUsize,
    rows: Vec<CorrelationRow>,
}
pub struct GraphCorrelateNetworksVTab;

impl VTab for GraphCorrelateNetworksVTab {
    type InitData = CorrelateNetworksInitData;
    type BindData = CorrelateNetworksBindData;

    fn bind(bind: &BindInfo) -> Result<Self::BindData, BoxError> {
        guard("GraphCorrelateNetworksVTab::bind", || {
            add_standard_columns(bind);
            Ok(CorrelateNetworksBindData {
                db_path: param_varchar(bind, 0, "db_path")?,
                min_confidence: param_f64(bind, 1, "min_confidence")?,
            })
        })
    }

    fn init(init: &InitInfo) -> Result<Self::InitData, BoxError> {
        guard("GraphCorrelateNetworksVTab::init", || {
            let bind_ref = bind_data::<Self>(init)?;
            let conn = Connection::open(&bind_ref.db_path)?;
            let rows = collect_network_correlations(&conn, bind_ref.min_confidence)?;
            Ok(CorrelateNetworksInitData {
                cursor: AtomicUsize::new(0),
                rows,
            })
        })
    }

    fn func(func: &TableFunctionInfo<Self>, output: &mut DataChunkHandle) -> Result<(), BoxError> {
        guard("GraphCorrelateNetworksVTab::func", || {
            emit_rows(
                &func.get_init_data().cursor,
                &func.get_init_data().rows,
                output,
            )
        })
    }

    fn parameters() -> Option<Vec<LogicalTypeHandle>> {
        Some(vec![
            LogicalTypeHandle::from(LogicalTypeId::Varchar),
            LogicalTypeHandle::from(LogicalTypeId::Double),
        ])
    }
}

fn collect_network_correlations(
    conn: &Connection,
    min_confidence: f64,
) -> Result<Vec<CorrelationRow>, BoxError> {
    if !table_exists(conn, "cross_cloud_network_topology")? {
        return Ok(Vec::new());
    }

    let threshold = min_confidence.clamp(0.0, 1.0);
    let mut stmt = conn.prepare(
        "SELECT id, connection_type, connection_id, connection_name,
                source_network_id, source_network_name, source_provider, source_region, source_account_id, source_cidr_blocks,
                target_network_id, target_network_name, target_provider, target_region, target_account_id, target_cidr_blocks,
                status, bandwidth, encryption, redundancy, source_gateway_id, target_gateway_id, metadata
         FROM cross_cloud_network_topology
         WHERE source_network_id IS NOT NULL AND target_network_id IS NOT NULL
           AND source_provider IS NOT NULL AND target_provider IS NOT NULL
           AND source_provider <> target_provider
         ORDER BY source_provider, target_provider, source_network_id, target_network_id, id",
    )?;
    let mut rows = stmt.query([])?;
    let mut out = Vec::new();
    let mut seen = HashSet::new();
    while let Some(row) = rows.next()? {
        let id: String = row.get::<_, Option<String>>(0)?.unwrap_or_default();
        let source_id = row.get::<_, String>(4)?;
        let target_id = row.get::<_, String>(10)?;
        let source_provider = row.get::<_, String>(6)?;
        let target_provider = row.get::<_, String>(12)?;
        let (source_id, target_id, source_provider, target_provider, swapped) =
            ordered_values(source_id, target_id, source_provider, target_provider);
        let connection_type = row.get::<_, Option<String>>(1)?.unwrap_or_default();
        let status = row.get::<_, Option<String>>(16)?.unwrap_or_default();
        let confidence = network_confidence(
            &status,
            row.get::<_, Option<String>>(9)?.as_deref(),
            row.get::<_, Option<String>>(15)?.as_deref(),
        );
        if confidence < threshold {
            continue;
        }
        let correlation_id = format!("network_topology:{}:{}:{}", id, source_id, target_id);
        if !seen.insert(correlation_id.clone()) {
            continue;
        }
        let evidence = json!({
            "topology_id": id,
            "connection_type": connection_type,
            "connection_id": row.get::<_, Option<String>>(2)?.unwrap_or_default(),
            "connection_name": row.get::<_, Option<String>>(3)?.unwrap_or_default(),
            "status": status,
            "bandwidth": row.get::<_, Option<String>>(17)?.unwrap_or_default(),
            "encryption": row.get::<_, Option<bool>>(18)?.unwrap_or(false),
            "redundancy": row.get::<_, Option<String>>(19)?.unwrap_or_default(),
            "source_network_name": if swapped { row.get::<_, Option<String>>(11)?.unwrap_or_default() } else { row.get::<_, Option<String>>(5)?.unwrap_or_default() },
            "target_network_name": if swapped { row.get::<_, Option<String>>(5)?.unwrap_or_default() } else { row.get::<_, Option<String>>(11)?.unwrap_or_default() },
            "source_region": if swapped { row.get::<_, Option<String>>(13)?.unwrap_or_default() } else { row.get::<_, Option<String>>(7)?.unwrap_or_default() },
            "target_region": if swapped { row.get::<_, Option<String>>(7)?.unwrap_or_default() } else { row.get::<_, Option<String>>(13)?.unwrap_or_default() },
            "source_account_id": if swapped { row.get::<_, Option<String>>(14)?.unwrap_or_default() } else { row.get::<_, Option<String>>(8)?.unwrap_or_default() },
            "target_account_id": if swapped { row.get::<_, Option<String>>(8)?.unwrap_or_default() } else { row.get::<_, Option<String>>(14)?.unwrap_or_default() },
            "source_cidr_blocks": if swapped { row.get::<_, Option<String>>(15)?.unwrap_or_default() } else { row.get::<_, Option<String>>(9)?.unwrap_or_default() },
            "target_cidr_blocks": if swapped { row.get::<_, Option<String>>(9)?.unwrap_or_default() } else { row.get::<_, Option<String>>(15)?.unwrap_or_default() },
            "source_gateway_id": if swapped { row.get::<_, Option<String>>(21)?.unwrap_or_default() } else { row.get::<_, Option<String>>(20)?.unwrap_or_default() },
            "target_gateway_id": if swapped { row.get::<_, Option<String>>(20)?.unwrap_or_default() } else { row.get::<_, Option<String>>(21)?.unwrap_or_default() },
            "metadata": row.get::<_, Option<String>>(22)?.unwrap_or_default(),
            "limitation": "topology-backed explicit connection only; CIDR overlap inference is not performed"
        }).to_string();
        out.push(CorrelationRow {
            correlation_id,
            correlation_type: "network_topology_connection".to_string(),
            source_id: source_id.clone(),
            target_id: target_id.clone(),
            source_provider,
            target_provider,
            confidence,
            evidence,
            description: format!(
                "Cross-provider network topology connection between {} and {}",
                source_id, target_id
            ),
        });
    }
    out.sort_by(sort_rows);
    Ok(out)
}

fn network_confidence(status: &str, source_cidrs: Option<&str>, target_cidrs: Option<&str>) -> f64 {
    let mut confidence: f64 = if matches!(
        status.to_ascii_lowercase().as_str(),
        "active" | "available" | "connected"
    ) {
        0.9
    } else {
        0.75
    };
    if source_cidrs.is_some_and(|v| !v.trim().is_empty())
        && target_cidrs.is_some_and(|v| !v.trim().is_empty())
    {
        confidence += 0.05;
    }
    confidence.min(1.0)
}
