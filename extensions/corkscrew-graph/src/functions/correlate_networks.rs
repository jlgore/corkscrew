use duckdb::core::{DataChunkHandle, Inserter, LogicalTypeHandle, LogicalTypeId};
use duckdb::vtab::{BindInfo, InitInfo, TableFunctionInfo, VTab};
use duckdb::{Connection, Result};
use serde_json::json;
use std::collections::HashSet;
use std::ffi::CString;
use std::sync::atomic::AtomicUsize;

use crate::functions::common::{chunk_capacity, next_chunk};

#[repr(C)]
pub struct CorrelateNetworksBindData {
    pub db_path: String,
    pub min_confidence: f64,
}

#[derive(Clone, Debug)]
struct CorrelationRow {
    correlation_id: String,
    correlation_type: String,
    source_id: String,
    target_id: String,
    source_provider: String,
    target_provider: String,
    confidence: f64,
    evidence: String,
    description: String,
}

pub struct CorrelateNetworksInitData {
    cursor: AtomicUsize,
    rows: Vec<CorrelationRow>,
}
pub struct GraphCorrelateNetworksVTab;

impl VTab for GraphCorrelateNetworksVTab {
    type InitData = CorrelateNetworksInitData;
    type BindData = CorrelateNetworksBindData;

    fn bind(bind: &BindInfo) -> Result<Self::BindData, Box<dyn std::error::Error>> {
        add_standard_columns(bind);
        Ok(CorrelateNetworksBindData {
            db_path: bind.get_parameter(0).to_string(),
            min_confidence: bind.get_parameter(1).to_string().parse::<f64>()?,
        })
    }

    fn init(init: &InitInfo) -> Result<Self::InitData, Box<dyn std::error::Error>> {
        let bind_ref = unsafe {
            init.get_bind_data::<CorrelateNetworksBindData>()
                .as_ref()
                .unwrap()
        };
        let conn = Connection::open(&bind_ref.db_path)?;
        let rows = collect_network_correlations(&conn, bind_ref.min_confidence)?;
        Ok(CorrelateNetworksInitData {
            cursor: AtomicUsize::new(0),
            rows,
        })
    }

    fn func(
        func: &TableFunctionInfo<Self>,
        output: &mut DataChunkHandle,
    ) -> Result<(), Box<dyn std::error::Error>> {
        emit_rows(
            &func.get_init_data().cursor,
            &func.get_init_data().rows,
            output,
        )
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
) -> Result<Vec<CorrelationRow>, Box<dyn std::error::Error>> {
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

fn ordered_values(
    source_id: String,
    target_id: String,
    source_provider: String,
    target_provider: String,
) -> (String, String, String, String, bool) {
    if (&source_provider, &source_id) <= (&target_provider, &target_id) {
        (
            source_id,
            target_id,
            source_provider,
            target_provider,
            false,
        )
    } else {
        (target_id, source_id, target_provider, source_provider, true)
    }
}

fn table_exists(conn: &Connection, table_name: &str) -> Result<bool, Box<dyn std::error::Error>> {
    let mut stmt = conn.prepare(
        "SELECT COUNT(*) FROM information_schema.tables WHERE lower(table_name) = lower(?)",
    )?;
    let count: i64 = stmt.query_row([table_name], |row| row.get(0))?;
    Ok(count > 0)
}

fn sort_rows(a: &CorrelationRow, b: &CorrelationRow) -> std::cmp::Ordering {
    b.confidence
        .partial_cmp(&a.confidence)
        .unwrap_or(std::cmp::Ordering::Equal)
        .then_with(|| a.source_provider.cmp(&b.source_provider))
        .then_with(|| a.target_provider.cmp(&b.target_provider))
        .then_with(|| a.source_id.cmp(&b.source_id))
        .then_with(|| a.target_id.cmp(&b.target_id))
}

fn add_standard_columns(bind: &BindInfo) {
    for name in [
        "correlation_id",
        "correlation_type",
        "source_id",
        "target_id",
        "source_provider",
        "target_provider",
    ] {
        bind.add_result_column(name, LogicalTypeHandle::from(LogicalTypeId::Varchar));
    }
    bind.add_result_column("confidence", LogicalTypeHandle::from(LogicalTypeId::Double));
    bind.add_result_column("evidence", LogicalTypeHandle::from(LogicalTypeId::Varchar));
    bind.add_result_column(
        "description",
        LogicalTypeHandle::from(LogicalTypeId::Varchar),
    );
}

fn emit_rows(
    cursor: &AtomicUsize,
    rows: &[CorrelationRow],
    output: &mut DataChunkHandle,
) -> Result<(), Box<dyn std::error::Error>> {
    let Some((start, end)) = next_chunk(cursor, rows.len(), chunk_capacity()) else {
        output.set_len(0);
        return Ok(());
    };
    let v0 = output.flat_vector(0);
    let v1 = output.flat_vector(1);
    let v2 = output.flat_vector(2);
    let v3 = output.flat_vector(3);
    let v4 = output.flat_vector(4);
    let v5 = output.flat_vector(5);
    let mut v6 = output.flat_vector(6);
    let v7 = output.flat_vector(7);
    let v8 = output.flat_vector(8);
    let confidence = v6.as_mut_slice::<f64>();
    for (out_idx, row) in rows[start..end].iter().enumerate() {
        v0.insert(out_idx, CString::new(row.correlation_id.clone())?);
        v1.insert(out_idx, CString::new(row.correlation_type.clone())?);
        v2.insert(out_idx, CString::new(row.source_id.clone())?);
        v3.insert(out_idx, CString::new(row.target_id.clone())?);
        v4.insert(out_idx, CString::new(row.source_provider.clone())?);
        v5.insert(out_idx, CString::new(row.target_provider.clone())?);
        confidence[out_idx] = row.confidence;
        v7.insert(out_idx, CString::new(row.evidence.clone())?);
        v8.insert(out_idx, CString::new(row.description.clone())?);
    }
    output.set_len(end - start);
    Ok(())
}
