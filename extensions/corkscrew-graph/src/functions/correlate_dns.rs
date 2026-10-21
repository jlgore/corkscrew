use duckdb::core::{DataChunkHandle, LogicalTypeHandle, LogicalTypeId};
use duckdb::vtab::{BindInfo, InitInfo, TableFunctionInfo, VTab};
use duckdb::{Connection, Result};
use serde_json::{json, Value};
use std::collections::{HashMap, HashSet};
use std::sync::atomic::AtomicUsize;

use crate::functions::correlate_common::{
    add_standard_columns, clean_string, emit_rows, sort_rows, table_exists, walk_group_pairs,
    CorrelationLimits, CorrelationRow, GroupKey, GroupMember,
};
use crate::functions::params::{bind_data, guard, param_bool, param_f64, param_varchar, BoxError};

#[repr(C)]
pub struct CorrelateDNSBindData {
    pub db_path: String,
    pub include_cname: bool,
    pub min_confidence: f64,
}

#[derive(Clone, Debug)]
struct DNSRecord {
    record_id: String,
    dns_name: String,
    record_type: String,
    record_values: Vec<String>,
    resource_id: String,
    resource_type: String,
    resource_name: String,
    provider: String,
    region: String,
    account_id: String,
    dns_service: String,
    zone_id: String,
    zone_name: String,
}

impl GroupMember for DNSRecord {
    fn provider(&self) -> &str {
        &self.provider
    }
}

pub struct CorrelateDNSInitData {
    cursor: AtomicUsize,
    rows: Vec<CorrelationRow>,
}

pub struct GraphCorrelateDNSVTab;

impl VTab for GraphCorrelateDNSVTab {
    type InitData = CorrelateDNSInitData;
    type BindData = CorrelateDNSBindData;

    fn bind(bind: &BindInfo) -> Result<Self::BindData, BoxError> {
        guard("GraphCorrelateDNSVTab::bind", || {
            add_standard_columns(bind);
            Ok(CorrelateDNSBindData {
                db_path: param_varchar(bind, 0, "db_path")?,
                include_cname: param_bool(bind, 1, "include_cname")?,
                min_confidence: param_f64(bind, 2, "min_confidence")?,
            })
        })
    }

    fn init(init: &InitInfo) -> Result<Self::InitData, BoxError> {
        guard("GraphCorrelateDNSVTab::init", || {
            let bind_ref = bind_data::<Self>(init)?;
            let conn = Connection::open(&bind_ref.db_path)?;
            let rows =
                collect_dns_correlations(&conn, bind_ref.include_cname, bind_ref.min_confidence)?;
            Ok(CorrelateDNSInitData {
                cursor: AtomicUsize::new(0),
                rows,
            })
        })
    }

    fn func(func: &TableFunctionInfo<Self>, output: &mut DataChunkHandle) -> Result<(), BoxError> {
        guard("GraphCorrelateDNSVTab::func", || {
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
            LogicalTypeHandle::from(LogicalTypeId::Boolean),
            LogicalTypeHandle::from(LogicalTypeId::Double),
        ])
    }
}

fn collect_dns_correlations(
    conn: &Connection,
    include_cname: bool,
    min_confidence: f64,
) -> Result<Vec<CorrelationRow>, BoxError> {
    if !table_exists(conn, "cross_cloud_dns_records")? {
        return Ok(Vec::new());
    }

    let mut stmt = conn.prepare(
        "SELECT id, dns_name, record_type, record_values, resource_id, resource_type, resource_name,
                provider, region, account_id, dns_service, zone_id, zone_name
         FROM cross_cloud_dns_records
         WHERE dns_name IS NOT NULL AND provider IS NOT NULL
         ORDER BY dns_name, provider, resource_id, id",
    )?;
    let mut rows = stmt.query([])?;
    let mut records = Vec::new();
    while let Some(row) = rows.next()? {
        let Some(dns_name) = clean_dns(row.get::<_, Option<String>>(1)?) else {
            continue;
        };
        let Some(provider) = clean_string(row.get::<_, Option<String>>(7)?) else {
            continue;
        };
        let record_id = row.get::<_, Option<String>>(0)?.unwrap_or_default();
        records.push(DNSRecord {
            record_id: record_id.clone(),
            dns_name,
            record_type: row.get::<_, Option<String>>(2)?.unwrap_or_default(),
            record_values: parse_json_strings(row.get::<_, Option<String>>(3)?),
            resource_id: clean_string(row.get::<_, Option<String>>(4)?).unwrap_or(record_id),
            resource_type: row.get::<_, Option<String>>(5)?.unwrap_or_default(),
            resource_name: row.get::<_, Option<String>>(6)?.unwrap_or_default(),
            provider,
            region: row.get::<_, Option<String>>(8)?.unwrap_or_default(),
            account_id: row.get::<_, Option<String>>(9)?.unwrap_or_default(),
            dns_service: row.get::<_, Option<String>>(10)?.unwrap_or_default(),
            zone_id: row.get::<_, Option<String>>(11)?.unwrap_or_default(),
            zone_name: row.get::<_, Option<String>>(12)?.unwrap_or_default(),
        });
    }

    let threshold = min_confidence.clamp(0.0, 1.0);
    let mut out = Vec::new();
    let mut seen = HashSet::new();
    let mut by_name: HashMap<String, Vec<&DNSRecord>> = HashMap::new();
    for record in &records {
        by_name
            .entry(record.dns_name.clone())
            .or_default()
            .push(record);
    }
    for (dns_name, group) in by_name {
        let key = GroupKey {
            kind: "dns_name_match",
            value: &dns_name,
        };
        correlate_pairs(&group, key, threshold, &mut seen, &mut out)?;
    }

    if include_cname {
        let mut by_cname_target: HashMap<String, Vec<&DNSRecord>> = HashMap::new();
        for record in &records {
            if !record.record_type.eq_ignore_ascii_case("CNAME") {
                continue;
            }
            for value in &record.record_values {
                if let Some(target) = clean_dns(Some(value.clone())) {
                    by_cname_target.entry(target).or_default().push(record);
                }
            }
        }
        for (target, group) in by_cname_target {
            let key = GroupKey {
                kind: "dns_cname_target_match",
                value: &target,
            };
            correlate_pairs(&group, key, threshold, &mut seen, &mut out)?;
        }
    }

    out.sort_by(sort_rows);
    Ok(out)
}

