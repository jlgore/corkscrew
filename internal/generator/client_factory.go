package generator

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// ClientFactory creates AWS service clients
type ClientFactory struct {
	cfg aws.Config
}

// NewClientFactory creates a new AWS client factory
func NewClientFactory(ctx context.Context) (*ClientFactory, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	return &ClientFactory{
		cfg: cfg,
	}, nil
}

// GetS3Client returns an S3 client
func (f *ClientFactory) GetS3Client(ctx context.Context) (*s3.Client, error) {
	return s3.NewFromConfig(f.cfg), nil
}

// GetEc2Client returns an EC2 client
func (f *ClientFactory) GetEc2Client(ctx context.Context) (*ec2.Client, error) {
	return ec2.NewFromConfig(f.cfg), nil
}

// GetRdsClient returns an RDS client
func (f *ClientFactory) GetRdsClient(ctx context.Context) (*rds.Client, error) {
	return rds.NewFromConfig(f.cfg), nil
}

// GetLambdaClient returns a Lambda client
func (f *ClientFactory) GetLambdaClient(ctx context.Context) (*lambda.Client, error) {
	return lambda.NewFromConfig(f.cfg), nil
}

// GetIamClient returns an IAM client
func (f *ClientFactory) GetIamClient(ctx context.Context) (*iam.Client, error) {
	return iam.NewFromConfig(f.cfg), nil
}

// GetDynamodbClient returns a DynamoDB client
func (f *ClientFactory) GetDynamodbClient(ctx context.Context) (*dynamodb.Client, error) {
	return dynamodb.NewFromConfig(f.cfg), nil
}

// GetEcsClient returns an ECS client
func (f *ClientFactory) GetEcsClient(ctx context.Context) (*ecs.Client, error) {
	return ecs.NewFromConfig(f.cfg), nil
}

// GetEksClient returns an EKS client
func (f *ClientFactory) GetEksClient(ctx context.Context) (*eks.Client, error) {
	return eks.NewFromConfig(f.cfg), nil
}

// GetCloudformationClient returns a CloudFormation client (placeholder)
func (f *ClientFactory) GetCloudformationClient(ctx context.Context) (interface{}, error) {
	// Would implement with actual CloudFormation client
	return nil, fmt.Errorf("CloudFormation client not implemented")
}

// GetElasticloadbalancingv2Client returns an ELBv2 client (placeholder)
func (f *ClientFactory) GetElasticloadbalancingv2Client(ctx context.Context) (interface{}, error) {
	// Would implement with actual ELBv2 client
	return nil, fmt.Errorf("ELBv2 client not implemented")
}