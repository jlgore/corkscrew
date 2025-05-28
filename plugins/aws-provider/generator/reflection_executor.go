package generator

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/google/uuid"
	"github.com/jlgore/corkscrew/internal/db"
	pb "github.com/jlgore/corkscrew/internal/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ReflectionExecutor executes AWS operations using reflection
type ReflectionExecutor struct {
	// Cache for method lookups
	methodCache map[string]reflect.Method
	// Database for API action logging
	dbLogger *db.GraphLoader
}

// NewReflectionExecutor creates a new reflection executor
func NewReflectionExecutor() *ReflectionExecutor {
	return &ReflectionExecutor{
		methodCache: make(map[string]reflect.Method),
	}
}

// NewReflectionExecutorWithLogging creates a new reflection executor with DuckDB logging
func NewReflectionExecutorWithLogging(dbLogger *db.GraphLoader) *ReflectionExecutor {
	return &ReflectionExecutor{
		methodCache: make(map[string]reflect.Method),
		dbLogger:    dbLogger,
	}
}

// ExecuteListOperation executes a List* operation and extracts resource references
func (r *ReflectionExecutor) ExecuteListOperation(ctx context.Context, client interface{}, operation AWSOperation, region string) ([]*pb.ResourceRef, error) {
	clientValue := reflect.ValueOf(client)
	clientType := clientValue.Type()

	// Find the method
	method, exists := clientType.MethodByName(operation.Name)
	if !exists {
		return nil, fmt.Errorf("method %s not found on client", operation.Name)
	}

	// Create input struct
	inputType := method.Type.In(2) // ctx, client, input
	if inputType.Kind() == reflect.Ptr {
		inputType = inputType.Elem()
	}

	inputValue := reflect.New(inputType)

	// Set pagination fields if needed
	if operation.Paginated {
		r.setPaginationFields(inputValue.Elem(), nil)
	}

	// Call the method with logging
	args := []reflect.Value{
		clientValue,
		reflect.ValueOf(ctx),
		inputValue,
	}

	start := time.Now()
	results := method.Func.Call(args)
	duration := time.Since(start)

	if len(results) != 2 {
		return nil, fmt.Errorf("unexpected number of return values from %s", operation.Name)
	}

	// Check for error
	success := results[1].IsNil()
	var errorMsg string
	if !success {
		err := results[1].Interface().(error)
		errorMsg = err.Error()
		
		// Log the failed API call
		r.logAPIAction(ctx, operation, region, false, duration, 0, errorMsg)
		return nil, fmt.Errorf("AWS API call failed: %w", err)
	}

	// Extract resources from output
	output := results[0].Interface()
	resourceRefs, err := r.extractResourceRefs(output, operation.ResourceType, region)
	
	// Log the successful API call
	resourceCount := 0
	if err == nil {
		resourceCount = len(resourceRefs)
	}
	r.logAPIAction(ctx, operation, region, true, duration, resourceCount, "")
	
	return resourceRefs, err
}

// ExecuteDescribeOperation executes a Describe*/Get* operation for a specific resource
func (r *ReflectionExecutor) ExecuteDescribeOperation(ctx context.Context, client interface{}, operation AWSOperation, ref *pb.ResourceRef, region string) (*pb.Resource, error) {
	clientValue := reflect.ValueOf(client)
	clientType := clientValue.Type()

	// Find the method
	method, exists := clientType.MethodByName(operation.Name)
	if !exists {
		return nil, fmt.Errorf("method %s not found on client", operation.Name)
	}

	// Create input struct
	inputType := method.Type.In(2) // ctx, client, input
	if inputType.Kind() == reflect.Ptr {
		inputType = inputType.Elem()
	}

	inputValue := reflect.New(inputType)

	// Set the resource identifier in the input
	r.setResourceIdentifier(inputValue.Elem(), ref, operation.ResourceType)

	// Call the method
	args := []reflect.Value{
		clientValue,
		reflect.ValueOf(ctx),
		inputValue,
	}

	results := method.Func.Call(args)
	if len(results) != 2 {
		return nil, fmt.Errorf("unexpected number of return values from %s", operation.Name)
	}

	// Check for error
	if !results[1].IsNil() {
		err := results[1].Interface().(error)
		return nil, fmt.Errorf("AWS API call failed: %w", err)
	}

	// Extract detailed resource from output
	output := results[0].Interface()
	return r.extractDetailedResource(output, ref, region)
}

