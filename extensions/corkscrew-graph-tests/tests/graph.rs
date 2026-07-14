use corkscrew_graph_tests::functions::blast::GraphBlastRadiusVTab;
use corkscrew_graph_tests::functions::correlate_connectivity::GraphCorrelateConnectivityVTab;
use corkscrew_graph_tests::functions::correlate_dns::GraphCorrelateDNSVTab;
use corkscrew_graph_tests::functions::correlate_domains::GraphCorrelateDomainsVTab;
use corkscrew_graph_tests::functions::correlate_identity::GraphCorrelateIdentityVTab;
use corkscrew_graph_tests::functions::correlate_ips::GraphCorrelateIPsVTab;
use corkscrew_graph_tests::functions::correlate_load_balancers::GraphCorrelateLoadBalancersVTab;
use corkscrew_graph_tests::functions::correlate_networks::GraphCorrelateNetworksVTab;
use corkscrew_graph_tests::functions::correlate_policies::GraphCorrelatePoliciesVTab;
use corkscrew_graph_tests::functions::correlate_secrets::GraphCorrelateSecretsVTab;
use corkscrew_graph_tests::functions::correlate_security::GraphCorrelateSecurityVTab;
use corkscrew_graph_tests::functions::info::GraphCacheInvalidateVTab;
use corkscrew_graph_tests::functions::info::GraphInfoVTab;
use corkscrew_graph_tests::functions::list_patterns::GraphListPatternsVTab;
use corkscrew_graph_tests::functions::match_pattern::GraphMatchPatternVTab;
use corkscrew_graph_tests::functions::paths::GraphShortestPathVTab;
use corkscrew_graph_tests::functions::reachable::GraphReachableVTab;
use corkscrew_graph_tests::functions::traverse::GraphTraverseVTab;
use corkscrew_graph_tests::graph::{cache, loader, schema};
use corkscrew_graph_tests::sql_macros;
use duckdb::Connection;
use std::sync::{Arc, Barrier};
use std::thread;
use tempfile::TempDir;

fn make_fixture(conn: &Connection) {
    conn.execute_batch(
        "CREATE TABLE aws_resources (
            id VARCHAR, type VARCHAR, name VARCHAR, region VARCHAR,
            account_id VARCHAR, arn VARCHAR, tags VARCHAR
        );
        CREATE TABLE aws_relationships (
            from_id VARCHAR, to_id VARCHAR,
            relationship_type VARCHAR, properties VARCHAR
        );
        INSERT INTO aws_resources VALUES
            ('r1','ec2','i-1','us-east-1','123',NULL,NULL),
            ('r2','ec2','i-2','us-east-1','123',NULL,NULL),
            ('r3','s3', 'b-1','us-east-1','123',NULL,NULL);
        INSERT INTO aws_relationships VALUES
            ('r1','r2','peer',NULL),
            ('r1','r3','reads',NULL);",
    )
    .unwrap();
}

fn make_pattern_fixture(conn: &Connection) {
    conn.execute_batch(
        "CREATE TABLE aws_resources (
            id VARCHAR, type VARCHAR, name VARCHAR, region VARCHAR,
            account_id VARCHAR, arn VARCHAR, tags VARCHAR
        );
        CREATE TABLE aws_relationships (
            from_id VARCHAR, to_id VARCHAR,
            relationship_type VARCHAR, properties VARCHAR
        );
        INSERT INTO aws_resources VALUES
            ('igw1','aws::ec2::InternetGateway','igw-1','us-east-1','123',NULL,NULL),
            ('sg1','aws::ec2::SecurityGroup','sg-1','us-east-1','123',NULL,NULL),
            ('inst1','aws::ec2::Instance','app-1','us-east-1','123',NULL,NULL),
            ('db1','aws::rds::DBInstance','db-1','us-east-1','123',NULL,NULL),
            ('lb1','aws::elasticloadbalancing::LoadBalancer','lb-1','us-east-1','123',NULL,NULL),
            ('bucket1','aws::s3::Bucket','bucket-1','us-east-1','123',NULL,NULL),
            ('pod1','kubernetes::Pod','pod-1','us-east-1','123',NULL,NULL),
            ('sa1','kubernetes::ServiceAccount','sa-1','us-east-1','123',NULL,NULL),
            ('role1','aws::iam::Role','role-1','us-east-1','123',NULL,NULL),
            ('lambda1','aws::lambda::Function','lambda-1','us-east-1','123',NULL,NULL),
            ('policy1','aws::iam::Policy','AdministratorAccess','us-east-1','123',NULL,NULL),
            ('role2','aws::iam::Role','external-role','us-east-1','999',NULL,NULL),
            ('inst2','aws::ec2::Instance','app-2','us-east-1','123',NULL,NULL),
            ('bucket2','aws::s3::Bucket','bucket-2','us-east-1','123',NULL,NULL);
        INSERT INTO aws_relationships VALUES
            ('igw1','sg1','allows',NULL),
            ('sg1','inst1','member_of',NULL),
            ('inst1','db1','connects_to',NULL),
            ('lb1','inst1','routes_to',NULL),
            ('igw1','inst1','exposes',NULL),
            ('inst1','bucket1','writes',NULL),
            ('pod1','sa1','uses',NULL),
            ('sa1','role1','assumes',NULL),
            ('lambda1','policy1','administratoraccess',NULL),
            ('role1','role2','assume_role',NULL),
            ('inst1','inst2','peer',NULL),
            ('inst2','bucket2','reads',NULL);",
    )
    .unwrap();
}

fn make_many_public_s3_matches_fixture(conn: &Connection) {
    conn.execute_batch(
        "CREATE TABLE aws_resources (
            id VARCHAR, type VARCHAR, name VARCHAR, region VARCHAR,
            account_id VARCHAR, arn VARCHAR, tags VARCHAR
        );
        CREATE TABLE aws_relationships (
            from_id VARCHAR, to_id VARCHAR,
            relationship_type VARCHAR, properties VARCHAR
        );",
    )
    .unwrap();

    for index in 0..300 {
        let sql = format!(
            "INSERT INTO aws_resources VALUES
                ('inst{index}','aws::ec2::Instance','inst{index}','us-east-1','123',NULL,NULL),
                ('bucket{index}','aws::s3::Bucket','bucket{index}','us-east-1','123',NULL,NULL);
             INSERT INTO aws_relationships VALUES
                ('inst{index}','bucket{index}','reads',NULL);"
        );
        conn.execute_batch(&sql).unwrap();
    }
}

fn make_shortest_path_edge_fixture(conn: &Connection) {
    conn.execute_batch(
        "CREATE TABLE aws_resources (
            id VARCHAR, type VARCHAR, name VARCHAR, region VARCHAR,
            account_id VARCHAR, arn VARCHAR, tags VARCHAR
        );
        CREATE TABLE aws_relationships (
            from_id VARCHAR, to_id VARCHAR,
            relationship_type VARCHAR, properties VARCHAR
        );
        INSERT INTO aws_resources VALUES
            ('a','ec2','a','us-east-1','123',NULL,NULL),
            ('b','ec2','b','us-east-1','123',NULL,NULL),
            ('c','ec2','c','us-east-1','123',NULL,NULL),
            ('isolated','ec2','isolated','us-east-1','123',NULL,NULL);
        INSERT INTO aws_relationships VALUES
            ('a','b','peer',NULL),
            ('b','c','peer',NULL);",
    )
    .unwrap();
}

fn make_cross_cloud_ip_fixture(conn: &Connection) {
    conn.execute_batch(
        "CREATE TABLE cross_cloud_ip_addresses (
            id VARCHAR,
            ip_address VARCHAR,
            ip_version VARCHAR,
            ip_type VARCHAR,
            ip_scope VARCHAR,
            resource_id VARCHAR,
            resource_type VARCHAR,
            resource_name VARCHAR,
            provider VARCHAR,
            region VARCHAR,
            account_id VARCHAR,
            vpc_id VARCHAR,
            subnet_id VARCHAR,
            network_interface_id VARCHAR,
            allocation_id VARCHAR,
            domain VARCHAR,
            tags JSON,
            metadata JSON,
            created_at TIMESTAMP,
            discovered_at TIMESTAMP,
            updated_at TIMESTAMP
        );
        INSERT INTO cross_cloud_ip_addresses VALUES
            ('ip-1','8.8.8.8','ipv4','elastic','global','aws-vm-1','aws::ec2::Instance','app-a','aws','us-east-1','111','vpc-1','subnet-1','eni-1',NULL,NULL,NULL,NULL,NULL,NULL,NULL),
            ('ip-2','8.8.8.8','ipv4','static','global','azure-vm-1','Microsoft.Compute/virtualMachines','app-a','azure','eastus','222','vnet-1','subnet-a','nic-1',NULL,NULL,NULL,NULL,NULL,NULL,NULL),
            ('ip-3','10.0.0.5','ipv4','private','local','aws-vm-2','aws::ec2::Instance','worker-a','aws','us-east-1','111','vpc-1','subnet-1','eni-2',NULL,NULL,NULL,NULL,NULL,NULL,NULL),
            ('ip-4','10.0.0.5','ipv4','private','local','gcp-vm-1','compute.googleapis.com/Instance','worker-g','gcp','us-central1','333','vpc-g','subnet-g','nic-g',NULL,NULL,NULL,NULL,NULL,NULL,NULL),
            ('ip-5','8.8.4.4','ipv4','elastic','global','aws-vm-3','aws::ec2::Instance','app-b','aws','us-east-1','111','vpc-1','subnet-1','eni-3',NULL,NULL,NULL,NULL,NULL,NULL,NULL);",
    )
    .unwrap();
}

fn make_cross_cloud_dns_fixture(conn: &Connection) {
    conn.execute_batch(
        "CREATE TABLE cross_cloud_dns_records (
            id VARCHAR, dns_name VARCHAR, record_type VARCHAR, record_values JSON, ttl INTEGER,
            resource_id VARCHAR, resource_type VARCHAR, resource_name VARCHAR, provider VARCHAR,
            region VARCHAR, account_id VARCHAR, dns_service VARCHAR, zone_id VARCHAR, zone_name VARCHAR,
            health_check_id VARCHAR, routing_policy VARCHAR, routing_policy_config JSON, tags JSON,
            metadata JSON, created_at TIMESTAMP, discovered_at TIMESTAMP, updated_at TIMESTAMP
        );
        INSERT INTO cross_cloud_dns_records VALUES
            ('dns-1','api.example.com.','A','[\"203.0.113.10\"]',60,'aws-zone-1','aws::route53::record','api','aws','us-east-1','111','route53','z1','example.com',NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL),
            ('dns-2','api.example.com','A','[\"203.0.113.20\"]',60,'azure-zone-1','Microsoft.Network/dnszones/A','api','azure','eastus','222','azure_dns','z2','example.com',NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL),
            ('dns-3','www.example.com','CNAME','[\"edge.example.net.\"]',60,'aws-cname-1','aws::route53::record','www','aws','us-east-1','111','route53','z1','example.com',NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL),
            ('dns-4','web.example.org','CNAME','[\"edge.example.net\"]',60,'gcp-cname-1','dns.googleapis.com/ResourceRecordSet','web','gcp','global','333','cloud_dns','z3','example.org',NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL);",
    )
    .unwrap();
}

