package main

import (
	pb "github.com/jlgore/corkscrew/internal/proto"
	contract "github.com/jlgore/corkscrew/pkg/correlation"
)

func enrichCorrelationEvidence(resources []*pb.Resource) {
	for _, resource := range resources {
		if resource == nil {
			continue
		}
		evidence := contract.InferNetworkEvidence(resource.RawData)
		if len(evidence) == 0 {
			continue
		}
		if attributes, err := contract.Attach(resource.Attributes, evidence...); err == nil {
			resource.Attributes = attributes
		}
	}
}
