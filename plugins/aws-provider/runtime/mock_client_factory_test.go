package runtime

// MockClientFactory is a simple mock for testing.
type MockClientFactory struct{}

func (m *MockClientFactory) GetClient(serviceName string) interface{}        { return nil }
func (m *MockClientFactory) GetAvailableServices() []string                  { return []string{"s3", "ec2", "lambda"} }
func (m *MockClientFactory) GetClientMethodNames(serviceName string) []string { return nil }
func (m *MockClientFactory) GetListOperations(serviceName string) []string    { return nil }
func (m *MockClientFactory) GetDescribeOperations(serviceName string) []string { return nil }
func (m *MockClientFactory) HasClient(serviceName string) bool                 { return true }
