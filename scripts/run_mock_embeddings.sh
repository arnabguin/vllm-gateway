#!/usr/bin/env bash
# Dev fallback: mock /v1/embeddings on :8001 when vllm-metal pooling is unavailable.
# Use with Metal chat on :8000 and gateway embeddings_url pointing here.

set -euo pipefail

PORT="${MOCK_PORT:-8001}"
if lsof -iTCP:"$PORT" -sTCP:LISTEN -t >/dev/null 2>&1; then
  echo "Port $PORT is already in use" >&2
  exit 1
fi

echo "Starting mock embeddings on http://127.0.0.1:$PORT (model id: mock-model)"
exec env MOCK_BIND=127.0.0.1 MOCK_PORT="$PORT" python3 "$(dirname "$0")/mock_vllm.py"
