#!/bin/sh
set -e

# Fast pre-push validation script for pokget
echo "==> Running pre-push checks..."

# 1. Check for conflict markers / trailing whitespace across pushed commits
z40="0000000000000000000000000000000000000000"
has_stdin=0

if [ ! -t 0 ]; then
  while read -r local_ref local_sha remote_ref remote_sha; do
    has_stdin=1
    if [ "$local_sha" = "$z40" ]; then
      continue
    fi
    if [ "$remote_sha" = "$z40" ]; then
      if git rev-parse --verify origin/main >/dev/null 2>&1; then
        range="origin/main..$local_sha"
      elif git rev-parse --verify HEAD~1 >/dev/null 2>&1; then
        range="HEAD~1..$local_sha"
      else
        range="$local_sha"
      fi
    else
      range="$remote_sha..$local_sha"
    fi
    git diff --check "$range"
  done
fi

if [ "$has_stdin" -eq 0 ]; then
  if git rev-parse --verify HEAD~1 >/dev/null 2>&1; then
    git diff --check HEAD~1 HEAD
  fi
fi

# 2. Check translations coverage (Node) if dependencies are installed
if [ -d "node_modules" ] && command -v node >/dev/null 2>&1 && command -v npm >/dev/null 2>&1; then
  echo "--> Checking translation coverage..."
  npm run test:translations
fi

echo "==> Pre-push checks passed!"
exit 0
