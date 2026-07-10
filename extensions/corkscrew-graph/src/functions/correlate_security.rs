use duckdb::core::{DataChunkHandle, Inserter, LogicalTypeHandle, LogicalTypeId};
use duckdb::vtab::{BindInfo, InitInfo, TableFunctionInfo, VTab};
use duckdb::{Connection, Result};
use serde_json::json;
use std::collections::HashSet;
use std::ffi::CString;
use std::sync::atomic::AtomicUsize;

use crate::functions::common::{chunk_capacity, next_chunk};

#[repr(C)]
pub struct CorrelateSecurityBindData {
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

pub struct CorrelateSecurityInitData {
    cursor: AtomicUsize,
    rows: Vec<CorrelationRow>,
}
pub struct GraphCorrelateSecurityVTab;

impl VTab for GraphCorrelateSecurityVTab {
    type InitData = CorrelateSecurityInitData;
    type BindData = CorrelateSecurityBindData;

    fn bind(bind: &BindInfo) -> Result<Self::BindData, Box<dyn std::error::Error>> {
        add_standard_columns(bind);
        Ok(CorrelateSecurityBindData {
            db_path: bind.get_parameter(0).to_string(),
            min_confidence: bind.get_parameter(1).to_string().parse::<f64>()?,
        })
    }

    fn init(init: &InitInfo) -> Result<Self::InitData, Box<dyn std::error::Error>> {
        let bind_ref = unsafe {
            init.get_bind_data::<CorrelateSecurityBindData>()
                .as_ref()
                .unwrap()
        };
        let conn = Connection::open(&bind_ref.db_path)?;
        let rows = collect_security_correlations(&conn, bind_ref.min_confidence)?;
        Ok(CorrelateSecurityInitData {
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

fn collect_security_correlations(
    conn: &Connection,
    min_confidence: f64,
) -> Result<Vec<CorrelationRow>, Box<dyn std::error::Error>> {
    if !table_exists(conn, "cross_cloud_security_correlations")? {
        return Ok(Vec::new());
    }
    let threshold = min_confidence.clamp(0.0, 1.0);
    let mut stmt = conn.prepare(
        "SELECT id, source_rule_id, source_resource_id, source_provider, source_region,
                target_rule_id, target_resource_id, target_provider, target_region,
                rule_overlap_analysis, overlap_percentage, overlap_type, protocol_correlation, port_overlap_analysis, cidr_overlap_analysis,
                security_pattern, access_pattern, direction_analysis, action_analysis, security_risk_level, risk_factors,
                correlation_method, confidence_score, evidence, validation_status
         FROM cross_cloud_security_correlations
         WHERE source_provider IS NOT NULL AND target_provider IS NOT NULL AND source_provider <> target_provider",
    )?;
    let mut rows = stmt.query([])?;
    let mut out = Vec::new();
    let mut seen = HashSet::new();
    while let Some(row) = rows.next()? {
        let confidence = row.get::<_, f64>(22)?;
        if confidence < threshold {
            continue;
        }
        let (source_id, target_id, source_provider, target_provider, swapped) =
            ordered_values(row.get(2)?, row.get(6)?, row.get(3)?, row.get(7)?);
        let id = row.get::<_, Option<String>>(0)?.unwrap_or_default();
        let cid = format!("security_rule:{}:{}:{}", id, source_id, target_id);
        if !seen.insert(cid.clone()) {
            continue;
        }
        let security_pattern = row.get::<_, Option<String>>(15)?.unwrap_or_default();
        let risk_level = row.get::<_, Option<String>>(19)?.unwrap_or_default();
        let evidence = json!({
            "table": "cross_cloud_security_correlations", "record_id": id,
            "source_rule_id": if swapped { row.get::<_, Option<String>>(5)?.unwrap_or_default() } else { row.get::<_, Option<String>>(1)?.unwrap_or_default() },
            "target_rule_id": if swapped { row.get::<_, Option<String>>(1)?.unwrap_or_default() } else { row.get::<_, Option<String>>(5)?.unwrap_or_default() },
            "source_region": if swapped { row.get::<_, Option<String>>(8)?.unwrap_or_default() } else { row.get::<_, Option<String>>(4)?.unwrap_or_default() },
            "target_region": if swapped { row.get::<_, Option<String>>(4)?.unwrap_or_default() } else { row.get::<_, Option<String>>(8)?.unwrap_or_default() },
            "rule_overlap_analysis": row.get::<_, Option<String>>(9)?.unwrap_or_default(), "overlap_percentage": row.get::<_, Option<f64>>(10)?.unwrap_or(0.0), "overlap_type": row.get::<_, Option<String>>(11)?.unwrap_or_default(),
            "protocol_correlation": row.get::<_, Option<String>>(12)?.unwrap_or_default(), "port_overlap_analysis": row.get::<_, Option<String>>(13)?.unwrap_or_default(), "cidr_overlap_analysis": row.get::<_, Option<String>>(14)?.unwrap_or_default(),
            "security_pattern": security_pattern, "access_pattern": row.get::<_, Option<String>>(16)?.unwrap_or_default(), "direction_analysis": row.get::<_, Option<String>>(17)?.unwrap_or_default(), "action_analysis": row.get::<_, Option<String>>(18)?.unwrap_or_default(),
            "security_risk_level": risk_level, "risk_factors": row.get::<_, Option<String>>(20)?.unwrap_or_default(), "correlation_method": row.get::<_, Option<String>>(21)?.unwrap_or_default(), "source_evidence": row.get::<_, Option<String>>(23)?.unwrap_or_default(), "validation_status": row.get::<_, Option<String>>(24)?.unwrap_or_default()
        }).to_string();
        out.push(CorrelationRow {
            correlation_id: cid,
            correlation_type: "security_rule_correlation".to_string(),
            source_id: source_id.clone(),
            target_id: target_id.clone(),
            source_provider,
            target_provider,
            confidence,
            evidence,
            description: format!(
                "Cross-provider security rule correlation between {} and {}",
                source_id, target_id
            ),
        });
    }
    out.sort_by(sort_rows);
    Ok(out)
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