// extractResourceRefs extracts resource references from List* operation output
func (r *ReflectionExecutor) extractResourceRefs(output interface{}, resourceType, region string) ([]*pb.ResourceRef, error) {
	outputValue := reflect.ValueOf(output)
	if outputValue.Kind() == reflect.Ptr {
		outputValue = outputValue.Elem()
	}

	var refs []*pb.ResourceRef

	// Look for common list field patterns
	listFieldNames := []string{
		resourceType + "s",     // Buckets, Instances, etc.
		resourceType + "Names", // TableNames, etc.
		resourceType,           // Bucket, Instance, etc.
		"Items",                // Generic Items
		"Resources",            // Generic Resources
		"Results",              // Generic Results
	}

	for _, fieldName := range listFieldNames {
		field := outputValue.FieldByName(fieldName)
		if !field.IsValid() || field.Kind() != reflect.Slice {
			continue
		}

		// Handle slice of strings (like DynamoDB TableNames)
		if field.Type().Elem().Kind() == reflect.String {
			for i := 0; i < field.Len(); i++ {
				name := field.Index(i).String()
				ref := &pb.ResourceRef{
					Id:     name,
					Name:   name,
					Type:   resourceType,
					Region: region,
				}
				refs = append(refs, ref)
			}
			break
		}

		// Handle slice of objects
		for i := 0; i < field.Len(); i++ {
			item := field.Index(i)
			ref := r.extractResourceRef(item, resourceType, region)
			if ref != nil {
				refs = append(refs, ref)
			}
		}
		break
	}

	return refs, nil
}

// extractResourceRef extracts a single resource reference from an item
func (r *ReflectionExecutor) extractResourceRef(item reflect.Value, resourceType, region string) *pb.ResourceRef {
	if item.Kind() == reflect.Ptr {
		if item.IsNil() {
			return nil
		}
		item = item.Elem()
	}

	ref := &pb.ResourceRef{
		Type:   resourceType,
		Region: region,
	}

	// Extract ID field
	idFieldNames := []string{
		resourceType + "Id",
		resourceType + "Name",
		"Id",
		"Name",
		"Identifier",
	}

	for _, fieldName := range idFieldNames {
		field := item.FieldByName(fieldName)
		if field.IsValid() && field.Kind() == reflect.String {
			ref.Id = field.String()
			break
		}
		// Handle *string fields
		if field.IsValid() && field.Kind() == reflect.Ptr && field.Type().Elem().Kind() == reflect.String {
			if !field.IsNil() {
				ref.Id = field.Elem().String()
				break
			}
		}
	}

	// Extract Name field (might be different from ID)
	nameFieldNames := []string{
		"Name",
		resourceType + "Name",
		"DisplayName",
		"Title",
	}

	for _, fieldName := range nameFieldNames {
		field := item.FieldByName(fieldName)
		if field.IsValid() && field.Kind() == reflect.String {
			ref.Name = field.String()
			break
		}
		// Handle *string fields
		if field.IsValid() && field.Kind() == reflect.Ptr && field.Type().Elem().Kind() == reflect.String {
			if !field.IsNil() {
				ref.Name = field.Elem().String()
				break
			}
		}
	}

	// Use ID as name if name is empty
	if ref.Name == "" {
		ref.Name = ref.Id
	}

	return ref
}

