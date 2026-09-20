package main

import (
	pb "github.com/jlgore/corkscrew/internal/proto"
	contract "github.com/jlgore/corkscrew/pkg/correlation"
)

func enrichCorrelationEvidence(resources []*pb.Resource) {
	for _, resource := range resources {
		if resource != nil {
			if evidence := contract.InferNetworkEvidence(resource.RawData); len(evidence) > 0 {
				if value, err := contract.Attach(resource.Attributes, evidence...); err == nil {
					resource.Attributes = value
				}
			}
		}
	}
}
