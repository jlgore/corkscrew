package db

import (
	"database/sql"
	"fmt"
)

// NetworkSchemaExtensions extends the unified database schema with Phase 2 network topology features
type NetworkSchemaExtensions struct {
	db *sql.DB
}

// NewNetworkSchemaExtensions creates a new network schema extension manager
func NewNetworkSchemaExtensions(db *sql.DB) *NetworkSchemaExtensions {
	return &NetworkSchemaExtensions{
		db: db,
	}
}

// CreateNetworkTopologyExtensions creates additional tables for Phase 2 network topology features
func (n *NetworkSchemaExtensions) CreateNetworkTopologyExtensions() error {
	// Create VPN connections table
	if err := n.createVPNConnectionsTable(); err != nil {
		return fmt.Errorf("failed to create VPN connections table: %w", err)
	}

	// Create network peering table
	if err := n.createNetworkPeeringTable(); err != nil {
		return fmt.Errorf("failed to create network peering table: %w", err)
	}

	// Create direct connections table
	if err := n.createDirectConnectionsTable(); err != nil {
		return fmt.Errorf("failed to create direct connections table: %w", err)
	}

	// Create enhanced DNS records table
	if err := n.createEnhancedDNSTable(); err != nil {
		return fmt.Errorf("failed to create enhanced DNS table: %w", err)
	}

	// Create load balancer topology table
	if err := n.createLoadBalancerTopologyTable(); err != nil {
		return fmt.Errorf("failed to create load balancer topology table: %w", err)
	}

	// Create security rule correlations table
	if err := n.createSecurityRuleCorrelationsTable(); err != nil {
		return fmt.Errorf("failed to create security rule correlations table: %w", err)
	}

	// Create network visualization metadata table
	if err := n.createVisualizationMetadataTable(); err != nil {
		return fmt.Errorf("failed to create visualization metadata table: %w", err)
	}

	return nil
}

// createVPNConnectionsTable creates a detailed VPN connections tracking table
func (n *NetworkSchemaExtensions) createVPNConnectionsTable() error {
	vpnSQL := `
CREATE TABLE IF NOT EXISTS cross_cloud_vpn_connections (
    -- Primary identifiers
    id VARCHAR PRIMARY KEY,                    -- Unique VPN connection ID
    connection_name VARCHAR NOT NULL,          -- VPN connection name
    
    -- Source endpoint
    source_resource_id VARCHAR NOT NULL,       -- Source VPN gateway/resource ID
    source_provider VARCHAR NOT NULL,          -- Source provider
    source_region VARCHAR NOT NULL,            -- Source region
    source_gateway_id VARCHAR,                 -- Source gateway ID
    source_public_ip VARCHAR,                  -- Source public IP
    source_local_networks JSON,                -- Source local network CIDRs
    
    -- Target endpoint
    target_resource_id VARCHAR NOT NULL,       -- Target VPN gateway/resource ID
    target_provider VARCHAR NOT NULL,          -- Target provider
    target_region VARCHAR NOT NULL,            -- Target region
    target_gateway_id VARCHAR,                 -- Target gateway ID
    target_public_ip VARCHAR,                  -- Target public IP
    target_remote_networks JSON,               -- Target remote network CIDRs
    
    -- Connection details
    connection_type VARCHAR NOT NULL,          -- site_to_site, point_to_site
    ike_version VARCHAR,                       -- IKE version (1.0, 2.0)
    encryption_algorithm VARCHAR,              -- Encryption algorithm
    authentication_method VARCHAR,             -- Authentication method
    shared_key_configured BOOLEAN,             -- Whether shared key is configured
    
    -- Tunnel information
    tunnel_count INTEGER DEFAULT 1,            -- Number of tunnels
    tunnel_status JSON,                        -- Status of each tunnel
    routing_type VARCHAR,                      -- static, dynamic
    bgp_asn_source VARCHAR,                    -- Source BGP ASN
    bgp_asn_target VARCHAR,                    -- Target BGP ASN
    
    -- Status and health
    connection_status VARCHAR NOT NULL,        -- active, inactive, pending
    last_status_change TIMESTAMP,              -- When status last changed
    uptime_percentage DOUBLE,                  -- Uptime percentage
    last_health_check TIMESTAMP,               -- Last health check
    
    -- Traffic and performance
    bytes_transferred_in BIGINT DEFAULT 0,     -- Bytes received
    bytes_transferred_out BIGINT DEFAULT 0,    -- Bytes sent
    packets_transferred_in BIGINT DEFAULT 0,   -- Packets received
    packets_transferred_out BIGINT DEFAULT 0,  -- Packets sent
    average_latency_ms DOUBLE,                 -- Average latency
    
    -- Configuration metadata
    mtu_size INTEGER,                          -- MTU size
    keepalive_interval INTEGER,                -- Keep-alive interval
    dead_peer_detection BOOLEAN,               -- DPD enabled
    nat_traversal BOOLEAN,                     -- NAT traversal enabled
    
    -- Tags and metadata
    tags JSON,                                 -- Connection tags
    metadata JSON,                             -- Additional metadata
    
    -- Correlation information
    correlation_id VARCHAR,                    -- Cross-cloud correlation ID
    confidence_score DOUBLE,                   -- Correlation confidence
    correlation_method VARCHAR,                -- How correlation was discovered
    
    -- Timestamps
    created_at TIMESTAMP,                      -- When connection was created
    discovered_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);`

	if _, err := n.db.Exec(vpnSQL); err != nil {
		return err
	}

	// Create indexes
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_vpn_source_resource ON cross_cloud_vpn_connections(source_resource_id)",
		"CREATE INDEX IF NOT EXISTS idx_vpn_target_resource ON cross_cloud_vpn_connections(target_resource_id)",
		"CREATE INDEX IF NOT EXISTS idx_vpn_providers ON cross_cloud_vpn_connections(source_provider, target_provider)",
		"CREATE INDEX IF NOT EXISTS idx_vpn_status ON cross_cloud_vpn_connections(connection_status)",
		"CREATE INDEX IF NOT EXISTS idx_vpn_correlation ON cross_cloud_vpn_connections(correlation_id)",
	}

	for _, idx := range indexes {
		if _, err := n.db.Exec(idx); err != nil {
			return fmt.Errorf("failed to create VPN index: %w", err)
		}
	}

	return nil
}

