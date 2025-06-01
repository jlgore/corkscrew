package main

import (
	"encoding/json"
	"strings"

	pb "github.com/jlgore/corkscrew/internal/proto"
)

// RelationshipExtractor extracts relationships between GCP resources
type RelationshipExtractor struct {
	patterns map[string][]RelationshipPattern
}

// RelationshipPattern defines a pattern for finding relationships
type RelationshipPattern struct {
	SourceType   string
	TargetType   string
	RelationType string
	Extractor    func(source, target *pb.ResourceRef) bool
}

// NewRelationshipExtractor creates a new relationship extractor
func NewRelationshipExtractor() *RelationshipExtractor {
	re := &RelationshipExtractor{
		patterns: make(map[string][]RelationshipPattern),
	}
	
	re.definePatterns()
	return re
}

// definePatterns defines all GCP resource relationship patterns
func (re *RelationshipExtractor) definePatterns() {
	// Instance to Disk relationships
	re.patterns["compute.googleapis.com/Instance"] = append(
		re.patterns["compute.googleapis.com/Instance"],
		RelationshipPattern{
			TargetType:   "compute.googleapis.com/Disk",
			RelationType: "uses",
			Extractor: func(instance, disk *pb.ResourceRef) bool {
				// Check if instance references the disk
				if data, ok := instance.BasicAttributes["resource_data"]; ok {
					return strings.Contains(data, disk.Name) || strings.Contains(data, disk.Id)
				}
				// Also check if they are in the same zone and project
				return re.isSameZoneAndProject(instance, disk)
			},
		},
	)
	
	// Instance to Network relationships
	re.patterns["compute.googleapis.com/Instance"] = append(
		re.patterns["compute.googleapis.com/Instance"],
		RelationshipPattern{
			TargetType:   "compute.googleapis.com/Network",
			RelationType: "connected_to",
			Extractor: func(instance, network *pb.ResourceRef) bool {
				// Parse instance network interfaces
				if data, ok := instance.BasicAttributes["resource_data"]; ok {
					var instanceData map[string]interface{}
					if err := json.Unmarshal([]byte(data), &instanceData); err == nil {
						// Check network interfaces
						if interfaces, ok := instanceData["networkInterfaces"].([]interface{}); ok {
							for _, iface := range interfaces {
								if ifaceMap, ok := iface.(map[string]interface{}); ok {
									if netRef, ok := ifaceMap["network"].(string); ok {
										if strings.Contains(netRef, network.Name) {
											return true
										}
									}
								}
							}
						}
					}
				}
				return false
			},
		},
	)
	
	// Instance to Subnetwork relationships
	re.patterns["compute.googleapis.com/Instance"] = append(
		re.patterns["compute.googleapis.com/Instance"],
		RelationshipPattern{
			TargetType:   "compute.googleapis.com/Subnetwork",
			RelationType: "uses_subnet",
			Extractor: func(instance, subnet *pb.ResourceRef) bool {
				if data, ok := instance.BasicAttributes["resource_data"]; ok {
					var instanceData map[string]interface{}
					if err := json.Unmarshal([]byte(data), &instanceData); err == nil {
						if interfaces, ok := instanceData["networkInterfaces"].([]interface{}); ok {
							for _, iface := range interfaces {
								if ifaceMap, ok := iface.(map[string]interface{}); ok {
									if subnetRef, ok := ifaceMap["subnetwork"].(string); ok {
										if strings.Contains(subnetRef, subnet.Name) {
											return true
										}
									}
								}
							}
						}
					}
				}
				return false
			},
		},
	)
	
	// Subnetwork to Network relationships
	re.patterns["compute.googleapis.com/Subnetwork"] = append(
		re.patterns["compute.googleapis.com/Subnetwork"],
		RelationshipPattern{
			TargetType:   "compute.googleapis.com/Network",
			RelationType: "belongs_to",
			Extractor: func(subnet, network *pb.ResourceRef) bool {
				if networkRef, ok := subnet.BasicAttributes["network"]; ok {
					return strings.Contains(networkRef, network.Name)
				}
				// Also check resource data
				if data, ok := subnet.BasicAttributes["resource_data"]; ok {
					return strings.Contains(data, network.Name) || strings.Contains(data, network.Id)
				}
				return false
			},
		},
	)
	
	// Disk to Snapshot relationships
	re.patterns["compute.googleapis.com/Disk"] = append(
		re.patterns["compute.googleapis.com/Disk"],
		RelationshipPattern{
			TargetType:   "compute.googleapis.com/Snapshot",
			RelationType: "has_snapshot",
			Extractor: func(disk, snapshot *pb.ResourceRef) bool {
				if sourceDisk, ok := snapshot.BasicAttributes["source_disk"]; ok {
					return strings.Contains(sourceDisk, disk.Name) || strings.Contains(sourceDisk, disk.Id)
				}
				return false
			},
		},
	)
	
	// Cluster to Node Pool relationships
	re.patterns["container.googleapis.com/Cluster"] = append(
		re.patterns["container.googleapis.com/Cluster"],
		RelationshipPattern{
			TargetType:   "container.googleapis.com/NodePool",
			RelationType: "contains",
			Extractor: func(cluster, nodePool *pb.ResourceRef) bool {
				// Node pools reference their parent cluster
				if clusterName, ok := nodePool.BasicAttributes["cluster_name"]; ok {
					return clusterName == cluster.Name
				}
				// Also check if the node pool ID contains the cluster name
				return strings.Contains(nodePool.Id, cluster.Name)
			},
		},
	)
	
	// Container Node Pool to Compute Network relationships
	re.patterns["container.googleapis.com/Cluster"] = append(
		re.patterns["container.googleapis.com/Cluster"],
		RelationshipPattern{
			TargetType:   "compute.googleapis.com/Network",
			RelationType: "uses_network",
			Extractor: func(cluster, network *pb.ResourceRef) bool {
				// Check if cluster uses the network
				if data, ok := cluster.BasicAttributes["resource_data"]; ok {
					var clusterData map[string]interface{}
					if err := json.Unmarshal([]byte(data), &clusterData); err == nil {
						if networkRef, ok := clusterData["network"].(string); ok {
							return strings.Contains(networkRef, network.Name)
						}
					}
				}
				return false
			},
		},
	)
	
	// Container Cluster to Subnetwork relationships
	re.patterns["container.googleapis.com/Cluster"] = append(
		re.patterns["container.googleapis.com/Cluster"],
		RelationshipPattern{
			TargetType:   "compute.googleapis.com/Subnetwork",
			RelationType: "uses_subnet",
			Extractor: func(cluster, subnet *pb.ResourceRef) bool {
				// Check if cluster uses the subnetwork
				if data, ok := cluster.BasicAttributes["resource_data"]; ok {
					var clusterData map[string]interface{}
					if err := json.Unmarshal([]byte(data), &clusterData); err == nil {
						if subnetRef, ok := clusterData["subnetwork"].(string); ok {
							return strings.Contains(subnetRef, subnet.Name)
						}
					}
				}
				return false
			},
		},
	)
	
	// Topic to Subscription relationships (Pub/Sub)
	re.patterns["pubsub.googleapis.com/Topic"] = append(
		re.patterns["pubsub.googleapis.com/Topic"],
		RelationshipPattern{
			TargetType:   "pubsub.googleapis.com/Subscription",
			RelationType: "publishes_to",
			Extractor: func(topic, subscription *pb.ResourceRef) bool {
				if topicRef, ok := subscription.BasicAttributes["topic"]; ok {
					return strings.Contains(topicRef, topic.Name) || strings.Contains(topicRef, topic.Id)
				}
				return false
			},
		},
	)
	
	// Cloud SQL Instance to Database relationships
	re.patterns["sqladmin.googleapis.com/Instance"] = append(
		re.patterns["sqladmin.googleapis.com/Instance"],
		RelationshipPattern{
			TargetType:   "sqladmin.googleapis.com/Database",
			RelationType: "hosts",
			Extractor: func(instance, database *pb.ResourceRef) bool {
				if instanceRef, ok := database.BasicAttributes["instance"]; ok {
					return strings.Contains(instanceRef, instance.Name)
				}
				// Check if database ID contains instance name
				return strings.Contains(database.Id, instance.Name)
			},
		},
	)
	
	// BigQuery Dataset to Table relationships
	re.patterns["bigqueryadmin.googleapis.com/Dataset"] = append(
		re.patterns["bigqueryadmin.googleapis.com/Dataset"],
		RelationshipPattern{
			TargetType:   "bigqueryadmin.googleapis.com/Table",
			RelationType: "contains",
			Extractor: func(dataset, table *pb.ResourceRef) bool {
				// Tables are nested under datasets in the resource hierarchy
				return strings.Contains(table.Id, dataset.Name)
			},
		},
	)
	
	// Cloud Run Service to Revision relationships
	re.patterns["run.googleapis.com/Service"] = append(
		re.patterns["run.googleapis.com/Service"],
		RelationshipPattern{
			TargetType:   "run.googleapis.com/Revision",
			RelationType: "has_revision",
			Extractor: func(service, revision *pb.ResourceRef) bool {
				// Revisions belong to services
				return strings.Contains(revision.Id, service.Name)
			},
		},
	)
	
	// App Engine Application to Service relationships
	re.patterns["appengine.googleapis.com/Application"] = append(
		re.patterns["appengine.googleapis.com/Application"],
		RelationshipPattern{
			TargetType:   "appengine.googleapis.com/Service",
			RelationType: "contains",
			Extractor: func(app, service *pb.ResourceRef) bool {
				// Services belong to applications
				return re.isSameProject(app, service)
			},
		},
	)
	
	// App Engine Service to Version relationships
	re.patterns["appengine.googleapis.com/Service"] = append(
		re.patterns["appengine.googleapis.com/Service"],
		RelationshipPattern{
			TargetType:   "appengine.googleapis.com/Version",
			RelationType: "has_version",
			Extractor: func(service, version *pb.ResourceRef) bool {
				// Versions belong to services
				return strings.Contains(version.Id, service.Name)
			},
		},
	)
}