// extractDetailedResource extracts detailed resource information from Describe*/Get* output
func (r *ReflectionExecutor) extractDetailedResource(output interface{}, ref *pb.ResourceRef, region string) (*pb.Resource, error) {
	outputValue := reflect.ValueOf(output)
	if outputValue.Kind() == reflect.Ptr {
		outputValue = outputValue.Elem()
	}

	resource := &pb.Resource{
		Id:     ref.Id,
		Name:   ref.Name,
		Type:   ref.Type,
		Region: region,
		Tags:   make(map[string]string),
	}

	// Look for the main resource object in the output
	resourceFieldNames := []string{
		ref.Type,                 // Instance, Bucket, etc.
		ref.Type + "Description", // InstanceDescription, etc.
		ref.Type + "Details",     // InstanceDetails, etc.
		"Resource",               // Generic Resource
		"Item",                   // Generic Item
	}

	var resourceObj reflect.Value
	for _, fieldName := range resourceFieldNames {
		field := outputValue.FieldByName(fieldName)
		if field.IsValid() {
			resourceObj = field
			break
		}
	}

	if !resourceObj.IsValid() {
		// Use the entire output as the resource object
		resourceObj = outputValue
	}

	if resourceObj.Kind() == reflect.Ptr {
		if resourceObj.IsNil() {
			return resource, nil
		}
		resourceObj = resourceObj.Elem()
	}

	// Extract ARN
	arnField := resourceObj.FieldByName("Arn")
	if !arnField.IsValid() {
		arnField = resourceObj.FieldByName("ARN")
	}
	if arnField.IsValid() {
		if arnField.Kind() == reflect.String {
			resource.Arn = arnField.String()
		} else if arnField.Kind() == reflect.Ptr && arnField.Type().Elem().Kind() == reflect.String {
			if !arnField.IsNil() {
				resource.Arn = arnField.Elem().String()
			}
		}
	}

	// Extract timestamps
	r.extractTimestamps(resourceObj, resource)

	// Extract tags
	r.extractTags(resourceObj, resource)

	// TODO: Store the raw configuration as JSON when RawConfig field is added to proto
	// resource.RawConfig = r.structToMap(resourceObj.Interface())

	return resource, nil
}

// setResourceIdentifier sets the resource identifier in the input struct
func (r *ReflectionExecutor) setResourceIdentifier(input reflect.Value, ref *pb.ResourceRef, resourceType string) {
	// Common identifier field patterns
	idFieldNames := []string{
		resourceType + "Id",
		resourceType + "Name",
		"Id",
		"Name",
		"Identifier",
	}

	for _, fieldName := range idFieldNames {
		field := input.FieldByName(fieldName)
		if field.IsValid() && field.CanSet() {
			if field.Kind() == reflect.String {
				field.SetString(ref.Id)
				return
			} else if field.Kind() == reflect.Ptr && field.Type().Elem().Kind() == reflect.String {
				strPtr := reflect.New(field.Type().Elem())
				strPtr.Elem().SetString(ref.Id)
				field.Set(strPtr)
				return
			}
		}
	}
}

// setPaginationFields sets pagination fields in the input struct
func (r *ReflectionExecutor) setPaginationFields(input reflect.Value, token interface{}) {
	paginationFields := []string{
		"NextToken",
		"Marker",
		"ContinuationToken",
		"PageToken",
	}

	for _, fieldName := range paginationFields {
		field := input.FieldByName(fieldName)
		if field.IsValid() && field.CanSet() {
			if token == nil {
				continue // Don't set anything for first page
			}

			if field.Kind() == reflect.String {
				if tokenStr, ok := token.(string); ok {
					field.SetString(tokenStr)
				}
			} else if field.Kind() == reflect.Ptr && field.Type().Elem().Kind() == reflect.String {
				if tokenStr, ok := token.(string); ok {
					strPtr := reflect.New(field.Type().Elem())
					strPtr.Elem().SetString(tokenStr)
					field.Set(strPtr)
				}
			}
		}
	}
}

// extractTimestamps extracts timestamp fields from the resource object
func (r *ReflectionExecutor) extractTimestamps(resourceObj reflect.Value, resource *pb.Resource) {
	timestampFields := map[string]**timestamppb.Timestamp{
		"CreatedAt":    &resource.CreatedAt,
		"CreationDate": &resource.CreatedAt,
		"CreateTime":   &resource.CreatedAt,
		"LaunchTime":   &resource.CreatedAt,
		"ModifiedAt":   &resource.ModifiedAt,
		"LastModified": &resource.ModifiedAt,
		"UpdatedAt":    &resource.ModifiedAt,
	}

	for fieldName, targetField := range timestampFields {
		field := resourceObj.FieldByName(fieldName)
		if field.IsValid() {
			var timeVal time.Time
			var ok bool

			if field.Kind() == reflect.Ptr {
				if field.IsNil() {
					continue
				}
				field = field.Elem()
			}

			// Handle time.Time
			if timeVal, ok = field.Interface().(time.Time); ok {
				*targetField = timestamppb.New(timeVal)
			}
		}
	}
}