fn make_cross_cloud_network_fixture(conn: &Connection) {
    conn.execute_batch(
        "CREATE TABLE cross_cloud_network_topology (
            id VARCHAR, connection_type VARCHAR, connection_id VARCHAR, connection_name VARCHAR,
            source_network_id VARCHAR, source_network_name VARCHAR, source_provider VARCHAR, source_region VARCHAR, source_account_id VARCHAR, source_cidr_blocks JSON,
            target_network_id VARCHAR, target_network_name VARCHAR, target_provider VARCHAR, target_region VARCHAR, target_account_id VARCHAR, target_cidr_blocks JSON,
            status VARCHAR, bandwidth VARCHAR, encryption BOOLEAN, redundancy VARCHAR, routing_tables JSON, route_propagation BOOLEAN,
            source_gateway_id VARCHAR, target_gateway_id VARCHAR, tags JSON, metadata JSON, created_at TIMESTAMP, discovered_at TIMESTAMP, updated_at TIMESTAMP
        );
        INSERT INTO cross_cloud_network_topology VALUES
            ('topo-1','peering','peer-1','aws-azure-peer','vpc-1','prod-vpc','aws','us-east-1','111','[\"10.0.0.0/16\"]','vnet-1','prod-vnet','azure','eastus','222','[\"10.1.0.0/16\"]','active','1Gbps',true,'regional',NULL,true,'vgw-1','vng-1',NULL,NULL,NULL,NULL,NULL);",
    )
    .unwrap();
}

fn make_cross_cloud_load_balancer_fixture(conn: &Connection) {
    conn.execute_batch(
        "CREATE TABLE cross_cloud_loadbalancer_topology (
            id VARCHAR, loadbalancer_id VARCHAR, loadbalancer_name VARCHAR, loadbalancer_type VARCHAR,
            provider VARCHAR, region VARCHAR, backend_targets JSON, backend_health_status JSON, cross_cloud_backends JSON,
            frontend_config JSON, ssl_certificates JSON, dns_configurations JSON, routing_rules JSON, path_patterns JSON,
            host_patterns JSON, health_check_configs JSON, health_check_results JSON, health_check_cross_cloud JSON,
            traffic_distribution_method VARCHAR, session_affinity BOOLEAN, sticky_sessions_config JSON,
            correlated_loadbalancers JSON, correlation_type VARCHAR, correlation_confidence DOUBLE, correlation_evidence JSON, shared_backends JSON,
            request_count_hourly BIGINT, request_count_daily BIGINT, response_time_avg_ms DOUBLE, response_time_p95_ms DOUBLE, error_rate_percentage DOUBLE,
            current_capacity INTEGER, max_capacity INTEGER, auto_scaling_enabled BOOLEAN, scaling_policies JSON,
            security_groups JSON, ssl_policies JSON, waf_configuration JSON, monitoring_enabled BOOLEAN, alert_configurations JSON,
            log_destinations JSON, tags JSON, metadata JSON, created_at TIMESTAMP, discovered_at TIMESTAMP, updated_at TIMESTAMP
        );
        INSERT INTO cross_cloud_loadbalancer_topology VALUES
            ('lb-topo-1','aws-lb-1','public-alb','application','aws','us-east-1','[{\"target\":\"10.0.1.10\",\"port\":80}]',NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,'round_robin',false,NULL,NULL,NULL,NULL,NULL,NULL,0,0,NULL,NULL,NULL,NULL,NULL,false,NULL,NULL,NULL,NULL,false,NULL,NULL,NULL,NULL,NULL,NULL,NULL),
            ('lb-topo-2','azure-lb-1','public-appgw','application','azure','eastus','[{\"ip_address\":\"10.0.1.10\",\"port\":80}]',NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,'round_robin',false,NULL,NULL,NULL,NULL,NULL,NULL,0,0,NULL,NULL,NULL,NULL,NULL,false,NULL,NULL,NULL,NULL,false,NULL,NULL,NULL,NULL,NULL,NULL,NULL);",
    )
    .unwrap();
}

fn make_cross_cloud_connectivity_fixture(conn: &Connection) {
    conn.execute_batch(
        "CREATE TABLE cross_cloud_vpn_connections (
            id VARCHAR, connection_name VARCHAR, source_resource_id VARCHAR, source_provider VARCHAR, source_region VARCHAR, source_gateway_id VARCHAR, source_public_ip VARCHAR, source_local_networks JSON,
            target_resource_id VARCHAR, target_provider VARCHAR, target_region VARCHAR, target_gateway_id VARCHAR, target_public_ip VARCHAR, target_remote_networks JSON,
            connection_type VARCHAR, ike_version VARCHAR, encryption_algorithm VARCHAR, authentication_method VARCHAR, shared_key_configured BOOLEAN, tunnel_count INTEGER, tunnel_status JSON, routing_type VARCHAR,
            bgp_asn_source VARCHAR, bgp_asn_target VARCHAR, connection_status VARCHAR, last_status_change TIMESTAMP, uptime_percentage DOUBLE, last_health_check TIMESTAMP,
            bytes_transferred_in BIGINT, bytes_transferred_out BIGINT, packets_transferred_in BIGINT, packets_transferred_out BIGINT, average_latency_ms DOUBLE,
            mtu_size INTEGER, keepalive_interval INTEGER, dead_peer_detection BOOLEAN, nat_traversal BOOLEAN, tags JSON, metadata JSON, correlation_id VARCHAR, confidence_score DOUBLE, correlation_method VARCHAR,
            created_at TIMESTAMP, discovered_at TIMESTAMP, updated_at TIMESTAMP
        );
        INSERT INTO cross_cloud_vpn_connections VALUES
            ('vpn-1','prod-vpn','aws-vgw-1','aws','us-east-1','vgw-1','198.51.100.10','[\"10.0.0.0/16\"]','azure-vng-1','azure','eastus','vng-1','198.51.100.20','[\"10.1.0.0/16\"]','site_to_site','2','aes256','psk',true,2,NULL,'dynamic','65001','65002','active',NULL,NULL,NULL,0,0,0,0,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,0.96,'fixture',NULL,NULL,NULL);",
    )
    .unwrap();
}

fn make_cross_cloud_security_fixture(conn: &Connection) {
    conn.execute_batch(
        "CREATE TABLE cross_cloud_security_correlations (
            id VARCHAR, source_rule_id VARCHAR, source_resource_id VARCHAR, source_provider VARCHAR, source_region VARCHAR,
            target_rule_id VARCHAR, target_resource_id VARCHAR, target_provider VARCHAR, target_region VARCHAR,
            rule_overlap_analysis JSON, overlap_percentage DOUBLE, overlap_type VARCHAR, protocol_correlation JSON, port_overlap_analysis JSON, cidr_overlap_analysis JSON,
            security_pattern VARCHAR, access_pattern VARCHAR, direction_analysis JSON, action_analysis JSON, security_risk_level VARCHAR, risk_factors JSON, potential_conflicts JSON, recommendations JSON,
            compliance_frameworks JSON, compliance_gaps JSON, policy_alignment JSON, correlation_method VARCHAR, confidence_score DOUBLE, evidence JSON, validation_status VARCHAR,
            rule_complexity_score DOUBLE, performance_impact VARCHAR, optimization_suggestions JSON, monitoring_enabled BOOLEAN, alert_thresholds JSON, last_violation_check TIMESTAMP, violation_count INTEGER,
            tags JSON, metadata JSON, discovered_at TIMESTAMP, last_analyzed TIMESTAMP, updated_at TIMESTAMP
        );
        INSERT INTO cross_cloud_security_correlations VALUES
            ('sec-1','aws-sg-1-ingress-0','aws-sg-1','aws','us-east-1','az-nsg-1-rule-0','az-nsg-1','azure','eastus','{\"protocol\":\"tcp\"}',100.0,'exact','{\"protocol\":\"tcp\"}','{\"ports\":[443]}','{\"cidrs\":[\"0.0.0.0/0\"]}','web_service','public','{\"direction\":\"inbound\"}','{\"action\":\"allow\"}','high','[\"public_https\"]',NULL,NULL,NULL,NULL,NULL,'fixture',0.91,'{\"reason\":\"same public HTTPS exposure\"}','validated',NULL,NULL,NULL,false,NULL,NULL,0,NULL,NULL,NULL,NULL,NULL);",
    )
    .unwrap();
}

fn make_cross_cloud_domain_fixture(conn: &Connection) {
    conn.execute_batch(
        "CREATE TABLE cross_cloud_dns_records (
            id VARCHAR, dns_name VARCHAR, record_type VARCHAR, record_values JSON, ttl INTEGER,
            resource_id VARCHAR, resource_type VARCHAR, resource_name VARCHAR, provider VARCHAR,
            region VARCHAR, account_id VARCHAR, dns_service VARCHAR, zone_id VARCHAR, zone_name VARCHAR,
            health_check_id VARCHAR, routing_policy VARCHAR, routing_policy_config JSON, tags JSON,
            metadata JSON, created_at TIMESTAMP, discovered_at TIMESTAMP, updated_at TIMESTAMP
        );
        INSERT INTO cross_cloud_dns_records VALUES
            ('dns-domain-1','api.example.com','A','[\"203.0.113.10\"]',60,'aws-zone-1','aws::route53::record','api','aws','us-east-1','111','route53','z1','example.com',NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL),
            ('dns-domain-2','web.example.com','A','[\"203.0.113.20\"]',60,'azure-zone-1','Microsoft.Network/dnszones/A','web','azure','eastus','222','azure_dns','z2','example.com.',NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL);
        CREATE TABLE certificate_correlations (
            id VARCHAR, source_cert_id VARCHAR, source_cert_name VARCHAR, source_cert_thumbprint VARCHAR, source_cert_serial_number VARCHAR, source_cloud_provider VARCHAR, source_region VARCHAR, source_account_id VARCHAR, source_resource_id VARCHAR,
            target_cert_id VARCHAR, target_cert_name VARCHAR, target_cert_thumbprint VARCHAR, target_cert_serial_number VARCHAR, target_cloud_provider VARCHAR, target_region VARCHAR, target_account_id VARCHAR, target_resource_id VARCHAR,
            correlation_type VARCHAR, chain_relationship VARCHAR, confidence_score DOUBLE, matching_attributes JSON, source_cert_details JSON, target_cert_details JSON, shared_attributes JSON,
            source_subject VARCHAR, source_issuer VARCHAR, source_common_name VARCHAR, source_sans JSON, target_subject VARCHAR, target_issuer VARCHAR, target_common_name VARCHAR, target_sans JSON,
            source_not_before TIMESTAMP, source_not_after TIMESTAMP, target_not_before TIMESTAMP, target_not_after TIMESTAMP,
            security_risk_level VARCHAR, security_risk_score DOUBLE, security_issues JSON, recommendations JSON, compliance_flags JSON, shared_secrets JSON, secret_correlations JSON, status VARCHAR, verified BOOLEAN,
            detected_at TIMESTAMP, last_verified_at TIMESTAMP, created_at TIMESTAMP, updated_at TIMESTAMP, metadata JSON
        );
        INSERT INTO certificate_correlations VALUES
            ('cert-1','aws-cert-1','api.example.com','abc',NULL,'aws','us-east-1','111','aws-lb-1','azure-cert-1','api.example.com','abc',NULL,'azure','eastus','222','azure-lb-1','thumbprint_match',NULL,0.97,'{\"thumbprint\":\"abc\"}',NULL,NULL,'{\"domain\":\"api.example.com\"}',NULL,NULL,'api.example.com','[\"api.example.com\"]',NULL,NULL,'api.example.com','[\"api.example.com\"]',NULL,NULL,NULL,NULL,'low',0.1,NULL,NULL,NULL,NULL,NULL,'active',true,NULL,NULL,NULL,NULL,NULL);",
    )
    .unwrap();
}