// ExtractRelationships finds all relationships between the given resources
func (re *RelationshipExtractor) ExtractRelationships(resources []*pb.ResourceRef) []*pb.Relationship {
	var relationships []*pb.Relationship
	
	// Create a map for faster lookups by type
	resourcesByType := make(map[string][]*pb.ResourceRef)
	for _, resource := range resources {
		resourcesByType[resource.Type] = append(resourcesByType[resource.Type], resource)
	}
	
	// Check each resource against patterns
	for _, source := range resources {
		patterns, ok := re.patterns[source.Type]
		if !ok {
			continue
		}
		
		for _, pattern := range patterns {
			// Get all potential targets of the right type
			targets, ok := resourcesByType[pattern.TargetType]
			if !ok {
				continue
			}
			
			for _, target := range targets {
				if pattern.Extractor(source, target) {
					rel := &pb.Relationship{
						TargetId: target.Id,
						RelationshipType:     pattern.RelationType,
						Properties: map[string]string{
							"source_id":   source.Id,
							"source_type": source.Type,
							"source_name": source.Name,
							"target_type": target.Type,
							"target_name": target.Name,
						},
					}
					relationships = append(relationships, rel)
				}
			}
		}
	}
	
	return relationships
}

// ExtractIAMRelationships extracts IAM-based relationships
func (re *RelationshipExtractor) ExtractIAMRelationships(resources []*pb.ResourceRef, iamPolicies map[string]*IAMPolicy) []*pb.Relationship {
	var relationships []*pb.Relationship
	
	for resourceID, policy := range iamPolicies {
		for _, binding := range policy.Bindings {
			for _, member := range binding.Members {
				// Extract service account relationships
				if strings.HasPrefix(member, "serviceAccount:") {
					saEmail := strings.TrimPrefix(member, "serviceAccount:")
					
					// Find the service account resource
					for _, resource := range resources {
						if resource.Type == "iam.googleapis.com/ServiceAccount" &&
						   (resource.Name == saEmail || strings.Contains(resource.Id, saEmail)) {
							rel := &pb.Relationship{
								TargetId: resourceID,
								RelationshipType:     "has_access_to",
								Properties: map[string]string{
									"source_id":   resource.Id,
									"source_type": resource.Type,
									"role":        binding.Role,
									"member":      member,
								},
							}
							relationships = append(relationships, rel)
						}
					}
				}
				
				// Extract user and group relationships
				if strings.HasPrefix(member, "user:") || strings.HasPrefix(member, "group:") {
					rel := &pb.Relationship{
						TargetId: resourceID,
						RelationshipType:     "has_access_to",
						Properties: map[string]string{
							"source_identity": member,
							"role":           binding.Role,
						},
					}
					relationships = append(relationships, rel)
				}
			}
		}
	}
	
	return relationships
}

