// Code generated by client_factory_generator.go at 2025-06-08T13:48:55-05:00. DO NOT EDIT.

package main

import (
	"log"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/jlgore/corkscrew/plugins/aws-provider/pkg/client"

	autoscaling "github.com/aws/aws-sdk-go-v2/service/autoscaling"

	cloudformation "github.com/aws/aws-sdk-go-v2/service/cloudformation"

	cloudwatch "github.com/aws/aws-sdk-go-v2/service/cloudwatch"

	dynamodb "github.com/aws/aws-sdk-go-v2/service/dynamodb"

	ec2 "github.com/aws/aws-sdk-go-v2/service/ec2"

	ecs "github.com/aws/aws-sdk-go-v2/service/ecs"

	eks "github.com/aws/aws-sdk-go-v2/service/eks"

	elasticloadbalancing "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancing"

	iam "github.com/aws/aws-sdk-go-v2/service/iam"

	kms "github.com/aws/aws-sdk-go-v2/service/kms"

	lambda "github.com/aws/aws-sdk-go-v2/service/lambda"

	rds "github.com/aws/aws-sdk-go-v2/service/rds"

	route53 "github.com/aws/aws-sdk-go-v2/service/route53"

	s3 "github.com/aws/aws-sdk-go-v2/service/s3"

	secretsmanager "github.com/aws/aws-sdk-go-v2/service/secretsmanager"

	sns "github.com/aws/aws-sdk-go-v2/service/sns"

	sqs "github.com/aws/aws-sdk-go-v2/service/sqs"

	ssm "github.com/aws/aws-sdk-go-v2/service/ssm"

	sts "github.com/aws/aws-sdk-go-v2/service/sts"
)

func init() {
	log.Printf("Initializing generated client factory with 19 AWS services")

	// Register all service client constructors

	client.RegisterConstructor("autoscaling", func(cfg aws.Config) interface{} {
		return autoscaling.NewFromConfig(cfg)
	})

	client.RegisterConstructor("cloudformation", func(cfg aws.Config) interface{} {
		return cloudformation.NewFromConfig(cfg)
	})

	client.RegisterConstructor("cloudwatch", func(cfg aws.Config) interface{} {
		return cloudwatch.NewFromConfig(cfg)
	})

	client.RegisterConstructor("dynamodb", func(cfg aws.Config) interface{} {
		return dynamodb.NewFromConfig(cfg)
	})

	client.RegisterConstructor("ec2", func(cfg aws.Config) interface{} {
		return ec2.NewFromConfig(cfg)
	})

	client.RegisterConstructor("ecs", func(cfg aws.Config) interface{} {
		return ecs.NewFromConfig(cfg)
	})

	client.RegisterConstructor("eks", func(cfg aws.Config) interface{} {
		return eks.NewFromConfig(cfg)
	})

	client.RegisterConstructor("elasticloadbalancing", func(cfg aws.Config) interface{} {
		return elasticloadbalancing.NewFromConfig(cfg)
	})

	client.RegisterConstructor("iam", func(cfg aws.Config) interface{} {
		return iam.NewFromConfig(cfg)
	})

	client.RegisterConstructor("kms", func(cfg aws.Config) interface{} {
		return kms.NewFromConfig(cfg)
	})

	client.RegisterConstructor("lambda", func(cfg aws.Config) interface{} {
		return lambda.NewFromConfig(cfg)
	})

	client.RegisterConstructor("rds", func(cfg aws.Config) interface{} {
		return rds.NewFromConfig(cfg)
	})

	client.RegisterConstructor("route53", func(cfg aws.Config) interface{} {
		return route53.NewFromConfig(cfg)
	})

	client.RegisterConstructor("s3", func(cfg aws.Config) interface{} {
		return s3.NewFromConfig(cfg)
	})

	client.RegisterConstructor("secretsmanager", func(cfg aws.Config) interface{} {
		return secretsmanager.NewFromConfig(cfg)
	})

	client.RegisterConstructor("sns", func(cfg aws.Config) interface{} {
		return sns.NewFromConfig(cfg)
	})

	client.RegisterConstructor("sqs", func(cfg aws.Config) interface{} {
		return sqs.NewFromConfig(cfg)
	})

	client.RegisterConstructor("ssm", func(cfg aws.Config) interface{} {
		return ssm.NewFromConfig(cfg)
	})

	client.RegisterConstructor("sts", func(cfg aws.Config) interface{} {
		return sts.NewFromConfig(cfg)
	})

	log.Printf("Successfully registered %d AWS service client constructors", len(client.ListRegisteredServices()))

	// Log all registered services for verification
	services := client.ListRegisteredServices()
	log.Printf("Registered services: %v", services)
}