// createNetworkPeeringTable creates a detailed network peering tracking table
func (n *NetworkSchemaExtensions) createNetworkPeeringTable() error {
	peeringSQL := `
CREATE TABLE IF NOT EXISTS cross_cloud_network_peering (
    -- Primary identifiers
    id VARCHAR PRIMARY KEY,                    -- Unique peering connection ID
    peering_name VARCHAR NOT NULL,             -- Peering connection name
    
    -- Source network
    source_network_id VARCHAR NOT NULL,       -- Source VPC/VNet ID
    source_network_name VARCHAR,              -- Source network name
    source_provider VARCHAR NOT NULL,         -- Source provider
    source_region VARCHAR NOT NULL,           -- Source region
    source_account_id VARCHAR,                -- Source account/subscription
    source_cidr_blocks JSON,                  -- Source CIDR blocks
    source_route_tables JSON,                 -- Source route tables
    
    -- Target network
    target_network_id VARCHAR NOT NULL,       -- Target VPC/VNet ID
    target_network_name VARCHAR,              -- Target network name
    target_provider VARCHAR NOT NULL,         -- Target provider
    target_region VARCHAR NOT NULL,           -- Target region
    target_account_id VARCHAR,                -- Target account/subscription
    target_cidr_blocks JSON,                  -- Target CIDR blocks
    target_route_tables JSON,                 -- Target route tables
    
    -- Peering configuration
    peering_type VARCHAR NOT NULL,            -- vpc_peering, vnet_peering, network_peering
    peering_state VARCHAR NOT NULL,           -- active, pending-acceptance, failed
    bidirectional BOOLEAN DEFAULT TRUE,       -- Is peering bidirectional
    allow_forwarded_traffic BOOLEAN,          -- Allow forwarded traffic
    allow_gateway_transit BOOLEAN,            -- Allow gateway transit
    use_remote_gateways BOOLEAN,              -- Use remote gateways
    
    -- DNS resolution
    dns_resolution_enabled BOOLEAN,           -- DNS resolution across peering
    allow_classic_link_over_peering BOOLEAN,  -- Allow classic link (AWS)
    
    -- Routing information
    route_propagation_enabled BOOLEAN,        -- Route propagation enabled
    static_routes JSON,                        -- Static routes configured
    advertised_routes JSON,                    -- Advertised routes
    received_routes JSON,                      -- Received routes
    
    -- Access control
    security_group_references JSON,           -- Cross-peering security group refs
    network_acl_associations JSON,            -- Network ACL associations
    
    -- Status and monitoring
    peering_status VARCHAR NOT NULL,           -- active, inactive, failed
    last_status_change TIMESTAMP,             -- When status last changed
    monitoring_enabled BOOLEAN,               -- Monitoring enabled
    flow_logs_enabled BOOLEAN,                -- Flow logs enabled
    
    -- Performance metrics
    data_transfer_in_gb DOUBLE DEFAULT 0,     -- Data transferred in GB
    data_transfer_out_gb DOUBLE DEFAULT 0,    -- Data transferred out GB
    connection_count INTEGER DEFAULT 0,       -- Active connections
    bandwidth_utilization DOUBLE,             -- Bandwidth utilization %
    
    -- Cost tracking
    data_transfer_cost DOUBLE,                -- Data transfer costs
    hourly_connection_cost DOUBLE,            -- Hourly connection costs
    
    -- Tags and metadata
    tags JSON,                                 -- Peering tags
    metadata JSON,                             -- Additional metadata
    
    -- Correlation information
    correlation_id VARCHAR,                    -- Cross-cloud correlation ID
    confidence_score DOUBLE,                   -- Correlation confidence
    correlation_method VARCHAR,                -- How correlation was discovered
    
    -- Timestamps
    created_at TIMESTAMP,                      -- When peering was created
    discovered_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);`

	if _, err := n.db.Exec(peeringSQL); err != nil {
		return err
	}

	// Create indexes
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_peering_source_network ON cross_cloud_network_peering(source_network_id)",
		"CREATE INDEX IF NOT EXISTS idx_peering_target_network ON cross_cloud_network_peering(target_network_id)",
		"CREATE INDEX IF NOT EXISTS idx_peering_providers ON cross_cloud_network_peering(source_provider, target_provider)",
		"CREATE INDEX IF NOT EXISTS idx_peering_state ON cross_cloud_network_peering(peering_state)",
		"CREATE INDEX IF NOT EXISTS idx_peering_status ON cross_cloud_network_peering(peering_status)",
		"CREATE INDEX IF NOT EXISTS idx_peering_correlation ON cross_cloud_network_peering(correlation_id)",
	}

	for _, idx := range indexes {
		if _, err := n.db.Exec(idx); err != nil {
			return fmt.Errorf("failed to create peering index: %w", err)
		}
	}

	return nil
}

