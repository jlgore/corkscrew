use duckdb::core::{DataChunkHandle, LogicalTypeHandle, LogicalTypeId};
use duckdb::vtab::{BindInfo, InitInfo, TableFunctionInfo, VTab};
use duckdb::{Connection, Result};
use serde_json::json;
use std::collections::{HashMap, HashSet};
use std::sync::atomic::AtomicUsize;

use crate::functions::correlate_common::{
    add_standard_columns, clean_string, emit_rows, ordered_values, sort_rows, table_exists,
    walk_group_pairs, CorrelationLimits, CorrelationRow, GroupKey, GroupMember,
};
use crate::functions::params::{bind_data, guard, param_f64, param_varchar, BoxError};

#[repr(C)]
pub struct CorrelateDomainsBindData {
    pub db_path: String,
    pub min_confidence: f64,
}

#[derive(Clone, Debug)]
struct DNSOwner {
    id: String,
    domain: String,
    resource_id: String,
    provider: String,
    region: String,
    account_id: String,
    zone_id: String,
    zone_name: String,
}

impl GroupMember for DNSOwner {
    fn provider(&self) -> &str {
        &self.provider
    }
}

pub struct CorrelateDomainsInitData {
    cursor: AtomicUsize,
    rows: Vec<CorrelationRow>,
}
pub struct GraphCorrelateDomainsVTab;

impl VTab for GraphCorrelateDomainsVTab {
    type InitData = CorrelateDomainsInitData;
    type BindData = CorrelateDomainsBindData;

