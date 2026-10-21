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
use crate::functions::params::{bind_data, guard, param_f64, param_varchar, BoxError};

#[repr(C)]
pub struct CorrelateLoadBalancersBindData {
    pub db_path: String,
    pub min_confidence: f64,
}

#[derive(Clone, Debug)]
struct LBRecord {
    id: String,
    lb_id: String,
    name: String,
    lb_type: String,
    provider: String,
    region: String,
    backend_tokens: Vec<String>,
    dns_tokens: Vec<String>,
}

impl GroupMember for LBRecord {
    fn provider(&self) -> &str {
        &self.provider
    }
}

pub struct CorrelateLoadBalancersInitData {
    cursor: AtomicUsize,
    rows: Vec<CorrelationRow>,
}
pub struct GraphCorrelateLoadBalancersVTab;

impl VTab for GraphCorrelateLoadBalancersVTab {
    type InitData = CorrelateLoadBalancersInitData;
    type BindData = CorrelateLoadBalancersBindData;

    fn bind(bind: &BindInfo) -> Result<Self::BindData, BoxError> {
        guard("GraphCorrelateLoadBalancersVTab::bind", || {
            add_standard_columns(bind);
            Ok(CorrelateLoadBalancersBindData {
                db_path: param_varchar(bind, 0, "db_path")?,
                min_confidence: param_f64(bind, 1, "min_confidence")?,
            })
        })
    }

    fn init(init: &InitInfo) -> Result<Self::InitData, BoxError> {
        guard("GraphCorrelateLoadBalancersVTab::init", || {
            let bind_ref = bind_data::<Self>(init)?;
            let conn = Connection::open(&bind_ref.db_path)?;
            let rows = collect_lb_correlations(&conn, bind_ref.min_confidence)?;
            Ok(CorrelateLoadBalancersInitData {
                cursor: AtomicUsize::new(0),
                rows,
            })
        })
    }