// createDirectConnectionsTable creates a detailed direct connections tracking table
func (n *NetworkSchemaExtensions) createDirectConnectionsTable() error {
	directSQL := `
CREATE TABLE IF NOT EXISTS cross_cloud_direct_connections (
    -- Primary identifiers
    id VARCHAR PRIMARY KEY,                    -- Unique connection ID
    connection_name VARCHAR NOT NULL,          -- Connection name
    
    -- Source endpoint
    source_resource_id VARCHAR NOT NULL,       -- Source resource ID
    source_provider VARCHAR NOT NULL,          -- Source provider
    source_region VARCHAR,                     -- Source region
    source_location VARCHAR,                   -- Physical location/facility
    source_vlan INTEGER,                       -- Source VLAN ID
    source_bandwidth VARCHAR,                  -- Source bandwidth
    
    -- Target endpoint
    target_resource_id VARCHAR NOT NULL,       -- Target resource ID
    target_provider VARCHAR NOT NULL,          -- Target provider
    target_region VARCHAR,                     -- Target region
    target_location VARCHAR,                   -- Physical location/facility
    target_vlan INTEGER,                       -- Target VLAN ID
    target_bandwidth VARCHAR,                  -- Target bandwidth
    
    -- Connection details
    connection_type VARCHAR NOT NULL,          -- direct_connect, expressroute, interconnect
    circuit_id VARCHAR,                        -- Circuit identifier
    service_provider VARCHAR,                  -- Service provider name
    service_provider_contact JSON,             -- Provider contact information
    
    -- Physical characteristics
    port_speed VARCHAR,                        -- Port speed (1Gbps, 10Gbps, etc.)
    port_type VARCHAR,                         -- Port type (fiber, copper)
    fiber_type VARCHAR,                        -- Fiber type (single-mode, multi-mode)
    connector_type VARCHAR,                    -- Connector type (LC, SC, etc.)
    
    -- Virtual interfaces
    virtual_interfaces JSON,                   -- Virtual interface configurations
    bgp_sessions JSON,                         -- BGP session information
    vlan_configurations JSON,                 -- VLAN configurations
    
    -- Routing configuration
    customer_asn INTEGER,                      -- Customer BGP ASN
    provider_asn INTEGER,                      -- Provider BGP ASN
    bgp_authentication_key VARCHAR,           -- BGP auth key (encrypted)
    advertised_prefixes JSON,                 -- Advertised IP prefixes
    received_prefixes JSON,                   -- Received IP prefixes
    
    -- Status and health
    connection_state VARCHAR NOT NULL,         -- available, down, pending
    link_status VARCHAR,                       -- up, down
    bgp_status VARCHAR,                        -- established, idle, active
    last_status_change TIMESTAMP,             -- When status last changed
    
    -- Performance metrics
    bandwidth_utilization_in DOUBLE,          -- Inbound bandwidth utilization %
    bandwidth_utilization_out DOUBLE,         -- Outbound bandwidth utilization %
    packet_loss_rate DOUBLE,                  -- Packet loss rate %
    latency_ms DOUBLE,                         -- Average latency in milliseconds
    error_count BIGINT DEFAULT 0,             -- Error count
    
    -- Traffic statistics
    bytes_in BIGINT DEFAULT 0,                -- Bytes received
    bytes_out BIGINT DEFAULT 0,               -- Bytes sent
    packets_in BIGINT DEFAULT 0,              -- Packets received
    packets_out BIGINT DEFAULT 0,             -- Packets sent
    
    -- Cost and billing
    monthly_cost DOUBLE,                       -- Monthly cost
    hourly_cost DOUBLE,                        -- Hourly cost
    data_transfer_cost DOUBLE,                 -- Data transfer costs
    billing_start_date TIMESTAMP,             -- Billing start date
    
    -- Redundancy and failover
    redundancy_level VARCHAR,                  -- none, local, regional, global
    failover_configuration JSON,              -- Failover configuration
    backup_connections JSON,                  -- Backup connection IDs
    
    -- Security
    macsec_enabled BOOLEAN DEFAULT FALSE,     -- MACsec encryption enabled
    encryption_type VARCHAR,                  -- Encryption type
    security_protocols JSON,                  -- Security protocols used
    
    -- Tags and metadata
    tags JSON,                                 -- Connection tags
    metadata JSON,                             -- Additional metadata
    sla_parameters JSON,                       -- SLA parameters
    
    -- Correlation information
    correlation_id VARCHAR,                    -- Cross-cloud correlation ID
    confidence_score DOUBLE,                   -- Correlation confidence
    correlation_method VARCHAR,                -- How correlation was discovered
    
    -- Timestamps
    created_at TIMESTAMP,                      -- When connection was created
    last_maintenance TIMESTAMP,               -- Last maintenance window
    next_maintenance TIMESTAMP,               -- Next scheduled maintenance
    discovered_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);`

	if _, err := n.db.Exec(directSQL); err != nil {
		return err
	}

	// Create indexes
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_direct_source_resource ON cross_cloud_direct_connections(source_resource_id)",
		"CREATE INDEX IF NOT EXISTS idx_direct_target_resource ON cross_cloud_direct_connections(target_resource_id)",
		"CREATE INDEX IF NOT EXISTS idx_direct_providers ON cross_cloud_direct_connections(source_provider, target_provider)",
		"CREATE INDEX IF NOT EXISTS idx_direct_state ON cross_cloud_direct_connections(connection_state)",
		"CREATE INDEX IF NOT EXISTS idx_direct_circuit ON cross_cloud_direct_connections(circuit_id)",
		"CREATE INDEX IF NOT EXISTS idx_direct_provider ON cross_cloud_direct_connections(service_provider)",
		"CREATE INDEX IF NOT EXISTS idx_direct_correlation ON cross_cloud_direct_connections(correlation_id)",
	}

	for _, idx := range indexes {
		if _, err := n.db.Exec(idx); err != nil {
			return fmt.Errorf("failed to create direct connection index: %w", err)
		}
	}

	return nil
}

