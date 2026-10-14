#!/usr/bin/env bash
# Starts the lab and runs the end-to-end check. The same two commands work in
# PowerShell or cmd on Windows.
set -euo pipefail
cd "$(dirname "$0")/.."
docker compose -f lab/compose.yaml up -d --build
docker compose -f lab/compose.yaml run --rm e2e