// Helper methods

// isSameProject checks if two resources are in the same project
func (re *RelationshipExtractor) isSameProject(resource1, resource2 *pb.ResourceRef) bool {
	project1, ok1 := resource1.BasicAttributes["project_id"]
	project2, ok2 := resource2.BasicAttributes["project_id"]
	
	if !ok1 || !ok2 {
		// Try to extract from resource ID
		project1 = re.extractProjectFromID(resource1.Id)
		project2 = re.extractProjectFromID(resource2.Id)
	}
	
	return project1 != "" && project1 == project2
}

// isSameZoneAndProject checks if two resources are in the same zone and project
func (re *RelationshipExtractor) isSameZoneAndProject(resource1, resource2 *pb.ResourceRef) bool {
	return re.isSameProject(resource1, resource2) && 
		   re.extractZoneFromRegion(resource1.Region) == re.extractZoneFromRegion(resource2.Region)
}

// extractProjectFromID extracts project ID from resource ID
func (re *RelationshipExtractor) extractProjectFromID(resourceID string) string {
	if strings.Contains(resourceID, "projects/") {
		parts := strings.Split(resourceID, "/")
		for i, part := range parts {
			if part == "projects" && i+1 < len(parts) {
				return parts[i+1]
			}
		}
	}
	return ""
}