// createEnhancedDNSTable creates an enhanced DNS records table for Phase 2 features
func (n *NetworkSchemaExtensions) createEnhancedDNSTable() error {
	enhancedDNSSQL := `
CREATE TABLE IF NOT EXISTS cross_cloud_enhanced_dns (
    -- Primary identifiers
    id VARCHAR PRIMARY KEY,                    -- Unique DNS record ID
    dns_name VARCHAR NOT NULL,                 -- DNS name
    
    -- DNS record details
    record_type VARCHAR NOT NULL,              -- A, AAAA, CNAME, MX, TXT, SRV, etc.
    record_values JSON NOT NULL,               -- Array of record values
    ttl INTEGER,                               -- Time to live
    priority INTEGER,                          -- Priority (for MX, SRV records)
    weight INTEGER,                            -- Weight for load balancing
    port INTEGER,                              -- Port (for SRV records)
    
    -- Multi-provider DNS management
    provider_records JSON,                     -- Records per provider
    synchronization_status VARCHAR,            -- synchronized, divergent, unknown
    last_sync_check TIMESTAMP,                -- Last synchronization check
    sync_conflicts JSON,                       -- Synchronization conflicts
    
    -- Geographic routing
    geo_location JSON,                         -- Geographic location config
    geo_continent VARCHAR,                     -- Continent code
    geo_country VARCHAR,                       -- Country code
    geo_subdivision VARCHAR,                   -- State/province code
    
    -- Health checks and monitoring
    health_check_config JSON,                 -- Health check configuration
    health_check_status VARCHAR,              -- healthy, unhealthy, unknown
    last_health_check TIMESTAMP,              -- Last health check time
    health_check_failures INTEGER DEFAULT 0,  -- Consecutive failures
    
    -- Load balancing configuration
    load_balancing_type VARCHAR,              -- round_robin, weighted, geolocation, latency
    routing_policy VARCHAR,                   -- simple, weighted, latency, geolocation, failover
    routing_policy_config JSON,               -- Routing policy configuration
    failover_config JSON,                     -- Failover configuration
    
    -- CNAME chain information
    cname_chain JSON,                          -- Complete CNAME resolution chain
    cname_depth INTEGER DEFAULT 0,            -- CNAME resolution depth
    cname_loop_detected BOOLEAN DEFAULT FALSE, -- CNAME loop detection
    ultimate_target VARCHAR,                   -- Final resolved target
    
    -- Cross-cloud correlation
    correlated_records JSON,                  -- Related DNS records across providers
    correlation_confidence DOUBLE,            -- Correlation confidence
    correlation_evidence JSON,                -- Evidence for correlation
    
    -- Traffic and performance
    query_count_daily BIGINT DEFAULT 0,       -- Daily query count
    query_count_weekly BIGINT DEFAULT 0,      -- Weekly query count
    response_time_avg_ms DOUBLE,              -- Average response time
    response_time_p95_ms DOUBLE,              -- 95th percentile response time
    
    -- Security
    dnssec_enabled BOOLEAN DEFAULT FALSE,     -- DNSSEC enabled
    dnssec_validation_status VARCHAR,         -- DNSSEC validation status
    certificate_transparency BOOLEAN,         -- Certificate transparency logging
    
    -- Provider-specific metadata
    resource_id VARCHAR,                       -- Associated resource ID (if any)
    resource_type VARCHAR,                     -- Associated resource type
    provider VARCHAR NOT NULL,                 -- DNS provider
    region VARCHAR,                            -- Region (if applicable)
    account_id VARCHAR,                        -- Account/subscription ID
    
    -- DNS service metadata
    dns_service VARCHAR,                       -- DNS service (Route53, Azure DNS, etc.)
    zone_id VARCHAR,                           -- DNS zone ID
    zone_name VARCHAR,                         -- DNS zone name
    hosted_zone_config JSON,                  -- Hosted zone configuration
    
    -- Tags and metadata
    tags JSON,                                 -- DNS record tags
    metadata JSON,                             -- Additional metadata
    
    -- Timestamps
    created_at TIMESTAMP,                      -- When DNS record was created
    last_modified TIMESTAMP,                  -- Last modification time
    discovered_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);`

	if _, err := n.db.Exec(enhancedDNSSQL); err != nil {
		return err
	}

	// Create indexes
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_enhanced_dns_name ON cross_cloud_enhanced_dns(dns_name)",
		"CREATE INDEX IF NOT EXISTS idx_enhanced_dns_type ON cross_cloud_enhanced_dns(record_type)",
		"CREATE INDEX IF NOT EXISTS idx_enhanced_dns_provider ON cross_cloud_enhanced_dns(provider)",
		"CREATE INDEX IF NOT EXISTS idx_enhanced_dns_zone ON cross_cloud_enhanced_dns(zone_id)",
		"CREATE INDEX IF NOT EXISTS idx_enhanced_dns_resource ON cross_cloud_enhanced_dns(resource_id)",
		"CREATE INDEX IF NOT EXISTS idx_enhanced_dns_health ON cross_cloud_enhanced_dns(health_check_status)",
		"CREATE INDEX IF NOT EXISTS idx_enhanced_dns_geo ON cross_cloud_enhanced_dns(geo_continent, geo_country)",
		"CREATE INDEX IF NOT EXISTS idx_enhanced_dns_cname_target ON cross_cloud_enhanced_dns(ultimate_target)",
	}

	for _, idx := range indexes {
		if _, err := n.db.Exec(idx); err != nil {
			return fmt.Errorf("failed to create enhanced DNS index: %w", err)
		}
	}

	return nil
}