fn make_cross_cloud_identity_fixture(conn: &Connection) {
    conn.execute_batch(
        "CREATE TABLE identity_federation_relationships (
            id VARCHAR, source_provider_id VARCHAR, source_provider_type VARCHAR, source_provider_name VARCHAR, source_cloud_provider VARCHAR, source_region VARCHAR, source_account_id VARCHAR,
            target_provider_id VARCHAR, target_provider_type VARCHAR, target_provider_name VARCHAR, target_cloud_provider VARCHAR, target_region VARCHAR, target_account_id VARCHAR,
            federation_type VARCHAR, federation_method VARCHAR, trust_policy JSON, trust_conditions JSON, oidc_issuer VARCHAR, oidc_endpoints JSON, client_ids JSON, scopes JSON,
            saml_entity_id VARCHAR, saml_sso_endpoint VARCHAR, certificate_thumbprints JSON, signing_certificates JSON, confidence_score DOUBLE, evidence JSON, matching_attributes JSON,
            security_risk_level VARCHAR, security_risk_score DOUBLE, security_issues JSON, recommendations JSON, status VARCHAR, verified BOOLEAN, verification_method VARCHAR
        );
        INSERT INTO identity_federation_relationships VALUES
            ('fed-1','aws-oidc-1','OIDC','eks-oidc','aws','us-east-1','111','azure-app-1','OIDC','aks-workload-id','azure','eastus','222','oidc_federation','issuer_match','{}','{}','https://issuer.example.com','[]','[\"api://app\"]','[]',NULL,NULL,'[\"abc\"]','[]',0.94,'{\"issuer\":\"match\"}','{\"issuer\":\"https://issuer.example.com\"}','HIGH',0.8,'[]','[]','active',true,'fixture');",
    )
    .unwrap();
}

fn make_cross_cloud_policy_fixture(conn: &Connection) {
    conn.execute_batch(
        "CREATE TABLE policy_similarity_analysis (
            id VARCHAR, source_policy_id VARCHAR, source_policy_name VARCHAR, source_policy_type VARCHAR, source_cloud_provider VARCHAR, source_region VARCHAR, source_account_id VARCHAR, source_resource_id VARCHAR,
            target_policy_id VARCHAR, target_policy_name VARCHAR, target_policy_type VARCHAR, target_cloud_provider VARCHAR, target_region VARCHAR, target_account_id VARCHAR, target_resource_id VARCHAR,
            similarity_score DOUBLE, similarity_type VARCHAR, matching_elements JSON, differences JSON, normalized_permissions JSON, source_policy_hash VARCHAR, target_policy_hash VARCHAR, source_statements JSON, target_statements JSON,
            risk_level VARCHAR, risk_score DOUBLE, risk_factors JSON, security_issues JSON, recommendations JSON, compliance_tags JSON, analysis_method VARCHAR, confidence_score DOUBLE, false_positive_likelihood DOUBLE, status VARCHAR, reviewed BOOLEAN
        );
        INSERT INTO policy_similarity_analysis VALUES
            ('pol-1','aws-pol-1','AdminLike','managed','aws','us-east-1','111','aws-role-1','az-roledef-1','OwnerLike','role','azure','eastus','222','az-sp-1',0.93,'highly_similar','[\"*:*\"]','[]','[\"admin\"]','hash-a','hash-b','[]','[]','HIGH',0.8,'[]','[]','[]','[]','fixture',0.92,0.05,'active',false);",
    )
    .unwrap();
}

fn make_cross_cloud_secret_fixture(conn: &Connection) {
    conn.execute_batch(
        "CREATE TABLE shared_secrets_correlation (
            id VARCHAR, secret_type VARCHAR, secret_name VARCHAR, secret_hash VARCHAR, cloud_provider VARCHAR, region VARCHAR, account_id VARCHAR, resource_id VARCHAR, service_name VARCHAR,
            security_risk_level VARCHAR, security_issues JSON, recommendations JSON, referenced_by JSON, cross_cloud_references JSON, usage_patterns JSON,
            encryption_status VARCHAR, access_control_status VARCHAR, correlation_confidence DOUBLE, correlation_method VARCHAR, correlation_evidence JSON, status VARCHAR
        );
        INSERT INTO shared_secrets_correlation VALUES
            ('sec-a','api_key','shared-api','hash-123','aws','us-east-1','111','aws-secret-1','secretsmanager','HIGH','[]','[]','[\"lambda\"]','[]','[]','encrypted','restricted',0.91,'hash_match','{\"hash\":\"hash-123\"}','active'),
            ('sec-b','api_key','shared-api','hash-123','azure','eastus','222','az-secret-1','keyvault','HIGH','[]','[]','[\"function\"]','[]','[]','encrypted','restricted',0.92,'hash_match','{\"hash\":\"hash-123\"}','active');",
    )
    .unwrap();
}

fn register_graph_functions(conn: &Connection) {
    conn.register_table_function::<GraphInfoVTab>("graph_info")
        .unwrap();
    conn.register_table_function::<GraphCacheInvalidateVTab>("graph_cache_invalidate")
        .unwrap();
    conn.register_table_function::<GraphListPatternsVTab>("graph_list_patterns")
        .unwrap();
    conn.register_table_function::<GraphTraverseVTab>("graph_traverse")
        .unwrap();
    conn.register_table_function::<GraphShortestPathVTab>("graph_shortest_path")
        .unwrap();
    conn.register_table_function::<GraphBlastRadiusVTab>("graph_blast_radius")
        .unwrap();
    conn.register_table_function::<GraphReachableVTab>("graph_reachable")
        .unwrap();
    conn.register_table_function::<GraphMatchPatternVTab>("graph_match_pattern")
        .unwrap();
    conn.register_table_function::<GraphCorrelateIPsVTab>("graph_correlate_ips")
        .unwrap();
    conn.register_table_function::<GraphCorrelateDNSVTab>("graph_correlate_dns")
        .unwrap();
    conn.register_table_function::<GraphCorrelateNetworksVTab>("graph_correlate_networks")
        .unwrap();
    conn.register_table_function::<GraphCorrelateLoadBalancersVTab>(
        "graph_correlate_load_balancers",
    )
    .unwrap();
    conn.register_table_function::<GraphCorrelateConnectivityVTab>("graph_correlate_connectivity")
        .unwrap();
    conn.register_table_function::<GraphCorrelateSecurityVTab>("graph_correlate_security")
        .unwrap();
    conn.register_table_function::<GraphCorrelateDomainsVTab>("graph_correlate_domains")
        .unwrap();
    conn.register_table_function::<GraphCorrelateIdentityVTab>("graph_correlate_identity")
        .unwrap();
    conn.register_table_function::<GraphCorrelatePoliciesVTab>("graph_correlate_policies")
        .unwrap();
    conn.register_table_function::<GraphCorrelateSecretsVTab>("graph_correlate_secrets")
        .unwrap();
    sql_macros::register_macros(conn).unwrap();
}

#[test]
fn detect_providers_finds_aws() {
    let conn = Connection::open_in_memory().unwrap();
    make_fixture(&conn);
    let providers = schema::detect_providers(&conn).unwrap();
    assert_eq!(providers.len(), 1);
    assert_eq!(providers[0].provider, "aws");
    assert_eq!(providers[0].resources_table, "aws_resources");
    assert_eq!(providers[0].relationships_table, "aws_relationships");
}

#[test]
fn load_graph_counts_nodes_and_edges() {
    let conn = Connection::open_in_memory().unwrap();
    make_fixture(&conn);
    let providers = schema::detect_providers(&conn).unwrap();
    let loaded = loader::load_graph(&conn, &providers).unwrap();
    assert_eq!(loaded.node_count, 3);
    assert_eq!(loaded.edge_count, 2);
    assert_eq!(
        loaded
            .graph
            .node_weights()
            .filter(|node| node.provider == "aws")
            .count(),
        3
    );
    assert_eq!(
        loaded
            .graph
            .edge_weights()
            .filter(|edge| edge.provider == "aws")
            .count(),
        2
    );
}

#[test]
fn load_graph_accepts_versioned_schema_relationship_views() {
    let conn = Connection::open_in_memory().unwrap();
    conn.execute_batch(
        "CREATE TABLE cloud_relationships (
            from_id VARCHAR, to_id VARCHAR, relationship_type VARCHAR,
            provider VARCHAR, properties VARCHAR
        );",
    )
    .unwrap();

    let providers = ["aws", "azure", "gcp", "kubernetes", "github", "cloudflare"];
    for provider in providers {
        conn.execute_batch(&format!(
            "CREATE TABLE {provider}_resources (
                id VARCHAR, type VARCHAR, name VARCHAR, region VARCHAR,
                account_id VARCHAR, arn VARCHAR, tags VARCHAR
            );
            INSERT INTO {provider}_resources VALUES
                ('{provider}:one', 'resource', 'one', 'global', 'account', NULL, NULL);
            CREATE VIEW {provider}_relationships AS
                SELECT from_id, to_id, relationship_type, properties
                FROM cloud_relationships WHERE provider = '{provider}';"
        ))
        .unwrap();
    }
    conn.execute(
        "INSERT INTO cloud_relationships VALUES
            ('github:one', 'cloudflare:one', 'deploys_to', 'github', NULL)",
        [],
    )
    .unwrap();

    let detected = schema::detect_providers(&conn).unwrap();
    let loaded = loader::load_graph(&conn, &detected).unwrap();
    assert_eq!(detected.len(), 6);
    assert_eq!(loaded.node_count, 6);
    assert_eq!(loaded.edge_count, 1);
    assert!(detected
        .iter()
        .any(|provider| provider.provider == "github"));
    assert!(detected
        .iter()
        .any(|provider| provider.provider == "cloudflare"));
}

#[test]
fn cache_get_or_load_then_invalidate() {
    let conn = Connection::open_in_memory().unwrap();
    make_fixture(&conn);

    let path = "test::cache_get_or_load";
    let g1 = cache::get_or_load(&conn, path).unwrap();
    let g2 = cache::get_or_load(&conn, path).unwrap();
    // Same Arc => cache hit
    assert!(std::sync::Arc::ptr_eq(&g1, &g2));

    cache::invalidate(path);
    let g3 = cache::get_or_load(&conn, path).unwrap();
    assert!(!std::sync::Arc::ptr_eq(&g1, &g3));
}

