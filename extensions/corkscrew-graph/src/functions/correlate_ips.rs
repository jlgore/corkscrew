use duckdb::core::{DataChunkHandle, Inserter, LogicalTypeHandle, LogicalTypeId};
use duckdb::vtab::{BindInfo, InitInfo, TableFunctionInfo, VTab};
use duckdb::{Connection, Result};
use serde_json::json;
use std::collections::{HashMap, HashSet};
use std::ffi::CString;
use std::net::{IpAddr, Ipv4Addr, Ipv6Addr};
use std::sync::atomic::AtomicUsize;

use crate::functions::common::{chunk_capacity, next_chunk};

#[repr(C)]
pub struct CorrelateIPsBindData {
    pub db_path: String,
    pub min_confidence: f64,
}

#[derive(Clone, Debug)]
struct IPRecord {
    record_id: String,
    ip_address: String,
    ip_type: String,
    resource_id: String,
    resource_type: String,
    resource_name: String,
    provider: String,
    region: String,
    account_id: String,
    vpc_id: String,
    subnet_id: String,
    network_interface_id: String,
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

pub struct CorrelateIPsInitData {
    cursor: AtomicUsize,
    rows: Vec<CorrelationRow>,
}

pub struct GraphCorrelateIPsVTab;

impl VTab for GraphCorrelateIPsVTab {
    type InitData = CorrelateIPsInitData;
    type BindData = CorrelateIPsBindData;

    fn bind(bind: &BindInfo) -> Result<Self::BindData, Box<dyn std::error::Error>> {
        bind.add_result_column(
            "correlation_id",
            LogicalTypeHandle::from(LogicalTypeId::Varchar),
        );
        bind.add_result_column(
            "correlation_type",
            LogicalTypeHandle::from(LogicalTypeId::Varchar),
        );
        bind.add_result_column("source_id", LogicalTypeHandle::from(LogicalTypeId::Varchar));
        bind.add_result_column("target_id", LogicalTypeHandle::from(LogicalTypeId::Varchar));
        bind.add_result_column(
            "source_provider",
            LogicalTypeHandle::from(LogicalTypeId::Varchar),
        );
        bind.add_result_column(
            "target_provider",
            LogicalTypeHandle::from(LogicalTypeId::Varchar),
        );
        bind.add_result_column("confidence", LogicalTypeHandle::from(LogicalTypeId::Double));
        bind.add_result_column("evidence", LogicalTypeHandle::from(LogicalTypeId::Varchar));
        bind.add_result_column(
            "description",
            LogicalTypeHandle::from(LogicalTypeId::Varchar),
        );

        Ok(CorrelateIPsBindData {
            db_path: bind.get_parameter(0).to_string(),
            min_confidence: bind.get_parameter(1).to_string().parse::<f64>()?,
        })
    }

    fn init(init: &InitInfo) -> Result<Self::InitData, Box<dyn std::error::Error>> {
        let bind_ref = unsafe {
            init.get_bind_data::<CorrelateIPsBindData>()
                .as_ref()
                .unwrap()
        };
        let conn = Connection::open(&bind_ref.db_path)?;
        let rows = collect_ip_correlations(&conn, bind_ref.min_confidence)?;
        Ok(CorrelateIPsInitData {
            cursor: AtomicUsize::new(0),
            rows,
        })
    }

