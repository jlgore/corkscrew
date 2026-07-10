use duckdb::core::{DataChunkHandle, Inserter, LogicalTypeHandle, LogicalTypeId};
use duckdb::vtab::{BindInfo, InitInfo, TableFunctionInfo, VTab};
use duckdb::{Connection, Result};
use serde_json::json;
use std::collections::HashSet;
use std::ffi::CString;
use std::sync::atomic::AtomicUsize;

use crate::functions::common::{chunk_capacity, next_chunk};

#[repr(C)]
pub struct CorrelateConnectivityBindData {
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

pub struct CorrelateConnectivityInitData {
    cursor: AtomicUsize,
    rows: Vec<CorrelationRow>,
}

pub struct GraphCorrelateConnectivityVTab;

impl VTab for GraphCorrelateConnectivityVTab {
    type InitData = CorrelateConnectivityInitData;
    type BindData = CorrelateConnectivityBindData;

    fn bind(bind: &BindInfo) -> Result<Self::BindData, Box<dyn std::error::Error>> {
        add_standard_columns(bind);
        Ok(CorrelateConnectivityBindData {
            db_path: bind.get_parameter(0).to_string(),
            min_confidence: bind.get_parameter(1).to_string().parse::<f64>()?,
        })
    }

    fn init(init: &InitInfo) -> Result<Self::InitData, Box<dyn std::error::Error>> {
        let bind_ref = unsafe {
            init.get_bind_data::<CorrelateConnectivityBindData>()
                .as_ref()
                .unwrap()
        };
        let conn = Connection::open(&bind_ref.db_path)?;
        let rows = collect_connectivity_correlations(&conn, bind_ref.min_confidence)?;
        Ok(CorrelateConnectivityInitData {
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

fn collect_connectivity_correlations(
    conn: &Connection,
    min_confidence: f64,
) -> Result<Vec<CorrelationRow>, Box<dyn std::error::Error>> {
    let threshold = min_confidence.clamp(0.0, 1.0);
    let mut rows = Vec::new();
    let mut seen = HashSet::new();
    collect_vpn(conn, threshold, &mut seen, &mut rows)?;
    collect_peering(conn, threshold, &mut seen, &mut rows)?;
    collect_direct(conn, threshold, &mut seen, &mut rows)?;
    rows.sort_by(sort_rows);
    Ok(rows)
}

fn collect_vpn(
    conn: &Connection,
    threshold: f64,
    seen: &mut HashSet<String>,
    out: &mut Vec<CorrelationRow>,
) -> Result<(), Box<dyn std::error::Error>> {
    if !table_exists(conn, "cross_cloud_vpn_connections")? {
        return Ok(());
    }
    let mut stmt = conn.prepare(
        "SELECT id, connection_name, source_resource_id, source_provider, source_region, source_gateway_id, source_public_ip, source_local_networks,
                target_resource_id, target_provider, target_region, target_gateway_id, target_public_ip, target_remote_networks,
                connection_type, ike_version, encryption_algorithm, authentication_method, shared_key_configured, tunnel_count, routing_type,
                bgp_asn_source, bgp_asn_target, connection_status, confidence_score, correlation_method
         FROM cross_cloud_vpn_connections
         WHERE source_provider IS NOT NULL AND target_provider IS NOT NULL AND source_provider <> target_provider",
    )?;
    let mut rows = stmt.query([])?;
    while let Some(row) = rows.next()? {
        let status = row.get::<_, Option<String>>(23)?.unwrap_or_default();
        let confidence = row
            .get::<_, Option<f64>>(24)?
            .unwrap_or_else(|| status_confidence(&status, 0.85));
        if confidence < threshold {
            continue;
        }
        let (source_id, target_id, source_provider, target_provider, swapped) =
            ordered_values(row.get(2)?, row.get(8)?, row.get(3)?, row.get(9)?);
        let id = row.get::<_, Option<String>>(0)?.unwrap_or_default();
        let cid = format!("connectivity_vpn:{}:{}:{}", id, source_id, target_id);
        if !seen.insert(cid.clone()) {
            continue;
        }
        let evidence = json!({
            "table": "cross_cloud_vpn_connections", "record_id": id, "connection_name": row.get::<_, Option<String>>(1)?.unwrap_or_default(),
            "connection_type": row.get::<_, Option<String>>(14)?.unwrap_or_default(), "status": row.get::<_, Option<String>>(23)?.unwrap_or_default(),
            "ike_version": row.get::<_, Option<String>>(15)?.unwrap_or_default(), "encryption_algorithm": row.get::<_, Option<String>>(16)?.unwrap_or_default(),
            "authentication_method": row.get::<_, Option<String>>(17)?.unwrap_or_default(), "shared_key_configured": row.get::<_, Option<bool>>(18)?.unwrap_or(false),
            "tunnel_count": row.get::<_, Option<i32>>(19)?.unwrap_or(0), "routing_type": row.get::<_, Option<String>>(20)?.unwrap_or_default(),
            "source_region": if swapped { row.get::<_, Option<String>>(10)?.unwrap_or_default() } else { row.get::<_, Option<String>>(4)?.unwrap_or_default() },
            "target_region": if swapped { row.get::<_, Option<String>>(4)?.unwrap_or_default() } else { row.get::<_, Option<String>>(10)?.unwrap_or_default() },
            "source_public_ip": if swapped { row.get::<_, Option<String>>(12)?.unwrap_or_default() } else { row.get::<_, Option<String>>(6)?.unwrap_or_default() },
            "target_public_ip": if swapped { row.get::<_, Option<String>>(6)?.unwrap_or_default() } else { row.get::<_, Option<String>>(12)?.unwrap_or_default() },
            "source_networks": if swapped { row.get::<_, Option<String>>(13)?.unwrap_or_default() } else { row.get::<_, Option<String>>(7)?.unwrap_or_default() },
            "target_networks": if swapped { row.get::<_, Option<String>>(7)?.unwrap_or_default() } else { row.get::<_, Option<String>>(13)?.unwrap_or_default() },
            "correlation_method": row.get::<_, Option<String>>(25)?.unwrap_or_default()
        }).to_string();
        out.push(CorrelationRow {
            correlation_id: cid,
            correlation_type: "connectivity_vpn".to_string(),
            source_id: source_id.clone(),
            target_id: target_id.clone(),
            source_provider,
            target_provider,
            confidence,
            evidence,
            description: format!(
                "Cross-provider VPN connectivity between {} and {}",
                source_id, target_id
            ),
        });
    }
    Ok(())
}

fn collect_peering(
    conn: &Connection,
    threshold: f64,
    seen: &mut HashSet<String>,
    out: &mut Vec<CorrelationRow>,
) -> Result<(), Box<dyn std::error::Error>> {
    if !table_exists(conn, "cross_cloud_network_peering")? {
        return Ok(());
    }
    let mut stmt = conn.prepare(
        "SELECT id, peering_name, source_network_id, source_network_name, source_provider, source_region, source_account_id, source_cidr_blocks,
                target_network_id, target_network_name, target_provider, target_region, target_account_id, target_cidr_blocks,
                peering_type, peering_state, bidirectional, dns_resolution_enabled, route_propagation_enabled, peering_status, confidence_score, correlation_method
         FROM cross_cloud_network_peering
         WHERE source_provider IS NOT NULL AND target_provider IS NOT NULL AND source_provider <> target_provider",
    )?;
    let mut rows = stmt.query([])?;
    while let Some(row) = rows.next()? {
        let status = row.get::<_, Option<String>>(19)?.unwrap_or_default();
        let confidence = row
            .get::<_, Option<f64>>(20)?
            .unwrap_or_else(|| status_confidence(&status, 0.85));
        if confidence < threshold {
            continue;
        }
        let (source_id, target_id, source_provider, target_provider, swapped) =
            ordered_values(row.get(2)?, row.get(8)?, row.get(4)?, row.get(10)?);
        let id = row.get::<_, Option<String>>(0)?.unwrap_or_default();
        let cid = format!("connectivity_peering:{}:{}:{}", id, source_id, target_id);
        if !seen.insert(cid.clone()) {
            continue;
        }
        let evidence = json!({
            "table": "cross_cloud_network_peering", "record_id": id, "peering_name": row.get::<_, Option<String>>(1)?.unwrap_or_default(),
            "peering_type": row.get::<_, Option<String>>(14)?.unwrap_or_default(), "peering_state": row.get::<_, Option<String>>(15)?.unwrap_or_default(), "peering_status": row.get::<_, Option<String>>(19)?.unwrap_or_default(),
            "bidirectional": row.get::<_, Option<bool>>(16)?.unwrap_or(false), "dns_resolution_enabled": row.get::<_, Option<bool>>(17)?.unwrap_or(false), "route_propagation_enabled": row.get::<_, Option<bool>>(18)?.unwrap_or(false),
            "source_network_name": if swapped { row.get::<_, Option<String>>(9)?.unwrap_or_default() } else { row.get::<_, Option<String>>(3)?.unwrap_or_default() },
            "target_network_name": if swapped { row.get::<_, Option<String>>(3)?.unwrap_or_default() } else { row.get::<_, Option<String>>(9)?.unwrap_or_default() },
            "source_cidr_blocks": if swapped { row.get::<_, Option<String>>(13)?.unwrap_or_default() } else { row.get::<_, Option<String>>(7)?.unwrap_or_default() },
            "target_cidr_blocks": if swapped { row.get::<_, Option<String>>(7)?.unwrap_or_default() } else { row.get::<_, Option<String>>(13)?.unwrap_or_default() },
            "correlation_method": row.get::<_, Option<String>>(21)?.unwrap_or_default()
        }).to_string();
        out.push(CorrelationRow {
            correlation_id: cid,
            correlation_type: "connectivity_peering".to_string(),
            source_id: source_id.clone(),
            target_id: target_id.clone(),
            source_provider,
            target_provider,
            confidence,
            evidence,
            description: format!(
                "Cross-provider network peering between {} and {}",
                source_id, target_id
            ),
        });
    }
    Ok(())
}

fn collect_direct(
    conn: &Connection,
    threshold: f64,
    seen: &mut HashSet<String>,
    out: &mut Vec<CorrelationRow>,
) -> Result<(), Box<dyn std::error::Error>> {
    if !table_exists(conn, "cross_cloud_direct_connections")? {
        return Ok(());
    }
    let mut stmt = conn.prepare(
        "SELECT id, connection_name, source_resource_id, source_provider, source_region, source_location, source_vlan, source_bandwidth,
                target_resource_id, target_provider, target_region, target_location, target_vlan, target_bandwidth,
                connection_type, circuit_id, service_provider, port_speed, customer_asn, provider_asn, advertised_prefixes, received_prefixes,
                connection_state, link_status, bgp_status, redundancy_level, confidence_score, correlation_method
         FROM cross_cloud_direct_connections
         WHERE source_provider IS NOT NULL AND target_provider IS NOT NULL AND source_provider <> target_provider",
    )?;
    let mut rows = stmt.query([])?;
    while let Some(row) = rows.next()? {
        let status = row.get::<_, Option<String>>(22)?.unwrap_or_default();
        let confidence = row
            .get::<_, Option<f64>>(26)?
            .unwrap_or_else(|| status_confidence(&status, 0.85));
        if confidence < threshold {
            continue;
        }
        let (source_id, target_id, source_provider, target_provider, swapped) =
            ordered_values(row.get(2)?, row.get(8)?, row.get(3)?, row.get(9)?);
        let id = row.get::<_, Option<String>>(0)?.unwrap_or_default();
        let cid = format!("connectivity_direct:{}:{}:{}", id, source_id, target_id);
        if !seen.insert(cid.clone()) {
            continue;
        }
        let evidence = json!({
            "table": "cross_cloud_direct_connections", "record_id": id, "connection_name": row.get::<_, Option<String>>(1)?.unwrap_or_default(),
            "connection_type": row.get::<_, Option<String>>(14)?.unwrap_or_default(), "circuit_id": row.get::<_, Option<String>>(15)?.unwrap_or_default(), "service_provider": row.get::<_, Option<String>>(16)?.unwrap_or_default(),
            "connection_state": row.get::<_, Option<String>>(22)?.unwrap_or_default(), "link_status": row.get::<_, Option<String>>(23)?.unwrap_or_default(), "bgp_status": row.get::<_, Option<String>>(24)?.unwrap_or_default(),
            "port_speed": row.get::<_, Option<String>>(17)?.unwrap_or_default(), "customer_asn": row.get::<_, Option<i32>>(18)?.unwrap_or(0), "provider_asn": row.get::<_, Option<i32>>(19)?.unwrap_or(0),
            "source_location": if swapped { row.get::<_, Option<String>>(11)?.unwrap_or_default() } else { row.get::<_, Option<String>>(5)?.unwrap_or_default() },
            "target_location": if swapped { row.get::<_, Option<String>>(5)?.unwrap_or_default() } else { row.get::<_, Option<String>>(11)?.unwrap_or_default() },
            "source_vlan": if swapped { row.get::<_, Option<i32>>(12)?.unwrap_or(0) } else { row.get::<_, Option<i32>>(6)?.unwrap_or(0) },
            "target_vlan": if swapped { row.get::<_, Option<i32>>(6)?.unwrap_or(0) } else { row.get::<_, Option<i32>>(12)?.unwrap_or(0) },
            "advertised_prefixes": row.get::<_, Option<String>>(20)?.unwrap_or_default(), "received_prefixes": row.get::<_, Option<String>>(21)?.unwrap_or_default(), "redundancy_level": row.get::<_, Option<String>>(25)?.unwrap_or_default(), "correlation_method": row.get::<_, Option<String>>(27)?.unwrap_or_default()
        }).to_string();
        out.push(CorrelationRow {
            correlation_id: cid,
            correlation_type: "connectivity_direct".to_string(),
            source_id: source_id.clone(),
            target_id: target_id.clone(),
            source_provider,
            target_provider,
            confidence,
            evidence,
            description: format!(
                "Cross-provider direct connectivity between {} and {}",
                source_id, target_id
            ),
        });
    }
    Ok(())
}

fn status_confidence(status: &str, base: f64) -> f64 {
    if matches!(
        status.to_ascii_lowercase().as_str(),
        "active" | "available" | "connected" | "established"
    ) {
        0.95
    } else {
        base
    }
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
