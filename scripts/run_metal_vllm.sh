#!/usr/bin/env bash
# Start real vLLM on Apple Silicon via vllm-metal (host :8000).
#
# One-time install:
#   curl -fsSL https://raw.githubusercontent.com/vllm-project/vllm-metal/main/install.sh | bash
#
# Hugging Face token (optional but recommended):
#   cp .env.example .env   # add HF_TOKEN=hf_...
#
# Chat / completions (this script, port 8000):
#   ./scripts/run_metal_vllm.sh
#
# Embeddings (separate process, port 8001):
#   ./scripts/run_metal_embeddings.sh
#
# Usage:
#   VLLM_MODEL=mlx-community/Llama-3.2-1B-Instruct-4bit ./scripts/run_metal_vllm.sh

set -euo pipefail

# shellcheck disable=SC1091
source "$(dirname "$0")/metal_common.sh"

metal_load_env
metal_export_hf_token
metal_require_vllm

MODEL="${VLLM_MODEL:-mlx-community/Qwen2.5-0.5B-Instruct-4bit}"
HOST="${VLLM_HOST:-0.0.0.0}"
PORT="${VLLM_PORT:-8000}"

metal_require_free_port "$PORT"
metal_log_hf_auth
metal_unset_launcher_env

MAX_MODEL_LEN="${VLLM_MAX_MODEL_LEN:-8192}"

echo "Starting vLLM Metal: model=$MODEL listen=$HOST:$PORT max_model_len=$MAX_MODEL_LEN"
exec vllm serve "$MODEL" --host "$HOST" --port "$PORT" --max-model-len "$MAX_MODEL_LEN"
