use duckdb::core::{DataChunkHandle, Inserter, LogicalTypeHandle, LogicalTypeId};
use duckdb::vtab::{BindInfo, InitInfo, TableFunctionInfo, VTab};
use duckdb::{Connection, Result};
use serde_json::{json, Value};
use std::collections::{HashMap, HashSet};
use std::ffi::CString;
use std::sync::atomic::AtomicUsize;

use crate::functions::common::{chunk_capacity, next_chunk};

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

pub struct CorrelateLoadBalancersInitData {
    cursor: AtomicUsize,
    rows: Vec<CorrelationRow>,
}
pub struct GraphCorrelateLoadBalancersVTab;

impl VTab for GraphCorrelateLoadBalancersVTab {
    type InitData = CorrelateLoadBalancersInitData;
    type BindData = CorrelateLoadBalancersBindData;

    fn bind(bind: &BindInfo) -> Result<Self::BindData, Box<dyn std::error::Error>> {
        add_standard_columns(bind);
        Ok(CorrelateLoadBalancersBindData {
            db_path: bind.get_parameter(0).to_string(),
            min_confidence: bind.get_parameter(1).to_string().parse::<f64>()?,
        })
    }

    fn init(init: &InitInfo) -> Result<Self::InitData, Box<dyn std::error::Error>> {
        let bind_ref = unsafe {
            init.get_bind_data::<CorrelateLoadBalancersBindData>()
                .as_ref()
                .unwrap()
        };
        let conn = Connection::open(&bind_ref.db_path)?;
        let rows = collect_lb_correlations(&conn, bind_ref.min_confidence)?;
        Ok(CorrelateLoadBalancersInitData {
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

fn collect_lb_correlations(
    conn: &Connection,
    min_confidence: f64,
) -> Result<Vec<CorrelationRow>, Box<dyn std::error::Error>> {
    if !table_exists(conn, "cross_cloud_loadbalancer_topology")? {
        return Ok(Vec::new());
    }
    let mut records = load_lb_records(conn)?;
    attach_dns_tokens(conn, &mut records)?;

    let threshold = min_confidence.clamp(0.0, 1.0);
    let mut out = Vec::new();
    let mut seen = HashSet::new();
    correlate_token_groups(&records, false, threshold, &mut seen, &mut out);
    correlate_token_groups(&records, true, threshold, &mut seen, &mut out);
    out.sort_by(sort_rows);
    Ok(out)
}

fn load_lb_records(conn: &Connection) -> Result<Vec<LBRecord>, Box<dyn std::error::Error>> {
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

fn attach_dns_tokens(
    conn: &Connection,
    records: &mut [LBRecord],
) -> Result<(), Box<dyn std::error::Error>> {
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
) {
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
    for (token, group) in by_token {
        for i in 0..group.len() {
            for j in (i + 1)..group.len() {
                let left = group[i];
                let right = group[j];
                if left.provider == right.provider || left.lb_id == right.lb_id {
                    continue;
                }
                let (source, target) = ordered_pair(left, right);
                let confidence = if dns { 0.8 } else { 0.9 };
                if confidence < threshold {
                    continue;
                }
                let kind = if dns {
                    "load_balancer_dns_target_match"
                } else {
                    "load_balancer_backend_match"
                };
                let correlation_id =
                    format!("{}:{}:{}:{}", kind, token, source.lb_id, target.lb_id);
                if !seen.insert(correlation_id.clone()) {
                    continue;
                }
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
                out.push(CorrelationRow {
                    correlation_id,
                    correlation_type: kind.to_string(),
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
                });
            }
        }
    }
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

fn table_exists(conn: &Connection, table_name: &str) -> Result<bool, Box<dyn std::error::Error>> {
    let mut stmt = conn.prepare(
        "SELECT COUNT(*) FROM information_schema.tables WHERE lower(table_name) = lower(?)",
    )?;
    let count: i64 = stmt.query_row([table_name], |row| row.get(0))?;
    Ok(count > 0)
}

fn clean_string(value: Option<String>) -> Option<String> {
    value
        .map(|v| v.trim().to_string())
        .filter(|v| !v.is_empty())
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
