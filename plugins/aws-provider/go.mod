module github.com/jlgore/corkscrew/plugins/aws-provider

go 1.24

toolchain go1.24.3

require (
	github.com/aws/aws-sdk-go-v2 v1.36.3
	github.com/aws/aws-sdk-go-v2/config v1.29.14
	github.com/aws/aws-sdk-go-v2/credentials v1.17.67
	github.com/aws/aws-sdk-go-v2/service/acm v1.32.0
	github.com/aws/aws-sdk-go-v2/service/apigatewayv2 v1.28.0
	github.com/aws/aws-sdk-go-v2/service/autoscaling v1.53.1
	github.com/aws/aws-sdk-go-v2/service/cloudformation v1.60.1
	github.com/aws/aws-sdk-go-v2/service/cloudtrail v1.46.3
	github.com/aws/aws-sdk-go-v2/service/cloudwatch v1.45.1
	github.com/aws/aws-sdk-go-v2/service/configservice v1.52.0
	github.com/aws/aws-sdk-go-v2/service/dynamodb v1.43.1
	github.com/aws/aws-sdk-go-v2/service/ec2 v1.224.0
	github.com/aws/aws-sdk-go-v2/service/ecs v1.57.3
	github.com/aws/aws-sdk-go-v2/service/eks v1.65.1
	github.com/aws/aws-sdk-go-v2/service/elasticache v1.46.0
	github.com/aws/aws-sdk-go-v2/service/elasticloadbalancing v1.29.4
	github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2 v1.45.2
	github.com/aws/aws-sdk-go-v2/service/glue v1.113.0
	github.com/aws/aws-sdk-go-v2/service/iam v1.42.0
	github.com/aws/aws-sdk-go-v2/service/kinesis v1.35.0
	github.com/aws/aws-sdk-go-v2/service/kms v1.38.3
	github.com/aws/aws-sdk-go-v2/service/lambda v1.71.2
	github.com/aws/aws-sdk-go-v2/service/rds v1.96.0
	github.com/aws/aws-sdk-go-v2/service/redshift v1.54.3
	github.com/aws/aws-sdk-go-v2/service/resourceexplorer2 v1.8.0
	github.com/aws/aws-sdk-go-v2/service/route53 v1.52.0
	github.com/aws/aws-sdk-go-v2/service/s3 v1.80.0
	github.com/aws/aws-sdk-go-v2/service/secretsmanager v1.35.5
	github.com/aws/aws-sdk-go-v2/service/sns v1.34.5
	github.com/aws/aws-sdk-go-v2/service/sqs v1.38.6
	github.com/aws/aws-sdk-go-v2/service/ssm v1.59.1
	github.com/aws/aws-sdk-go-v2/service/sts v1.33.19
	github.com/hashicorp/go-plugin v1.6.0
	github.com/jlgore/corkscrew v0.0.0
	github.com/marcboeker/go-duckdb v1.8.5
	github.com/stretchr/testify v1.10.0
	golang.org/x/sync v0.14.0
	golang.org/x/time v0.5.0
	google.golang.org/protobuf v1.36.6
)

replace github.com/jlgore/corkscrew => ../..

require (
	github.com/apache/arrow-go/v18 v18.3.0 // indirect
	github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream v1.6.10 // indirect
	github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.16.30 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.3.34 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.6.34 // indirect
	github.com/aws/aws-sdk-go-v2/internal/ini v1.8.3 // indirect
	github.com/aws/aws-sdk-go-v2/internal/v4a v1.3.34 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.12.3 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/checksum v1.7.2 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/endpoint-discovery v1.10.15 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.12.15 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/s3shared v1.18.15 // indirect
	github.com/aws/aws-sdk-go-v2/service/sso v1.25.3 // indirect
	github.com/aws/aws-sdk-go-v2/service/ssooidc v1.30.1 // indirect
	github.com/aws/smithy-go v1.22.3 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/fatih/color v1.15.0 // indirect
	github.com/go-viper/mapstructure/v2 v2.2.1 // indirect
	github.com/goccy/go-json v0.10.5 // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/google/flatbuffers v25.2.10+incompatible // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/hashicorp/go-hclog v1.5.0 // indirect
	github.com/hashicorp/yamux v0.1.1 // indirect
	github.com/klauspost/compress v1.18.0 // indirect
	github.com/klauspost/cpuid/v2 v2.2.10 // indirect
	github.com/mattn/go-colorable v0.1.13 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mitchellh/go-testing-interface v1.14.1 // indirect
	github.com/oklog/run v1.1.0 // indirect
	github.com/pierrec/lz4/v4 v4.1.22 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/rogpeppe/go-internal v1.14.1 // indirect
	github.com/stretchr/objx v0.5.2 // indirect
	github.com/zeebo/xxh3 v1.0.2 // indirect
	golang.org/x/exp v0.0.0-20250506013437-ce4c2cf36ca6 // indirect
	golang.org/x/mod v0.24.0 // indirect
	golang.org/x/net v0.40.0 // indirect
	golang.org/x/sys v0.33.0 // indirect
	golang.org/x/text v0.25.0 // indirect
	golang.org/x/tools v0.33.0 // indirect
	golang.org/x/xerrors v0.0.0-20240903120638-7835f813f4da // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20250218202821-56aae31c358a // indirect
	google.golang.org/grpc v1.72.1 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
