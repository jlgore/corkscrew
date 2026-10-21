use duckdb::core::{DataChunkHandle, LogicalTypeHandle, LogicalTypeId};
use duckdb::vtab::{BindInfo, InitInfo, TableFunctionInfo, VTab};
use duckdb::{Connection, Result};
use serde_json::json;
use std::collections::{HashMap, HashSet};
use std::sync::atomic::AtomicUsize;

use crate::functions::correlate_common::{
    add_standard_columns, emit_rows, sort_rows, table_exists, walk_group_pairs, CorrelationLimits,
    CorrelationRow, GroupKey, GroupMember,
};
use crate::functions::params::{bind_data, guard, param_f64, param_varchar, BoxError};

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

impl GroupMember for SecretRecord {
    fn provider(&self) -> &str {
        &self.provider
    }
}
pub struct CorrelateSecretsInitData {
    cursor: AtomicUsize,
    rows: Vec<CorrelationRow>,
}
pub struct GraphCorrelateSecretsVTab;

impl VTab for GraphCorrelateSecretsVTab {
    type InitData = CorrelateSecretsInitData;
    type BindData = CorrelateSecretsBindData;
    fn bind(bind: &BindInfo) -> Result<Self::BindData, BoxError> {
        guard("GraphCorrelateSecretsVTab::bind", || {
            add_standard_columns(bind);
            Ok(CorrelateSecretsBindData {
                db_path: param_varchar(bind, 0, "db_path")?,
                min_confidence: param_f64(bind, 1, "min_confidence")?,
            })
        })
    }
    fn init(init: &InitInfo) -> Result<Self::InitData, BoxError> {
        guard("GraphCorrelateSecretsVTab::init", || {
            let bind_ref = bind_data::<Self>(init)?;
            let conn = Connection::open(&bind_ref.db_path)?;
            let rows = collect_secret_correlations(&conn, bind_ref.min_confidence)?;
            Ok(CorrelateSecretsInitData {
                cursor: AtomicUsize::new(0),
                rows,
            })
        })
    }
    fn func(func: &TableFunctionInfo<Self>, output: &mut DataChunkHandle) -> Result<(), BoxError> {
        guard("GraphCorrelateSecretsVTab::func", || {
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

fn collect_secret_correlations(
    conn: &Connection,
    min_confidence: f64,
) -> Result<Vec<CorrelationRow>, BoxError> {
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
    for (hash, group) in &by_hash {
        let key = GroupKey {
            kind: "shared_secret_match",
            value: hash,
        };
        walk_group_pairs(
            group,
            key,
            CorrelationLimits::DEFAULT,
            &mut out,
            |left, right| {
                let row = secret_pair_row(left, right, hash, threshold)?;
                seen.insert(row.correlation_id.clone()).then_some(row)
            },
        )?;
    }
    out.sort_by(sort_rows);
    Ok(out)
}

/// The correlation row for one pair of secrets sharing `hash`, or `None` when
/// the pair is same-provider, same-resource, or below `threshold`.
fn secret_pair_row(
    left: &SecretRecord,
    right: &SecretRecord,
    hash: &str,
    threshold: f64,
) -> Option<CorrelationRow> {
    if left.provider == right.provider || left.resource_id == right.resource_id {
        return None;
    }
    let (source, target) = ordered_pair(left, right);
    let confidence = source.confidence.min(target.confidence).max(0.9);
    if confidence < threshold {
        return None;
    }
    let evidence = json!({ "table": "shared_secrets_correlation", "secret_hash": hash, "secret_type": source.secret_type, "source_record_id": source.id, "target_record_id": target.id, "source_secret_name": source.secret_name, "target_secret_name": target.secret_name, "source_service_name": source.service_name, "target_service_name": target.service_name, "source_region": source.region, "target_region": target.region, "source_account_id": source.account_id, "target_account_id": target.account_id, "source_risk_level": source.risk_level, "target_risk_level": target.risk_level, "source_status": source.status, "target_status": target.status, "source_method": source.method, "target_method": target.method, "source_evidence": source.evidence, "target_evidence": target.evidence }).to_string();
    Some(CorrelationRow {
        correlation_id: format!(
            "shared_secret:{}:{}:{}",
            hash, source.resource_id, target.resource_id
        ),
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
    })
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