#[test]
fn cache_ttl_setting_zero_forces_reload() {
    let conn = Connection::open_in_memory().unwrap();
    make_fixture(&conn);

    let path = "test::cache_ttl_setting_zero_forces_reload";
    let g1 = cache::get_or_load(&conn, path).unwrap();
    conn.execute("SET VARIABLE corkscrew_graph_cache_ttl = 0", [])
        .unwrap();
    let g2 = cache::get_or_load(&conn, path).unwrap();

    assert!(!std::sync::Arc::ptr_eq(&g1, &g2));
}

#[test]
fn cache_get_or_load_is_shared_across_threads() {
    let temp_dir = TempDir::new().unwrap();
    let fixture_path = temp_dir
        .path()
        .join("fixture.duckdb")
        .to_string_lossy()
        .into_owned();

    let fixture_conn = Connection::open(&fixture_path).unwrap();
    make_fixture(&fixture_conn);
    drop(fixture_conn);

    let barrier = Arc::new(Barrier::new(8));
    let mut handles = Vec::new();
    for _ in 0..8 {
        let fixture_path = fixture_path.clone();
        let barrier = Arc::clone(&barrier);
        handles.push(thread::spawn(move || {
            let conn = Connection::open(&fixture_path).unwrap();
            barrier.wait();
            cache::get_or_load(&conn, &fixture_path).unwrap()
        }));
    }

    let graphs = handles
        .into_iter()
        .map(|handle| handle.join().unwrap())
        .collect::<Vec<_>>();

    for graph in &graphs[1..] {
        assert!(std::sync::Arc::ptr_eq(&graphs[0], graph));
    }
}

#[test]
fn cache_readers_and_invalidator_can_race_safely() {
    let temp_dir = TempDir::new().unwrap();
    let fixture_path = temp_dir
        .path()
        .join("fixture.duckdb")
        .to_string_lossy()
        .into_owned();

    let fixture_conn = Connection::open(&fixture_path).unwrap();
    make_fixture(&fixture_conn);
    drop(fixture_conn);

    let path_for_invalidator = fixture_path.clone();
    let invalidator = thread::spawn(move || {
        for _ in 0..50 {
            cache::invalidate(&path_for_invalidator);
        }
    });

    let mut handles = Vec::new();
    for _ in 0..6 {
        let fixture_path = fixture_path.clone();
        handles.push(thread::spawn(move || {
            let conn = Connection::open(&fixture_path).unwrap();
            for _ in 0..25 {
                let loaded = cache::get_or_load(&conn, &fixture_path).unwrap();
                assert_eq!(loaded.node_count, 3);
                assert_eq!(loaded.edge_count, 2);
            }
        }));
    }

    invalidator.join().unwrap();
    for handle in handles {
        handle.join().unwrap();
    }
}

#[test]
fn detect_providers_empty_when_no_tables() {
    let conn = Connection::open_in_memory().unwrap();
    let providers = schema::detect_providers(&conn).unwrap();
    assert!(providers.is_empty());
}

#[test]
fn graph_info_table_function_returns_provider_counts() {
    let temp_dir = TempDir::new().unwrap();
    let fixture_path = temp_dir
        .path()
        .join("fixture.duckdb")
        .to_string_lossy()
        .into_owned();

    let fixture_conn = Connection::open(&fixture_path).unwrap();
    make_fixture(&fixture_conn);
    drop(fixture_conn);

    let conn = Connection::open_in_memory().unwrap();
    register_graph_functions(&conn);

    let mut stmt = conn
        .prepare("SELECT provider, nodes, edges FROM graph_info(?) ORDER BY provider")
        .unwrap();
    let mut rows = stmt.query([fixture_path.as_str()]).unwrap();

    let row = rows.next().unwrap().unwrap();
    let provider: String = row.get(0).unwrap();
    let nodes: i64 = row.get(1).unwrap();
    let edges: i64 = row.get(2).unwrap();

    assert_eq!(provider, "aws");
    assert_eq!(nodes, 3);
    assert_eq!(edges, 2);
    assert!(rows.next().unwrap().is_none());
}

#[test]
fn graph_info_table_function_handles_multiple_providers() {
    let temp_dir = TempDir::new().unwrap();
    let fixture_path = temp_dir
        .path()
        .join("fixture.duckdb")
        .to_string_lossy()
        .into_owned();

    let fixture_conn = Connection::open(&fixture_path).unwrap();
    fixture_conn
        .execute_batch(
            "CREATE TABLE aws_resources (
                id VARCHAR, type VARCHAR, name VARCHAR, region VARCHAR,
                account_id VARCHAR, arn VARCHAR, tags VARCHAR
            );
            CREATE TABLE aws_relationships (
                from_id VARCHAR, to_id VARCHAR,
                relationship_type VARCHAR, properties VARCHAR
            );
            CREATE TABLE azure_resources (
                id VARCHAR, type VARCHAR, name VARCHAR, region VARCHAR,
                account_id VARCHAR, arn VARCHAR, tags VARCHAR
            );
            CREATE TABLE azure_relationships (
                from_id VARCHAR, to_id VARCHAR,
                relationship_type VARCHAR, properties VARCHAR
            );
            INSERT INTO aws_resources VALUES
                ('aws-r1','ec2','i-1','us-east-1','123',NULL,NULL),
                ('aws-r2','s3','b-1','us-east-1','123',NULL,NULL);
            INSERT INTO aws_relationships VALUES
                ('aws-r1','aws-r2','reads',NULL);
            INSERT INTO azure_resources VALUES
                ('az-r1','vm','vm-1','eastus','sub-1',NULL,NULL);
            INSERT INTO azure_relationships VALUES
                ('az-r1','az-r1','self',NULL);",
        )
        .unwrap();
    drop(fixture_conn);

    let conn = Connection::open_in_memory().unwrap();
    register_graph_functions(&conn);

    let mut stmt = conn
        .prepare("SELECT provider, nodes, edges FROM graph_info(?) ORDER BY provider")
        .unwrap();
    let mut rows = stmt.query([fixture_path.as_str()]).unwrap();

    let aws = rows.next().unwrap().unwrap();
    assert_eq!(aws.get::<_, String>(0).unwrap(), "aws");
    assert_eq!(aws.get::<_, i64>(1).unwrap(), 2);
    assert_eq!(aws.get::<_, i64>(2).unwrap(), 1);

    let azure = rows.next().unwrap().unwrap();
    assert_eq!(azure.get::<_, String>(0).unwrap(), "azure");
    assert_eq!(azure.get::<_, i64>(1).unwrap(), 1);
    assert_eq!(azure.get::<_, i64>(2).unwrap(), 1);

    assert!(rows.next().unwrap().is_none());
}

#[test]
fn graph_traverse_returns_paths_and_hops() {
    let temp_dir = TempDir::new().unwrap();
    let fixture_path = temp_dir
        .path()
        .join("fixture.duckdb")
        .to_string_lossy()
        .into_owned();

    let fixture_conn = Connection::open(&fixture_path).unwrap();
    make_fixture(&fixture_conn);
    drop(fixture_conn);

    let conn = Connection::open_in_memory().unwrap();
    register_graph_functions(&conn);

    let mut stmt = conn
        .prepare("SELECT node_id, hop_count, CAST(path_ids AS VARCHAR), CAST(relationship_types AS VARCHAR) FROM graph_traverse(?, ?, CAST(? AS INTEGER), ?, ?) ORDER BY hop_count, node_id")
        .unwrap();
    let rows = stmt
        .query_map(
            [fixture_path.as_str(), "r1", "3", "outbound", "NULL"],
            |row| {
                Ok((
                    row.get::<_, String>(0)?,
                    row.get::<_, i32>(1)?,
                    row.get::<_, String>(2)?,
                    row.get::<_, String>(3)?,
                ))
            },
        )
        .unwrap()
        .collect::<Result<Vec<_>, _>>()
        .unwrap();

    assert_eq!(rows.len(), 2);
    assert_eq!(rows[0].0, "r2");
    assert_eq!(rows[0].1, 1);
    assert_eq!(rows[0].2, "[r1, r2]".to_string());
    assert_eq!(rows[0].3, "[peer]".to_string());
    assert_eq!(rows[1].0, "r3");
}

#[test]
fn graph_traverse_filter_type_uses_case_insensitive_contains() {
    let temp_dir = TempDir::new().unwrap();
    let fixture_path = temp_dir
        .path()
        .join("fixture.duckdb")
        .to_string_lossy()
        .into_owned();

    let fixture_conn = Connection::open(&fixture_path).unwrap();
    fixture_conn
        .execute_batch(
            "CREATE TABLE aws_resources (
                id VARCHAR, type VARCHAR, name VARCHAR, region VARCHAR,
                account_id VARCHAR, arn VARCHAR, tags VARCHAR
            );
            CREATE TABLE aws_relationships (
                from_id VARCHAR, to_id VARCHAR,
                relationship_type VARCHAR, properties VARCHAR
            );
            INSERT INTO aws_resources VALUES
                ('r1','aws::ec2::Instance','i-1','us-east-1','123',NULL,NULL),
                ('r2','aws::ec2::Instance','i-2','us-east-1','123',NULL,NULL),
                ('r3','aws::s3::Bucket','b-1','us-east-1','123',NULL,NULL);
            INSERT INTO aws_relationships VALUES
                ('r1','r2','peer',NULL),
                ('r1','r3','reads',NULL);",
        )
        .unwrap();
    drop(fixture_conn);

    let conn = Connection::open_in_memory().unwrap();
    register_graph_functions(&conn);

    let rows = conn
        .prepare(
            "SELECT node_id FROM graph_traverse(?, ?, CAST(? AS INTEGER), ?, ?) ORDER BY node_id",
        )
        .unwrap()
        .query_map(
            [fixture_path.as_str(), "r1", "2", "outbound", "instance"],
            |row| row.get::<_, String>(0),
        )
        .unwrap()
        .collect::<Result<Vec<_>, _>>()
        .unwrap();

    assert_eq!(rows, vec!["r2".to_string()]);
}

#[test]
fn graph_traverse_accepts_actual_sql_null_filter() {
    let temp_dir = TempDir::new().unwrap();
    let fixture_path = temp_dir
        .path()
        .join("fixture.duckdb")
        .to_string_lossy()
        .into_owned();

    let fixture_conn = Connection::open(&fixture_path).unwrap();
    make_fixture(&fixture_conn);
    drop(fixture_conn);

    let conn = Connection::open_in_memory().unwrap();
    register_graph_functions(&conn);

    let rows = conn
        .prepare("SELECT node_id FROM graph_traverse(?, ?, CAST(? AS INTEGER), ?, NULL) ORDER BY node_id")
        .unwrap()
        .query_map([fixture_path.as_str(), "r1", "3", "outbound"], |row| {
            row.get::<_, String>(0)
        })
        .unwrap()
        .collect::<Result<Vec<_>, _>>()
        .unwrap();

    assert_eq!(rows, vec!["r2".to_string(), "r3".to_string()]);
}