fn correlate_pairs(
    group: &[&DNSRecord],
    key: GroupKey<'_>,
    threshold: f64,
    seen: &mut HashSet<String>,
    out: &mut Vec<CorrelationRow>,
) -> Result<(), BoxError> {
    walk_group_pairs(
        group,
        key,
        CorrelationLimits::DEFAULT,
        out,
        |left, right| {
            let row = dns_pair_row(left, right, key, threshold)?;
            seen.insert(row.correlation_id.clone()).then_some(row)
        },
    )
}

/// The correlation row for one pair of DNS records sharing `key.value`, or
/// `None` when the pair is same-provider, same-resource, or below `threshold`.
fn dns_pair_row(
    left: &DNSRecord,
    right: &DNSRecord,
    key: GroupKey<'_>,
    threshold: f64,
) -> Option<CorrelationRow> {
    if left.provider == right.provider || left.resource_id == right.resource_id {
        return None;
    }
    let (source, target) = ordered_pair(left, right);
    let confidence = dns_confidence(source, target, key.kind);
    if confidence < threshold {
        return None;
    }
    let evidence = json!({
        "match_value": key.value,
        "source_record_id": source.record_id,
        "target_record_id": target.record_id,
        "source_dns_name": source.dns_name,
        "target_dns_name": target.dns_name,
        "source_record_type": source.record_type,
        "target_record_type": target.record_type,
        "source_record_values": source.record_values,
        "target_record_values": target.record_values,
        "source_resource_type": source.resource_type,
        "target_resource_type": target.resource_type,
        "source_region": source.region,
        "target_region": target.region,
        "source_account_id": source.account_id,
        "target_account_id": target.account_id,
        "source_dns_service": source.dns_service,
        "target_dns_service": target.dns_service,
        "source_zone_id": source.zone_id,
        "target_zone_id": target.zone_id,
        "source_zone_name": source.zone_name,
        "target_zone_name": target.zone_name,
    })
    .to_string();
    Some(CorrelationRow {
        correlation_id: format!(
            "{}:{}:{}:{}",
            key.kind, key.value, source.resource_id, target.resource_id
        ),
        correlation_type: key.kind.to_string(),
        source_id: source.resource_id.clone(),
        target_id: target.resource_id.clone(),
        source_provider: source.provider.clone(),
        target_provider: target.provider.clone(),
        confidence,
        evidence,
        description: format!(
            "DNS records {} and {} share {}",
            display_name(source),
            display_name(target),
            key.value
        ),
    })
}

fn dns_confidence(source: &DNSRecord, target: &DNSRecord, kind: &str) -> f64 {
    let mut confidence: f64 = if kind == "dns_name_match" { 0.85 } else { 0.8 };
    if source.record_type.eq_ignore_ascii_case(&target.record_type) {
        confidence += 0.05;
    }
    if !source.zone_name.is_empty() && source.zone_name.eq_ignore_ascii_case(&target.zone_name) {
        confidence += 0.05;
    }
    if values_overlap(&source.record_values, &target.record_values) {
        confidence += 0.05;
    }
    confidence.min(1.0)
}

fn values_overlap(left: &[String], right: &[String]) -> bool {
    let right_set: HashSet<String> = right
        .iter()
        .filter_map(|v| clean_dns(Some(v.clone())))
        .collect();
    left.iter()
        .filter_map(|v| clean_dns(Some(v.clone())))
        .any(|v| right_set.contains(&v))
}

fn parse_json_strings(value: Option<String>) -> Vec<String> {
    let Some(value) = value else {
        return Vec::new();
    };
    let Ok(parsed) = serde_json::from_str::<Value>(&value) else {
        return vec![value];
    };
    match parsed {
        Value::Array(values) => values.into_iter().filter_map(json_to_string).collect(),
        other => json_to_string(other).into_iter().collect(),
    }
}

fn json_to_string(value: Value) -> Option<String> {
    match value {
        Value::String(s) => clean_string(Some(s)),
        Value::Number(n) => Some(n.to_string()),
        _ => None,
    }
}

fn clean_dns(value: Option<String>) -> Option<String> {
    clean_string(value)
        .map(|v| v.trim_end_matches('.').to_ascii_lowercase())
        .filter(|v| !v.is_empty())
}

fn ordered_pair<'a>(left: &'a DNSRecord, right: &'a DNSRecord) -> (&'a DNSRecord, &'a DNSRecord) {
    if (&left.provider, &left.resource_id, &left.record_id)
        <= (&right.provider, &right.resource_id, &right.record_id)
    {
        (left, right)
    } else {
        (right, left)
    }
}

fn display_name(record: &DNSRecord) -> &str {
    if record.resource_name.is_empty() {
        &record.resource_id
    } else {
        &record.resource_name
    }
}
