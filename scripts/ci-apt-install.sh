#!/usr/bin/env bash
set -euo pipefail

if (( $# == 0 )); then
  echo 'Usage: ci-apt-install.sh PACKAGE...' >&2
  exit 2
fi

# CI needs Ubuntu packages only. Unrelated runner repositories (for example
# Chrome) must not block Tesseract installation during a repository sync outage.
# Keep the runner's signed Ubuntu sources and all APT integrity checks intact.
ubuntu_sources=/etc/apt/sources.list.d/ubuntu.sources
if [[ ! -s "$ubuntu_sources" ]]; then
  echo "Missing Ubuntu package sources: $ubuntu_sources" >&2
  exit 1
fi
apt_options=(
  -o "Dir::Etc::sourcelist=$ubuntu_sources"
  -o 'Dir::Etc::sourceparts=-'
  -o 'APT::Update::Error-Mode=any'
  -o 'Acquire::Retries=3'
)
sudo apt-get "${apt_options[@]}" update
sudo apt-get "${apt_options[@]}" install -y "$@"