// extractZoneFromRegion extracts zone from region string
func (re *RelationshipExtractor) extractZoneFromRegion(region string) string {
	// If it's already a zone (contains hyphens and ends with a letter), return as is
	if strings.Count(region, "-") >= 2 {
		return region
	}
	return ""
}

// extractLabels extracts labels from resource data
func (re *RelationshipExtractor) extractLabels(data string) map[string]string {
	labels := make(map[string]string)
	
	var resourceData map[string]interface{}
	if err := json.Unmarshal([]byte(data), &resourceData); err != nil {
		return labels
	}
	
	// Check for labels field
	if labelsData, ok := resourceData["labels"]; ok {
		if labelsMap, ok := labelsData.(map[string]interface{}); ok {
			for k, v := range labelsMap {
				if str, ok := v.(string); ok {
					labels[k] = str
				}
			}
		}
	}
	
	return labels
}

// findResourcesByLabel finds resources that have a specific label
func (re *RelationshipExtractor) findResourcesByLabel(resources []*pb.ResourceRef, labelKey, labelValue string) []*pb.ResourceRef {
	var matches []*pb.ResourceRef
	
	for _, resource := range resources {
		// Check basic attributes for labels
		if labelVal, ok := resource.BasicAttributes["label_"+labelKey]; ok && labelVal == labelValue {
			matches = append(matches, resource)
			continue
		}
		
		// Check resource data for labels
		if data, ok := resource.BasicAttributes["resource_data"]; ok {
			labels := re.extractLabels(data)
			if val, exists := labels[labelKey]; exists && val == labelValue {
				matches = append(matches, resource)
			}
		}
	}
	
	return matches
}

// ExtractLabelBasedRelationships finds relationships based on matching labels
func (re *RelationshipExtractor) ExtractLabelBasedRelationships(resources []*pb.ResourceRef) []*pb.Relationship {
	var relationships []*pb.Relationship
	
	// Common label patterns for relationships
	labelPatterns := []string{
		"app",
		"component", 
		"tier",
		"environment",
		"version",
		"cluster",
		"service",
	}
	
	for _, labelKey := range labelPatterns {
		// Group resources by label value
		labelGroups := make(map[string][]*pb.ResourceRef)
		
		for _, resource := range resources {
			var labelValue string
			
			// Check basic attributes
			if val, ok := resource.BasicAttributes["label_"+labelKey]; ok {
				labelValue = val
			} else if data, ok := resource.BasicAttributes["resource_data"]; ok {
				// Check resource data
				labels := re.extractLabels(data)
				if val, exists := labels[labelKey]; exists {
					labelValue = val
				}
			}
			
			if labelValue != "" {
				labelGroups[labelValue] = append(labelGroups[labelValue], resource)
			}
		}
		
		// Create relationships within each group
		for labelValue, groupResources := range labelGroups {
			if len(groupResources) <= 1 {
				continue
			}
			
			// Create relationships between resources in the same group
			for i, source := range groupResources {
				for j, target := range groupResources {
					if i != j {
						rel := &pb.Relationship{
							TargetId: target.Id,
							RelationshipType:     "related_by_label",
							Properties: map[string]string{
								"source_id":    source.Id,
								"source_type":  source.Type,
								"source_name":  source.Name,
								"target_type":  target.Type,
								"target_name":  target.Name,
								"label_key":    labelKey,
								"label_value":  labelValue,
							},
						}
						relationships = append(relationships, rel)
					}
				}
			}
		}
	}
	
	return relationships
}