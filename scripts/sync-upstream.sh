#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"

echo "======================================================"
echo "        AI Desk Upstream Submodule Synchronizer       "
echo "======================================================"

cd "$ROOT_DIR"

echo "--> Pulling latest upstream commits for submodules..."
git submodule update --remote --init --recursive

echo "--> Building pristine submodule binaries..."
make build-submodules

echo "--> Running AI Desk regression test suite..."
make test

echo "======================================================"
echo "  ✓ Upstream synchronization completed successfully!  "
echo "======================================================"
