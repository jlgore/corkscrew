package main

import (
	"fmt"
	"log"
	"strings"
)

// DemoAzureSchemaGenerator demonstrates the ARM schema to DuckDB generator functionality
func DemoAzureSchemaGenerator() {
	fmt.Println("=== Azure ARM Schema to DuckDB Generator Demo ===\n")
	
	// Create a new schema generator
	generator := NewAzureAdvancedSchemaGenerator()
	
	// Demo 1: Virtual Machine Schema Generation
	fmt.Println("1. VIRTUAL MACHINE SCHEMA GENERATION")
	fmt.Println("=====================================")
	
	vmResourceInfo := AzureResourceInfo{
		Service:    "compute",
		Type:       "virtualMachines",
		FullType:   "Microsoft.Compute/virtualMachines",
		APIVersion: "2021-03-01",
		Properties: map[string]interface{}{
			"hardwareProfile": map[string]interface{}{
				"vmSize": "Standard_D2s_v3",
			},
			"storageProfile": map[string]interface{}{
				"osDisk": map[string]interface{}{
					"name":         "vm-os-disk",
					"diskSizeGB":   float64(30),
					"osType":       "Linux",
					"createOption": "FromImage",
					"managedDisk": map[string]interface{}{
						"id":                 "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Compute/disks/disk1",
						"storageAccountType": "Premium_LRS",
					},
				},
			},
			"provisioningState": "Succeeded",
		},
	}
	
	// Parse properties
	vmProperties := generator.ParseResourceProperties(vmResourceInfo)
	fmt.Printf("Discovered %d properties for Virtual Machines\n", len(vmProperties))
	
	fmt.Println("\n" + strings.Repeat("=", 50))
	fmt.Println("Demo completed! The Azure Schema Generator provides:")
	fmt.Println("✓ ARM property parsing and intelligent flattening")
	fmt.Println("✓ Optimized DuckDB schema generation")
	fmt.Println("✓ Analytical views for common query patterns")
	fmt.Println("✓ Configurable optimization rules")
	fmt.Println("✓ Schema evolution support")
	fmt.Println("✓ Performance optimizations (indexes, partitioning)")
	fmt.Println("✓ Compliance and governance views")
}

// RunDemo executes the demo if called directly
func RunDemo() {
	log.SetFlags(0) // Remove timestamp from log output
	DemoAzureSchemaGenerator()
}