# Observability Stack E2E Testing Setup

## Quick Start

```bash
# Run unit tests only (no cluster required)
go run mage.go e2e:observability

# Run full E2E tests (requires cluster and Docker Hub authentication)
export DOCKER_HUB_USERNAME=your-username
export DOCKER_HUB_TOKEN=your-token
go run mage.go e2e:observabilityFull
```

## Docker Hub Rate Limiting

The full E2E test suite pulls multiple Docker images from Docker Hub. **Unauthenticated Docker Hub access is limited to 100 pulls per 6 hours per IP address**, which is easily exhausted during testing.

### Solution: Docker Hub Authentication

1. **Create a Docker Hub Personal Access Token**:
   - Go to https://hub.docker.com/settings/security
   - Click "New Access Token"
   - Name it (e.g., "tilt-e2e-tests")
   - Copy the token

2. **Set environment variables**:
   ```bash
   export DOCKER_HUB_USERNAME=your-username
   export DOCKER_HUB_TOKEN=your-pat-token
   ```

3. **Run E2E tests**:
   ```bash
   go run mage.go e2e:observabilityFull
   ```

The test suite will automatically configure the kind cluster with ImagePullSecrets for Docker Hub authentication.

### Alternative: Registry Mirrors

If you have access to a container registry mirror:

```bash
export DOCKER_MIRROR=harbor.example.com/docker-hub
export GHCR_MIRROR=harbor.example.com/ghcr
go run mage.go e2e:observabilityFull
```

## Image Pre-Loading

The E2E suite attempts to pre-load images into the kind cluster to reduce pulls:

```bash
# Images are saved to /tmp/observability-images.tar
# Then loaded into kind with: kind load image-archive
```

This reduces API calls but still requires initial image pulls.

## Test Structure

### Unit Tests (tests/observability/extension_test.go)
- **No build tag** - always run
- Test Starlark validation and configuration
- No cluster required
- Fast (< 1 second)

**Run with:**
```bash
go test ./tests/observability/
```

### Integration Tests (tests/observability/e2e_ginkgo_test.go)
- **Requires build tag: `-tags=e2e`**
- Tests actual stack deployment and functionality
- Requires running cluster with services
- Slower (2-5 minutes)

**Run with:**
```bash
go test -tags=e2e ./tests/observability/
```

## CI/CD Recommendations

For CI/CD pipelines:

1. **Always authenticate with Docker Hub**:
   ```yaml
   env:
     DOCKER_HUB_USERNAME: ${{ secrets.DOCKER_HUB_USERNAME }}
     DOCKER_HUB_TOKEN: ${{ secrets.DOCKER_HUB_TOKEN }}
   ```

2. **Use registry mirrors** if available

3. **Cache images** between runs:
   ```yaml
   - uses: docker/setup-buildx-action@v2
   - uses: actions/cache@v3
     with:
       path: /tmp/docker-cache
       key: ${{ runner.os }}-docker-${{ hashFiles('**/go.mod') }}
   ```

## Troubleshooting

### Rate Limited (429 Too Many Requests)

**Symptom:**
```
Failed to pull image: 429 Too Many Requests - Server message:
toomanyrequests: You have reached your unauthenticated pull rate limit.
```

**Solutions:**
1. Set up Docker Hub authentication (recommended)
2. Wait for rate limit to reset (6 hours)
3. Use a registry mirror
4. Run unit tests only: `go run mage.go e2e:observability`

### Image Pre-Loading Fails

**Symptom:**
```
ERROR: failed to load image: ctr: rpc error: code = NotFound desc = content digest sha256:... not found
```

**Cause:** Multi-platform image format incompatibility with kind's containerd

**Impact:** Non-critical - images will be pulled from Docker Hub instead

**Fix:** Authenticate with Docker Hub to avoid rate limits

## Validation Workflow

To ensure all changes work end-to-end:

1. **Run unit tests 5 times** (fast validation):
   ```bash
   for i in {1..5}; do go test ./tests/observability/ || exit 1; done
   ```

2. **Run full E2E once** (with Docker Hub auth):
   ```bash
   export DOCKER_HUB_USERNAME=...
   export DOCKER_HUB_TOKEN=...
   go run mage.go e2e:observabilityFull
   ```

3. **For comprehensive validation**, run E2E 3-5 times:
   ```bash
   for i in {1..5}; do
     echo "==> E2E run $i/5"
     go run mage.go e2e:observabilityFull || exit 1
   done
   ```
