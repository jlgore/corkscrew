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
pub struct CorrelatePoliciesBindData {
    pub db_path: String,
    pub min_confidence: f64,
}
pub struct CorrelatePoliciesInitData {
    cursor: AtomicUsize,
    rows: Vec<CorrelationRow>,
}
pub struct GraphCorrelatePoliciesVTab;

impl VTab for GraphCorrelatePoliciesVTab {
    type InitData = CorrelatePoliciesInitData;
    type BindData = CorrelatePoliciesBindData;
    fn bind(bind: &BindInfo) -> Result<Self::BindData, BoxError> {
        guard("GraphCorrelatePoliciesVTab::bind", || {
            add_standard_columns(bind);
            Ok(CorrelatePoliciesBindData {
                db_path: param_varchar(bind, 0, "db_path")?,
                min_confidence: param_f64(bind, 1, "min_confidence")?,
            })
        })
    }
    fn init(init: &InitInfo) -> Result<Self::InitData, BoxError> {
        guard("GraphCorrelatePoliciesVTab::init", || {
            let bind_ref = bind_data::<Self>(init)?;
            let conn = Connection::open(&bind_ref.db_path)?;
            let rows = collect_policy_correlations(&conn, bind_ref.min_confidence)?;
            Ok(CorrelatePoliciesInitData {
                cursor: AtomicUsize::new(0),
                rows,
            })
        })
    }
    fn func(func: &TableFunctionInfo<Self>, output: &mut DataChunkHandle) -> Result<(), BoxError> {
        guard("GraphCorrelatePoliciesVTab::func", || {
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

fn collect_policy_correlations(
    conn: &Connection,
    min_confidence: f64,
) -> Result<Vec<CorrelationRow>, BoxError> {
    if !table_exists(conn, "policy_similarity_analysis")? {
        return Ok(Vec::new());
    }
    let threshold = min_confidence.clamp(0.0, 1.0);
    let mut stmt = conn.prepare(
        "SELECT id, source_policy_id, source_policy_name, source_policy_type, source_cloud_provider, source_region, source_account_id, source_resource_id,
                target_policy_id, target_policy_name, target_policy_type, target_cloud_provider, target_region, target_account_id, target_resource_id,
                similarity_score, similarity_type, matching_elements, differences, normalized_permissions, source_policy_hash, target_policy_hash, source_statements, target_statements,
                risk_level, risk_score, risk_factors, security_issues, recommendations, compliance_tags, analysis_method, confidence_score, false_positive_likelihood, status, reviewed
         FROM policy_similarity_analysis
         WHERE source_cloud_provider IS NOT NULL AND target_cloud_provider IS NOT NULL AND source_cloud_provider <> target_cloud_provider",
    )?;
    let mut rows = stmt.query([])?;
    let mut out = Vec::new();
    let mut seen = HashSet::new();
    while let Some(row) = rows.next()? {
        let similarity = row.get::<_, f64>(15)?;
        let confidence = row.get::<_, Option<f64>>(31)?.unwrap_or(similarity);
        if confidence < threshold {
            continue;
        }
        let (source_id, target_id, source_provider, target_provider, swapped) =
            ordered_values(row.get(1)?, row.get(8)?, row.get(4)?, row.get(11)?);
        let id = row.get::<_, Option<String>>(0)?.unwrap_or_default();
        let cid = format!("policy_similarity:{}:{}:{}", id, source_id, target_id);
        if !seen.insert(cid.clone()) {
            continue;
        }
        let evidence = json!({
            "table": "policy_similarity_analysis", "record_id": id, "similarity_score": similarity, "similarity_type": row.get::<_, Option<String>>(16)?.unwrap_or_default(), "status": row.get::<_, Option<String>>(33)?.unwrap_or_default(), "reviewed": row.get::<_, Option<bool>>(34)?.unwrap_or(false),
            "source_policy_name": if swapped { row.get::<_, Option<String>>(9)?.unwrap_or_default() } else { row.get::<_, Option<String>>(2)?.unwrap_or_default() }, "target_policy_name": if swapped { row.get::<_, Option<String>>(2)?.unwrap_or_default() } else { row.get::<_, Option<String>>(9)?.unwrap_or_default() },
            "source_policy_type": if swapped { row.get::<_, Option<String>>(10)?.unwrap_or_default() } else { row.get::<_, Option<String>>(3)?.unwrap_or_default() }, "target_policy_type": if swapped { row.get::<_, Option<String>>(3)?.unwrap_or_default() } else { row.get::<_, Option<String>>(10)?.unwrap_or_default() },
            "source_resource_id": if swapped { row.get::<_, Option<String>>(14)?.unwrap_or_default() } else { row.get::<_, Option<String>>(7)?.unwrap_or_default() }, "target_resource_id": if swapped { row.get::<_, Option<String>>(7)?.unwrap_or_default() } else { row.get::<_, Option<String>>(14)?.unwrap_or_default() },
            "source_policy_hash": if swapped { row.get::<_, Option<String>>(21)?.unwrap_or_default() } else { row.get::<_, Option<String>>(20)?.unwrap_or_default() }, "target_policy_hash": if swapped { row.get::<_, Option<String>>(20)?.unwrap_or_default() } else { row.get::<_, Option<String>>(21)?.unwrap_or_default() },
            "matching_elements": row.get::<_, Option<String>>(17)?.unwrap_or_default(), "differences": row.get::<_, Option<String>>(18)?.unwrap_or_default(), "normalized_permissions": row.get::<_, Option<String>>(19)?.unwrap_or_default(), "source_statements": row.get::<_, Option<String>>(22)?.unwrap_or_default(), "target_statements": row.get::<_, Option<String>>(23)?.unwrap_or_default(), "risk_level": row.get::<_, Option<String>>(24)?.unwrap_or_default(), "risk_score": row.get::<_, Option<f64>>(25)?.unwrap_or(0.0), "risk_factors": row.get::<_, Option<String>>(26)?.unwrap_or_default(), "security_issues": row.get::<_, Option<String>>(27)?.unwrap_or_default(), "recommendations": row.get::<_, Option<String>>(28)?.unwrap_or_default(), "compliance_tags": row.get::<_, Option<String>>(29)?.unwrap_or_default(), "analysis_method": row.get::<_, Option<String>>(30)?.unwrap_or_default(), "false_positive_likelihood": row.get::<_, Option<f64>>(32)?.unwrap_or(0.0)
        }).to_string();
        out.push(CorrelationRow {
            correlation_id: cid,
            correlation_type: "policy_similarity".to_string(),
            source_id: source_id.clone(),
            target_id: target_id.clone(),
            source_provider,
            target_provider,
            confidence,
            evidence,
            description: format!(
                "Cross-provider policy similarity between {} and {}",
                source_id, target_id
            ),
        });
    }
    out.sort_by(sort_rows);
    Ok(out)
}
