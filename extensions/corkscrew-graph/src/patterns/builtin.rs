use super::{PatternDefinition, PatternEdgeDefinition, PatternNodeDefinition};

pub fn list() -> Vec<PatternDefinition> {
    vec![
        internet_to_database(),
        k8s_privileged_to_cloud(),
        public_lb_to_private_db(),
        public_s3_via_instance(),
        unencrypted_data_path(),
        lateral_movement_risk(),
        cross_account_trust(),
        overprivileged_lambda(),
    ]
}

pub fn lookup(name: &str) -> Option<PatternDefinition> {
    match name {
        "internet_to_database" => Some(internet_to_database()),
        "k8s_privileged_to_cloud" => Some(k8s_privileged_to_cloud()),
        "public_lb_to_private_db" => Some(public_lb_to_private_db()),
        "public_s3_via_instance" => Some(public_s3_via_instance()),
        "unencrypted_data_path" => Some(unencrypted_data_path()),
        "lateral_movement_risk" => Some(lateral_movement_risk()),
        "cross_account_trust" => Some(cross_account_trust()),
        "overprivileged_lambda" => Some(overprivileged_lambda()),
        _ => None,
    }
}

fn internet_to_database() -> PatternDefinition {
    PatternDefinition {
        name: "internet_to_database".to_string(),
        description: "Internet gateway path through security controls and compute to a database".to_string(),
        nodes: vec![
            PatternNodeDefinition {
                label: "igw".to_string(),
                type_filter: Some("InternetGateway".to_string()),
            },
            PatternNodeDefinition {
                label: "sg".to_string(),
                type_filter: Some("SecurityGroup".to_string()),
            },
            PatternNodeDefinition {
                label: "instance".to_string(),
                type_filter: Some("Instance".to_string()),
            },
            PatternNodeDefinition {
                label: "db".to_string(),
                type_filter: Some("DBInstance".to_string()),
            },
        ],
        edges: vec![
            PatternEdgeDefinition {
                from: "igw".to_string(),
                to: "sg".to_string(),
                rel_filter: Some("allows".to_string()),
            },
            PatternEdgeDefinition {
                from: "sg".to_string(),
                to: "instance".to_string(),
                rel_filter: Some("member_of".to_string()),
            },
            PatternEdgeDefinition {
                from: "instance".to_string(),
                to: "db".to_string(),
                rel_filter: Some("connects_to".to_string()),
            },
        ],
    }
}

fn public_s3_via_instance() -> PatternDefinition {
    PatternDefinition {
        name: "public_s3_via_instance".to_string(),
        description: "Instance with a reads path to an S3 resource".to_string(),
        nodes: vec![
            PatternNodeDefinition {
                label: "instance".to_string(),
                type_filter: Some("ec2".to_string()),
            },
            PatternNodeDefinition {
                label: "bucket".to_string(),
                type_filter: Some("s3".to_string()),
            },
        ],
        edges: vec![PatternEdgeDefinition {
            from: "instance".to_string(),
            to: "bucket".to_string(),
            rel_filter: Some("reads".to_string()),
        }],
    }
}

fn public_lb_to_private_db() -> PatternDefinition {
    PatternDefinition {
        name: "public_lb_to_private_db".to_string(),
        description: "Load balancer path through compute to a database".to_string(),
        nodes: vec![
            PatternNodeDefinition {
                label: "lb".to_string(),
                type_filter: Some("LoadBalancer".to_string()),
            },
            PatternNodeDefinition {
                label: "instance".to_string(),
                type_filter: Some("Instance".to_string()),
            },
            PatternNodeDefinition {
                label: "db".to_string(),
                type_filter: Some("DBInstance".to_string()),
            },
        ],
        edges: vec![
            PatternEdgeDefinition {
                from: "lb".to_string(),
                to: "instance".to_string(),
                rel_filter: Some("routes_to".to_string()),
            },
            PatternEdgeDefinition {
                from: "instance".to_string(),
                to: "db".to_string(),
                rel_filter: Some("connects_to".to_string()),
            },
        ],
    }
}