    fn bind(bind: &BindInfo) -> Result<Self::BindData, BoxError> {
        guard("GraphCorrelateDomainsVTab::bind", || {
            add_standard_columns(bind);
            Ok(CorrelateDomainsBindData {
                db_path: param_varchar(bind, 0, "db_path")?,
                min_confidence: param_f64(bind, 1, "min_confidence")?,
            })
        })
    }
    fn init(init: &InitInfo) -> Result<Self::InitData, BoxError> {
        guard("GraphCorrelateDomainsVTab::init", || {
            let bind_ref = bind_data::<Self>(init)?;
            let conn = Connection::open(&bind_ref.db_path)?;
            let rows = collect_domain_correlations(&conn, bind_ref.min_confidence)?;
            Ok(CorrelateDomainsInitData {
                cursor: AtomicUsize::new(0),
                rows,
            })
        })
    }
    fn func(func: &TableFunctionInfo<Self>, output: &mut DataChunkHandle) -> Result<(), BoxError> {
        guard("GraphCorrelateDomainsVTab::func", || {
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

fn collect_domain_correlations(
    conn: &Connection,
    min_confidence: f64,
) -> Result<Vec<CorrelationRow>, BoxError> {
    let threshold = min_confidence.clamp(0.0, 1.0);
    let mut out = Vec::new();
    let mut seen = HashSet::new();
    collect_dns_domain_matches(conn, threshold, &mut seen, &mut out)?;
    collect_certificate_matches(conn, threshold, &mut seen, &mut out)?;
    out.sort_by(sort_rows);
    Ok(out)
}

fn collect_dns_domain_matches(
    conn: &Connection,
    threshold: f64,
    seen: &mut HashSet<String>,
    out: &mut Vec<CorrelationRow>,
) -> Result<(), BoxError> {
    if !table_exists(conn, "cross_cloud_dns_records")? {
        return Ok(());
    }
    let mut stmt = conn.prepare(
        "SELECT id, dns_name, resource_id, provider, region, account_id, zone_id, zone_name
         FROM cross_cloud_dns_records WHERE provider IS NOT NULL ORDER BY zone_name, dns_name, provider, id",
    )?;
    let mut rows = stmt.query([])?;
    let mut by_domain: HashMap<String, Vec<DNSOwner>> = HashMap::new();
    while let Some(row) = rows.next()? {
        let id = row.get::<_, Option<String>>(0)?.unwrap_or_default();
        let zone_name = normalize_domain(row.get::<_, Option<String>>(7)?);
        let dns_name = normalize_domain(row.get::<_, Option<String>>(1)?);
        let Some(domain) = zone_name.or(dns_name) else {
            continue;
        };
        let Some(provider) = clean_string(row.get::<_, Option<String>>(3)?) else {
            continue;
        };
        by_domain.entry(domain.clone()).or_default().push(DNSOwner {
            id: id.clone(),
            domain,
            resource_id: clean_string(row.get::<_, Option<String>>(2)?).unwrap_or(id),
            provider,
            region: row.get::<_, Option<String>>(4)?.unwrap_or_default(),
            account_id: row.get::<_, Option<String>>(5)?.unwrap_or_default(),
            zone_id: row.get::<_, Option<String>>(6)?.unwrap_or_default(),
            zone_name: row.get::<_, Option<String>>(7)?.unwrap_or_default(),
        });
    }
    for (domain, owners) in &by_domain {
        let key = GroupKey {
            kind: "domain_dns_zone_match",
            value: domain,
        };
        walk_group_pairs(
            owners,
            key,
            CorrelationLimits::DEFAULT,
            out,
            |left, right| {
                let row = domain_pair_row(left, right, threshold)?;
                seen.insert(row.correlation_id.clone()).then_some(row)
            },
        )?;
    }
    Ok(())
}

/// The correlation row for one pair of DNS owners of the same domain, or `None`
/// when the pair is same-provider, same-resource, or below `threshold`.
fn domain_pair_row(left: &DNSOwner, right: &DNSOwner, threshold: f64) -> Option<CorrelationRow> {
    if left.provider == right.provider || left.resource_id == right.resource_id {
        return None;
    }
    let (source, target) = if (&left.provider, &left.resource_id, &left.id)
        <= (&right.provider, &right.resource_id, &right.id)
    {
        (left, right)
    } else {
        (right, left)
    };
    let confidence = 0.85;
    if confidence < threshold {
        return None;
    }
    assert!(source.domain == target.domain);
    let domain = &source.domain;
    let evidence = json!({ "table": "cross_cloud_dns_records", "domain": domain, "source_record_id": source.id, "target_record_id": target.id, "source_zone_id": source.zone_id, "target_zone_id": target.zone_id, "source_zone_name": source.zone_name, "target_zone_name": target.zone_name, "source_region": source.region, "target_region": target.region, "source_account_id": source.account_id, "target_account_id": target.account_id }).to_string();
    Some(CorrelationRow {
        correlation_id: format!(
            "domain_dns:{}:{}:{}",
            domain, source.resource_id, target.resource_id
        ),
        correlation_type: "domain_dns_zone_match".to_string(),
        source_id: source.resource_id.clone(),
        target_id: target.resource_id.clone(),
        source_provider: source.provider.clone(),
        target_provider: target.provider.clone(),
        confidence,
        evidence,
        description: format!("Cross-provider DNS ownership for domain {}", domain),
    })
}

fn collect_certificate_matches(
    conn: &Connection,
    threshold: f64,
    seen: &mut HashSet<String>,
    out: &mut Vec<CorrelationRow>,
) -> Result<(), BoxError> {
    if !table_exists(conn, "certificate_correlations")? {
        return Ok(());
    }
    let mut stmt = conn.prepare(
        "SELECT id, source_cert_id, source_cert_name, source_cert_thumbprint, source_cloud_provider, source_region, source_account_id, source_resource_id,
                target_cert_id, target_cert_name, target_cert_thumbprint, target_cloud_provider, target_region, target_account_id, target_resource_id,
                correlation_type, confidence_score, matching_attributes, source_common_name, source_sans, target_common_name, target_sans, shared_attributes, security_risk_level, status
         FROM certificate_correlations WHERE source_cloud_provider IS NOT NULL AND target_cloud_provider IS NOT NULL AND source_cloud_provider <> target_cloud_provider",
    )?;
    let mut rows = stmt.query([])?;
    while let Some(row) = rows.next()? {
        let confidence = row.get::<_, f64>(16)?;
        if confidence < threshold {
            continue;
        }
        let (source_id, target_id, source_provider, target_provider, swapped) =
            ordered_values(row.get(1)?, row.get(8)?, row.get(4)?, row.get(11)?);
        let id = row.get::<_, Option<String>>(0)?.unwrap_or_default();
        let cid = format!("domain_certificate:{}:{}:{}", id, source_id, target_id);
        if !seen.insert(cid.clone()) {
            continue;
        }
        let evidence = json!({
            "table": "certificate_correlations", "record_id": id, "correlation_type": row.get::<_, Option<String>>(15)?.unwrap_or_default(), "status": row.get::<_, Option<String>>(24)?.unwrap_or_default(),
            "source_cert_name": if swapped { row.get::<_, Option<String>>(9)?.unwrap_or_default() } else { row.get::<_, Option<String>>(2)?.unwrap_or_default() },
            "target_cert_name": if swapped { row.get::<_, Option<String>>(2)?.unwrap_or_default() } else { row.get::<_, Option<String>>(9)?.unwrap_or_default() },
            "source_thumbprint": if swapped { row.get::<_, Option<String>>(10)?.unwrap_or_default() } else { row.get::<_, Option<String>>(3)?.unwrap_or_default() },
            "target_thumbprint": if swapped { row.get::<_, Option<String>>(3)?.unwrap_or_default() } else { row.get::<_, Option<String>>(10)?.unwrap_or_default() },
            "source_common_name": if swapped { row.get::<_, Option<String>>(20)?.unwrap_or_default() } else { row.get::<_, Option<String>>(18)?.unwrap_or_default() },
            "target_common_name": if swapped { row.get::<_, Option<String>>(18)?.unwrap_or_default() } else { row.get::<_, Option<String>>(20)?.unwrap_or_default() },
            "source_sans": if swapped { row.get::<_, Option<String>>(21)?.unwrap_or_default() } else { row.get::<_, Option<String>>(19)?.unwrap_or_default() },
            "target_sans": if swapped { row.get::<_, Option<String>>(19)?.unwrap_or_default() } else { row.get::<_, Option<String>>(21)?.unwrap_or_default() },
            "matching_attributes": row.get::<_, Option<String>>(17)?.unwrap_or_default(), "shared_attributes": row.get::<_, Option<String>>(22)?.unwrap_or_default(), "security_risk_level": row.get::<_, Option<String>>(23)?.unwrap_or_default(),
            "source_resource_id": if swapped { row.get::<_, Option<String>>(14)?.unwrap_or_default() } else { row.get::<_, Option<String>>(7)?.unwrap_or_default() },
            "target_resource_id": if swapped { row.get::<_, Option<String>>(7)?.unwrap_or_default() } else { row.get::<_, Option<String>>(14)?.unwrap_or_default() }
        }).to_string();
        out.push(CorrelationRow {
            correlation_id: cid,
            correlation_type: "domain_certificate_match".to_string(),
            source_id: source_id.clone(),
            target_id: target_id.clone(),
            source_provider,
            target_provider,
            confidence,
            evidence,
            description: format!(
                "Cross-provider certificate/domain correlation between {} and {}",
                source_id, target_id
            ),
        });
    }
    Ok(())
}

fn normalize_domain(value: Option<String>) -> Option<String> {
    clean_string(value)
        .map(|v| v.trim_end_matches('.').to_ascii_lowercase())
        .filter(|v| !v.is_empty())
}