    fn func(
        func: &TableFunctionInfo<Self>,
        output: &mut DataChunkHandle,
    ) -> Result<(), Box<dyn std::error::Error>> {
        let init_data = func.get_init_data();
        let Some((start, end)) =
            next_chunk(&init_data.cursor, init_data.rows.len(), chunk_capacity())
        else {
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
        let confidence_slice = v6.as_mut_slice::<f64>();

        for (out_idx, row) in init_data.rows[start..end].iter().enumerate() {
            v0.insert(out_idx, CString::new(row.correlation_id.clone())?);
            v1.insert(out_idx, CString::new(row.correlation_type.clone())?);
            v2.insert(out_idx, CString::new(row.source_id.clone())?);
            v3.insert(out_idx, CString::new(row.target_id.clone())?);
            v4.insert(out_idx, CString::new(row.source_provider.clone())?);
            v5.insert(out_idx, CString::new(row.target_provider.clone())?);
            confidence_slice[out_idx] = row.confidence;
            v7.insert(out_idx, CString::new(row.evidence.clone())?);
            v8.insert(out_idx, CString::new(row.description.clone())?);
        }
        output.set_len(end - start);
        Ok(())
    }

    fn parameters() -> Option<Vec<LogicalTypeHandle>> {
        Some(vec![
            LogicalTypeHandle::from(LogicalTypeId::Varchar),
            LogicalTypeHandle::from(LogicalTypeId::Double),
        ])
    }
}

fn collect_ip_correlations(
    conn: &Connection,
    min_confidence: f64,
) -> Result<Vec<CorrelationRow>, Box<dyn std::error::Error>> {
    if !table_exists(conn, "cross_cloud_ip_addresses")? {
        return Ok(Vec::new());
    }

    let mut stmt = conn.prepare(
        "SELECT
            id, ip_address, ip_type, resource_id, resource_type, resource_name,
            provider, region, account_id, vpc_id, subnet_id, network_interface_id
         FROM cross_cloud_ip_addresses
         WHERE ip_address IS NOT NULL AND resource_id IS NOT NULL AND provider IS NOT NULL
         ORDER BY ip_address, provider, resource_id, id",
    )?;
    let mut rows = stmt.query([])?;
    let mut by_ip: HashMap<String, Vec<IPRecord>> = HashMap::new();

    while let Some(row) = rows.next()? {
        let ip_address: Option<String> = row.get(1)?;
        let resource_id: Option<String> = row.get(3)?;
        let provider: Option<String> = row.get(6)?;
        let Some(ip_address) = clean_string(ip_address) else {
            continue;
        };
        let Some(resource_id) = clean_string(resource_id) else {
            continue;
        };
        let Some(provider) = clean_string(provider) else {
            continue;
        };

        let record = IPRecord {
            record_id: row.get::<_, Option<String>>(0)?.unwrap_or_default(),
            ip_address: ip_address.clone(),
            ip_type: row.get::<_, Option<String>>(2)?.unwrap_or_default(),
            resource_id,
            resource_type: row.get::<_, Option<String>>(4)?.unwrap_or_default(),
            resource_name: row.get::<_, Option<String>>(5)?.unwrap_or_default(),
            provider,
            region: row.get::<_, Option<String>>(7)?.unwrap_or_default(),
            account_id: row.get::<_, Option<String>>(8)?.unwrap_or_default(),
            vpc_id: row.get::<_, Option<String>>(9)?.unwrap_or_default(),
            subnet_id: row.get::<_, Option<String>>(10)?.unwrap_or_default(),
            network_interface_id: row.get::<_, Option<String>>(11)?.unwrap_or_default(),
        };
        by_ip.entry(ip_address).or_default().push(record);
    }

    let threshold = min_confidence.clamp(0.0, 1.0);
    let mut seen = HashSet::new();
    let mut correlations = Vec::new();

    for records in by_ip.values() {
        for i in 0..records.len() {
            for j in (i + 1)..records.len() {
                let left = &records[i];
                let right = &records[j];
                if left.provider == right.provider || left.resource_id == right.resource_id {
                    continue;
                }

                let (source, target) = ordered_pair(left, right);
                let confidence = calculate_confidence(source, target);
                if confidence < threshold {
                    continue;
                }

                let correlation_id = format!(
                    "ip_match:{}:{}:{}",
                    source.ip_address, source.resource_id, target.resource_id
                );
                if !seen.insert(correlation_id.clone()) {
                    continue;
                }

                let evidence = json!({
                    "shared_ip_address": source.ip_address,
                    "ip_classification": ip_classification(&source.ip_address),
                    "source_ip_type": source.ip_type,
                    "target_ip_type": target.ip_type,
                    "source_record_id": source.record_id,
                    "target_record_id": target.record_id,
                    "source_resource_type": source.resource_type,
                    "target_resource_type": target.resource_type,
                    "source_region": source.region,
                    "target_region": target.region,
                    "source_account_id": source.account_id,
                    "target_account_id": target.account_id,
                    "source_vpc_id": source.vpc_id,
                    "target_vpc_id": target.vpc_id,
                    "source_subnet_id": source.subnet_id,
                    "target_subnet_id": target.subnet_id,
                    "source_network_interface_id": source.network_interface_id,
                    "target_network_interface_id": target.network_interface_id,
                })
                .to_string();

                correlations.push(CorrelationRow {
                    correlation_id,
                    correlation_type: "ip_match".to_string(),
                    source_id: source.resource_id.clone(),
                    target_id: target.resource_id.clone(),
                    source_provider: source.provider.clone(),
                    target_provider: target.provider.clone(),
                    confidence,
                    evidence,
                    description: format!(
                        "Resources {} and {} share IP address {}",
                        display_name(source),
                        display_name(target),
                        source.ip_address
                    ),
                });
            }
        }
    }

    correlations.sort_by(|a, b| {
        b.confidence
            .partial_cmp(&a.confidence)
            .unwrap_or(std::cmp::Ordering::Equal)
            .then_with(|| a.source_provider.cmp(&b.source_provider))
            .then_with(|| a.target_provider.cmp(&b.target_provider))
            .then_with(|| a.source_id.cmp(&b.source_id))
            .then_with(|| a.target_id.cmp(&b.target_id))
    });
    Ok(correlations)
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
        .map(|value| value.trim().to_string())
        .filter(|value| !value.is_empty())
}

fn ordered_pair<'a>(left: &'a IPRecord, right: &'a IPRecord) -> (&'a IPRecord, &'a IPRecord) {
    let left_key = (&left.provider, &left.resource_id, &left.record_id);
    let right_key = (&right.provider, &right.resource_id, &right.record_id);
    if left_key <= right_key {
        (left, right)
    } else {
        (right, left)
    }
}

fn calculate_confidence(source: &IPRecord, target: &IPRecord) -> f64 {
    let mut confidence: f64 = 0.5;
    if is_public_ip(&source.ip_address) {
        confidence += 0.3;
    }
    if is_allocated_type(&source.ip_type) || is_allocated_type(&target.ip_type) {
        confidence += 0.2;
    }
    confidence.min(1.0)
}

fn is_allocated_type(value: &str) -> bool {
    matches!(
        value.to_ascii_lowercase().as_str(),
        "elastic" | "reserved" | "static"
    )
}

fn ip_classification(value: &str) -> &'static str {
    if is_public_ip(value) {
        "public"
    } else {
        "private_or_reserved"
    }
}