#[test]
fn graph_traverse_both_direction_deduplicates_bidirectional_neighbors() {
    let temp_dir = TempDir::new().unwrap();
    let fixture_path = temp_dir
        .path()
        .join("fixture.duckdb")
        .to_string_lossy()
        .into_owned();

    let fixture_conn = Connection::open(&fixture_path).unwrap();
    fixture_conn
        .execute_batch(
            "CREATE TABLE aws_resources (
                id VARCHAR, type VARCHAR, name VARCHAR, region VARCHAR,
                account_id VARCHAR, arn VARCHAR, tags VARCHAR
            );
            CREATE TABLE aws_relationships (
                from_id VARCHAR, to_id VARCHAR,
                relationship_type VARCHAR, properties VARCHAR
            );
            INSERT INTO aws_resources VALUES
                ('a','ec2','a','us-east-1','123',NULL,NULL),
                ('b','ec2','b','us-east-1','123',NULL,NULL);
            INSERT INTO aws_relationships VALUES
                ('a','b','outbound_rel',NULL),
                ('b','a','inbound_rel',NULL);",
        )
        .unwrap();
    drop(fixture_conn);

    let conn = Connection::open_in_memory().unwrap();
    register_graph_functions(&conn);

    let rows = conn
        .prepare("SELECT node_id, CAST(relationship_types AS VARCHAR) FROM graph_traverse(?, ?, CAST(? AS INTEGER), ?, ?) ORDER BY node_id")
        .unwrap()
        .query_map([fixture_path.as_str(), "a", "1", "both", "NULL"], |row| {
            Ok((row.get::<_, String>(0)?, row.get::<_, String>(1)?))
        })
        .unwrap()
        .collect::<Result<Vec<_>, _>>()
        .unwrap();

    assert_eq!(rows, vec![("b".to_string(), "[outbound_rel]".to_string())]);
}

#[test]
fn graph_shortest_path_returns_ordered_route() {
    let temp_dir = TempDir::new().unwrap();
    let fixture_path = temp_dir
        .path()
        .join("fixture.duckdb")
        .to_string_lossy()
        .into_owned();

    let fixture_conn = Connection::open(&fixture_path).unwrap();
    make_fixture(&fixture_conn);
    drop(fixture_conn);

    let conn = Connection::open_in_memory().unwrap();
    register_graph_functions(&conn);

    let rows = conn
        .prepare("SELECT hop_number, resource_id, incoming_rel_type, total_hops FROM graph_shortest_path(?, ?, ?, CAST(? AS BOOLEAN)) ORDER BY hop_number")
        .unwrap()
        .query_map([fixture_path.as_str(), "r1", "r3", "false"], |row| {
            Ok((
                row.get::<_, i32>(0)?,
                row.get::<_, String>(1)?,
                row.get::<_, Option<String>>(2)?,
                row.get::<_, i32>(3)?,
            ))
        })
        .unwrap()
        .collect::<Result<Vec<_>, _>>()
        .unwrap();

    assert_eq!(rows.len(), 2);
    assert_eq!(rows[0], (0, "r1".to_string(), None, 1));
    assert_eq!(rows[1], (1, "r3".to_string(), Some("reads".to_string()), 1));
}

#[test]
fn graph_shortest_path_returns_empty_when_source_missing() {
    let temp_dir = TempDir::new().unwrap();
    let fixture_path = temp_dir
        .path()
        .join("fixture.duckdb")
        .to_string_lossy()
        .into_owned();

    let fixture_conn = Connection::open(&fixture_path).unwrap();
    make_fixture(&fixture_conn);
    drop(fixture_conn);

    let conn = Connection::open_in_memory().unwrap();
    register_graph_functions(&conn);

    let count = conn
        .query_row(
            "SELECT COUNT(*) FROM graph_shortest_path(?, ?, ?, CAST(? AS BOOLEAN))",
            [fixture_path.as_str(), "missing", "r3", "false"],
            |row| row.get::<_, i64>(0),
        )
        .unwrap();

    assert_eq!(count, 0);
}

#[test]
fn graph_shortest_path_returns_empty_when_target_missing() {
    let temp_dir = TempDir::new().unwrap();
    let fixture_path = temp_dir
        .path()
        .join("fixture.duckdb")
        .to_string_lossy()
        .into_owned();

    let fixture_conn = Connection::open(&fixture_path).unwrap();
    make_fixture(&fixture_conn);
    drop(fixture_conn);

    let conn = Connection::open_in_memory().unwrap();
    register_graph_functions(&conn);

    let count = conn
        .query_row(
            "SELECT COUNT(*) FROM graph_shortest_path(?, ?, ?, CAST(? AS BOOLEAN))",
            [fixture_path.as_str(), "r1", "missing", "false"],
            |row| row.get::<_, i64>(0),
        )
        .unwrap();

    assert_eq!(count, 0);
}

#[test]
fn graph_shortest_path_returns_empty_when_no_path_exists() {
    let temp_dir = TempDir::new().unwrap();
    let fixture_path = temp_dir
        .path()
        .join("fixture.duckdb")
        .to_string_lossy()
        .into_owned();

    let fixture_conn = Connection::open(&fixture_path).unwrap();
    make_shortest_path_edge_fixture(&fixture_conn);
    drop(fixture_conn);

    let conn = Connection::open_in_memory().unwrap();
    register_graph_functions(&conn);

    let count = conn
        .query_row(
            "SELECT COUNT(*) FROM graph_shortest_path(?, ?, ?, CAST(? AS BOOLEAN))",
            [fixture_path.as_str(), "a", "isolated", "false"],
            |row| row.get::<_, i64>(0),
        )
        .unwrap();

    assert_eq!(count, 0);
}

#[test]
fn graph_shortest_path_returns_single_row_for_self_path() {
    let temp_dir = TempDir::new().unwrap();
    let fixture_path = temp_dir
        .path()
        .join("fixture.duckdb")
        .to_string_lossy()
        .into_owned();

    let fixture_conn = Connection::open(&fixture_path).unwrap();
    make_shortest_path_edge_fixture(&fixture_conn);
    drop(fixture_conn);

    let conn = Connection::open_in_memory().unwrap();
    register_graph_functions(&conn);

    let rows = conn
        .prepare("SELECT hop_number, resource_id, incoming_rel_type, total_hops FROM graph_shortest_path(?, ?, ?, CAST(? AS BOOLEAN))")
        .unwrap()
        .query_map([fixture_path.as_str(), "a", "a", "false"], |row| {
            Ok((
                row.get::<_, i32>(0)?,
                row.get::<_, String>(1)?,
                row.get::<_, Option<String>>(2)?,
                row.get::<_, i32>(3)?,
            ))
        })
        .unwrap()
        .collect::<Result<Vec<_>, _>>()
        .unwrap();

    assert_eq!(rows, vec![(0, "a".to_string(), None, 0)]);
}

#[test]
fn graph_reachable_reports_matches() {
    let temp_dir = TempDir::new().unwrap();
    let fixture_path = temp_dir
        .path()
        .join("fixture.duckdb")
        .to_string_lossy()
        .into_owned();

    let fixture_conn = Connection::open(&fixture_path).unwrap();
    make_fixture(&fixture_conn);
    drop(fixture_conn);

    let conn = Connection::open_in_memory().unwrap();
    register_graph_functions(&conn);

    let result = conn
        .query_row(
            "SELECT is_reachable, match_count, closest_hop, example_id FROM graph_reachable(?, ?, ?, CAST(? AS INTEGER))",
            [fixture_path.as_str(), "r1", "s3", "3"],
            |row| {
                Ok((
                    row.get::<_, bool>(0)?,
                    row.get::<_, i64>(1)?,
                    row.get::<_, i32>(2)?,
                    row.get::<_, String>(3)?,
                ))
            },
        )
        .unwrap();

    assert_eq!(result, (true, 1, 1, "r3".to_string()));
}

#[test]
fn graph_blast_radius_aggregates_by_type() {
    let temp_dir = TempDir::new().unwrap();
    let fixture_path = temp_dir
        .path()
        .join("fixture.duckdb")
        .to_string_lossy()
        .into_owned();

    let fixture_conn = Connection::open(&fixture_path).unwrap();
    make_fixture(&fixture_conn);
    drop(fixture_conn);

    let conn = Connection::open_in_memory().unwrap();
    register_graph_functions(&conn);

    let rows = conn
        .prepare("SELECT resource_type, reachable_count, max_hop_distance, CAST(sample_ids AS VARCHAR) FROM graph_blast_radius(?, ?, CAST(? AS INTEGER)) ORDER BY resource_type")
        .unwrap()
        .query_map([fixture_path.as_str(), "r1", "3"], |row| {
            Ok((
                row.get::<_, String>(0)?,
                row.get::<_, i32>(1)?,
                row.get::<_, i32>(2)?,
                row.get::<_, String>(3)?,
            ))
        })
        .unwrap()
        .collect::<Result<Vec<_>, _>>()
        .unwrap();

    assert_eq!(rows.len(), 2);
    assert_eq!(rows[0], ("ec2".to_string(), 1, 1, "[r2]".to_string()));
    assert_eq!(rows[1], ("s3".to_string(), 1, 1, "[r3]".to_string()));
}

#[test]
fn graph_match_pattern_matches_builtin_pattern() {
    let temp_dir = TempDir::new().unwrap();
    let fixture_path = temp_dir
        .path()
        .join("fixture.duckdb")
        .to_string_lossy()
        .into_owned();

    let fixture_conn = Connection::open(&fixture_path).unwrap();
    make_fixture(&fixture_conn);
    drop(fixture_conn);

    let conn = Connection::open_in_memory().unwrap();
    register_graph_functions(&conn);

    let rows = conn
        .prepare("SELECT match_id, pattern_node, resource_id FROM graph_match_pattern(?, ?) ORDER BY match_id, pattern_node")
        .unwrap()
        .query_map([fixture_path.as_str(), "public_s3_via_instance"], |row| {
            Ok((
                row.get::<_, i32>(0)?,
                row.get::<_, String>(1)?,
                row.get::<_, String>(2)?,
            ))
        })
        .unwrap()
        .collect::<Result<Vec<_>, _>>()
        .unwrap();

    assert_eq!(
        rows,
        vec![
            (0, "bucket".to_string(), "r3".to_string()),
            (0, "instance".to_string(), "r1".to_string()),
        ]
    );
}

#[test]
fn graph_match_pattern_matches_inline_json_pattern() {
    let temp_dir = TempDir::new().unwrap();
    let fixture_path = temp_dir
        .path()
        .join("fixture.duckdb")
        .to_string_lossy()
        .into_owned();

    let fixture_conn = Connection::open(&fixture_path).unwrap();
    make_fixture(&fixture_conn);
    drop(fixture_conn);

    let conn = Connection::open_in_memory().unwrap();
    register_graph_functions(&conn);

    let pattern_json = r#"{
        "name": "peer_link",
        "description": "EC2 peer relationship",
        "nodes": [
            { "label": "left", "type_filter": "ec2" },
            { "label": "right", "type_filter": "ec2" }
        ],
        "edges": [
            { "from": "left", "to": "right", "rel_filter": "peer" }
        ]
    }"#;

    let rows = conn
        .prepare("SELECT match_id, pattern_node, resource_id FROM graph_match_pattern(?, ?) ORDER BY match_id, pattern_node")
        .unwrap()
        .query_map([fixture_path.as_str(), pattern_json], |row| {
            Ok((
                row.get::<_, i32>(0)?,
                row.get::<_, String>(1)?,
                row.get::<_, String>(2)?,
            ))
        })
        .unwrap()
        .collect::<Result<Vec<_>, _>>()
        .unwrap();

    assert_eq!(
        rows,
        vec![
            (0, "left".to_string(), "r1".to_string()),
            (0, "right".to_string(), "r2".to_string()),
        ]
    );
}

