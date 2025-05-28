package discovery

// createIAMStrategy creates the hierarchical discovery strategy for IAM
func createIAMStrategy() DiscoveryStrategy {
	return DiscoveryStrategy{
		ServiceName: "iam",
		Description: "IAM hierarchical discovery: global resources first, then user/role-specific resources",
		Phases: []DiscoveryPhase{
			{
				Name:        "global-resources",
				Description: "Discover global IAM resources that don't require parameters",
				Operations: []string{
					"ListUsers",
					"ListRoles",
					"ListGroups",
					"ListPolicies",
					"ListInstanceProfiles",
					"ListOpenIDConnectProviders",
					"ListSAMLProviders",
					"ListServerCertificates",
					"ListVirtualMFADevices",
					"ListAccountAliases",
				},
			},
			{
				Name:        "user-resources",
				Description: "Discover user-specific resources using discovered users",
				DependsOn:   []string{"global-resources"},
				Operations: []string{
					"ListUserTags",
					"ListUserPolicies",
					"ListAttachedUserPolicies",
					"ListGroupsForUser",
				},
				ParamProvider: iamUserParameterProvider,
			},
			{
				Name:        "role-resources",
				Description: "Discover role-specific resources using discovered roles",
				DependsOn:   []string{"global-resources"},
				Operations: []string{
					"ListRoleTags",
					"ListRolePolicies",
					"ListAttachedRolePolicies",
					"ListInstanceProfilesForRole",
				},
				ParamProvider: iamRoleParameterProvider,
			},
			{
				Name:        "group-resources",
				Description: "Discover group-specific resources using discovered groups",
				DependsOn:   []string{"global-resources"},
				Operations: []string{
					"ListGroupPolicies",
					"ListAttachedGroupPolicies",
				},
				ParamProvider: iamGroupParameterProvider,
			},
			{
				Name:        "policy-resources",
				Description: "Discover policy-specific resources using discovered policies",
				DependsOn:   []string{"global-resources"},
				Operations: []string{
					"ListPolicyTags",
					"ListPolicyVersions",
					"ListEntitiesForPolicy",
				},
				ParamProvider: iamPolicyParameterProvider,
			},
		},
	}
}

// iamUserParameterProvider provides parameters for user-specific IAM operations
func iamUserParameterProvider(operationName string, discovered DiscoveryContext) map[string]interface{} {
	users := discovered.GetDiscoveredResourcesByOperation("ListUsers")
	if len(users) == 0 {
		return nil
	}

	switch operationName {
	case "ListUserTags", "ListUserPolicies", "ListAttachedUserPolicies", "ListGroupsForUser":
		// Use the first available user - in production this would iterate through all users
		// Ensure we're using the correct field (Name for UserName parameter)
		if users[0].Name != "" {
			return map[string]interface{}{
				"UserName": users[0].Name,
			}
		}
		// Fallback to ID if Name is empty
		if users[0].ID != "" {
			return map[string]interface{}{
				"UserName": users[0].ID,
			}
		}
	}
	return nil
}

// iamRoleParameterProvider provides parameters for role-specific IAM operations
func iamRoleParameterProvider(operationName string, discovered DiscoveryContext) map[string]interface{} {
	roles := discovered.GetDiscoveredResourcesByOperation("ListRoles")
	if len(roles) == 0 {
		return nil
	}

	switch operationName {
	case "ListRoleTags", "ListRolePolicies", "ListAttachedRolePolicies", "ListInstanceProfilesForRole":
		// Use the correct field (Name for RoleName parameter)
		if roles[0].Name != "" {
			return map[string]interface{}{
				"RoleName": roles[0].Name,
			}
		}
		// Fallback to ID if Name is empty
		if roles[0].ID != "" {
			return map[string]interface{}{
				"RoleName": roles[0].ID,
			}
		}
	}
	return nil
}

