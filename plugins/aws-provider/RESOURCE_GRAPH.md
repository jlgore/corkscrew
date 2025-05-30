# AWS Resource Relationship Graph

The Resource Relationship Graph analyzes AWS SDK code to automatically discover and map relationships between AWS resources. This helps understand resource dependencies and optimize scanning order.

## Features

- **Automatic Relationship Detection**: Analyzes field names and types to detect relationships
- **Cross-Service Support**: Identifies relationships between different AWS services
- **Multiple Output Formats**: JSON, Mermaid diagrams, and dependency ordering
- **Relationship Types**: References, contains, manages, depends_on
- **Cardinality Detection**: One-to-one, one-to-many, many-to-many

## Usage

### Building the Graph

```go
import "github.com/jlgore/corkscrew/plugins/aws-provider/discovery"

// Build graph from service analysis
services := map[string]*discovery.ServiceAnalysis{
    // ... service analysis data
}

graph := discovery.BuildResourceRelationshipGraph(services)
```

### Command Line Tool

```bash
# Build the tool
go build -o build-resource-graph ./cmd/build-resource-graph

# Generate JSON graph
./build-resource-graph -format json -output graph.json

# Generate Mermaid diagram
./build-resource-graph -format mermaid -output diagram.mmd

# Get dependency order for scanning
./build-resource-graph -format deps

# Analyze specific service relationships
./build-resource-graph -analyze ec2 -format json
```

## Relationship Types

### 1. References
A resource holds a reference to another resource (most common).

Example: EC2 Instance references a VPC via VpcId field.

### 2. Contains
A parent-child relationship where one resource contains another.

Example: VPC contains Subnets, RDS DBSubnetGroup contains multiple Subnets.

### 3. Manages
One resource manages the lifecycle of another.

Example: CloudFormation Stack manages multiple resources.

### 4. Depends On
Explicit dependency where one resource requires another to exist.

Example: Lambda Function depends on IAM Role.

## Example Relationships

### EC2 Relationships
```
EC2::Instance -> VPC::VPC (via VpcId) - references, one-to-one
EC2::Instance -> EC2::Subnet (via SubnetId) - references, one-to-one  
EC2::Instance -> EC2::SecurityGroup (via SecurityGroupIds) - references, one-to-many
EC2::Instance -> IAM::InstanceProfile (via IamInstanceProfile) - references, one-to-one
```

### Lambda Relationships
```
Lambda::Function -> IAM::Role (via Role) - references, one-to-one
Lambda::Function -> EC2::Subnet (via VpcConfig.SubnetIds) - references, one-to-many
Lambda::Function -> EC2::SecurityGroup (via VpcConfig.SecurityGroupIds) - references, one-to-many
```

### RDS Relationships
```
RDS::DBInstance -> RDS::DBSubnetGroup (via DBSubnetGroupName) - references, one-to-one
RDS::DBSubnetGroup -> EC2::Subnet (via SubnetIds) - contains, one-to-many
RDS::DBInstance -> EC2::SecurityGroup (via VpcSecurityGroupIds) - references, one-to-many
```

## Output Formats

### JSON Format
```json
{
  "nodes": [
    {
      "id": "ec2::Instance",
      "service": "ec2",
      "resourceType": "Instance",
      "label": "ec2::Instance",
      "inDegree": 2,
      "outDegree": 5
    }
  ],
  "edges": [
    {
      "id": "edge_0",
      "source": "ec2::Instance",
      "target": "ec2::Vpc",
      "label": "VpcId",
      "relationType": "references",
      "cardinality": "one-to-one"
    }
  ],
  "serviceMap": {
    "ec2": ["Instance", "Vpc", "Subnet", "SecurityGroup"]
  },
  "stats": {
    "totalNodes": 10,
    "totalEdges": 15,
    "totalServices": 4,
    "totalRelationships": 15
  }
}
```

### Mermaid Diagram
```mermaid
graph TD
    subgraph ec2[EC2 Service]
        ec2_Instance[Instance]
        ec2_Vpc[Vpc]
        ec2_Subnet[Subnet]
    end
    
    subgraph iam[IAM Service]
        iam_Role[Role]
        iam_InstanceProfile[InstanceProfile]
    end
    
    ec2_Instance -->|VpcId (one-to-one)| ec2_Vpc
    ec2_Instance -->|IamInstanceProfile (one-to-one)| iam_InstanceProfile
```

### Dependency Order
Resources are sorted in dependency order for optimal scanning:

1. IAM::Role (no dependencies)
2. EC2::Vpc (no dependencies)
3. EC2::SecurityGroup (depends on VPC)
4. EC2::Subnet (depends on VPC)
5. EC2::Instance (depends on VPC, Subnet, SecurityGroup)

## Integration with Scanner

The relationship graph can optimize resource scanning:

```go
// Get optimal scanning order
order := graph.GetDependencyOrder()

// Scan resources in dependency order
for _, resourceKey := range order {
    parts := strings.Split(resourceKey, "::")
    service := parts[0]
    resourceType := parts[1]
    
    // Scan this resource type
    scanner.ScanResource(service, resourceType)
}
```

## Extending Relationships

To add new relationship patterns, update the following:

1. **Field Pattern Detection** in `inferTargetFromFieldName()`:
```go
resourceMappings["newFieldPattern"] = TargetInfo{
    Service: "targetService",
    Resource: "TargetResource",
}
```

2. **Cross-Service Relationships** in `detectCrossServiceRelationships()`:
```go
crossServicePatterns = append(crossServicePatterns, struct{...}{
    SourceService:  "service1",
    SourceResource: "Resource1",
    TargetService:  "service2", 
    TargetResource: "Resource2",
    RelationType:   "references",
    Cardinality:    "one-to-one",
})
```

## Testing

Run the tests to verify relationship detection:

```bash
cd plugins/aws-provider/discovery
go test -v -run TestBuildResourceRelationshipGraph
```

This will output discovered relationships and generate example visualizations.