#[test]
fn graph_match_pattern_matches_multihop_builtin_patterns() {
    let temp_dir = TempDir::new().unwrap();
    let fixture_path = temp_dir
        .path()
        .join("fixture.duckdb")
        .to_string_lossy()
        .into_owned();

    let fixture_conn = Connection::open(&fixture_path).unwrap();
    make_pattern_fixture(&fixture_conn);
    drop(fixture_conn);

    let conn = Connection::open_in_memory().unwrap();
    register_graph_functions(&conn);

    let expectations = [
        ("internet_to_database", 4_i64),
        ("public_lb_to_private_db", 3_i64),
        ("unencrypted_data_path", 3_i64),
        ("k8s_privileged_to_cloud", 3_i64),
    ];

    for (pattern_name, expected_rows) in expectations {
        let row_count = conn
            .query_row(
                "SELECT COUNT(*) FROM graph_match_pattern(?, ?)",
                [fixture_path.as_str(), pattern_name],
                |row| row.get::<_, i64>(0),
            )
            .unwrap();
        assert_eq!(
            row_count, expected_rows,
            "unexpected row count for {pattern_name}"
        );
    }
}

#[test]
fn graph_match_pattern_surfaces_truncation() {
    let temp_dir = TempDir::new().unwrap();
    let fixture_path = temp_dir
        .path()
        .join("fixture.duckdb")
        .to_string_lossy()
        .into_owned();

    let fixture_conn = Connection::open(&fixture_path).unwrap();
    make_many_public_s3_matches_fixture(&fixture_conn);
    drop(fixture_conn);

    let conn = Connection::open_in_memory().unwrap();
    register_graph_functions(&conn);

    // Truncation is detected by the caller: COUNT(DISTINCT match_id) == MAX_MATCHES.
    let distinct_matches = conn
        .query_row(
            "SELECT COUNT(DISTINCT match_id) FROM graph_match_pattern(?, ?)",
            [fixture_path.as_str(), "public_s3_via_instance"],
            |row| row.get::<_, i64>(0),
        )
        .unwrap();

    assert_eq!(distinct_matches, 256);
}

#[test]
fn graph_list_patterns_returns_builtin_registry() {
    let conn = Connection::open_in_memory().unwrap();
    register_graph_functions(&conn);

    let rows = conn
        .prepare("SELECT pattern_name, node_count, edge_count FROM graph_list_patterns() ORDER BY pattern_name")
        .unwrap()
        .query_map([], |row| {
            Ok((
                row.get::<_, String>(0)?,
                row.get::<_, i32>(1)?,
                row.get::<_, i32>(2)?,
            ))
        })
        .unwrap()
        .collect::<Result<Vec<_>, _>>()
        .unwrap();

    assert_eq!(
        rows,
        vec![
            ("cross_account_trust".to_string(), 2, 1),
            ("internet_to_database".to_string(), 4, 3),
            ("k8s_privileged_to_cloud".to_string(), 3, 2),
            ("lateral_movement_risk".to_string(), 2, 1),
            ("overprivileged_lambda".to_string(), 2, 1),
            ("public_lb_to_private_db".to_string(), 3, 2),
            ("public_s3_via_instance".to_string(), 2, 1),
            ("unencrypted_data_path".to_string(), 3, 2),
        ]
    );
}

#[test]
fn graph_cache_invalidate_table_function_returns_success() {
    let temp_dir = TempDir::new().unwrap();
    let fixture_path = temp_dir
        .path()
        .join("fixture.duckdb")
        .to_string_lossy()
        .into_owned();

    let fixture_conn = Connection::open(&fixture_path).unwrap();
    make_fixture(&fixture_conn);
    drop(fixture_conn);

    let conn = Connection::open_in_memory().unwrap();
    register_graph_functions(&conn);

    let before = conn
        .query_row(
            "SELECT nodes FROM graph_info(?)",
            [fixture_path.as_str()],
            |row| row.get::<_, i64>(0),
        )
        .unwrap();
    let invalidated = conn
        .query_row(
            "SELECT invalidated FROM graph_cache_invalidate(?)",
            [fixture_path.as_str()],
            |row| row.get::<_, i64>(0),
        )
        .unwrap();
    let after = conn
        .query_row(
            "SELECT nodes FROM graph_info(?)",
            [fixture_path.as_str()],
            |row| row.get::<_, i64>(0),
        )
        .unwrap();

    assert_eq!(before, 3);
    assert_eq!(invalidated, 1);
    assert_eq!(after, 3);
}

#[test]
fn cloud_path_macro_wraps_shortest_path() {
    let temp_dir = TempDir::new().unwrap();
    let fixture_path = temp_dir
        .path()
        .join("fixture.duckdb")
        .to_string_lossy()
        .into_owned();

    let fixture_conn = Connection::open(&fixture_path).unwrap();
    make_fixture(&fixture_conn);
    drop(fixture_conn);

    let conn = Connection::open_in_memory().unwrap();
    register_graph_functions(&conn);

    let rows = conn
        .prepare("SELECT hop_number, resource_id FROM cloud_path(?, ?, ?) ORDER BY hop_number")
        .unwrap()
        .query_map([fixture_path.as_str(), "r1", "r3"], |row| {
            Ok((row.get::<_, i32>(0)?, row.get::<_, String>(1)?))
        })
        .unwrap()
        .collect::<Result<Vec<_>, _>>()
        .unwrap();

    assert_eq!(rows, vec![(0, "r1".to_string()), (1, "r3".to_string())]);
}

#[test]
fn blast_radius_macro_wraps_blast_radius_function() {
    let temp_dir = TempDir::new().unwrap();
    let fixture_path = temp_dir
        .path()
        .join("fixture.duckdb")
        .to_string_lossy()
        .into_owned();

    let fixture_conn = Connection::open(&fixture_path).unwrap();
    make_fixture(&fixture_conn);
    drop(fixture_conn);

    let conn = Connection::open_in_memory().unwrap();
    register_graph_functions(&conn);

    let rows = conn
        .prepare("SELECT resource_type, reachable_count FROM blast_radius(?, ?, 3) ORDER BY resource_type")
        .unwrap()
        .query_map([fixture_path.as_str(), "r1"], |row| {
            Ok((row.get::<_, String>(0)?, row.get::<_, i32>(1)?))
        })
        .unwrap()
        .collect::<Result<Vec<_>, _>>()
        .unwrap();

    assert_eq!(rows, vec![("ec2".to_string(), 1), ("s3".to_string(), 1)]);
}

#[test]
fn attack_patterns_macro_wraps_pattern_listing() {
    let conn = Connection::open_in_memory().unwrap();
    register_graph_functions(&conn);

    let names = conn
        .prepare("SELECT pattern_name FROM attack_patterns() ORDER BY pattern_name")
        .unwrap()
        .query_map([], |row| row.get::<_, String>(0))
        .unwrap()
        .collect::<Result<Vec<_>, _>>()
        .unwrap();

    assert_eq!(
        names,
        vec![
            "cross_account_trust".to_string(),
            "internet_to_database".to_string(),
            "k8s_privileged_to_cloud".to_string(),
            "lateral_movement_risk".to_string(),
            "overprivileged_lambda".to_string(),
            "public_lb_to_private_db".to_string(),
            "public_s3_via_instance".to_string(),
            "unencrypted_data_path".to_string(),
        ]
    );
}

#[test]
fn load_graph_skips_relationships_with_missing_nodes() {
    let conn = Connection::open_in_memory().unwrap();
    make_fixture(&conn);
    conn.execute(
        "INSERT INTO aws_relationships VALUES ('missing','r1','broken',NULL)",
        [],
    )
    .unwrap();

    let providers = schema::detect_providers(&conn).unwrap();
    let loaded = loader::load_graph(&conn, &providers).unwrap();

    assert_eq!(loaded.node_count, 3);
    assert_eq!(loaded.edge_count, 2);
}

#[test]
fn detect_providers_finds_multiple_clouds() {
    let conn = Connection::open_in_memory().unwrap();
    conn.execute_batch(
        "CREATE TABLE aws_resources (
            id VARCHAR, type VARCHAR, name VARCHAR, region VARCHAR,
            account_id VARCHAR, arn VARCHAR, tags VARCHAR
        );
        CREATE TABLE aws_relationships (
            from_id VARCHAR, to_id VARCHAR,
            relationship_type VARCHAR, properties VARCHAR
        );
        CREATE TABLE azure_resources (
            id VARCHAR, type VARCHAR, name VARCHAR, region VARCHAR,
            account_id VARCHAR, arn VARCHAR, tags VARCHAR
        );
        CREATE TABLE azure_relationships (
            from_id VARCHAR, to_id VARCHAR,
            relationship_type VARCHAR, properties VARCHAR
        );",
    )
    .unwrap();

    let providers = schema::detect_providers(&conn).unwrap();
    assert_eq!(providers.len(), 2);
    assert_eq!(providers[0].provider, "aws");
    assert_eq!(providers[1].provider, "azure");
}

// --- PR4: chunking, mtime auto-invalidation, convention-based detection ---

/// Builds a star graph with one source and `leaves` leaves, all `reads`
/// relationships from the source. Produces row counts that span multiple
/// DuckDB output chunks (vector size ~2048).
fn make_star_fixture(conn: &Connection, leaves: usize) {
    conn.execute_batch(
        "CREATE TABLE aws_resources (
            id VARCHAR, type VARCHAR, name VARCHAR, region VARCHAR,
            account_id VARCHAR, arn VARCHAR, tags VARCHAR
        );
        CREATE TABLE aws_relationships (
            from_id VARCHAR, to_id VARCHAR,
            relationship_type VARCHAR, properties VARCHAR
        );
        INSERT INTO aws_resources VALUES
            ('hub','ec2','hub','us-east-1','123',NULL,NULL);",
    )
    .unwrap();

    let mut appender_res = conn.appender("aws_resources").unwrap();
    for i in 0..leaves {
        appender_res
            .append_row(duckdb::params![
                format!("leaf{i}"),
                "s3",
                format!("leaf-{i}"),
                "us-east-1",
                "123",
                Option::<String>::None,
                Option::<String>::None,
            ])
            .unwrap();
    }
    appender_res.flush().unwrap();
    drop(appender_res);

    let mut appender_rel = conn.appender("aws_relationships").unwrap();
    for i in 0..leaves {
        appender_rel
            .append_row(duckdb::params![
                "hub",
                format!("leaf{i}"),
                "reads",
                Option::<String>::None,
            ])
            .unwrap();
    }
    appender_rel.flush().unwrap();
}

