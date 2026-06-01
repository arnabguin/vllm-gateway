#!/usr/bin/env bash
# One entry point for local development.
#
#   ./scripts/dev.sh mock          # mock vLLM + gateway stack + simulator
#   ./scripts/dev.sh metal         # host vLLM Metal + gateway stack + simulator
#   DEV_SIMULATOR=0 ./scripts/dev.sh metal   # skip load sidecar
#   ./scripts/dev.sh stop          # stop Docker + host vLLM processes
#   ./scripts/dev.sh status        # show what's listening
#   ./scripts/dev.sh logs          # all Docker services
#   ./scripts/dev.sh logs gateway  # one service (gateway, clickhouse, simulator, …)
#
# Optional: run a single Metal server in the foreground (advanced):
#   ./scripts/dev.sh chat          # chat/completions on :8000
#   ./scripts/dev.sh embed           # embeddings on :8001

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

DEV_DIR="$ROOT/.dev"
PIDS_FILE="$DEV_DIR/pids"

# shellcheck disable=SC1091
source "$ROOT/scripts/metal_common.sh"

compose() {
  docker compose "$@"
}

load_env() {
  metal_load_env
}

wait_for_port() {
  local port="$1"
  local label="$2"
  local tries="${3:-60}"
  local pid="${4:-}"
  for ((i = 1; i <= tries; i++)); do
    if [[ -n "$pid" ]] && ! kill -0 "$pid" 2>/dev/null; then
      echo "$label process exited before :$port was ready (see $DEV_DIR/*.log)" >&2
      return 1
    fi
    if curl -sf "http://127.0.0.1:${port}/v1/models" >/dev/null 2>&1; then
      echo "$label ready on :$port"
      return 0
    fi
    if ((i % 5 == 0)); then
      echo "  … still waiting for $label on :$port (${i}/${tries})"
    fi
    sleep 2
  done
  echo "Timed out waiting for $label on :$port (see $DEV_DIR/*.log)" >&2
  return 1
}

ensure_dev_dir() {
  mkdir -p "$DEV_DIR"
}

record_pid() {
  ensure_dev_dir
  echo "$1" >>"$PIDS_FILE"
}

# Set by start_metal_background; used by cmd_metal for fail-fast waits.
METAL_LAST_PID=""

start_metal_background() {
  local script="$1"
  local name="$2"
  ensure_dev_dir
  local log="$DEV_DIR/${name}.log"
  : >"$log"
  nohup "$script" >>"$log" 2>&1 &
  METAL_LAST_PID="$!"
  record_pid "$METAL_LAST_PID"
  echo "Started $name (pid $METAL_LAST_PID, log: $log)"
}

stop_host_vllm() {
  if [[ -f "$PIDS_FILE" ]]; then
    while read -r pid; do
      [[ -n "$pid" ]] || continue
      kill "$pid" 2>/dev/null || true
    done <"$PIDS_FILE"
    rm -f "$PIDS_FILE"
  fi
  for port in 8000 8001; do
    local pids
    pids="$(lsof -tiTCP:"$port" -sTCP:LISTEN 2>/dev/null || true)"
    if [[ -n "$pids" ]]; then
      echo "Stopping process on :$port"
      # shellcheck disable=SC2086
      kill $pids 2>/dev/null || true
    fi
  done
}

# DEV_SIMULATOR=0 skips the load sidecar (default: start it).
start_simulator() {
  local mode="$1"
  if [[ "${DEV_SIMULATOR:-1}" == "0" ]]; then
    echo "Simulator: skipped (DEV_SIMULATOR=0)"
    return 0
  fi

  echo "Starting load simulator..."
  local stream_frac="${SIM_STREAM_FRACTION:-0.33}"
  if [[ "$mode" == "metal" ]]; then
    SIM_MODEL="${VLLM_MODEL:-mlx-community/Qwen2.5-0.5B-Instruct-4bit}" \
    SIM_ROTATE_PATHS=1 \
    SIM_INSTRUCT=1 \
    SIM_STREAM_FRACTION="$stream_frac" \
      compose --profile simulator up -d simulator
  else
    SIM_MODEL=mock-model \
    SIM_ROTATE_PATHS=1 \
    SIM_STREAM_FRACTION="$stream_frac" \
      compose --profile mock --profile simulator up -d simulator
  fi
}

