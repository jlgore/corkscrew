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
pub struct CorrelateSecurityBindData {
    pub db_path: String,
    pub min_confidence: f64,
}

pub struct CorrelateSecurityInitData {
    cursor: AtomicUsize,
    rows: Vec<CorrelationRow>,
}
pub struct GraphCorrelateSecurityVTab;

impl VTab for GraphCorrelateSecurityVTab {
    type InitData = CorrelateSecurityInitData;
    type BindData = CorrelateSecurityBindData;

    fn bind(bind: &BindInfo) -> Result<Self::BindData, BoxError> {
        guard("GraphCorrelateSecurityVTab::bind", || {
            add_standard_columns(bind);
            Ok(CorrelateSecurityBindData {
                db_path: param_varchar(bind, 0, "db_path")?,
                min_confidence: param_f64(bind, 1, "min_confidence")?,
            })
        })
    }

    fn init(init: &InitInfo) -> Result<Self::InitData, BoxError> {
        guard("GraphCorrelateSecurityVTab::init", || {
            let bind_ref = bind_data::<Self>(init)?;
            let conn = Connection::open(&bind_ref.db_path)?;
            let rows = collect_security_correlations(&conn, bind_ref.min_confidence)?;
            Ok(CorrelateSecurityInitData {
                cursor: AtomicUsize::new(0),
                rows,
            })
        })
    }

    fn func(func: &TableFunctionInfo<Self>, output: &mut DataChunkHandle) -> Result<(), BoxError> {
        guard("GraphCorrelateSecurityVTab::func", || {
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

fn collect_security_correlations(
    conn: &Connection,
    min_confidence: f64,
) -> Result<Vec<CorrelationRow>, BoxError> {
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