#[test]
fn graph_traverse_returns_all_rows_across_multiple_chunks() {
    let temp_dir = TempDir::new().unwrap();
    let fixture_path = temp_dir
        .path()
        .join("fixture.duckdb")
        .to_string_lossy()
        .into_owned();

    let fixture_conn = Connection::open(&fixture_path).unwrap();
    make_star_fixture(&fixture_conn, 5000);
    drop(fixture_conn);

    let conn = Connection::open_in_memory().unwrap();
    register_graph_functions(&conn);

    let row_count: i64 = conn
        .query_row(
            "SELECT COUNT(*) FROM graph_traverse(?, ?, 1, 'outbound', 'NULL')",
            [fixture_path.as_str(), "hub"],
            |row| row.get(0),
        )
        .unwrap();
    assert_eq!(
        row_count, 5000,
        "traverse must emit every leaf across chunks"
    );

    // Sanity check that the chunked output still produces distinct rows (i.e.
    // we're not just emitting the same first 2048 over and over).
    let distinct: i64 = conn
        .query_row(
            "SELECT COUNT(DISTINCT node_id) FROM graph_traverse(?, ?, 1, 'outbound', 'NULL')",
            [fixture_path.as_str(), "hub"],
            |row| row.get(0),
        )
        .unwrap();
    assert_eq!(distinct, 5000);
}

#[test]
fn graph_blast_radius_chunks_correctly_with_many_types() {
    let temp_dir = TempDir::new().unwrap();
    let fixture_path = temp_dir
        .path()
        .join("fixture.duckdb")
        .to_string_lossy()
        .into_owned();

    // 3000 distinct resource types means blast's per-type rollup also crosses
    // the chunk boundary.
    let fixture_conn = Connection::open(&fixture_path).unwrap();
    fixture_conn
        .execute_batch(
            "CREATE TABLE aws_resources (
                id VARCHAR, type VARCHAR, name VARCHAR, region VARCHAR,
                account_id VARCHAR, arn VARCHAR, tags VARCHAR
            );
            CREATE TABLE aws_relationships (
                from_id VARCHAR, to_id VARCHAR,
                relationship_type VARCHAR, properties VARCHAR
            );
            INSERT INTO aws_resources VALUES
                ('hub','ec2','hub','us-east-1','123',NULL,NULL);",
        )
        .unwrap();

    let mut a_res = fixture_conn.appender("aws_resources").unwrap();
    let mut a_rel = fixture_conn.appender("aws_relationships").unwrap();
    for i in 0..3000 {
        a_res
            .append_row(duckdb::params![
                format!("leaf{i}"),
                format!("type{i}"),
                format!("leaf-{i}"),
                "us-east-1",
                "123",
                Option::<String>::None,
                Option::<String>::None,
            ])
            .unwrap();
        a_rel
            .append_row(duckdb::params![
                "hub",
                format!("leaf{i}"),
                "reads",
                Option::<String>::None,
            ])
            .unwrap();
    }
    a_res.flush().unwrap();
    a_rel.flush().unwrap();
    drop(a_res);
    drop(a_rel);
    drop(fixture_conn);

    let conn = Connection::open_in_memory().unwrap();
    register_graph_functions(&conn);

    let row_count: i64 = conn
        .query_row(
            "SELECT COUNT(*) FROM graph_blast_radius(?, ?, 3)",
            [fixture_path.as_str(), "hub"],
            |row| row.get(0),
        )
        .unwrap();
    assert_eq!(row_count, 3000);
}

#[test]
fn graph_reachable_handles_large_match_count() {
    let temp_dir = TempDir::new().unwrap();
    let fixture_path = temp_dir
        .path()
        .join("fixture.duckdb")
        .to_string_lossy()
        .into_owned();

    let fixture_conn = Connection::open(&fixture_path).unwrap();
    make_star_fixture(&fixture_conn, 5000);
    drop(fixture_conn);

    let conn = Connection::open_in_memory().unwrap();
    register_graph_functions(&conn);

    let (is_reachable, match_count, closest_hop) = conn
        .query_row(
            "SELECT is_reachable, match_count, closest_hop FROM graph_reachable(?, ?, ?, 3)",
            [fixture_path.as_str(), "hub", "s3"],
            |row| {
                Ok((
                    row.get::<_, bool>(0)?,
                    row.get::<_, i64>(1)?,
                    row.get::<_, i32>(2)?,
                ))
            },
        )
        .unwrap();
    assert!(is_reachable);
    assert_eq!(match_count, 5000);
    assert_eq!(closest_hop, 1);
}

#[test]
fn cache_auto_invalidates_when_file_changes_on_disk() {
    let temp_dir = TempDir::new().unwrap();
    let fixture_path = temp_dir
        .path()
        .join("fixture.duckdb")
        .to_string_lossy()
        .into_owned();

    {
        let conn1 = Connection::open(&fixture_path).unwrap();
        make_fixture(&conn1);
    }

    // Load via a connection that sees the file's tables — the cache uses this
    // conn to detect providers and read rows, and uses `fixture_path` as both
    // the cache key and the fingerprint source.
    let load_conn = Connection::open(&fixture_path).unwrap();
    let g1 = cache::get_or_load(&load_conn, &fixture_path).unwrap();
    assert_eq!(g1.node_count, 3);
    drop(load_conn);

    // Sleep past mtime resolution (1 s on most filesystems), then rewrite the
    // backing file with a different shape. No explicit graph_cache_invalidate.
    std::thread::sleep(std::time::Duration::from_millis(1100));
    std::fs::remove_file(&fixture_path).unwrap();
    {
        let conn2 = Connection::open(&fixture_path).unwrap();
        conn2
            .execute_batch(
                "CREATE TABLE aws_resources (
                    id VARCHAR, type VARCHAR, name VARCHAR, region VARCHAR,
                    account_id VARCHAR, arn VARCHAR, tags VARCHAR
                );
                CREATE TABLE aws_relationships (
                    from_id VARCHAR, to_id VARCHAR,
                    relationship_type VARCHAR, properties VARCHAR
                );
                INSERT INTO aws_resources VALUES
                    ('r1','ec2','i-1','us-east-1','123',NULL,NULL),
                    ('r2','ec2','i-2','us-east-1','123',NULL,NULL),
                    ('r3','s3','b-1','us-east-1','123',NULL,NULL),
                    ('r4','s3','b-2','us-east-1','123',NULL,NULL);
                INSERT INTO aws_relationships VALUES
                    ('r1','r2','peer',NULL),
                    ('r1','r3','reads',NULL);",
            )
            .unwrap();
    }

    let load_conn2 = Connection::open(&fixture_path).unwrap();
    let g2 = cache::get_or_load(&load_conn2, &fixture_path).unwrap();
    assert!(
        !std::sync::Arc::ptr_eq(&g1, &g2),
        "expected fresh load after file mtime change"
    );
    assert_eq!(g2.node_count, 4);
}

#[test]
fn detect_providers_auto_picks_up_unknown_prefix() {
    let conn = Connection::open_in_memory().unwrap();
    conn.execute_batch(
        "CREATE TABLE synthetic_resources (
            id VARCHAR, type VARCHAR, name VARCHAR, region VARCHAR,
            account_id VARCHAR, arn VARCHAR, tags VARCHAR
        );
        CREATE TABLE synthetic_relationships (
            from_id VARCHAR, to_id VARCHAR,
            relationship_type VARCHAR, properties VARCHAR
        );",
    )
    .unwrap();

    let providers = schema::detect_providers(&conn).unwrap();
    assert_eq!(providers.len(), 1);
    assert_eq!(providers[0].provider, "synthetic");
    assert_eq!(providers[0].resources_table, "synthetic_resources");
    assert_eq!(providers[0].relationships_table, "synthetic_relationships");
}

#[test]
fn detect_providers_ignores_unpaired_tables() {
    let conn = Connection::open_in_memory().unwrap();
    conn.execute_batch(
        "CREATE TABLE orphan_resources (
            id VARCHAR, type VARCHAR, name VARCHAR, region VARCHAR,
            account_id VARCHAR, arn VARCHAR, tags VARCHAR
        );
        CREATE TABLE solo_relationships (
            from_id VARCHAR, to_id VARCHAR,
            relationship_type VARCHAR, properties VARCHAR
        );",
    )
    .unwrap();

    let providers = schema::detect_providers(&conn).unwrap();
    assert!(
        providers.is_empty(),
        "prefixes without both tables must be skipped"
    );
}

#[test]
fn detect_providers_excludes_reserved_cloud_prefix() {
    let conn = Connection::open_in_memory().unwrap();
    conn.execute_batch(
        "CREATE TABLE cloud_resources (
            id VARCHAR, type VARCHAR, name VARCHAR, region VARCHAR,
            account_id VARCHAR, arn VARCHAR, tags VARCHAR
        );
        CREATE TABLE cloud_relationships (
            from_id VARCHAR, to_id VARCHAR,
            relationship_type VARCHAR, properties VARCHAR
        );
        CREATE TABLE aws_resources (
            id VARCHAR, type VARCHAR, name VARCHAR, region VARCHAR,
            account_id VARCHAR, arn VARCHAR, tags VARCHAR
        );
        CREATE TABLE aws_relationships (
            from_id VARCHAR, to_id VARCHAR,
            relationship_type VARCHAR, properties VARCHAR
        );",
    )
    .unwrap();

    let providers = schema::detect_providers(&conn).unwrap();
    assert_eq!(providers.len(), 1);
    assert_eq!(providers[0].provider, "aws");
}

#[test]
fn graph_correlate_ips_finds_cross_provider_shared_public_ip() {
    let temp_dir = TempDir::new().unwrap();
    let fixture_path = temp_dir
        .path()
        .join("fixture.duckdb")
        .to_string_lossy()
        .into_owned();

    let fixture_conn = Connection::open(&fixture_path).unwrap();
    make_cross_cloud_ip_fixture(&fixture_conn);
    drop(fixture_conn);

    let conn = Connection::open_in_memory().unwrap();
    register_graph_functions(&conn);

    let (source_id, target_id, source_provider, target_provider, confidence, evidence) = conn
        .query_row(
            "SELECT source_id, target_id, source_provider, target_provider, confidence, evidence
             FROM graph_correlate_ips(?, 0.8)
             ORDER BY source_id, target_id",
            [fixture_path.as_str()],
            |row| {
                Ok((
                    row.get::<_, String>(0)?,
                    row.get::<_, String>(1)?,
                    row.get::<_, String>(2)?,
                    row.get::<_, String>(3)?,
                    row.get::<_, f64>(4)?,
                    row.get::<_, String>(5)?,
                ))
            },
        )
        .unwrap();

    assert_eq!(source_id, "aws-vm-1");
    assert_eq!(target_id, "azure-vm-1");
    assert_eq!(source_provider, "aws");
    assert_eq!(target_provider, "azure");
    assert_eq!(confidence, 1.0);
    assert!(evidence.contains("\"shared_ip_address\":\"8.8.8.8\""));
    assert!(evidence.contains("\"ip_classification\":\"public\""));
}

#[test]
fn graph_correlate_ips_filters_by_min_confidence() {
    let temp_dir = TempDir::new().unwrap();
    let fixture_path = temp_dir
        .path()
        .join("fixture.duckdb")
        .to_string_lossy()
        .into_owned();

    let fixture_conn = Connection::open(&fixture_path).unwrap();
    make_cross_cloud_ip_fixture(&fixture_conn);
    drop(fixture_conn);

    let conn = Connection::open_in_memory().unwrap();
    register_graph_functions(&conn);

    let all_count: i64 = conn
        .query_row(
            "SELECT COUNT(*) FROM graph_correlate_ips(?, 0.5)",
            [fixture_path.as_str()],
            |row| row.get(0),
        )
        .unwrap();
    let high_confidence_count: i64 = conn
        .query_row(
            "SELECT COUNT(*) FROM graph_correlate_ips(?, 0.8)",
            [fixture_path.as_str()],
            |row| row.get(0),
        )
        .unwrap();

    assert_eq!(all_count, 2);
    assert_eq!(high_confidence_count, 1);
}