// createLoadBalancerTopologyTable creates a load balancer topology tracking table
func (n *NetworkSchemaExtensions) createLoadBalancerTopologyTable() error {
	lbTopoSQL := `
CREATE TABLE IF NOT EXISTS cross_cloud_loadbalancer_topology (
    -- Primary identifiers
    id VARCHAR PRIMARY KEY,                    -- Unique topology record ID
    
    -- Load balancer information
    loadbalancer_id VARCHAR NOT NULL,         -- Load balancer resource ID
    loadbalancer_name VARCHAR,                -- Load balancer name
    loadbalancer_type VARCHAR NOT NULL,       -- application, network, classic, traffic_manager
    provider VARCHAR NOT NULL,                -- Cloud provider
    region VARCHAR,                           -- Region
    
    -- Backend targets
    backend_targets JSON,                     -- Array of backend target configurations
    backend_health_status JSON,              -- Health status of backends
    cross_cloud_backends JSON,               -- Backends in other cloud providers
    
    -- Frontend configuration
    frontend_config JSON,                     -- Frontend/listener configurations
    ssl_certificates JSON,                   -- SSL certificate information
    dns_configurations JSON,                 -- DNS configurations
    
    -- Routing rules
    routing_rules JSON,                       -- Routing rules and policies
    path_patterns JSON,                       -- Path-based routing patterns
    host_patterns JSON,                       -- Host-based routing patterns
    
    -- Health checks
    health_check_configs JSON,               -- Health check configurations
    health_check_results JSON,               -- Latest health check results
    health_check_cross_cloud JSON,           -- Cross-cloud health check correlations
    
    -- Traffic distribution
    traffic_distribution_method VARCHAR,     -- round_robin, weighted, least_connections
    session_affinity BOOLEAN,                -- Session affinity enabled
    sticky_sessions_config JSON,             -- Sticky session configuration
    
    -- Cross-cloud correlation
    correlated_loadbalancers JSON,           -- Correlated LBs in other providers
    correlation_type VARCHAR,                -- Type of correlation
    correlation_confidence DOUBLE,           -- Correlation confidence
    correlation_evidence JSON,               -- Evidence for correlation
    shared_backends JSON,                    -- Shared backend resources
    
    -- Performance metrics
    request_count_hourly BIGINT DEFAULT 0,   -- Hourly request count
    request_count_daily BIGINT DEFAULT 0,    -- Daily request count
    response_time_avg_ms DOUBLE,             -- Average response time
    response_time_p95_ms DOUBLE,             -- 95th percentile response time
    error_rate_percentage DOUBLE,            -- Error rate percentage
    
    -- Capacity and scaling
    current_capacity INTEGER,                -- Current capacity
    max_capacity INTEGER,                    -- Maximum capacity
    auto_scaling_enabled BOOLEAN,            -- Auto-scaling enabled
    scaling_policies JSON,                   -- Scaling policies
    
    -- Security
    security_groups JSON,                    -- Associated security groups
    ssl_policies JSON,                       -- SSL/TLS policies
    waf_configuration JSON,                  -- WAF configuration
    
    -- Monitoring and alerting
    monitoring_enabled BOOLEAN,              -- Monitoring enabled
    alert_configurations JSON,               -- Alert configurations
    log_destinations JSON,                   -- Log destinations
    
    -- Tags and metadata
    tags JSON,                               -- Load balancer tags
    metadata JSON,                           -- Additional metadata
    
    -- Timestamps
    created_at TIMESTAMP,                    -- When LB was created
    discovered_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);`

	if _, err := n.db.Exec(lbTopoSQL); err != nil {
		return err
	}

	// Create indexes
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_lb_topo_lb_id ON cross_cloud_loadbalancer_topology(loadbalancer_id)",
		"CREATE INDEX IF NOT EXISTS idx_lb_topo_provider ON cross_cloud_loadbalancer_topology(provider)",
		"CREATE INDEX IF NOT EXISTS idx_lb_topo_type ON cross_cloud_loadbalancer_topology(loadbalancer_type)",
		"CREATE INDEX IF NOT EXISTS idx_lb_topo_correlation ON cross_cloud_loadbalancer_topology(correlation_type)",
	}

	for _, idx := range indexes {
		if _, err := n.db.Exec(idx); err != nil {
			return fmt.Errorf("failed to create LB topology index: %w", err)
		}
	}

	return nil
}