// iamGroupParameterProvider provides parameters for group-specific IAM operations
func iamGroupParameterProvider(operationName string, discovered DiscoveryContext) map[string]interface{} {
	groups := discovered.GetDiscoveredResourcesByOperation("ListGroups")
	if len(groups) == 0 {
		return nil
	}

	switch operationName {
	case "ListGroupPolicies", "ListAttachedGroupPolicies":
		// Use the correct field (Name for GroupName parameter)
		if groups[0].Name != "" {
			return map[string]interface{}{
				"GroupName": groups[0].Name,
			}
		}
		// Fallback to ID if Name is empty
		if groups[0].ID != "" {
			return map[string]interface{}{
				"GroupName": groups[0].ID,
			}
		}
	}
	return nil
}

// iamPolicyParameterProvider provides parameters for policy-specific IAM operations
func iamPolicyParameterProvider(operationName string, discovered DiscoveryContext) map[string]interface{} {
	policies := discovered.GetDiscoveredResourcesByOperation("ListPolicies")
	if len(policies) == 0 {
		return nil
	}

	switch operationName {
	case "ListPolicyTags", "ListPolicyVersions", "ListEntitiesForPolicy":
		// Use ARN for policy operations (this is correct)
		if policies[0].ARN != "" {
			return map[string]interface{}{
				"PolicyArn": policies[0].ARN,
			}
		}
	}
	return nil
}

// createS3Strategy creates the hierarchical discovery strategy for S3
func createS3Strategy() DiscoveryStrategy {
	return DiscoveryStrategy{
		ServiceName: "s3",
		Description: "S3 hierarchical discovery: buckets first, then bucket-specific resources with region handling",
		Phases: []DiscoveryPhase{
			{
				Name:        "global-resources",
				Description: "Discover S3 buckets (global but region-aware)",
				Operations: []string{
					"ListBuckets",
				},
			},
			{
				Name:        "bucket-resources",
				Description: "Discover bucket-specific resources using discovered buckets",
				DependsOn:   []string{"global-resources"},
				Operations: []string{
					"ListObjects",
					"ListObjectsV2",
					"ListMultipartUploads",
				},
				ParamProvider: s3BucketParameterProvider,
			},
		},
	}
}

// s3BucketParameterProvider provides parameters for bucket-specific S3 operations
func s3BucketParameterProvider(operationName string, discovered DiscoveryContext) map[string]interface{} {
	buckets := discovered.GetDiscoveredResourcesByOperation("ListBuckets")
	if len(buckets) == 0 {
		return nil
	}

	switch operationName {
	case "ListObjects", "ListObjectsV2", "ListMultipartUploads":
		// Use bucket name and add region-aware parameters
		params := map[string]interface{}{
			"Bucket": buckets[0].Name,
		}

		// Add MaxKeys to limit results and avoid overwhelming responses
		if operationName == "ListObjects" || operationName == "ListObjectsV2" {
			params["MaxKeys"] = 100
		}

		return params
	}
	return nil
}

// createEC2Strategy creates the hierarchical discovery strategy for EC2
func createEC2Strategy() DiscoveryStrategy {
	return DiscoveryStrategy{
		ServiceName: "ec2",
		Description: "EC2 hierarchical discovery: global resources with appropriate filters",
		Phases: []DiscoveryPhase{
			{
				Name:        "global-resources",
				Description: "Discover EC2 resources with appropriate default filters",
				Operations: []string{
					"DescribeInstances",
					"DescribeVolumes",
					"DescribeSnapshots",
					"DescribeImages",
					"DescribeVpcs",
					"DescribeSubnets",
					"DescribeSecurityGroups",
					"DescribeKeyPairs",
				},
				ParamProvider: ec2DefaultParameterProvider,
			},
		},
	}
}

// ec2DefaultParameterProvider provides default parameters for EC2 operations
func ec2DefaultParameterProvider(operationName string, discovered DiscoveryContext) map[string]interface{} {
	switch operationName {
	case "DescribeSnapshots", "DescribeImages":
		// Use "self" owner filter to avoid overwhelming results
		return map[string]interface{}{
			"Owners": []string{"self"},
		}
	case "DescribeInstances", "DescribeVolumes":
		// Set reasonable max results to avoid overwhelming responses
		return map[string]interface{}{
			"MaxResults": 100,
		}
	}
	return nil
}
