# Shared helpers for vllm-metal launch scripts. Source from run_metal_*.sh only.

metal_script_root() {
  local src="${BASH_SOURCE[1]:-${BASH_SOURCE[0]}}"
  cd "$(dirname "$src")/.." && pwd
}

metal_load_env() {
  local root env_file
  root="$(metal_script_root)"
  env_file="${VLLM_ENV_FILE:-$root/.env}"
  if [[ ! -f "$env_file" ]]; then
    return 0
  fi
  set -a
  # shellcheck disable=SC1090
  source "$env_file"
  set +a
}

metal_export_hf_token() {
  if [[ -n "${HF_TOKEN:-}" && -z "${HUGGING_FACE_HUB_TOKEN:-}" ]]; then
    export HUGGING_FACE_HUB_TOKEN="$HF_TOKEN"
  elif [[ -n "${HUGGING_FACE_HUB_TOKEN:-}" && -z "${HF_TOKEN:-}" ]]; then
    export HF_TOKEN="$HUGGING_FACE_HUB_TOKEN"
  fi
}

metal_require_vllm() {
  local venv="${VLLM_METAL_VENV:-$HOME/.venv-vllm-metal}"
  if [[ ! -x "$venv/bin/vllm" ]]; then
    echo "vllm-metal not found at $venv" >&2
    echo "Install: curl -fsSL https://raw.githubusercontent.com/vllm-project/vllm-metal/main/install.sh | bash" >&2
    exit 1
  fi
  # shellcheck disable=SC1091
  source "$venv/bin/activate"
}

metal_log_hf_auth() {
  if [[ -n "${HF_TOKEN:-}" ]]; then
    echo "HF Hub: authenticated (HF_TOKEN set)"
  else
    echo "HF Hub: unauthenticated (set HF_TOKEN in .env or your shell to remove rate-limit warnings)"
  fi
}

metal_require_free_port() {
  local port="$1"
  if lsof -iTCP:"$port" -sTCP:LISTEN -t >/dev/null 2>&1; then
    echo "Port $port is already in use:" >&2
    lsof -iTCP:"$port" -sTCP:LISTEN >&2 || true
    exit 1
  fi
}

metal_unset_launcher_env() {
  # Our .env keys are for these scripts only; vLLM warns if they leak through.
  unset VLLM_MODEL VLLM_EMBEDDING_MODEL VLLM_EMBEDDING_PORT VLLM_EMBEDDING_HOST
  unset VLLM_HOST VLLM_PORT VLLM_METAL_VENV VLLM_ENV_FILE
}

metal_validate_embedding_model() {
  local model="$1"
  if [[ "$model" == intfloat/* || "$model" == *e5-small* || "$model" == BAAI/* ]]; then
    echo "ERROR: $model cannot run on vllm-metal (BERT / sentence-transformers)." >&2
    echo "Update .env:" >&2
    echo "  VLLM_EMBEDDING_MODEL=mlx-community/Qwen3-Embedding-0.6B-8bit" >&2
    exit 1
  fi
}