// createSecurityRuleCorrelationsTable creates a security rule correlations tracking table
func (n *NetworkSchemaExtensions) createSecurityRuleCorrelationsTable() error {
	securitySQL := `
CREATE TABLE IF NOT EXISTS cross_cloud_security_correlations (
    -- Primary identifiers
    id VARCHAR PRIMARY KEY,                    -- Unique correlation ID
    
    -- Source security rule
    source_rule_id VARCHAR NOT NULL,          -- Source rule identifier
    source_resource_id VARCHAR NOT NULL,      -- Source security group/firewall ID
    source_provider VARCHAR NOT NULL,         -- Source provider
    source_region VARCHAR,                    -- Source region
    
    -- Target security rule
    target_rule_id VARCHAR NOT NULL,          -- Target rule identifier
    target_resource_id VARCHAR NOT NULL,      -- Target security group/firewall ID
    target_provider VARCHAR NOT NULL,         -- Target provider
    target_region VARCHAR,                    -- Target region
    
    -- Rule details comparison
    rule_overlap_analysis JSON,               -- Detailed overlap analysis
    overlap_percentage DOUBLE,                -- Percentage of rule overlap
    overlap_type VARCHAR,                     -- exact, partial, conflicting, complementary
    
    -- Protocol and port analysis
    protocol_correlation JSON,               -- Protocol correlation details
    port_overlap_analysis JSON,              -- Port overlap analysis
    cidr_overlap_analysis JSON,              -- CIDR block overlap analysis
    
    -- Security pattern analysis
    security_pattern VARCHAR,                -- web_service, database, ssh_access, etc.
    access_pattern VARCHAR,                  -- public, private, internal, restricted
    direction_analysis JSON,                 -- Inbound/outbound direction analysis
    action_analysis JSON,                    -- Allow/deny action analysis
    
    -- Risk assessment
    security_risk_level VARCHAR,             -- low, medium, high, critical
    risk_factors JSON,                       -- Risk factor analysis
    potential_conflicts JSON,               -- Potential security conflicts
    recommendations JSON,                    -- Security recommendations
    
    -- Compliance correlation
    compliance_frameworks JSON,             -- Applicable compliance frameworks
    compliance_gaps JSON,                   -- Compliance gaps identified
    policy_alignment JSON,                  -- Policy alignment analysis
    
    -- Correlation metadata
    correlation_method VARCHAR NOT NULL,     -- How correlation was discovered
    confidence_score DOUBLE NOT NULL,        -- Correlation confidence (0-1)
    evidence JSON,                           -- Evidence supporting correlation
    validation_status VARCHAR,              -- validated, pending, failed
    
    -- Performance impact
    rule_complexity_score DOUBLE,           -- Rule complexity score
    performance_impact VARCHAR,             -- low, medium, high
    optimization_suggestions JSON,          -- Optimization suggestions
    
    -- Monitoring and alerting
    monitoring_enabled BOOLEAN DEFAULT FALSE, -- Monitoring enabled
    alert_thresholds JSON,                   -- Alert thresholds
    last_violation_check TIMESTAMP,         -- Last violation check
    violation_count INTEGER DEFAULT 0,      -- Number of violations detected
    
    -- Tags and metadata
    tags JSON,                               -- Correlation tags
    metadata JSON,                           -- Additional metadata
    
    -- Timestamps
    discovered_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    last_analyzed TIMESTAMP,                 -- Last analysis timestamp
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);`

	if _, err := n.db.Exec(securitySQL); err != nil {
		return err
	}

	// Create indexes
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_sec_corr_source_rule ON cross_cloud_security_correlations(source_rule_id)",
		"CREATE INDEX IF NOT EXISTS idx_sec_corr_target_rule ON cross_cloud_security_correlations(target_rule_id)",
		"CREATE INDEX IF NOT EXISTS idx_sec_corr_source_resource ON cross_cloud_security_correlations(source_resource_id)",
		"CREATE INDEX IF NOT EXISTS idx_sec_corr_target_resource ON cross_cloud_security_correlations(target_resource_id)",
		"CREATE INDEX IF NOT EXISTS idx_sec_corr_providers ON cross_cloud_security_correlations(source_provider, target_provider)",
		"CREATE INDEX IF NOT EXISTS idx_sec_corr_overlap ON cross_cloud_security_correlations(overlap_type)",
		"CREATE INDEX IF NOT EXISTS idx_sec_corr_pattern ON cross_cloud_security_correlations(security_pattern)",
		"CREATE INDEX IF NOT EXISTS idx_sec_corr_risk ON cross_cloud_security_correlations(security_risk_level)",
		"CREATE INDEX IF NOT EXISTS idx_sec_corr_confidence ON cross_cloud_security_correlations(confidence_score)",
	}

	for _, idx := range indexes {
		if _, err := n.db.Exec(idx); err != nil {
			return fmt.Errorf("failed to create security correlation index: %w", err)
		}
	}

	return nil
}

