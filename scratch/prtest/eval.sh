#!/usr/bin/env bash
# Usage: eval.sh <pr-number> [worktree-path]
# Checks out the PR head in the prtest worktree and runs build+vet+green-package tests in container.
set -euo pipefail
PR="$1"
WT="${2:-${WT:-/c/DR/Nextcloud/BUILD/pokget-prtest}}"
cd "$WT" || exit 9
git checkout -q "origin/pr/$PR" 2>/dev/null || { echo "CHECKOUT_FAIL pr$PR"; exit 9; }
echo "=== PR $PR @ $(git rev-parse --short HEAD) ==="
MSYS_NO_PATHCONV=1 docker run --rm \
-v "$WT:/app" \
-v pokget-gomod:/go/pkg/mod -v pokget-gocache:/root/.cache/go-build \
-w /app pokget-gotest sh -c '
set -eo pipefail
echo "--- BUILD ---"
build_out=$(go build ./... 2>&1) && build_rc=0 || build_rc=$?
echo "$build_out" | head -25
if [ $build_rc -ne 0 ]; then echo "BUILD_FAIL (exit $build_rc)"; exit $build_rc; fi
echo "BUILD_OK"

echo "--- VET ---"
vet_out=$(go vet ./... 2>&1) && vet_rc=0 || vet_rc=$?
echo "$vet_out" | grep -vE "^#|exit status" | head -25 || true
if [ $vet_rc -ne 0 ]; then echo "VET_FAIL (exit $vet_rc)"; exit $vet_rc; fi
echo "VET_DONE"

echo "--- TEST(green) ---"
test_out=$(go test ./internal/auth/... ./internal/config/... ./internal/errors/... ./internal/middleware/... ./internal/models/... ./internal/worker/... 2>&1) && test_rc=0 || test_rc=$?
echo "$test_out" | grep -E "^(ok|FAIL|---|\?)" | head -40 || true
if [ $test_rc -ne 0 ]; then echo "TEST_FAIL (exit $test_rc)"; exit $test_rc; fi
echo "TEST_DONE"
'