fn is_public_ip(value: &str) -> bool {
    match value.parse::<IpAddr>() {
        Ok(IpAddr::V4(ip)) => is_public_ipv4(ip),
        Ok(IpAddr::V6(ip)) => is_public_ipv6(ip),
        Err(_) => false,
    }
}

fn is_public_ipv4(ip: Ipv4Addr) -> bool {
    !(ip.is_private()
        || ip.is_loopback()
        || ip.is_link_local()
        || ip.is_broadcast()
        || ip.is_documentation()
        || ip.is_unspecified()
        || ip.is_multicast())
}

fn is_public_ipv6(ip: Ipv6Addr) -> bool {
    !(ip.is_loopback()
        || ip.is_unspecified()
        || ip.is_multicast()
        || is_unique_local_ipv6(ip)
        || is_unicast_link_local_ipv6(ip))
}

fn is_unique_local_ipv6(ip: Ipv6Addr) -> bool {
    (ip.segments()[0] & 0xfe00) == 0xfc00
}

fn is_unicast_link_local_ipv6(ip: Ipv6Addr) -> bool {
    (ip.segments()[0] & 0xffc0) == 0xfe80
}

fn display_name(record: &IPRecord) -> &str {
    if record.resource_name.is_empty() {
        &record.resource_id
    } else {
        &record.resource_name
    }
}