// createVisualizationMetadataTable creates a table to store visualization metadata and layouts
func (n *NetworkSchemaExtensions) createVisualizationMetadataTable() error {
	vizSQL := `
CREATE TABLE IF NOT EXISTS cross_cloud_visualization_metadata (
    -- Primary identifiers
    id VARCHAR PRIMARY KEY,                    -- Unique visualization ID
    visualization_name VARCHAR NOT NULL,      -- Visualization name
    visualization_type VARCHAR NOT NULL,      -- topology, correlations, security, etc.
    
    -- Scope and filters
    provider_scope JSON,                      -- Providers included
    region_scope JSON,                        -- Regions included
    resource_filters JSON,                    -- Resource type filters
    correlation_filters JSON,                 -- Correlation type filters
    
    -- Layout information
    layout_algorithm VARCHAR,                 -- force_directed, hierarchical, circular
    layout_parameters JSON,                   -- Algorithm-specific parameters
    node_positions JSON,                      -- Saved node positions
    cluster_definitions JSON,                 -- Cluster definitions
    
    -- Visual styling
    theme_name VARCHAR,                       -- Visual theme
    color_scheme JSON,                        -- Color scheme definitions
    node_styling_rules JSON,                  -- Node styling rules
    edge_styling_rules JSON,                  -- Edge styling rules
    
    -- Interactive features
    zoom_level DOUBLE DEFAULT 1.0,           -- Current zoom level
    pan_position JSON,                        -- Pan position (x, y)
    selected_nodes JSON,                      -- Currently selected nodes
    hidden_nodes JSON,                        -- Hidden nodes
    collapsed_clusters JSON,                  -- Collapsed clusters
    
    -- Rendering options
    show_labels BOOLEAN DEFAULT TRUE,         -- Show node labels
    show_edge_labels BOOLEAN DEFAULT FALSE,   -- Show edge labels
    show_providers BOOLEAN DEFAULT TRUE,      -- Show provider groupings
    show_regions BOOLEAN DEFAULT TRUE,        -- Show region groupings
    show_confidence_scores BOOLEAN DEFAULT TRUE, -- Show confidence scores
    
    -- Export formats
    supported_formats JSON,                   -- Supported export formats
    last_export_format VARCHAR,               -- Last used export format
    export_settings JSON,                     -- Export-specific settings
    
    -- Performance settings
    max_nodes INTEGER DEFAULT 100,            -- Maximum nodes to display
    max_edges INTEGER DEFAULT 200,            -- Maximum edges to display
    detail_level VARCHAR DEFAULT 'medium',    -- low, medium, high
    rendering_quality VARCHAR DEFAULT 'balanced', -- fast, balanced, high_quality
    
    -- Collaboration features
    is_shared BOOLEAN DEFAULT FALSE,          -- Is visualization shared
    shared_with JSON,                         -- Users/groups with access
    sharing_permissions JSON,                 -- Sharing permissions
    version_history JSON,                     -- Version history
    
    -- Analytics
    view_count INTEGER DEFAULT 0,            -- Number of times viewed
    last_viewed TIMESTAMP,                   -- Last viewed timestamp
    average_view_duration DOUBLE,            -- Average view duration in seconds
    interaction_count INTEGER DEFAULT 0,     -- Number of interactions
    
    -- Refresh and updates
    auto_refresh BOOLEAN DEFAULT FALSE,       -- Auto-refresh enabled
    refresh_interval INTEGER,                -- Refresh interval in minutes
    last_refresh TIMESTAMP,                  -- Last refresh timestamp
    data_staleness_threshold INTEGER DEFAULT 60, -- Data staleness threshold in minutes
    
    -- Tags and metadata
    tags JSON,                               -- Visualization tags
    description TEXT,                        -- Visualization description
    created_by VARCHAR,                      -- Creator user ID
    metadata JSON,                           -- Additional metadata
    
    -- Timestamps
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    last_accessed TIMESTAMP
);`

	if _, err := n.db.Exec(vizSQL); err != nil {
		return err
	}

	// Create indexes
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_viz_meta_name ON cross_cloud_visualization_metadata(visualization_name)",
		"CREATE INDEX IF NOT EXISTS idx_viz_meta_type ON cross_cloud_visualization_metadata(visualization_type)",
		"CREATE INDEX IF NOT EXISTS idx_viz_meta_shared ON cross_cloud_visualization_metadata(is_shared)",
		"CREATE INDEX IF NOT EXISTS idx_viz_meta_created_by ON cross_cloud_visualization_metadata(created_by)",
		"CREATE INDEX IF NOT EXISTS idx_viz_meta_last_viewed ON cross_cloud_visualization_metadata(last_viewed)",
	}

	for _, idx := range indexes {
		if _, err := n.db.Exec(idx); err != nil {
			return fmt.Errorf("failed to create visualization metadata index: %w", err)
		}
	}

	return nil
}

