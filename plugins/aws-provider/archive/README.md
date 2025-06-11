# AWS Provider Legacy Code Archive

This directory contains code that was removed during the migration to UnifiedScanner-only implementation.

## Migration Date
**Date:** 2025-06-05
**Commit:** [TBD - will be updated after commit]

## What Was Archived

### 1. Hardcoded Services (`hardcoded-services/`)
- **hardcoded_service_lists.go**: All hardcoded AWS service arrays and lists
- **hardcoded_methods.go**: Methods that returned hardcoded service information
- **Purpose**: These contained static lists of AWS services that prevented dynamic discovery

### 2. Feature Flags (`feature-flags/`)
- **migration_feature_flags.go**: Migration control flags and related logic
- **Purpose**: Temporary flags used during the transition period

### 3. Legacy Scanners (`legacy-scanners/`)
- **factory.go**: Original client factory with hardcoded service imports
- **unified_scanner_18_services_test.go**: Test file with hardcoded service configurations
- **Purpose**: Service-specific implementations that were replaced by dynamic scanning

### 4. Old Discovery (`old-discovery/`)
- Will contain deprecated discovery mechanisms that relied on hardcoded service lists

## Files That Were Modified (Not Archived)

The following files had hardcoded content removed but were not archived because they contain essential functionality:

1. **aws_provider.go** - Removed feature flags and hardcoded service methods
2. **discovery/service_discovery.go** - Removed hardcoded popular services list
3. **discovery/service_availability.go** - Removed hardcoded common services
4. **classification/operation_classifier.go** - Removed service-specific patterns
5. **registry/service_registry.go** - Removed fallback service definitions
6. **generator/aws_analyzer.go** - Removed hardcoded known services

## Migration Goals Achieved

✅ **Complete removal of hardcoded service lists**
- No more static arrays of AWS services
- All service discovery is now dynamic

✅ **Elimination of migration feature flags**
- No more conditional logic based on migration state
- UnifiedScanner is now the only scanning mechanism

✅ **Cleanup of legacy fallback mechanisms**
- No more fallback to hardcoded services
- Fail-fast behavior when dynamic discovery fails

## Emergency Restoration

If emergency restoration is needed:

1. **DO NOT** restore feature flags - they were temporary migration aids
2. **Consider carefully** before restoring hardcoded services - defeats the purpose of dynamic discovery
3. **Prefer** fixing the dynamic discovery system instead of reverting to static lists

## Testing After Migration

After migration, verify:
- [ ] All AWS services are discovered dynamically
- [ ] No hardcoded service references remain in active code
- [ ] UnifiedScanner handles all service scanning
- [ ] Tests pass without hardcoded service configurations
- [ ] Performance is acceptable with dynamic discovery

## References

- **Original Migration Plan**: [Link to migration document]
- **UnifiedScanner Documentation**: `pkg/scanner/README.md`
- **Dynamic Discovery Documentation**: `discovery/README.md`