package main

import (
	"context"
	"fmt"
	"time"

	cloudflare "github.com/cloudflare/cloudflare-go/v6"
	"github.com/cloudflare/cloudflare-go/v6/kv"
	"github.com/cloudflare/cloudflare-go/v6/queues"
	"github.com/cloudflare/cloudflare-go/v6/r2"
	pb "github.com/jlgore/corkscrew/internal/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (p *CloudflareProvider) scanStorage(ctx context.Context) ([]*pb.Resource, []string) {
	accountIDs, err := p.accountIDsForWorkers(ctx)
	if err != nil {
		return nil, []string{fmt.Sprintf("accounts list failed: %v", err)}
	}

	resources := make([]*pb.Resource, 0)
	var errs []string

	for _, accountID := range accountIDs {
		buckets, err := p.listR2Buckets(ctx, accountID)
		if err != nil {
			errs = append(errs, fmt.Sprintf("r2 buckets list failed for account %s: %v", accountID, err))
		} else {
			for _, bucket := range buckets {
				resources = append(resources, p.r2BucketResource(accountID, bucket))
			}
		}

		namespaces, err := p.listKVNamespaces(ctx, accountID)
		if err != nil {
			errs = append(errs, fmt.Sprintf("kv namespaces list failed for account %s: %v", accountID, err))
		} else {
			for _, namespace := range namespaces {
				resources = append(resources, p.kvNamespaceResource(accountID, namespace))
			}
		}

		queuesList, err := p.listQueues(ctx, accountID)
		if err != nil {
			errs = append(errs, fmt.Sprintf("queues list failed for account %s: %v", accountID, err))
		} else {
			for _, queue := range queuesList {
				resources = append(resources, p.queueResource(accountID, queue))
			}
		}
	}

	return resources, errs
}

func (p *CloudflareProvider) listR2Buckets(ctx context.Context, accountID string) ([]r2.Bucket, error) {
	res, err := p.client.R2.Buckets.List(ctx, r2.BucketListParams{AccountID: cloudflare.F(accountID)})
	if err != nil {
		return nil, err
	}
	if res == nil {
		return nil, nil
	}
	return append([]r2.Bucket(nil), res.Buckets...), nil
}

func (p *CloudflareProvider) listKVNamespaces(ctx context.Context, accountID string) ([]kv.Namespace, error) {
	iter := p.client.KV.Namespaces.ListAutoPaging(ctx, kv.NamespaceListParams{AccountID: cloudflare.F(accountID)})
	results := make([]kv.Namespace, 0)
	for iter.Next() {
		results = append(results, iter.Current())
	}
	if err := iter.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

func (p *CloudflareProvider) listQueues(ctx context.Context, accountID string) ([]queues.Queue, error) {
	iter := p.client.Queues.ListAutoPaging(ctx, queues.QueueListParams{AccountID: cloudflare.F(accountID)})
	results := make([]queues.Queue, 0)
	for iter.Next() {
		results = append(results, iter.Current())
	}
	if err := iter.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

func (p *CloudflareProvider) r2BucketResource(accountID string, bucket r2.Bucket) *pb.Resource {
	attrs := map[string]string{
		"account_id":    accountID,
		"bucket_name":   bucket.Name,
		"location":      string(bucket.Location),
		"jurisdiction":  string(bucket.Jurisdiction),
		"storage_class": string(bucket.StorageClass),
	}
	resource := p.resource("storage", "r2_bucket", accountID+"/r2/"+bucket.Name, bucket.Name, accountID, bucket, attrs)
	if createdAt, err := time.Parse(time.RFC3339, bucket.CreationDate); err == nil {
		resource.CreatedAt = timestamppb.New(createdAt)
	}
	return resource
}

func (p *CloudflareProvider) kvNamespaceResource(accountID string, namespace kv.Namespace) *pb.Resource {
	attrs := map[string]string{
		"account_id":            accountID,
		"namespace_id":          namespace.ID,
		"title":                 namespace.Title,
		"supports_url_encoding": fmt.Sprintf("%t", namespace.SupportsURLEncoding),
	}
	return p.resource("storage", "kv_namespace", namespace.ID, namespace.Title, accountID, namespace, attrs)
}

func (p *CloudflareProvider) queueResource(accountID string, queue queues.Queue) *pb.Resource {
	attrs := map[string]string{
		"account_id":            accountID,
		"queue_id":              queue.QueueID,
		"queue_name":            queue.QueueName,
		"consumers_total_count": fmt.Sprintf("%.0f", queue.ConsumersTotalCount),
		"producers_total_count": fmt.Sprintf("%.0f", queue.ProducersTotalCount),
	}
	resource := p.resource("storage", "queue", queue.QueueID, queue.QueueName, accountID, queue, attrs)
	if createdAt, err := time.Parse(time.RFC3339, queue.CreatedOn); err == nil {
		resource.CreatedAt = timestamppb.New(createdAt)
	}
	if modifiedAt, err := time.Parse(time.RFC3339, queue.ModifiedOn); err == nil {
		resource.ModifiedAt = timestamppb.New(modifiedAt)
	}
	return resource
}
