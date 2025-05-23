package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/hashicorp/go-plugin"
	"github.com/jlgore/corkscrew-generator/internal/shared"
	pb "github.com/jlgore/corkscrew-generator/internal/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	ServiceName = "s3"
	Version     = "1.0.0"
)

type S3Scanner struct{}

func (s *S3Scanner) Scan(ctx context.Context, req *pb.ScanRequest) (*pb.ScanResponse, error) {
	start := time.Now()
	stats := &pb.ScanStats{
		ResourceCounts: make(map[string]int32),
	}

	// Load AWS config
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(req.Region),
	)
	if err != nil {
		return &pb.ScanResponse{
			Error: fmt.Sprintf("failed to load AWS config: %v", err),
		}, nil
	}

	client := s3.NewFromConfig(cfg)

	var resources []*pb.Resource

	// Scan Bucket resources
	bucketResources, err := s.scanBuckets(ctx, client, req)
	if err != nil {
		log.Printf("Error scanning buckets: %v", err)
		stats.FailedResources += int32(len(bucketResources))
	} else {
		resources = append(resources, bucketResources...)
		stats.ResourceCounts["Bucket"] = int32(len(bucketResources))
	}

	// Scan Objects for each bucket (limited sample)
	for _, bucket := range bucketResources {
		objectResources, err := s.scanObjects(ctx, client, req, bucket.Name)
		if err != nil {
			log.Printf("Error scanning objects for bucket %s: %v", bucket.Name, err)
			stats.FailedResources += int32(len(objectResources))
		} else {
			resources = append(resources, objectResources...)
			stats.ResourceCounts["Object"] += int32(len(objectResources))
		}
	}

	stats.TotalResources = int32(len(resources))
	stats.DurationMs = time.Since(start).Milliseconds()

	return &pb.ScanResponse{
		Resources: resources,
		Stats:     stats,
		Metadata: map[string]string{
			"service":   ServiceName,
			"version":   Version,
			"region":    req.Region,
			"scan_time": time.Now().Format(time.RFC3339),
		},
	}, nil
}

func (s *S3Scanner) GetSchemas(ctx context.Context, req *pb.Empty) (*pb.SchemaResponse, error) {
	schemas := []*pb.Schema{
		{
			Name: "s3_buckets",
			Sql: `CREATE TABLE s3_buckets (
				id VARCHAR PRIMARY KEY,
				name VARCHAR NOT NULL,
				region VARCHAR,
				creation_date TIMESTAMP,
				versioning_status VARCHAR,
				encryption_enabled BOOLEAN,
				public_access_blocked BOOLEAN,
				tags JSON,
				raw_data JSON
			)`,
			Description: "S3 bucket resources with configuration details",
		},
		{
			Name: "s3_objects",
			Sql: `CREATE TABLE s3_objects (
				id VARCHAR PRIMARY KEY,
				bucket_name VARCHAR NOT NULL,
				key VARCHAR NOT NULL,
				size BIGINT,
				last_modified TIMESTAMP,
				etag VARCHAR,
				storage_class VARCHAR,
				tags JSON,
				raw_data JSON,
				FOREIGN KEY (bucket_name) REFERENCES s3_buckets(name)
			)`,
			Description: "S3 object resources within buckets",
		},
	}

	return &pb.SchemaResponse{Schemas: schemas}, nil
}

func (s *S3Scanner) GetServiceInfo(ctx context.Context, req *pb.Empty) (*pb.ServiceInfoResponse, error) {
	return &pb.ServiceInfoResponse{
		ServiceName: ServiceName,
		Version:     Version,
		SupportedResources: []string{
			"Bucket",
			"Object",
		},
		RequiredPermissions: []string{
			"s3:ListAllMyBuckets",
			"s3:GetBucketLocation",
			"s3:GetBucketVersioning",
			"s3:GetBucketEncryption",
			"s3:GetBucketPublicAccessBlock",
			"s3:GetBucketTagging",
			"s3:ListBucket",
			"s3:GetObjectTagging",
		},
		Capabilities: map[string]string{
			"streaming":    "true",
			"pagination":   "true",
			"parallelism":  "10",
			"relationships": "true",
		},
	}, nil
}

func (s *S3Scanner) StreamScan(req *pb.ScanRequest, stream pb.Scanner_StreamScanServer) error {
	ctx := stream.Context()

	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(req.Region),
	)
	if err != nil {
		return fmt.Errorf("failed to load AWS config: %w", err)
	}

	client := s3.NewFromConfig(cfg)

	// Stream buckets
	if err := s.streamBuckets(ctx, client, req, stream); err != nil {
		return fmt.Errorf("error streaming buckets: %w", err)
	}

	return nil
}

func (s *S3Scanner) scanBuckets(ctx context.Context, client *s3.Client, req *pb.ScanRequest) ([]*pb.Resource, error) {
	var resources []*pb.Resource

	// List all buckets
	result, err := client.ListBuckets(ctx, &s3.ListBucketsInput{})
	if err != nil {
		return nil, fmt.Errorf("failed to list buckets: %w", err)
	}

	for _, bucket := range result.Buckets {
		resource, err := s.convertBucketToResource(ctx, client, bucket, req.Region)
		if err != nil {
			log.Printf("Error converting bucket %s: %v", *bucket.Name, err)
			continue
		}
		resources = append(resources, resource)
	}

	return resources, nil
}

