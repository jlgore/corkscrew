module github.com/jlgore/corkscrew/plugins/azure-provider

go 1.26.2

require (
	github.com/Azure/azure-sdk-for-go/sdk/azcore v1.21.1
	github.com/Azure/azure-sdk-for-go/sdk/azidentity v1.13.1
	github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v6 v6.4.0
	github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/managementgroups/armmanagementgroups v1.2.0
	github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v6 v6.2.0
	github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resourcegraph/armresourcegraph v0.9.0
	github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources v1.2.0
	github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armsubscriptions v1.3.0
	github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/storage/armstorage v1.8.1
	github.com/duckdb/duckdb-go/v2 v2.10504.0
	github.com/hashicorp/go-plugin v1.8.0
	github.com/jlgore/corkscrew v0.1.2
	github.com/microsoftgraph/msgraph-sdk-go v1.97.0
	github.com/stretchr/testify v1.11.1
	golang.org/x/time v0.15.0
	google.golang.org/protobuf v1.36.11
)

require github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/authorization/armauthorization v1.0.0

require (
	github.com/Azure/azure-sdk-for-go/sdk/internal v1.12.0 // indirect
	github.com/AzureAD/microsoft-authentication-library-for-go v1.7.1 // indirect
	github.com/apache/arrow-go/v18 v18.6.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/duckdb/duckdb-go-bindings v0.10504.0 // indirect
	github.com/duckdb/duckdb-go-bindings/lib/darwin-amd64 v0.10504.0 // indirect
	github.com/duckdb/duckdb-go-bindings/lib/darwin-arm64 v0.10504.0 // indirect
	github.com/duckdb/duckdb-go-bindings/lib/linux-amd64 v0.10504.0 // indirect
	github.com/duckdb/duckdb-go-bindings/lib/linux-arm64 v0.10504.0 // indirect
	github.com/duckdb/duckdb-go-bindings/lib/windows-amd64 v0.10504.0 // indirect
	github.com/fatih/color v1.19.0 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-viper/mapstructure/v2 v2.5.0 // indirect
	github.com/goccy/go-json v0.10.6 // indirect
	github.com/golang-jwt/jwt/v5 v5.3.1 // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/google/flatbuffers v25.12.19+incompatible // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/hashicorp/go-hclog v1.6.3 // indirect
	github.com/hashicorp/yamux v0.1.2 // indirect
	github.com/klauspost/compress v1.18.6 // indirect
	github.com/klauspost/cpuid/v2 v2.3.0 // indirect
	github.com/kylelemons/godebug v1.1.0 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.22 // indirect
	github.com/microsoft/kiota-abstractions-go v1.9.4 // indirect
	github.com/microsoft/kiota-authentication-azure-go v1.3.1 // indirect
	github.com/microsoft/kiota-http-go v1.5.6 // indirect
	github.com/microsoft/kiota-serialization-form-go v1.1.3 // indirect
	github.com/microsoft/kiota-serialization-json-go v1.1.2 // indirect
	github.com/microsoft/kiota-serialization-multipart-go v1.1.2 // indirect
	github.com/microsoft/kiota-serialization-text-go v1.1.3 // indirect
	github.com/microsoftgraph/msgraph-sdk-go-core v1.4.0 // indirect
	github.com/oklog/run v1.2.0 // indirect
	github.com/pierrec/lz4/v4 v4.1.26 // indirect
	github.com/pkg/browser v0.0.0-20240102092130-5ac0b6a4141c // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/std-uritemplate/std-uritemplate/go/v2 v2.0.8 // indirect
	github.com/stretchr/objx v0.5.2 // indirect
	github.com/zeebo/xxh3 v1.1.0 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/otel v1.43.0 // indirect
	go.opentelemetry.io/otel/metric v1.43.0 // indirect
	go.opentelemetry.io/otel/trace v1.43.0 // indirect
	golang.org/x/crypto v0.50.0 // indirect
	golang.org/x/exp v0.0.0-20260410095643-746e56fc9e2f // indirect
	golang.org/x/net v0.53.0 // indirect
	golang.org/x/sys v0.43.0 // indirect
	golang.org/x/text v0.36.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260504160031-60b97b32f348 // indirect
	google.golang.org/grpc v1.81.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

exclude google.golang.org/genproto v0.0.0-20200526211855-cb27e3aa2013

replace github.com/jlgore/corkscrew => ../..