#[test]
fn graph_correlate_ips_returns_empty_when_ip_table_missing() {
    let temp_dir = TempDir::new().unwrap();
    let fixture_path = temp_dir
        .path()
        .join("fixture.duckdb")
        .to_string_lossy()
        .into_owned();

    let fixture_conn = Connection::open(&fixture_path).unwrap();
    make_fixture(&fixture_conn);
    drop(fixture_conn);

    let conn = Connection::open_in_memory().unwrap();
    register_graph_functions(&conn);

    let count: i64 = conn
        .query_row(
            "SELECT COUNT(*) FROM graph_correlate_ips(?, 0.5)",
            [fixture_path.as_str()],
            |row| row.get(0),
        )
        .unwrap();
    assert_eq!(count, 0);
}

#[test]
fn graph_correlate_dns_finds_name_and_cname_matches() {
    let temp_dir = TempDir::new().unwrap();
    let fixture_path = temp_dir
        .path()
        .join("fixture.duckdb")
        .to_string_lossy()
        .into_owned();

    let fixture_conn = Connection::open(&fixture_path).unwrap();
    make_cross_cloud_dns_fixture(&fixture_conn);
    drop(fixture_conn);

    let conn = Connection::open_in_memory().unwrap();
    register_graph_functions(&conn);

    let count_without_cname: i64 = conn
        .query_row(
            "SELECT COUNT(*) FROM graph_correlate_dns(?, false, 0.8)",
            [fixture_path.as_str()],
            |row| row.get(0),
        )
        .unwrap();
    let count_with_cname: i64 = conn
        .query_row(
            "SELECT COUNT(*) FROM graph_correlate_dns(?, true, 0.8)",
            [fixture_path.as_str()],
            |row| row.get(0),
        )
        .unwrap();
    let evidence: String = conn
        .query_row(
            "SELECT evidence FROM graph_correlate_dns(?, true, 0.8) WHERE correlation_type = 'dns_cname_target_match'",
            [fixture_path.as_str()],
            |row| row.get(0),
        )
        .unwrap();

    assert_eq!(count_without_cname, 1);
    assert_eq!(count_with_cname, 2);
    assert!(evidence.contains("edge.example.net"));
}

#[test]
fn graph_correlate_networks_uses_explicit_topology() {
    let temp_dir = TempDir::new().unwrap();
    let fixture_path = temp_dir
        .path()
        .join("fixture.duckdb")
        .to_string_lossy()
        .into_owned();

    let fixture_conn = Connection::open(&fixture_path).unwrap();
    make_cross_cloud_network_fixture(&fixture_conn);
    drop(fixture_conn);

    let conn = Connection::open_in_memory().unwrap();
    register_graph_functions(&conn);

    let (source_id, target_id, confidence, evidence): (String, String, f64, String) = conn
        .query_row(
            "SELECT source_id, target_id, confidence, evidence FROM graph_correlate_networks(?, 0.8)",
            [fixture_path.as_str()],
            |row| Ok((row.get(0)?, row.get(1)?, row.get(2)?, row.get(3)?)),
        )
        .unwrap();

    assert_eq!(source_id, "vpc-1");
    assert_eq!(target_id, "vnet-1");
    assert_eq!(confidence, 0.9500000000000001);
    assert!(evidence.contains("topology-backed explicit connection only"));
}

#[test]
fn graph_correlate_load_balancers_finds_shared_backend() {
    let temp_dir = TempDir::new().unwrap();
    let fixture_path = temp_dir
        .path()
        .join("fixture.duckdb")
        .to_string_lossy()
        .into_owned();

    let fixture_conn = Connection::open(&fixture_path).unwrap();
    make_cross_cloud_load_balancer_fixture(&fixture_conn);
    drop(fixture_conn);

    let conn = Connection::open_in_memory().unwrap();
    register_graph_functions(&conn);

    let (source_id, target_id, confidence, evidence): (String, String, f64, String) = conn
        .query_row(
            "SELECT source_id, target_id, confidence, evidence FROM graph_correlate_load_balancers(?, 0.8)",
            [fixture_path.as_str()],
            |row| Ok((row.get(0)?, row.get(1)?, row.get(2)?, row.get(3)?)),
        )
        .unwrap();

    assert_eq!(source_id, "aws-lb-1");
    assert_eq!(target_id, "azure-lb-1");
    assert_eq!(confidence, 0.9);
    assert!(evidence.contains("10.0.1.10"));
}

#[test]
fn graph_correlate_connectivity_finds_explicit_vpn() {
    let temp_dir = TempDir::new().unwrap();
    let fixture_path = temp_dir
        .path()
        .join("fixture.duckdb")
        .to_string_lossy()
        .into_owned();

    let fixture_conn = Connection::open(&fixture_path).unwrap();
    make_cross_cloud_connectivity_fixture(&fixture_conn);
    drop(fixture_conn);

    let conn = Connection::open_in_memory().unwrap();
    register_graph_functions(&conn);

    let (source_id, target_id, confidence, evidence): (String, String, f64, String) = conn
        .query_row(
            "SELECT source_id, target_id, confidence, evidence FROM graph_correlate_connectivity(?, 0.8)",
            [fixture_path.as_str()],
            |row| Ok((row.get(0)?, row.get(1)?, row.get(2)?, row.get(3)?)),
        )
        .unwrap();

    assert_eq!(source_id, "aws-vgw-1");
    assert_eq!(target_id, "azure-vng-1");
    assert_eq!(confidence, 0.96);
    assert!(evidence.contains("cross_cloud_vpn_connections"));
}

#[test]
fn graph_correlate_security_uses_security_correlation_table() {
    let temp_dir = TempDir::new().unwrap();
    let fixture_path = temp_dir
        .path()
        .join("fixture.duckdb")
        .to_string_lossy()
        .into_owned();

    let fixture_conn = Connection::open(&fixture_path).unwrap();
    make_cross_cloud_security_fixture(&fixture_conn);
    drop(fixture_conn);

    let conn = Connection::open_in_memory().unwrap();
    register_graph_functions(&conn);

    let (source_id, target_id, confidence, evidence): (String, String, f64, String) = conn
        .query_row(
            "SELECT source_id, target_id, confidence, evidence FROM graph_correlate_security(?, 0.8)",
            [fixture_path.as_str()],
            |row| Ok((row.get(0)?, row.get(1)?, row.get(2)?, row.get(3)?)),
        )
        .unwrap();

    assert_eq!(source_id, "aws-sg-1");
    assert_eq!(target_id, "az-nsg-1");
    assert_eq!(confidence, 0.91);
    assert!(evidence.contains("public_https"));
}

#[test]
fn graph_correlate_domains_finds_dns_and_certificate_matches() {
    let temp_dir = TempDir::new().unwrap();
    let fixture_path = temp_dir
        .path()
        .join("fixture.duckdb")
        .to_string_lossy()
        .into_owned();

    let fixture_conn = Connection::open(&fixture_path).unwrap();
    make_cross_cloud_domain_fixture(&fixture_conn);
    drop(fixture_conn);

    let conn = Connection::open_in_memory().unwrap();
    register_graph_functions(&conn);

    let count: i64 = conn
        .query_row(
            "SELECT COUNT(*) FROM graph_correlate_domains(?, 0.8)",
            [fixture_path.as_str()],
            |row| row.get(0),
        )
        .unwrap();
    let evidence: String = conn
        .query_row(
            "SELECT evidence FROM graph_correlate_domains(?, 0.9) WHERE correlation_type = 'domain_certificate_match'",
            [fixture_path.as_str()],
            |row| row.get(0),
        )
        .unwrap();

    assert_eq!(count, 2);
    assert!(evidence.contains("api.example.com"));
}

#[test]
fn graph_correlate_identity_finds_federation_relationship() {
    let temp_dir = TempDir::new().unwrap();
    let fixture_path = temp_dir
        .path()
        .join("fixture.duckdb")
        .to_string_lossy()
        .into_owned();

    let fixture_conn = Connection::open(&fixture_path).unwrap();
    make_cross_cloud_identity_fixture(&fixture_conn);
    drop(fixture_conn);

    let conn = Connection::open_in_memory().unwrap();
    register_graph_functions(&conn);

    let (source_id, target_id, confidence, evidence): (String, String, f64, String) = conn
        .query_row(
            "SELECT source_id, target_id, confidence, evidence FROM graph_correlate_identity(?, 0.8)",
            [fixture_path.as_str()],
            |row| Ok((row.get(0)?, row.get(1)?, row.get(2)?, row.get(3)?)),
        )
        .unwrap();

    assert_eq!(source_id, "aws-oidc-1");
    assert_eq!(target_id, "azure-app-1");
    assert_eq!(confidence, 0.94);
    assert!(evidence.contains("https://issuer.example.com"));
}

#[test]
fn graph_correlate_policies_finds_similarity_row() {
    let temp_dir = TempDir::new().unwrap();
    let fixture_path = temp_dir
        .path()
        .join("fixture.duckdb")
        .to_string_lossy()
        .into_owned();

    let fixture_conn = Connection::open(&fixture_path).unwrap();
    make_cross_cloud_policy_fixture(&fixture_conn);
    drop(fixture_conn);

    let conn = Connection::open_in_memory().unwrap();
    register_graph_functions(&conn);

    let (source_id, target_id, confidence, evidence): (String, String, f64, String) = conn
        .query_row(
            "SELECT source_id, target_id, confidence, evidence FROM graph_correlate_policies(?, 0.8)",
            [fixture_path.as_str()],
            |row| Ok((row.get(0)?, row.get(1)?, row.get(2)?, row.get(3)?)),
        )
        .unwrap();

    assert_eq!(source_id, "aws-pol-1");
    assert_eq!(target_id, "az-roledef-1");
    assert_eq!(confidence, 0.92);
    assert!(evidence.contains("highly_similar"));
}

#[test]
fn graph_correlate_secrets_finds_shared_hash_across_providers() {
    let temp_dir = TempDir::new().unwrap();
    let fixture_path = temp_dir
        .path()
        .join("fixture.duckdb")
        .to_string_lossy()
        .into_owned();

    let fixture_conn = Connection::open(&fixture_path).unwrap();
    make_cross_cloud_secret_fixture(&fixture_conn);
    drop(fixture_conn);

    let conn = Connection::open_in_memory().unwrap();
    register_graph_functions(&conn);

    let (source_id, target_id, confidence, evidence): (String, String, f64, String) = conn
        .query_row(
            "SELECT source_id, target_id, confidence, evidence FROM graph_correlate_secrets(?, 0.8)",
            [fixture_path.as_str()],
            |row| Ok((row.get(0)?, row.get(1)?, row.get(2)?, row.get(3)?)),
        )
        .unwrap();

    assert_eq!(source_id, "aws-secret-1");
    assert_eq!(target_id, "az-secret-1");
    assert_eq!(confidence, 0.91);
    assert!(evidence.contains("hash-123"));
}
