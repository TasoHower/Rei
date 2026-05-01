#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"

ADDR="${ADDR:-:8191}"
PORT="${ADDR##*:}"
MCP_PORT="${MCP_PORT:-9089}"

# test-mcp defaults to http://127.0.0.1:9089/mcp; set TEST_SERVER_DISABLE_MCP=1 to skip.
# Override MCP URL via LOOPFORGE_MCP_TEST_URL or TEST_SERVER_MCP_URL.

echo "==> building test-mcp ..."
go build -o ./bin/test-mcp ./cmd/test-mcp

echo "==> building test-server ..."
go build -o ./bin/test-server .

echo "==> starting test-mcp on http://localhost:${MCP_PORT}/mcp ..."
TEST_MCP_ADDR=":${MCP_PORT}" ./bin/test-mcp &
MCP_PID=$!

echo "==> starting test-server on http://localhost:${PORT} ..."
echo "    Skill Runtime demo: place SKILL.md under ./skills (see ./skills/demo), optional LOOPFORGE_SKILL_PATH"

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

# Trap exit to clean up test-mcp
trap "kill $MCP_PID 2>/dev/null || true; wait $MCP_PID 2>/dev/null || true" EXIT INT TERM

exec ./bin/test-server "$@"
