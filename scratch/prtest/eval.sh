#!/usr/bin/env bash
# Usage: eval.sh <pr-number>
# Checks out the PR head in the prtest worktree and runs build+vet+green-package tests in container.
set -u
PR="$1"
WT="/c/DR/Nextcloud/BUILD/pokget-prtest"
cd "$WT" || exit 9
git checkout -q "origin/pr/$PR" 2>/dev/null || { echo "CHECKOUT_FAIL pr$PR"; exit 9; }
echo "=== PR $PR @ $(git rev-parse --short HEAD) ==="
MSYS_NO_PATHCONV=1 docker run --rm \
  -v "C:/DR/Nextcloud/BUILD/pokget-prtest:/app" \
  -v pokget-gomod:/go/pkg/mod -v pokget-gocache:/root/.cache/go-build \
  -w /app pokget-gotest sh -c '
    echo "--- BUILD ---";  go build ./... 2>&1 | head -25 && echo "BUILD_OK"
    echo "--- VET ---";    go vet ./... 2>&1 | grep -vE "^#|exit status" | head -25; echo "VET_DONE"
    echo "--- TEST(green) ---"; go test ./internal/auth/... ./internal/config/... ./internal/errors/... ./internal/middleware/... ./internal/models/... ./internal/worker/... 2>&1 | grep -E "^(ok|FAIL|---|\?)" | head -40
  '
