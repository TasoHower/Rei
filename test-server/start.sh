#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"

ADDR="${ADDR:-:8080}"
PORT="${ADDR##*:}"

echo "==> building test-server ..."
go build -o ./bin/test-server .

echo "==> starting on http://localhost:${PORT}"

# macOS: open browser after server is ready
(
  for i in $(seq 1 30); do
    sleep 0.3
    if curl -s -o /dev/null "http://localhost:${PORT}/" 2>/dev/null; then
      open "http://localhost:${PORT}" 2>/dev/null || true
      break
    fi
  done
) &

exec ./bin/test-server
