# Release Process

This document describes how to create a new release for FastReader.

## Automatic Release Process

The project uses GitHub Actions to automatically build and release binaries when a new version tag is pushed.

### Steps to Create a Release

1. **Update version information** (if needed):
   - Update any version references in documentation
   - Update CHANGELOG.md with new features and fixes

2. **Create and push a version tag**:
   ```bash
   # Create a new tag (replace x.y.z with actual version)
   git tag v1.0.0
   
   # Push the tag to trigger the release workflow
   git push origin v1.0.0
   ```

3. **Monitor the release process**:
   - Go to the "Actions" tab in the GitHub repository
   - Watch the "Release" workflow complete
   - The workflow will automatically:
     - Build binaries for all supported platforms
     - Create compressed packages (.tar.gz for Unix, .zip for Windows)
     - Create a GitHub release with all binaries attached
     - Generate release notes

### Supported Platforms

The release workflow builds binaries for:

- **Linux**: amd64, 386, arm64, arm
- **Windows**: amd64, 386
- **macOS**: amd64 (Intel), arm64 (Apple Silicon)
- **FreeBSD**: amd64

### Manual Release (if needed)

If you need to create a release manually:

```bash
# Build all platforms
make all

# Create release packages
make release

# Upload to GitHub releases manually
```

### Troubleshooting

- If the workflow fails, check the Actions logs for details
- Ensure the Makefile targets work correctly locally before pushing tags
- Verify that the repository has the necessary permissions for creating releases

### Version Naming Convention

Use semantic versioning (semver) for tags:
- `v1.0.0` - Major release
- `v1.1.0` - Minor release (new features)
- `v1.1.1` - Patch release (bug fixes)
- `v1.0.0-beta.1` - Pre-release versions
