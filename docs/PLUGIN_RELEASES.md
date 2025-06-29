# Plugin Release Workflows

This document explains how to build and release Corkscrew cloud provider plugins using GitHub Actions.

## Overview

Each cloud provider plugin has its own dedicated workflow for building and releasing:

- **AWS Provider**: `.github/workflows/build-aws-provider.yml`
- **Azure Provider**: `.github/workflows/build-azure-provider.yml` 
- **GCP Provider**: `.github/workflows/build-gcp-provider.yml`
- **Kubernetes Provider**: `.github/workflows/build-kubernetes-provider.yml`

Additionally, there's a coordinating workflow to build all plugins together:

- **All Plugins**: `.github/workflows/release-all-plugins.yml`

## Individual Plugin Workflows

### Automatic Triggers

Each plugin workflow automatically runs when:

- **Push to main**: Changes to the plugin's directory (e.g., `plugins/aws-provider/**`)
- **Pull Request**: Changes to the plugin's directory or shared dependencies
- **Manual**: Via workflow dispatch in GitHub Actions UI

### Manual Plugin Release

To manually build and release a specific plugin:

1. Go to **Actions** tab in GitHub
2. Select the desired plugin workflow (e.g., "Build AWS Provider")  
3. Click **Run workflow**
4. Fill in the parameters:
   - **Version**: Version tag (e.g., `v1.0.0-aws.1`)
   - **Create Release**: Set to `true` to create a GitHub release

### Build Outputs

Each workflow produces:

- **Multi-platform binaries**: Linux, macOS, Windows (AMD64 + ARM64)
- **Checksums**: SHA256 checksums for all binaries
- **GitHub Release** (if enabled): With installation instructions
- **Build Artifacts**: Available for 30 days

## Coordinated Plugin Release

### Release All Plugins

To build and release all plugins together:

1. Go to **Actions** → **Release All Plugins**
2. Click **Run workflow**
3. Configure parameters:
   - **Main Version**: Base version (e.g., `v1.0.0`)
   - **Version Suffix**: Plugin suffix (e.g., `.1`)
   - **Create Releases**: Set to `true` for actual releases

This creates versioned releases like:
- `v1.0.0-aws.1` (AWS Provider)
- `v1.0.0-azure.1` (Azure Provider) 
- `v1.0.0-gcp.1` (GCP Provider)
- `v1.0.0-k8s.1` (Kubernetes Provider)

### Automatic Trigger

The "Release All Plugins" workflow also triggers automatically when:
- A new tag/release is published on the main repository

## Version Naming Convention

### Individual Plugins
- Format: `v{major}.{minor}.{patch}-{provider}.{build}`
- Examples:
  - `v1.0.0-aws.1` (AWS Provider v1.0.0, build 1)
  - `v1.2.3-azure.2` (Azure Provider v1.2.3, build 2)

### Development Builds
- Non-release builds use commit hash: `v0.0.0-{7-char-hash}`
- Example: `v0.0.0-abc1234`

## Installation

### From GitHub Releases

```bash
# Download and install AWS provider
wget https://github.com/jlgore/corkscrew/releases/download/v1.0.0-aws.1/aws-provider-linux-amd64
chmod +x aws-provider-linux-amd64
mkdir -p ~/.corkscrew/plugins
mv aws-provider-linux-amd64 ~/.corkscrew/plugins/aws-provider

# Verify installation
corkscrew plugin list
```

### Auto-installation (Future)

When plugin auto-download is implemented, users will be able to:

```bash
# Auto-download and install plugins
corkscrew plugin install aws
corkscrew plugin install azure
corkscrew plugin install gcp
corkscrew plugin install kubernetes
```

## Development Workflow

### Testing Plugin Changes

1. **Push changes** to plugin directory
2. **Automatic build** triggers on push to main
3. **Download artifacts** from GitHub Actions
4. **Test locally** before creating releases

### Creating Plugin Releases

1. **Test thoroughly** with automatic builds
2. **Create release** via manual workflow dispatch
3. **Update documentation** if needed
4. **Announce release** to users

## Registry Updates

When plugins are released via "Release All Plugins":

- `plugins/registry.json` is automatically updated
- New release URLs are inserted
- Registry is committed back to repository

## Troubleshooting

### Build Failures

1. **Check logs** in GitHub Actions
2. **Verify dependencies** in `go.mod`
3. **Test locally** with `make build-{provider}-plugin`
4. **Check protobuf generation** with `make generate-proto`

### Missing Binaries

1. **Verify release was created** in GitHub Releases
2. **Check workflow completed** successfully
3. **Confirm version tags** are correct

### Plugin Not Found

1. **Verify plugin directory** exists: `plugins/{provider}-provider/`
2. **Check main.go** exists in plugin directory
3. **Confirm build script** or Makefile target

## Monitoring

### Build Status

Monitor plugin builds via:
- **GitHub Actions** dashboard
- **Commit status checks** on PRs
- **Release notifications**

### Release Health

Track release success via:
- **Download counts** in GitHub Releases
- **User feedback** on installation
- **Integration test results**

---

## Next Steps

1. **Implement plugin auto-download** in CLI
2. **Add plugin update notifications**
3. **Create plugin marketplace/catalog**
4. **Add plugin dependency management**