fn unencrypted_data_path() -> PatternDefinition {
    PatternDefinition {
        name: "unencrypted_data_path".to_string(),
        description: "Internet-reachable compute with a path to storage".to_string(),
        nodes: vec![
            PatternNodeDefinition {
                label: "igw".to_string(),
                type_filter: Some("InternetGateway".to_string()),
            },
            PatternNodeDefinition {
                label: "instance".to_string(),
                type_filter: Some("Instance".to_string()),
            },
            PatternNodeDefinition {
                label: "storage".to_string(),
                type_filter: Some("Bucket".to_string()),
            },
        ],
        edges: vec![
            PatternEdgeDefinition {
                from: "igw".to_string(),
                to: "instance".to_string(),
                rel_filter: Some("exposes".to_string()),
            },
            PatternEdgeDefinition {
                from: "instance".to_string(),
                to: "storage".to_string(),
                rel_filter: Some("writes".to_string()),
            },
        ],
    }
}

fn lateral_movement_risk() -> PatternDefinition {
    PatternDefinition {
        name: "lateral_movement_risk".to_string(),
        description: "Peer-connected EC2 resources".to_string(),
        nodes: vec![
            PatternNodeDefinition {
                label: "source".to_string(),
                type_filter: Some("ec2".to_string()),
            },
            PatternNodeDefinition {
                label: "target".to_string(),
                type_filter: Some("ec2".to_string()),
            },
        ],
        edges: vec![PatternEdgeDefinition {
            from: "source".to_string(),
            to: "target".to_string(),
            rel_filter: Some("peer".to_string()),
        }],
    }
}

fn cross_account_trust() -> PatternDefinition {
    PatternDefinition {
        name: "cross_account_trust".to_string(),
        description: "IAM role with cross-account trust relationship".to_string(),
        nodes: vec![
            PatternNodeDefinition {
                label: "role".to_string(),
                type_filter: Some("role".to_string()),
            },
            PatternNodeDefinition {
                label: "external_role".to_string(),
                type_filter: Some("role".to_string()),
            },
        ],
        edges: vec![PatternEdgeDefinition {
            from: "role".to_string(),
            to: "external_role".to_string(),
            rel_filter: Some("assume_role".to_string()),
        }],
    }
}

fn overprivileged_lambda() -> PatternDefinition {
    PatternDefinition {
        name: "overprivileged_lambda".to_string(),
        description: "Lambda-like compute with admin-level policy path".to_string(),
        nodes: vec![
            PatternNodeDefinition {
                label: "function".to_string(),
                type_filter: Some("lambda".to_string()),
            },
            PatternNodeDefinition {
                label: "policy".to_string(),
                type_filter: Some("policy".to_string()),
            },
        ],
        edges: vec![PatternEdgeDefinition {
            from: "function".to_string(),
            to: "policy".to_string(),
            rel_filter: Some("administratoraccess".to_string()),
        }],
    }
}

fn k8s_privileged_to_cloud() -> PatternDefinition {
    PatternDefinition {
        name: "k8s_privileged_to_cloud".to_string(),
        description: "Kubernetes pod with a path to a cloud IAM role".to_string(),
        nodes: vec![
            PatternNodeDefinition {
                label: "pod".to_string(),
                type_filter: Some("Pod".to_string()),
            },
            PatternNodeDefinition {
                label: "service_account".to_string(),
                type_filter: Some("ServiceAccount".to_string()),
            },
            PatternNodeDefinition {
                label: "role".to_string(),
                type_filter: Some("Role".to_string()),
            },
        ],
        edges: vec![
            PatternEdgeDefinition {
                from: "pod".to_string(),
                to: "service_account".to_string(),
                rel_filter: Some("uses".to_string()),
            },
            PatternEdgeDefinition {
                from: "service_account".to_string(),
                to: "role".to_string(),
                rel_filter: Some("assumes".to_string()),
            },
        ],
    }
}
