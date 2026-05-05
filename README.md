# Pokget

## CI/CD Pipeline

The project uses GitHub Actions for CI/CD. The pipeline includes:

- **Linting**: Uses `golangci-lint` to check code quality.
- **Security**: 
  - `govulncheck`: Scans for known vulnerabilities in dependencies.
  - `gosec`: Inspects source code for security problems.
- **Docker**: 
  - Automatically builds and pushes a Docker image to GitHub Container Registry (GHCR) on pushes to the `main` branch.
  - **Manual Trigger**: The Docker build can be manually triggered via the "Actions" tab in GitHub. You can specify a `target_branch` to build the image from.

### Manual Docker Build

To manually trigger a Docker build:
1. Go to the **Actions** tab in your GitHub repository.
2. Select the **CI/CD Pipeline** workflow.
3. Click **Run workflow**.
4. (Optional) Specify the `target_branch`.
5. Click **Run workflow**.

## Deployment Examples

### Local Development (Build from Source)
```bash
docker-compose up -d
```

### GHCR Production (Pull Pre-built Image)
Use this if you want to run the latest version without building locally:
```bash
docker-compose -f docker-compose.ghcr.yml up -d
```
