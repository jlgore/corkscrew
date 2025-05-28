package types

// AWSResourceRef represents a lightweight reference to an AWS resource
type AWSResourceRef struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Type     string            `json:"type"`
	Service  string            `json:"service"`
	Region   string            `json:"region"`
	ARN      string            `json:"arn,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}