print_summary() {
  local mode="$1"
  local simulator_note="running"
  if [[ "${DEV_SIMULATOR:-1}" == "0" ]]; then
    simulator_note="not started (set DEV_SIMULATOR=1 or: docker compose --profile simulator up -d simulator)"
  fi

  cat <<EOF

══════════════════════════════════════════════════════════════
  Stack ready ($mode)
══════════════════════════════════════════════════════════════

  Started:
    • Docker: clickhouse, gateway, prometheus, grafana
EOF
  if [[ "$mode" == "metal" ]]; then
    echo "    • Host:   vLLM Metal chat :8000, embeddings :8001"
  else
    echo "    • Docker: mock-vllm :8000"
  fi
  cat <<EOF
    • Simulator: $simulator_note

  API & health
    Gateway     http://127.0.0.1:8080
    Health      http://127.0.0.1:8080/health

  Dashboards (login admin / admin)
    Grafana     http://127.0.0.1:3000
    Live (Prom) http://127.0.0.1:3000/d/prometheus-gateway
    History (CH) http://127.0.0.1:3000/d/clickhouse-attribution

  Prometheus  http://127.0.0.1:9090

  Watch logs (run in separate terminals)
    All Docker  ./scripts/dev.sh logs
    Gateway     ./scripts/dev.sh logs gateway
    ClickHouse  ./scripts/dev.sh logs clickhouse
    Prometheus  ./scripts/dev.sh logs prometheus
    Grafana     ./scripts/dev.sh logs grafana
    Simulator   ./scripts/dev.sh logs simulator
EOF
  if [[ "$mode" == "mock" ]]; then
    echo "    Mock vLLM   ./scripts/dev.sh logs mock-vllm"
  fi
  if [[ "$mode" == "metal" ]]; then
    cat <<EOF
    Metal chat  tail -f $DEV_DIR/vllm-chat.log
    Metal embed tail -f $DEV_DIR/vllm-embeddings.log
    Metal both  tail -f $DEV_DIR/vllm-*.log
EOF
  fi
  cat <<EOF

  Stop everything: ./scripts/dev.sh stop

EOF
}

cmd_mock() {
  load_env
  echo "Starting mock stack (vLLM in Docker, gateway via env overrides)..."
  VLLM_URL=http://mock-vllm:8000 \
  VLLM_EMBEDDINGS_URL= \
  CLICKHOUSE_ADDR=clickhouse:9000 \
    compose --profile mock up -d --build
  start_simulator mock
  print_summary mock
}

cmd_metal() {
  load_env

  # Both servers share one Metal GPU. Parallel startup with default memory (0.9 each)
  # routinely OOMs; start sequentially with a capped fraction per process.
  local dual_frac="${VLLM_METAL_DUAL_MEMORY_FRACTION:-0.40}"
  export VLLM_METAL_MEMORY_FRACTION="$dual_frac"

  local chat_pid="" embed_pid=""

  if ! lsof -iTCP:8001 -sTCP:LISTEN -t >/dev/null 2>&1; then
    echo "Starting embeddings vLLM (VLLM_METAL_MEMORY_FRACTION=$dual_frac)..."
    start_metal_background "$ROOT/scripts/run_metal_embeddings.sh" "vllm-embeddings"
    embed_pid="$METAL_LAST_PID"
    wait_for_port 8001 "Embeddings vLLM" 90 "$embed_pid"
  else
    echo "Embeddings vLLM already listening on :8001"
  fi

  if ! lsof -iTCP:8000 -sTCP:LISTEN -t >/dev/null 2>&1; then
    echo "Starting chat vLLM (VLLM_METAL_MEMORY_FRACTION=$dual_frac)..."
    start_metal_background "$ROOT/scripts/run_metal_vllm.sh" "vllm-chat"
    chat_pid="$METAL_LAST_PID"
    wait_for_port 8000 "Chat vLLM" 90 "$chat_pid"
  else
    echo "Chat vLLM already listening on :8000"
  fi

  echo "Starting gateway stack (Docker → host vLLM via host.docker.internal)..."
  VLLM_URL=http://host.docker.internal:8000 \
  VLLM_EMBEDDINGS_URL=http://host.docker.internal:8001 \
  CLICKHOUSE_ADDR=clickhouse:9000 \
    compose up -d --build

  start_simulator metal
  print_summary metal
}

cmd_stop() {
  compose --profile mock --profile simulator down --remove-orphans 2>/dev/null || \
    compose --profile simulator down --remove-orphans 2>/dev/null || \
    compose down --remove-orphans 2>/dev/null || true
  stop_host_vllm
  echo "Stopped Docker stack and host vLLM processes."
}

cmd_status() {
  echo "=== Docker ==="
  compose ps 2>/dev/null || echo "(compose not running)"
  echo
  echo "=== Host ports ==="
  for port in 8000 8001 8080; do
    if lsof -iTCP:"$port" -sTCP:LISTEN -t >/dev/null 2>&1; then
      echo ":$port listening"
      lsof -iTCP:"$port" -sTCP:LISTEN | tail -n +2 || true
    else
      echo ":$port free"
    fi
  done
}

cmd_logs() {
  # Include optional profiles so simulator / mock-vllm logs work after dev.sh up.
  compose --profile mock --profile simulator logs -f "${@:2}"
}

usage() {
  sed -n '2,12p' "$0"
  exit 1
}

case "${1:-}" in
  mock) cmd_mock ;;
  metal) cmd_metal ;;
  stop|down) cmd_stop ;;
  status) cmd_status ;;
  logs) cmd_logs "$@" ;;
  chat) exec "$ROOT/scripts/run_metal_vllm.sh" ;;
  embed) exec "$ROOT/scripts/run_metal_embeddings.sh" ;;
  -h|--help|help|"") usage ;;
  *)
    echo "Unknown command: $1" >&2
    usage
    ;;
esac