// CreateNetworkAnalyticsViews creates analytical views for network topology analysis
func (n *NetworkSchemaExtensions) CreateNetworkAnalyticsViews() error {
	// Create VPN connections summary view
	vpnSummaryView := `
CREATE OR REPLACE VIEW vpn_connections_summary AS
SELECT 
    source_provider,
    target_provider,
    connection_type,
    COUNT(*) as connection_count,
    AVG(confidence_score) as avg_confidence,
    AVG(uptime_percentage) as avg_uptime,
    SUM(bytes_transferred_in + bytes_transferred_out) as total_bytes_transferred,
    COUNT(CASE WHEN connection_status = 'active' THEN 1 END) as active_connections
FROM cross_cloud_vpn_connections
GROUP BY source_provider, target_provider, connection_type;`

	if _, err := n.db.Exec(vpnSummaryView); err != nil {
		return fmt.Errorf("failed to create VPN summary view: %w", err)
	}

	// Create network peering summary view
	peeringSummaryView := `
CREATE OR REPLACE VIEW network_peering_summary AS
SELECT 
    source_provider,
    target_provider,
    peering_type,
    COUNT(*) as peering_count,
    AVG(confidence_score) as avg_confidence,
    COUNT(CASE WHEN peering_state = 'active' THEN 1 END) as active_peerings,
    SUM(data_transfer_in_gb + data_transfer_out_gb) as total_data_transfer_gb,
    AVG(bandwidth_utilization) as avg_bandwidth_utilization
FROM cross_cloud_network_peering
GROUP BY source_provider, target_provider, peering_type;`

	if _, err := n.db.Exec(peeringSummaryView); err != nil {
		return fmt.Errorf("failed to create peering summary view: %w", err)
	}

	// Create cross-cloud correlation summary view
	correlationSummaryView := `
CREATE OR REPLACE VIEW cross_cloud_correlation_summary AS
SELECT 
    source_provider,
    target_provider,
    correlation_type,
    COUNT(*) as correlation_count,
    AVG(confidence_score) as avg_confidence,
    MIN(confidence_score) as min_confidence,
    MAX(confidence_score) as max_confidence,
    COUNT(CASE WHEN confidence_score > 0.8 THEN 1 END) as high_confidence_count,
    COUNT(CASE WHEN status = 'active' THEN 1 END) as active_correlations
FROM cross_cloud_correlations
GROUP BY source_provider, target_provider, correlation_type
ORDER BY correlation_count DESC;`

	if _, err := n.db.Exec(correlationSummaryView); err != nil {
		return fmt.Errorf("failed to create correlation summary view: %w", err)
	}

	return nil
}
