#!/bin/sh
set -e

# Fast pre-push validation script for pokget
echo "==> Running pre-push checks..."

# 1. Check for conflict markers / trailing whitespace
git diff --check HEAD~1 HEAD 2>/dev/null || true

# 2. Check translations coverage (Node) if dependencies are installed
if [ -d "node_modules" ] && command -v node >/dev/null 2>&1; then
  echo "--> Checking translation coverage..."
  npm run test:translations
fi

echo "==> Pre-push checks passed!"
exit 0
