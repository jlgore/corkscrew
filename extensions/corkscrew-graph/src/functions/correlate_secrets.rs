use duckdb::core::{DataChunkHandle, Inserter, LogicalTypeHandle, LogicalTypeId};
use duckdb::vtab::{BindInfo, InitInfo, TableFunctionInfo, VTab};
use duckdb::{Connection, Result};
use serde_json::json;
use std::collections::{HashMap, HashSet};
use std::ffi::CString;
use std::sync::atomic::AtomicUsize;

use crate::functions::common::{chunk_capacity, next_chunk};

#[repr(C)]
pub struct CorrelateSecretsBindData {
    pub db_path: String,
    pub min_confidence: f64,
}
#[derive(Clone, Debug)]
struct SecretRecord {
    id: String,
    secret_type: String,
    secret_name: String,
    provider: String,
    region: String,
    account_id: String,
    resource_id: String,
    service_name: String,
    risk_level: String,
    confidence: f64,
    method: String,
    evidence: String,
    status: String,
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
pub struct CorrelateSecretsInitData {
    cursor: AtomicUsize,
    rows: Vec<CorrelationRow>,
}
pub struct GraphCorrelateSecretsVTab;

impl VTab for GraphCorrelateSecretsVTab {
    type InitData = CorrelateSecretsInitData;
    type BindData = CorrelateSecretsBindData;
    fn bind(bind: &BindInfo) -> Result<Self::BindData, Box<dyn std::error::Error>> {
        add_standard_columns(bind);
        Ok(CorrelateSecretsBindData {
            db_path: bind.get_parameter(0).to_string(),
            min_confidence: bind.get_parameter(1).to_string().parse::<f64>()?,
        })
    }
    fn init(init: &InitInfo) -> Result<Self::InitData, Box<dyn std::error::Error>> {
        let bind_ref = unsafe {
            init.get_bind_data::<CorrelateSecretsBindData>()
                .as_ref()
                .unwrap()
        };
        let conn = Connection::open(&bind_ref.db_path)?;
        let rows = collect_secret_correlations(&conn, bind_ref.min_confidence)?;
        Ok(CorrelateSecretsInitData {
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

fn collect_secret_correlations(
    conn: &Connection,
    min_confidence: f64,
) -> Result<Vec<CorrelationRow>, Box<dyn std::error::Error>> {
    if !table_exists(conn, "shared_secrets_correlation")? {
        return Ok(Vec::new());
    }
    let mut stmt = conn.prepare(
        "SELECT id, secret_type, secret_name, secret_hash, cloud_provider, region, account_id, resource_id, service_name,
                security_risk_level, security_issues, recommendations, referenced_by, cross_cloud_references, usage_patterns,
                encryption_status, access_control_status, correlation_confidence, correlation_method, correlation_evidence, status
         FROM shared_secrets_correlation
         WHERE secret_hash IS NOT NULL AND cloud_provider IS NOT NULL AND resource_id IS NOT NULL",
    )?;
    let mut rows = stmt.query([])?;
    let mut by_hash: HashMap<String, Vec<SecretRecord>> = HashMap::new();
    while let Some(row) = rows.next()? {
        let hash = row.get::<_, String>(3)?;
        by_hash.entry(hash).or_default().push(SecretRecord { id: row.get::<_, Option<String>>(0)?.unwrap_or_default(), secret_type: row.get::<_, Option<String>>(1)?.unwrap_or_default(), secret_name: row.get::<_, Option<String>>(2)?.unwrap_or_default(), provider: row.get::<_, String>(4)?, region: row.get::<_, Option<String>>(5)?.unwrap_or_default(), account_id: row.get::<_, Option<String>>(6)?.unwrap_or_default(), resource_id: row.get::<_, String>(7)?, service_name: row.get::<_, Option<String>>(8)?.unwrap_or_default(), risk_level: row.get::<_, Option<String>>(9)?.unwrap_or_default(), confidence: row.get::<_, Option<f64>>(17)?.unwrap_or(0.9), method: row.get::<_, Option<String>>(18)?.unwrap_or_default(), evidence: json!({ "security_issues": row.get::<_, Option<String>>(10)?.unwrap_or_default(), "recommendations": row.get::<_, Option<String>>(11)?.unwrap_or_default(), "referenced_by": row.get::<_, Option<String>>(12)?.unwrap_or_default(), "cross_cloud_references": row.get::<_, Option<String>>(13)?.unwrap_or_default(), "usage_patterns": row.get::<_, Option<String>>(14)?.unwrap_or_default(), "encryption_status": row.get::<_, Option<String>>(15)?.unwrap_or_default(), "access_control_status": row.get::<_, Option<String>>(16)?.unwrap_or_default(), "correlation_evidence": row.get::<_, Option<String>>(19)?.unwrap_or_default() }).to_string(), status: row.get::<_, Option<String>>(20)?.unwrap_or_default() });
    }
    let threshold = min_confidence.clamp(0.0, 1.0);
    let mut out = Vec::new();
    let mut seen = HashSet::new();
    for (hash, group) in by_hash {
        for i in 0..group.len() {
            for j in (i + 1)..group.len() {
                let left = &group[i];
                let right = &group[j];
                if left.provider == right.provider || left.resource_id == right.resource_id {
                    continue;
                }
                let (source, target) = ordered_pair(left, right);
                let confidence = source.confidence.min(target.confidence).max(0.9);
                if confidence < threshold {
                    continue;
                }
                let cid = format!(
                    "shared_secret:{}:{}:{}",
                    hash, source.resource_id, target.resource_id
                );
                if !seen.insert(cid.clone()) {
                    continue;
                }
                let evidence = json!({ "table": "shared_secrets_correlation", "secret_hash": hash, "secret_type": source.secret_type, "source_record_id": source.id, "target_record_id": target.id, "source_secret_name": source.secret_name, "target_secret_name": target.secret_name, "source_service_name": source.service_name, "target_service_name": target.service_name, "source_region": source.region, "target_region": target.region, "source_account_id": source.account_id, "target_account_id": target.account_id, "source_risk_level": source.risk_level, "target_risk_level": target.risk_level, "source_status": source.status, "target_status": target.status, "source_method": source.method, "target_method": target.method, "source_evidence": source.evidence, "target_evidence": target.evidence }).to_string();
                out.push(CorrelationRow {
                    correlation_id: cid,
                    correlation_type: "shared_secret_match".to_string(),
                    source_id: source.resource_id.clone(),
                    target_id: target.resource_id.clone(),
                    source_provider: source.provider.clone(),
                    target_provider: target.provider.clone(),
                    confidence,
                    evidence,
                    description: format!(
                        "Cross-provider shared {} material between {} and {}",
                        source.secret_type, source.resource_id, target.resource_id
                    ),
                });
            }
        }
    }
    out.sort_by(sort_rows);
    Ok(out)
}

fn ordered_pair<'a>(
    left: &'a SecretRecord,
    right: &'a SecretRecord,
) -> (&'a SecretRecord, &'a SecretRecord) {
    if (&left.provider, &left.resource_id, &left.id)
        <= (&right.provider, &right.resource_id, &right.id)
    {
        (left, right)
    } else {
        (right, left)
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