// extractTags extracts tags from the resource object
func (r *ReflectionExecutor) extractTags(resourceObj reflect.Value, resource *pb.Resource) {
	tagsField := resourceObj.FieldByName("Tags")
	if !tagsField.IsValid() {
		return
	}

	if tagsField.Kind() == reflect.Ptr {
		if tagsField.IsNil() {
			return
		}
		tagsField = tagsField.Elem()
	}

	// Handle slice of tag objects
	if tagsField.Kind() == reflect.Slice {
		for i := 0; i < tagsField.Len(); i++ {
			tag := tagsField.Index(i)
			if tag.Kind() == reflect.Ptr {
				tag = tag.Elem()
			}

			keyField := tag.FieldByName("Key")
			valueField := tag.FieldByName("Value")

			if keyField.IsValid() && valueField.IsValid() {
				var key, value string

				if keyField.Kind() == reflect.String {
					key = keyField.String()
				} else if keyField.Kind() == reflect.Ptr && keyField.Type().Elem().Kind() == reflect.String {
					if !keyField.IsNil() {
						key = keyField.Elem().String()
					}
				}

				if valueField.Kind() == reflect.String {
					value = valueField.String()
				} else if valueField.Kind() == reflect.Ptr && valueField.Type().Elem().Kind() == reflect.String {
					if !valueField.IsNil() {
						value = valueField.Elem().String()
					}
				}

				if key != "" {
					resource.Tags[key] = value
				}
			}
		}
	}
}

// structToMap converts a struct to a map for JSON storage
func (r *ReflectionExecutor) structToMap(obj interface{}) map[string]interface{} {
	result := make(map[string]interface{})

	value := reflect.ValueOf(obj)
	if value.Kind() == reflect.Ptr {
		if value.IsNil() {
			return result
		}
		value = value.Elem()
	}

	if value.Kind() != reflect.Struct {
		return result
	}

	typ := value.Type()
	for i := 0; i < value.NumField(); i++ {
		field := value.Field(i)
		fieldType := typ.Field(i)

		// Skip unexported fields
		if !fieldType.IsExported() {
			continue
		}

		fieldName := fieldType.Name

		// Convert field value to interface{}
		var fieldValue interface{}
		if field.CanInterface() {
			fieldValue = field.Interface()
		}

		result[fieldName] = fieldValue
	}

	return result
}

// logAPIAction logs an API action execution to DuckDB if logging is enabled
func (r *ReflectionExecutor) logAPIAction(ctx context.Context, operation AWSOperation, region string, success bool, duration time.Duration, resourceCount int, errorMsg string) {
	if r.dbLogger == nil {
		return // Logging not enabled
	}

	// Determine operation type
	opType := "Unknown"
	switch {
	case operation.IsList:
		opType = "List"
	case operation.IsDescribe:
		opType = "Describe"
	case operation.IsGet:
		opType = "Get"
	}

	record := db.APIActionRecord{
		ID:            uuid.New().String(),
		Service:       operation.Method[:len(operation.Method)-len(operation.Name)], // Extract service from method
		OperationName: operation.Name,
		OperationType: opType,
		ExecutionTime: time.Now().Format(time.RFC3339),
		Region:        region,
		Success:       success,
		DurationMs:    duration.Milliseconds(),
		ResourceCount: resourceCount,
		ErrorMessage:  errorMsg,
		Metadata: map[string]interface{}{
			"input_type":    operation.InputType,
			"output_type":   operation.OutputType,
			"resource_type": operation.ResourceType,
			"paginated":     operation.Paginated,
		},
	}

	// Log asynchronously to avoid blocking the main operation
	go func() {
		if err := r.dbLogger.LogAPIAction(context.Background(), record); err != nil {
			// Log the error but don't fail the operation
			fmt.Printf("Failed to log API action: %v\n", err)
		}
	}()
}
