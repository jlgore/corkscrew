use duckdb::core::{DataChunkHandle, Inserter, LogicalTypeHandle, LogicalTypeId};
use duckdb::vtab::{BindInfo, InitInfo, TableFunctionInfo, VTab};
use duckdb::{Connection, Result};
use serde_json::json;
use std::collections::HashSet;
use std::ffi::CString;
use std::sync::atomic::AtomicUsize;

use crate::functions::common::{chunk_capacity, next_chunk};

#[repr(C)]
pub struct CorrelateIdentityBindData {
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

pub struct CorrelateIdentityInitData {
    cursor: AtomicUsize,
    rows: Vec<CorrelationRow>,
}
pub struct GraphCorrelateIdentityVTab;

impl VTab for GraphCorrelateIdentityVTab {
    type InitData = CorrelateIdentityInitData;
    type BindData = CorrelateIdentityBindData;

    fn bind(bind: &BindInfo) -> Result<Self::BindData, Box<dyn std::error::Error>> {
        add_standard_columns(bind);
        Ok(CorrelateIdentityBindData {
            db_path: bind.get_parameter(0).to_string(),
            min_confidence: bind.get_parameter(1).to_string().parse::<f64>()?,
        })
    }
    fn init(init: &InitInfo) -> Result<Self::InitData, Box<dyn std::error::Error>> {
        let bind_ref = unsafe {
            init.get_bind_data::<CorrelateIdentityBindData>()
                .as_ref()
                .unwrap()
        };
        let conn = Connection::open(&bind_ref.db_path)?;
        let rows = collect_identity_correlations(&conn, bind_ref.min_confidence)?;
        Ok(CorrelateIdentityInitData {
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

fn collect_identity_correlations(
    conn: &Connection,
    min_confidence: f64,
) -> Result<Vec<CorrelationRow>, Box<dyn std::error::Error>> {
    let threshold = min_confidence.clamp(0.0, 1.0);
    let mut out = Vec::new();
    let mut seen = HashSet::new();
    collect_federation(conn, threshold, &mut seen, &mut out)?;
    collect_roles(conn, threshold, &mut seen, &mut out)?;
    out.sort_by(sort_rows);
    Ok(out)
}

fn collect_federation(
    conn: &Connection,
    threshold: f64,
    seen: &mut HashSet<String>,
    out: &mut Vec<CorrelationRow>,
) -> Result<(), Box<dyn std::error::Error>> {
    if !table_exists(conn, "identity_federation_relationships")? {
        return Ok(());
    }
    let mut stmt = conn.prepare(
        "SELECT id, source_provider_id, source_provider_type, source_provider_name, source_cloud_provider, source_region, source_account_id,
                target_provider_id, target_provider_type, target_provider_name, target_cloud_provider, target_region, target_account_id,
                federation_type, federation_method, trust_policy, trust_conditions, oidc_issuer, oidc_endpoints, client_ids, scopes,
                saml_entity_id, saml_sso_endpoint, certificate_thumbprints, signing_certificates, confidence_score, evidence, matching_attributes,
                security_risk_level, security_risk_score, security_issues, recommendations, status, verified, verification_method
         FROM identity_federation_relationships
         WHERE source_cloud_provider IS NOT NULL AND target_cloud_provider IS NOT NULL AND source_cloud_provider <> target_cloud_provider",
    )?;
    let mut rows = stmt.query([])?;
    while let Some(row) = rows.next()? {
        let confidence = row.get::<_, f64>(25)?;
        if confidence < threshold {
            continue;
        }
        let (source_id, target_id, source_provider, target_provider, swapped) =
            ordered_values(row.get(1)?, row.get(7)?, row.get(4)?, row.get(10)?);
        let id = row.get::<_, Option<String>>(0)?.unwrap_or_default();
        let cid = format!("identity_federation:{}:{}:{}", id, source_id, target_id);
        if !seen.insert(cid.clone()) {
            continue;
        }
        let evidence = json!({
            "table": "identity_federation_relationships", "record_id": id, "federation_type": row.get::<_, Option<String>>(13)?.unwrap_or_default(), "federation_method": row.get::<_, Option<String>>(14)?.unwrap_or_default(), "status": row.get::<_, Option<String>>(32)?.unwrap_or_default(), "verified": row.get::<_, Option<bool>>(33)?.unwrap_or(false), "verification_method": row.get::<_, Option<String>>(34)?.unwrap_or_default(),
            "source_provider_type": if swapped { row.get::<_, Option<String>>(8)?.unwrap_or_default() } else { row.get::<_, Option<String>>(2)?.unwrap_or_default() }, "target_provider_type": if swapped { row.get::<_, Option<String>>(2)?.unwrap_or_default() } else { row.get::<_, Option<String>>(8)?.unwrap_or_default() },
            "source_provider_name": if swapped { row.get::<_, Option<String>>(9)?.unwrap_or_default() } else { row.get::<_, Option<String>>(3)?.unwrap_or_default() }, "target_provider_name": if swapped { row.get::<_, Option<String>>(3)?.unwrap_or_default() } else { row.get::<_, Option<String>>(9)?.unwrap_or_default() },
            "source_region": if swapped { row.get::<_, Option<String>>(11)?.unwrap_or_default() } else { row.get::<_, Option<String>>(5)?.unwrap_or_default() }, "target_region": if swapped { row.get::<_, Option<String>>(5)?.unwrap_or_default() } else { row.get::<_, Option<String>>(11)?.unwrap_or_default() },
            "source_account_id": if swapped { row.get::<_, Option<String>>(12)?.unwrap_or_default() } else { row.get::<_, Option<String>>(6)?.unwrap_or_default() }, "target_account_id": if swapped { row.get::<_, Option<String>>(6)?.unwrap_or_default() } else { row.get::<_, Option<String>>(12)?.unwrap_or_default() },
            "trust_policy": row.get::<_, Option<String>>(15)?.unwrap_or_default(), "trust_conditions": row.get::<_, Option<String>>(16)?.unwrap_or_default(), "oidc_issuer": row.get::<_, Option<String>>(17)?.unwrap_or_default(), "oidc_endpoints": row.get::<_, Option<String>>(18)?.unwrap_or_default(), "client_ids": row.get::<_, Option<String>>(19)?.unwrap_or_default(), "scopes": row.get::<_, Option<String>>(20)?.unwrap_or_default(), "saml_entity_id": row.get::<_, Option<String>>(21)?.unwrap_or_default(), "saml_sso_endpoint": row.get::<_, Option<String>>(22)?.unwrap_or_default(), "certificate_thumbprints": row.get::<_, Option<String>>(23)?.unwrap_or_default(), "signing_certificates": row.get::<_, Option<String>>(24)?.unwrap_or_default(), "source_evidence": row.get::<_, Option<String>>(26)?.unwrap_or_default(), "matching_attributes": row.get::<_, Option<String>>(27)?.unwrap_or_default(), "security_risk_level": row.get::<_, Option<String>>(28)?.unwrap_or_default(), "security_risk_score": row.get::<_, Option<f64>>(29)?.unwrap_or(0.0), "security_issues": row.get::<_, Option<String>>(30)?.unwrap_or_default(), "recommendations": row.get::<_, Option<String>>(31)?.unwrap_or_default()
        }).to_string();
        out.push(CorrelationRow {
            correlation_id: cid,
            correlation_type: "identity_federation".to_string(),
            source_id: source_id.clone(),
            target_id: target_id.clone(),
            source_provider,
            target_provider,
            confidence,
            evidence,
            description: format!(
                "Cross-provider identity federation between {} and {}",
                source_id, target_id
            ),
        });
    }
    Ok(())
}

fn collect_roles(
    conn: &Connection,
    threshold: f64,
    seen: &mut HashSet<String>,
    out: &mut Vec<CorrelationRow>,
) -> Result<(), Box<dyn std::error::Error>> {
    if !table_exists(conn, "security_role_relationships")? {
        return Ok(());
    }
    let mut stmt = conn.prepare(
        "SELECT id, source_role_id, source_role_arn, source_role_name, source_cloud_provider, source_region, source_account_id,
                target_role_id, target_role_arn, target_role_name, target_cloud_provider, target_region, target_account_id,
                relationship_type, assumption_chain, trusted_principals, trust_conditions, source_permissions, target_permissions, effective_permissions,
                confidence_score, risk_score, escalation_paths, security_issues, recommendations, evidence, detection_method, status, verified, remediation_status
         FROM security_role_relationships
         WHERE source_cloud_provider IS NOT NULL AND target_cloud_provider IS NOT NULL AND source_cloud_provider <> target_cloud_provider",
    )?;
    let mut rows = stmt.query([])?;
    while let Some(row) = rows.next()? {
        let confidence = row.get::<_, f64>(20)?;
        if confidence < threshold {
            continue;
        }
        let (source_id, target_id, source_provider, target_provider, swapped) =
            ordered_values(row.get(1)?, row.get(7)?, row.get(4)?, row.get(10)?);
        let id = row.get::<_, Option<String>>(0)?.unwrap_or_default();
        let cid = format!("identity_role:{}:{}:{}", id, source_id, target_id);
        if !seen.insert(cid.clone()) {
            continue;
        }
        let evidence = json!({
            "table": "security_role_relationships", "record_id": id, "relationship_type": row.get::<_, Option<String>>(13)?.unwrap_or_default(), "status": row.get::<_, Option<String>>(27)?.unwrap_or_default(), "verified": row.get::<_, Option<bool>>(28)?.unwrap_or(false), "remediation_status": row.get::<_, Option<String>>(29)?.unwrap_or_default(),
            "source_role_arn": if swapped { row.get::<_, Option<String>>(8)?.unwrap_or_default() } else { row.get::<_, Option<String>>(2)?.unwrap_or_default() }, "target_role_arn": if swapped { row.get::<_, Option<String>>(2)?.unwrap_or_default() } else { row.get::<_, Option<String>>(8)?.unwrap_or_default() },
            "source_role_name": if swapped { row.get::<_, Option<String>>(9)?.unwrap_or_default() } else { row.get::<_, Option<String>>(3)?.unwrap_or_default() }, "target_role_name": if swapped { row.get::<_, Option<String>>(3)?.unwrap_or_default() } else { row.get::<_, Option<String>>(9)?.unwrap_or_default() },
            "source_account_id": if swapped { row.get::<_, Option<String>>(12)?.unwrap_or_default() } else { row.get::<_, Option<String>>(6)?.unwrap_or_default() }, "target_account_id": if swapped { row.get::<_, Option<String>>(6)?.unwrap_or_default() } else { row.get::<_, Option<String>>(12)?.unwrap_or_default() },
            "assumption_chain": row.get::<_, Option<String>>(14)?.unwrap_or_default(), "trusted_principals": row.get::<_, Option<String>>(15)?.unwrap_or_default(), "trust_conditions": row.get::<_, Option<String>>(16)?.unwrap_or_default(), "source_permissions": row.get::<_, Option<String>>(17)?.unwrap_or_default(), "target_permissions": row.get::<_, Option<String>>(18)?.unwrap_or_default(), "effective_permissions": row.get::<_, Option<String>>(19)?.unwrap_or_default(), "risk_score": row.get::<_, Option<f64>>(21)?.unwrap_or(0.0), "escalation_paths": row.get::<_, Option<String>>(22)?.unwrap_or_default(), "security_issues": row.get::<_, Option<String>>(23)?.unwrap_or_default(), "recommendations": row.get::<_, Option<String>>(24)?.unwrap_or_default(), "source_evidence": row.get::<_, Option<String>>(25)?.unwrap_or_default(), "detection_method": row.get::<_, Option<String>>(26)?.unwrap_or_default()
        }).to_string();
        out.push(CorrelationRow {
            correlation_id: cid,
            correlation_type: "identity_role_trust".to_string(),
            source_id: source_id.clone(),
            target_id: target_id.clone(),
            source_provider,
            target_provider,
            confidence,
            evidence,
            description: format!(
                "Cross-provider role trust between {} and {}",
                source_id, target_id
            ),
        });
    }
    Ok(())
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
