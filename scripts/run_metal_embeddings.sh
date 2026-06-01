#!/usr/bin/env bash
# Start a pooling (embedding) vLLM Metal server on host :8001.
#
# Chat/completions use run_metal_vllm.sh on :8000. Embeddings need a separate
# process with --runner pooling on :8001. Prefer ./scripts/dev.sh metal (starts both).
# Usage:
#   ./scripts/run_metal_embeddings.sh
#
# Default model is MLX-native (vllm-metal docs). intfloat/e5-small (BERT) fails on Metal.

set -euo pipefail

# shellcheck disable=SC1091
source "$(dirname "$0")/metal_common.sh"

metal_load_env
metal_export_hf_token
metal_require_vllm

MODEL="${VLLM_EMBEDDING_MODEL:-mlx-community/Qwen3-Embedding-0.6B-8bit}"
HOST="${VLLM_EMBEDDING_HOST:-0.0.0.0}"
PORT="${VLLM_EMBEDDING_PORT:-8001}"

metal_validate_embedding_model "$MODEL"
metal_require_free_port "$PORT"
metal_log_hf_auth
metal_unset_launcher_env

echo "Starting vLLM Metal embeddings: model=$MODEL runner=pooling listen=$HOST:$PORT"
exec vllm serve "$MODEL" --runner pooling --host "$HOST" --port "$PORT" --max-model-len 512