func (s *S3Scanner) scanObjects(ctx context.Context, client *s3.Client, req *pb.ScanRequest, bucketName string) ([]*pb.Resource, error) {
	var resources []*pb.Resource

	// List objects in bucket (limited to first 100 for demo)
	maxKeys := int32(100)
	if req.MaxResults > 0 && req.MaxResults < maxKeys {
		maxKeys = req.MaxResults
	}

	result, err := client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket:  aws.String(bucketName),
		MaxKeys: &maxKeys,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list objects in bucket %s: %w", bucketName, err)
	}

	for _, object := range result.Contents {
		resource, err := s.convertObjectToResource(ctx, client, object, bucketName, req.Region)
		if err != nil {
			log.Printf("Error converting object %s: %v", *object.Key, err)
			continue
		}
		resources = append(resources, resource)
	}

	return resources, nil
}

func (s *S3Scanner) convertBucketToResource(ctx context.Context, client *s3.Client, bucket types.Bucket, region string) (*pb.Resource, error) {
	bucketName := *bucket.Name

	// Get bucket location
	locationResult, err := client.GetBucketLocation(ctx, &s3.GetBucketLocationInput{
		Bucket: aws.String(bucketName),
	})
	if err != nil {
		log.Printf("Warning: Could not get location for bucket %s: %v", bucketName, err)
	}

	actualRegion := region
	if locationResult != nil && locationResult.LocationConstraint != "" {
		actualRegion = string(locationResult.LocationConstraint)
	}

	// Get additional bucket properties
	attributes := make(map[string]interface{})

	// Get versioning
	if versioningResult, err := client.GetBucketVersioning(ctx, &s3.GetBucketVersioningInput{
		Bucket: aws.String(bucketName),
	}); err == nil {
		attributes["versioning_status"] = string(versioningResult.Status)
	}

	// Get encryption
	if encryptionResult, err := client.GetBucketEncryption(ctx, &s3.GetBucketEncryptionInput{
		Bucket: aws.String(bucketName),
	}); err == nil {
		attributes["encryption_enabled"] = len(encryptionResult.ServerSideEncryptionConfiguration.Rules) > 0
	}

	// Get public access block
	if pabResult, err := client.GetPublicAccessBlock(ctx, &s3.GetPublicAccessBlockInput{
		Bucket: aws.String(bucketName),
	}); err == nil {
		attributes["public_access_blocked"] = pabResult.PublicAccessBlockConfiguration.BlockPublicAcls != nil && *pabResult.PublicAccessBlockConfiguration.BlockPublicAcls
	}

	// Get tags
	tags := make(map[string]string)
	if taggingResult, err := client.GetBucketTagging(ctx, &s3.GetBucketTaggingInput{
		Bucket: aws.String(bucketName),
	}); err == nil {
		for _, tag := range taggingResult.TagSet {
			tags[*tag.Key] = *tag.Value
		}
	}

	// Convert to JSON
	rawData, _ := json.Marshal(bucket)
	attributesJSON, _ := json.Marshal(attributes)

	return &pb.Resource{
		Type:      "Bucket",
		Id:        bucketName,
		Arn:       fmt.Sprintf("arn:aws:s3:::%s", bucketName),
		Name:      bucketName,
		Region:    actualRegion,
		RawData:   string(rawData),
		Tags:      tags,
		CreatedAt: timestamppb.New(*bucket.CreationDate),
		Attributes: string(attributesJSON),
		Relationships: []*pb.Relationship{
			// Buckets can have relationships to objects, but we'll add those when scanning objects
		},
	}, nil
}

func (s *S3Scanner) convertObjectToResource(ctx context.Context, client *s3.Client, object types.Object, bucketName, region string) (*pb.Resource, error) {
	objectKey := *object.Key
	objectId := fmt.Sprintf("%s/%s", bucketName, objectKey)

	// Get object tags
	tags := make(map[string]string)
	if taggingResult, err := client.GetObjectTagging(ctx, &s3.GetObjectTaggingInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(objectKey),
	}); err == nil {
		for _, tag := range taggingResult.TagSet {
			tags[*tag.Key] = *tag.Value
		}
	}

	// Convert to JSON
	rawData, _ := json.Marshal(object)

	attributes := map[string]interface{}{
		"size":          *object.Size,
		"storage_class": string(object.StorageClass),
		"etag":          *object.ETag,
	}
	attributesJSON, _ := json.Marshal(attributes)

	return &pb.Resource{
		Type:     "Object",
		Id:       objectId,
		ParentId: bucketName,
		Name:     objectKey,
		Region:   region,
		RawData:  string(rawData),
		Tags:     tags,
		ModifiedAt: timestamppb.New(*object.LastModified),
		Attributes: string(attributesJSON),
		Relationships: []*pb.Relationship{
			{
				TargetId:         bucketName,
				TargetType:       "Bucket",
				RelationshipType: "contained_in",
				Properties: map[string]string{
					"relationship": "object_to_bucket",
				},
			},
		},
	}, nil
}

func (s *S3Scanner) streamBuckets(ctx context.Context, client *s3.Client, req *pb.ScanRequest, stream pb.Scanner_StreamScanServer) error {
	result, err := client.ListBuckets(ctx, &s3.ListBucketsInput{})
	if err != nil {
		return fmt.Errorf("failed to list buckets: %w", err)
	}

	for _, bucket := range result.Buckets {
		resource, err := s.convertBucketToResource(ctx, client, bucket, req.Region)
		if err != nil {
			log.Printf("Error converting bucket %s: %v", *bucket.Name, err)
			continue
		}

		if err := stream.Send(resource); err != nil {
			return fmt.Errorf("failed to send bucket resource: %w", err)
		}
	}

	return nil
}

func main() {
	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: shared.HandshakeConfig,
		Plugins: map[string]plugin.Plugin{
			"scanner": &shared.ScannerGRPCPlugin{
				Impl: &S3Scanner{},
			},
		},
		GRPCServer: plugin.DefaultGRPCServer,
	})
}