    fn func(func: &TableFunctionInfo<Self>, output: &mut DataChunkHandle) -> Result<(), BoxError> {
        guard("GraphCorrelateLoadBalancersVTab::func", || {
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

fn collect_lb_correlations(
    conn: &Connection,
    min_confidence: f64,
) -> Result<Vec<CorrelationRow>, BoxError> {
    if !table_exists(conn, "cross_cloud_loadbalancer_topology")? {
        return Ok(Vec::new());
    }
    let mut records = load_lb_records(conn)?;
    attach_dns_tokens(conn, &mut records)?;

    let threshold = min_confidence.clamp(0.0, 1.0);
    let mut out = Vec::new();
    let mut seen = HashSet::new();
    correlate_token_groups(&records, false, threshold, &mut seen, &mut out)?;
    correlate_token_groups(&records, true, threshold, &mut seen, &mut out)?;
    out.sort_by(sort_rows);
    Ok(out)
}

fn load_lb_records(conn: &Connection) -> Result<Vec<LBRecord>, BoxError> {
    let mut stmt = conn.prepare(
        "SELECT id, loadbalancer_id, loadbalancer_name, loadbalancer_type, provider, region, backend_targets
         FROM cross_cloud_loadbalancer_topology
         WHERE loadbalancer_id IS NOT NULL AND provider IS NOT NULL
         ORDER BY provider, loadbalancer_id, id",
    )?;
    let mut rows = stmt.query([])?;
    let mut records = Vec::new();
    while let Some(row) = rows.next()? {
        let id: String = row.get::<_, Option<String>>(0)?.unwrap_or_default();
        records.push(LBRecord {
            id: id.clone(),
            lb_id: row.get::<_, Option<String>>(1)?.unwrap_or(id),
            name: row.get::<_, Option<String>>(2)?.unwrap_or_default(),
            lb_type: row.get::<_, Option<String>>(3)?.unwrap_or_default(),
            provider: row.get::<_, Option<String>>(4)?.unwrap_or_default(),
            region: row.get::<_, Option<String>>(5)?.unwrap_or_default(),
            backend_tokens: extract_tokens(row.get::<_, Option<String>>(6)?),
            dns_tokens: Vec::new(),
        });
    }
    Ok(records)
}

fn attach_dns_tokens(conn: &Connection, records: &mut [LBRecord]) -> Result<(), BoxError> {
    if !table_exists(conn, "cross_cloud_dns_records")? {
        return Ok(());
    }
    let mut by_id: HashMap<String, usize> = HashMap::new();
    for (idx, record) in records.iter().enumerate() {
        by_id.insert(record.lb_id.clone(), idx);
    }
    let mut stmt = conn.prepare("SELECT resource_id, dns_name, record_values FROM cross_cloud_dns_records WHERE resource_id IS NOT NULL")?;
    let mut rows = stmt.query([])?;
    while let Some(row) = rows.next()? {
        let Some(resource_id) = clean_string(row.get::<_, Option<String>>(0)?) else {
            continue;
        };
        let Some(idx) = by_id.get(&resource_id).copied() else {
            continue;
        };
        if let Some(name) = clean_token(row.get::<_, Option<String>>(1)?) {
            records[idx].dns_tokens.push(name);
        }
        records[idx]
            .dns_tokens
            .extend(extract_tokens(row.get::<_, Option<String>>(2)?));
    }
    Ok(())
}

fn correlate_token_groups(
    records: &[LBRecord],
    dns: bool,
    threshold: f64,
    seen: &mut HashSet<String>,
    out: &mut Vec<CorrelationRow>,
) -> Result<(), BoxError> {
    let mut by_token: HashMap<String, Vec<&LBRecord>> = HashMap::new();
    for record in records {
        let tokens = if dns {
            &record.dns_tokens
        } else {
            &record.backend_tokens
        };
        for token in tokens {
            by_token.entry(token.clone()).or_default().push(record);
        }
    }
    let kind = if dns {
        "load_balancer_dns_target_match"
    } else {
        "load_balancer_backend_match"
    };
    for (token, group) in &by_token {
        let key = GroupKey { kind, value: token };
        walk_group_pairs(
            group,
            key,
            CorrelationLimits::DEFAULT,
            out,
            |left, right| {
                let row = lb_pair_row(left, right, key, dns, threshold)?;
                seen.insert(row.correlation_id.clone()).then_some(row)
            },
        )?;
    }
    Ok(())
}

/// The correlation row for one pair of load balancers sharing `key.value`, or
/// `None` when the pair is same-provider, same-balancer, or below `threshold`.
fn lb_pair_row(
    left: &LBRecord,
    right: &LBRecord,
    key: GroupKey<'_>,
    dns: bool,
    threshold: f64,
) -> Option<CorrelationRow> {
    if left.provider == right.provider || left.lb_id == right.lb_id {
        return None;
    }
    let (source, target) = ordered_pair(left, right);
    let confidence = if dns { 0.8 } else { 0.9 };
    if confidence < threshold {
        return None;
    }
    let token = key.value;
    let evidence = json!({
        "shared_target": token,
        "source_topology_id": source.id,
        "target_topology_id": target.id,
        "source_name": source.name,
        "target_name": target.name,
        "source_loadbalancer_type": source.lb_type,
        "target_loadbalancer_type": target.lb_type,
        "source_region": source.region,
        "target_region": target.region,
        "match_source": if dns { "cross_cloud_dns_records" } else { "cross_cloud_loadbalancer_topology.backend_targets" }
    }).to_string();
    Some(CorrelationRow {
        correlation_id: format!("{}:{}:{}:{}", key.kind, token, source.lb_id, target.lb_id),
        correlation_type: key.kind.to_string(),
        source_id: source.lb_id.clone(),
        target_id: target.lb_id.clone(),
        source_provider: source.provider.clone(),
        target_provider: target.provider.clone(),
        confidence,
        evidence,
        description: format!(
            "Load balancers {} and {} share target {}",
            display_name(source),
            display_name(target),
            token
        ),
    })
}

fn extract_tokens(value: Option<String>) -> Vec<String> {
    let Some(value) = value else {
        return Vec::new();
    };
    let Ok(parsed) = serde_json::from_str::<Value>(&value) else {
        return clean_token(Some(value)).into_iter().collect();
    };
    let mut out = Vec::new();
    collect_json_tokens(&parsed, &mut out);
    out.sort();
    out.dedup();
    out
}

fn collect_json_tokens(value: &Value, out: &mut Vec<String>) {
    match value {
        Value::String(s) => {
            if let Some(token) = clean_token(Some(s.clone())) {
                out.push(token);
            }
        }
        Value::Array(values) => values.iter().for_each(|v| collect_json_tokens(v, out)),
        Value::Object(map) => {
            for (key, value) in map {
                let key = key.to_ascii_lowercase();
                if matches!(
                    key.as_str(),
                    "target"
                        | "target_id"
                        | "backend"
                        | "backend_id"
                        | "resource_id"
                        | "ip"
                        | "ip_address"
                        | "address"
                        | "dns"
                        | "dns_name"
                        | "hostname"
                        | "host"
                ) {
                    collect_json_tokens(value, out);
                }
            }
        }
        _ => {}
    }
}

fn clean_token(value: Option<String>) -> Option<String> {
    clean_string(value)
        .map(|v| v.trim_end_matches('.').to_ascii_lowercase())
        .filter(|v| !v.is_empty())
}
fn ordered_pair<'a>(left: &'a LBRecord, right: &'a LBRecord) -> (&'a LBRecord, &'a LBRecord) {
    if (&left.provider, &left.lb_id, &left.id) <= (&right.provider, &right.lb_id, &right.id) {
        (left, right)
    } else {
        (right, left)
    }
}
fn display_name(record: &LBRecord) -> &str {
    if record.name.is_empty() {
        &record.lb_id
    } else {
        &record.name
    }
}
