package main

import (
	"encoding/json"
	"testing"

	pb "github.com/jlgore/corkscrew/internal/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractBasicRelationships(t *testing.T) {
	provider := &KubernetesProvider{}

	t.Run("owner references", func(t *testing.T) {
		// Create a pod with owner reference
		podData := map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata": map[string]interface{}{
				"name":      "test-pod",
				"namespace": "default",
				"ownerReferences": []interface{}{
					map[string]interface{}{
						"apiVersion": "apps/v1",
						"kind":       "ReplicaSet",
						"name":       "test-replicaset",
						"uid":        "rs-uid-123",
						"controller": true,
					},
				},
			},
		}

		rawData, _ := json.Marshal(podData)
		resource := &pb.Resource{
			Id:      "default/default/Pod/test-pod",
			Type:    "Pod",
			RawData: string(rawData),
		}

		relationships := provider.extractBasicRelationships(resource)
		
		require.Len(t, relationships, 1)
		assert.Equal(t, "OWNED_BY", relationships[0].Type)
		assert.Equal(t, "default/default/ReplicaSet/test-replicaset", relationships[0].TargetId)
		assert.Equal(t, "ReplicaSet", relationships[0].Properties["owner_kind"])
		assert.Equal(t, "test-replicaset", relationships[0].Properties["owner_name"])
		assert.Equal(t, "true", relationships[0].Properties["controller"])
	})

	t.Run("pod labels for service selection", func(t *testing.T) {
		podData := map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata": map[string]interface{}{
				"name":      "web-pod",
				"namespace": "default",
				"labels": map[string]interface{}{
					"app":  "web",
					"tier": "frontend",
				},
			},
		}

		rawData, _ := json.Marshal(podData)
		resource := &pb.Resource{
			Id:      "default/default/Pod/web-pod",
			Type:    "Pod",
			RawData: string(rawData),
		}

		relationships := provider.extractBasicRelationships(resource)
		
		// Should have a SELECTED_BY relationship (without target, as it needs matching)
		require.Len(t, relationships, 1)
		assert.Equal(t, "SELECTED_BY", relationships[0].Type)
		assert.Contains(t, relationships[0].Properties["pod_labels"], "app=web")
		assert.Contains(t, relationships[0].Properties["pod_labels"], "tier=frontend")
	})

	t.Run("service selector", func(t *testing.T) {
		serviceData := map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Service",
			"metadata": map[string]interface{}{
				"name":      "web-service",
				"namespace": "default",
			},
			"spec": map[string]interface{}{
				"selector": map[string]interface{}{
					"app": "web",
				},
			},
		}

		rawData, _ := json.Marshal(serviceData)
		resource := &pb.Resource{
			Id:      "default/default/Service/web-service",
			Type:    "Service",
			RawData: string(rawData),
		}

		relationships := provider.extractBasicRelationships(resource)
		
		require.Len(t, relationships, 1)
		assert.Equal(t, "SELECTS", relationships[0].Type)
		assert.Equal(t, "app=web", relationships[0].Properties["selector"])
	})

	t.Run("configmap volume mount", func(t *testing.T) {
		podData := map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata": map[string]interface{}{
				"name":      "app-pod",
				"namespace": "default",
			},
			"spec": map[string]interface{}{
				"volumes": []interface{}{
					map[string]interface{}{
						"name": "config-volume",
						"configMap": map[string]interface{}{
							"name": "app-config",
						},
					},
				},
			},
		}

		rawData, _ := json.Marshal(podData)
		resource := &pb.Resource{
			Id:      "default/default/Pod/app-pod",
			Type:    "Pod",
			RawData: string(rawData),
		}

		relationships := provider.extractBasicRelationships(resource)
		
		require.Len(t, relationships, 1)
		assert.Equal(t, "MOUNTS", relationships[0].Type)
		assert.Equal(t, "default/default/ConfigMap/app-config", relationships[0].TargetId)
		assert.Equal(t, "config-volume", relationships[0].Properties["volume_name"])
		assert.Equal(t, "configMap", relationships[0].Properties["mount_type"])
	})

	t.Run("secret volume mount", func(t *testing.T) {
		podData := map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata": map[string]interface{}{
				"name":      "app-pod",
				"namespace": "default",
			},
			"spec": map[string]interface{}{
				"volumes": []interface{}{
					map[string]interface{}{
						"name": "secret-volume",
						"secret": map[string]interface{}{
							"secretName": "app-secret",
						},
					},
				},
			},
		}

		rawData, _ := json.Marshal(podData)
		resource := &pb.Resource{
			Id:      "default/default/Pod/app-pod",
			Type:    "Pod",
			RawData: string(rawData),
		}

		relationships := provider.extractBasicRelationships(resource)
		
		require.Len(t, relationships, 1)
		assert.Equal(t, "MOUNTS", relationships[0].Type)
		assert.Equal(t, "default/default/Secret/app-secret", relationships[0].TargetId)
		assert.Equal(t, "secret", relationships[0].Properties["mount_type"])
	})

	t.Run("pvc volume mount", func(t *testing.T) {
		podData := map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata": map[string]interface{}{
				"name":      "db-pod",
				"namespace": "default",
			},
			"spec": map[string]interface{}{
				"volumes": []interface{}{
					map[string]interface{}{
						"name": "data-volume",
						"persistentVolumeClaim": map[string]interface{}{
							"claimName": "db-pvc",
						},
					},
				},
			},
		}

		rawData, _ := json.Marshal(podData)
		resource := &pb.Resource{
			Id:      "default/default/Pod/db-pod",
			Type:    "Pod",
			RawData: string(rawData),
		}

		relationships := provider.extractBasicRelationships(resource)
		
		require.Len(t, relationships, 1)
		assert.Equal(t, "MOUNTS", relationships[0].Type)
		assert.Equal(t, "default/default/PersistentVolumeClaim/db-pvc", relationships[0].TargetId)
		assert.Equal(t, "pvc", relationships[0].Properties["mount_type"])
	})

	t.Run("ingress to service", func(t *testing.T) {
		ingressData := map[string]interface{}{
			"apiVersion": "networking.k8s.io/v1",
			"kind":       "Ingress",
			"metadata": map[string]interface{}{
				"name":      "web-ingress",
				"namespace": "default",
			},
			"spec": map[string]interface{}{
				"rules": []interface{}{
					map[string]interface{}{
						"host": "example.com",
						"http": map[string]interface{}{
							"paths": []interface{}{
								map[string]interface{}{
									"path": "/",
									"backend": map[string]interface{}{
										"service": map[string]interface{}{
											"name": "web-service",
											"port": map[string]interface{}{
												"number": 80,
											},
										},
									},
								},
							},
						},
					},
				},
			},
		}

		rawData, _ := json.Marshal(ingressData)
		resource := &pb.Resource{
			Id:      "default/default/Ingress/web-ingress",
			Type:    "Ingress",
			RawData: string(rawData),
		}

		relationships := provider.extractBasicRelationships(resource)
		
		require.Len(t, relationships, 1)
		assert.Equal(t, "ROUTES_TO", relationships[0].Type)
		assert.Equal(t, "default/default/Service/web-service", relationships[0].TargetId)
		assert.Equal(t, "/", relationships[0].Properties["path"])
		assert.Equal(t, "example.com", relationships[0].Properties["host"])
	})

	t.Run("network policy", func(t *testing.T) {
		netpolData := map[string]interface{}{
			"apiVersion": "networking.k8s.io/v1",
			"kind":       "NetworkPolicy",
			"metadata": map[string]interface{}{
				"name":      "web-netpol",
				"namespace": "default",
			},
			"spec": map[string]interface{}{
				"podSelector": map[string]interface{}{
					"matchLabels": map[string]interface{}{
						"app": "web",
					},
				},
			},
		}

		rawData, _ := json.Marshal(netpolData)
		resource := &pb.Resource{
			Id:      "default/default/NetworkPolicy/web-netpol",
			Type:    "NetworkPolicy",
			RawData: string(rawData),
		}

		relationships := provider.extractBasicRelationships(resource)
		
		require.Len(t, relationships, 1)
		assert.Equal(t, "APPLIES_TO", relationships[0].Type)
		assert.Equal(t, "app=web", relationships[0].Properties["pod_selector"])
	})

	t.Run("empty raw data", func(t *testing.T) {
		resource := &pb.Resource{
			Id:      "test-resource",
			Type:    "Pod",
			RawData: "",
		}

		relationships := provider.extractBasicRelationships(resource)
		assert.Empty(t, relationships)
	})

	t.Run("invalid json", func(t *testing.T) {
		resource := &pb.Resource{
			Id:      "test-resource",
			Type:    "Pod",
			RawData: "invalid json",
		}

		relationships := provider.extractBasicRelationships(resource)
		assert.Empty(t, relationships)
	})
}

func TestLabelsToString(t *testing.T) {
	provider := &KubernetesProvider{}

	labels := map[string]interface{}{
		"app":     "web",
		"version": "v1",
		"tier":    "frontend",
	}

	result := provider.labelsToString(labels)
	
	// Result should contain all label pairs
	assert.Contains(t, result, "app=web")
	assert.Contains(t, result, "version=v1")
	assert.Contains(t, result, "tier=frontend")
	
	// Should be comma-separated
	assert.Contains(t, result, ",")
}