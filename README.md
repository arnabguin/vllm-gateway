# vllm-gateway

A lightweight Go reverse proxy in front of [vLLM](https://github.com/vllm-project/vllm) (or a dev mock) that attributes inference usage and latency to internal teams, persists history in ClickHouse, and exposes live metrics via Prometheus and Grafana.

## What it does

- Proxies OpenAI-style **`POST /v1/completions`**, **`POST /v1/chat/completions`**, and **`POST /v1/embeddings`** (completions/chat support buffered and **`stream: true`** with time-to-first-token)
- Requires **`X-Team-ID`** (optional `X-Project`, `X-User-ID`)
- Records per-request token usage, latency, and **`ttft_ms`** in **`request_events`**
- Flushes per-team latency and TTFT percentiles to **`request_metrics`** every **`metrics.emit_interval`** (default 15s)
- Scrapes vLLM **`/metrics`** into **`vllm_system_metrics`** on the same interval
- Exposes **`GET /v1/metrics`** (Prometheus) and **`GET /health`** (checks vLLM `/v1/models`)

## Features

| Area | Details |
|------|---------|
| Proxy paths | `/v1/completions`, `/v1/chat/completions`, `/v1/embeddings` |
| Streaming | SSE relay, first-content TTFT, usage on final chunks |
| Storage | `request_events`, `request_metrics`, `vllm_system_metrics` |
| Observability | Gateway Prometheus metrics; provisioned Grafana dashboards |
| Dev stack | Docker Compose, mock vLLM, load simulator |
| Tests | `go test ./...`; Docker integration tests with `-tags=integration` |

## Architecture

```text
Clients (curl, apps, load simulator)
        │  X-Team-ID
        ▼
┌───────────────────┐     SSE / JSON      ┌─────────────┐
│  vLLM gateway     │ ──────────────────► │ vLLM / mock │
│  (Go, :8080)      │ ◄────────────────── │  (:8000)    │
└─────────┬─────────┘                     └─────────────┘
          │
          ├──► ClickHouse (request_events, request_metrics, vllm_system_metrics)
          ├──► GET /v1/metrics ──► Prometheus ──► Grafana (live)
          └──► 15s scrape vLLM /metrics
```

| Path | What | Interval |
|------|------|----------|
| Per request | `request_events` (tokens, latency, `ttft_ms` for streams) | Immediate |
| Window rollup | `request_metrics` (latency + TTFT percentiles per team) | 15s (configurable) |
| System | `vllm_system_metrics` (queue, running) | 15s |
| Live | Prometheus histograms and gauges | 5s scrape |

## Quick start

One script, one config file (`config.yaml`). Docker overrides hostnames via env vars — no separate docker configs.

```bash
cp .env.example .env    # optional: HF_TOKEN + model names for Metal
./scripts/dev.sh mock   # mock vLLM, no GPU
# or
./scripts/dev.sh metal  # real vLLM on Apple Silicon + same stack
```

| Service | URL |
|---------|-----|
| Gateway | http://127.0.0.1:8080 |
| Grafana | http://127.0.0.1:3000 (`admin` / `admin`) |
| Prometheus | http://127.0.0.1:9090 |

```bash
curl -s -X POST http://127.0.0.1:8080/v1/completions \
  -H "Content-Type: application/json" \
  -H "X-Team-ID: engineering" \
  -d '{"model":"mock-model","prompt":"hi","max_tokens":10}'

./scripts/dev.sh stop
```

Optional load sidecar: `docker compose --profile simulator up -d simulator`

## Prerequisites

- **Go 1.26+** (see `go.mod`)
- **Docker** (for ClickHouse and optional gateway image)
- **Python 3** (for local mock vLLM on Apple Silicon)

## Project layout

```text
cmd/gateway/          # entrypoint
internal/config/      # YAML config
internal/proxy/       # HTTP proxy + health
internal/scraper/     # Prometheus scrape
internal/storage/     # ClickHouse + noop
monitoring/           # Prometheus + Grafana dashboards
scripts/dev.sh               # mock or Metal: one command starts everything
scripts/mock_vllm.py         # dev mock (no GPU)
scripts/load_simulator.py    # steady-rate traffic for dashboards
config.yaml                  # single config; Docker overrides via env
```

## Local development (macOS / Apple Silicon)

Real vLLM in Docker usually needs NVIDIA CUDA. For gateway development, use the **mock vLLM** server.

### 1. Start ClickHouse

```bash
docker rm -f clickhouse 2>/dev/null
docker run -d --name clickhouse \
  -p 9000:9000 -p 8123:8123 \
  -e CLICKHOUSE_USER=default \
  -e CLICKHOUSE_PASSWORD=devpassword \
  clickhouse/clickhouse-server
```

Match `clickhouse.password` in `config.yaml` (`devpassword` in the sample file).

### 2. Start mock vLLM (terminal 1)

```bash
cd vllm-gateway
python3 scripts/mock_vllm.py
```

Listens on `http://127.0.0.1:8000` with model id `mock-model`.

### 3. Start gateway (terminal 2)

```bash
go run ./cmd/gateway --config=config.yaml
```

Expect logs like:

```text
clickhouse connected: addr=127.0.0.1:9000 database=vllm_attribution
vllm metrics scraper started: interval=15s
request_metrics emit started: interval=15s
listening on :8080
```

### 4. Send a test request (terminal 3)

```bash
curl -s -X POST http://127.0.0.1:8080/v1/completions \
  -H "Content-Type: application/json" \
  -H "X-Team-ID: engineering" \
  -d '{"model":"mock-model","prompt":"hi","max_tokens":10}'
```

Missing team header → **400**. Gateway up, mock down → **503** on `/health`.

Embeddings (non-streaming only; `prompt_tokens` recorded, `completion_tokens = 0`):

```bash
curl -s -X POST http://127.0.0.1:8080/v1/embeddings \
  -H "Content-Type: application/json" \
  -H "X-Team-ID: engineering" \
  -d '{"model":"mock-model","input":"hello"}'
```

### 5. Streaming and TTFT (optional)

Streaming records **`ttft_ms`** on the first content token (role-only chat deltas do not count). Non-streaming requests keep `ttft_ms = 0`. The gateway injects **`stream_options.include_usage`** on streaming completion/chat requests so vLLM returns token counts in the final SSE chunk (without it, `prompt_tokens` / `completion_tokens` stay 0 in ClickHouse).

```bash
curl -N -s -X POST http://127.0.0.1:8080/v1/completions \
  -H "Content-Type: application/json" \
  -H "X-Team-ID: engineering" \
  -H "Accept: text/event-stream" \
  -d '{"model":"mock-model","prompt":"hi","max_tokens":10,"stream":true}'
```

Steady multi-team load with TTFT (for Grafana):

```bash
python3 scripts/load_simulator.py --stream --interval 2 --teams 5
```

### 6. Query ClickHouse

```bash
docker exec clickhouse clickhouse-client \
  --user default --password devpassword -q \
  "SELECT timestamp, team_id, model, prompt_tokens, completion_tokens, latency_ms, ttft_ms
   FROM vllm_attribution.request_events
   ORDER BY timestamp DESC LIMIT 5"

docker exec clickhouse clickhouse-client \
  --user default --password devpassword -q \
  "SELECT timestamp, queue_depth, running_requests
   FROM vllm_attribution.vllm_system_metrics
   ORDER BY timestamp DESC LIMIT 5"
```

Mock metrics: `running_requests=2`, `queue_depth=5`.

## Configuration

One file: **`config.yaml`**. Values target localhost (`127.0.0.1`) for running the gateway binary directly on your machine.

When the gateway runs **inside Docker**, Compose sets env overrides so it reaches the right upstreams:

| Env var | Mock stack | Metal stack |
|---------|------------|-------------|
| `VLLM_URL` | `http://mock-vllm:8000` | `http://host.docker.internal:8000` |
| `VLLM_EMBEDDINGS_URL` | *(empty — uses chat URL)* | `http://host.docker.internal:8001` |
| `CLICKHOUSE_ADDR` | `clickhouse:9000` | `clickhouse:9000` |

`./scripts/dev.sh mock` and `./scripts/dev.sh metal` set these automatically. Integration tests still use `config.integration.yaml` via `docker-compose.integration.yml`.

> **Dev credentials:** Compose and sample configs use `devpassword` (ClickHouse) and Grafana `admin` / `admin`. These are for local development only — change them before any shared or production deployment.

| Key | Purpose |
|-----|---------|
| `gateway.listen_addr` | Gateway bind address (`:8080`) |
| `vllm.url` | vLLM OpenAI base URL (completions/chat) |
| `vllm.embeddings_url` | Separate embedding upstream (optional; falls back to `url`) |
| `vllm.metrics_scrape_interval` | Scrape period (`15s`) |
| `clickhouse.addr` | Native protocol host:port (`9000`) |
| `clickhouse.database` | Database name |
| `clickhouse.username` / `password` | Auth |
| `metrics.emit_interval` | Flush `request_metrics` window (`15s`; `0` disables) |

If ClickHouse is unreachable at startup, the gateway logs a warning and uses **noop storage** (requests still proxy; nothing persisted).

## Docker

`./scripts/dev.sh` is the recommended path. Under the hood:

| Command | What runs |
|---------|-----------|
| `./scripts/dev.sh mock` | clickhouse, gateway, prometheus, grafana, mock-vllm, simulator |
| `./scripts/dev.sh metal` | Host vLLM on :8000/:8001, then same Docker stack + simulator |
| `./scripts/dev.sh stop` | Stops Docker and host vLLM |
| `./scripts/dev.sh status` | Port / compose status |
| `./scripts/dev.sh chat` / `embed` | Foreground Metal server (advanced) |

Optional profile **`simulator`**: started automatically by `./scripts/dev.sh` (disable with `DEV_SIMULATOR=0`).

### Mock stack

```bash
./scripts/dev.sh mock
```

Test:

```bash
curl -s -X POST http://127.0.0.1:8080/v1/completions \
  -H "Content-Type: application/json" \
  -H "X-Team-ID: engineering" \
  -d '{"model":"mock-model","prompt":"hi","max_tokens":10}'
```

### Metal stack (Apple Silicon)

One-time vllm-metal install:

```bash
curl -fsSL https://raw.githubusercontent.com/vllm-project/vllm-metal/main/install.sh | bash
cp .env.example .env   # add HF_TOKEN, adjust models if needed
./scripts/dev.sh metal
```

Gateway reaches host vLLM via `host.docker.internal`. Do **not** run mock and Metal together (both want host `:8000`).

**`/v1/chat/completions`** — pass `messages` (simplest for instruct models):

```bash
curl -s -X POST http://127.0.0.1:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "X-Team-ID: engineering" \
  -d '{
    "model": "mlx-community/Qwen2.5-0.5B-Instruct-4bit",
    "messages": [{"role": "user", "content": "Say hello in one word."}],
    "max_tokens": 10
  }' | python3 -m json.tool
```

**`/v1/completions`** — also works on instruct models; wrap your user text in the model's **chat template** (Qwen2 example below). A bare `"prompt": "hello"` skips the template and produces garbage — the gateway still attributes tokens correctly.

```bash
curl -s -X POST http://127.0.0.1:8080/v1/completions \
  -H "Content-Type: application/json" \
  -H "X-Team-ID: engineering" \
  -d "$(python3 -c "
import json
prompt = '''<|im_start|>user
Say hello in one word.
<|im_start|>assistant
'''
print(json.dumps({
  'model': 'mlx-community/Qwen2.5-0.5B-Instruct-4bit',
  'prompt': prompt,
  'max_tokens': 10,
}))
")" | python3 -m json.tool
```

Load simulator with `/v1/completions` on Metal: add `--instruct` (or `SIM_INSTRUCT=1`) to apply the Qwen2 wrapper automatically.

Embeddings:

```bash
curl -s -X POST http://127.0.0.1:8080/v1/embeddings \
  -H "Content-Type: application/json" \
  -H "X-Team-ID: engineering" \
  -d '{"model":"mlx-community/Qwen3-Embedding-0.6B-8bit","input":"gpu cost"}'
```

Stop: `./scripts/dev.sh stop`

### Load simulator (optional sidecar)

`scripts/load_simulator.py` sends `POST /v1/completions`, `/v1/chat/completions`, or `/v1/embeddings` on a fixed interval. By default it rotates across **teams** (`sim-team-1`, `sim-team-2`, …) on a single path. Use **`--rotate-paths`** to cycle all three routes each request. Use **`--stream-fraction`** / `SIM_STREAM_FRACTION` (0–1) so a random subset of completion/chat requests use `stream: true` (TTFT on Grafana); `--stream` / `SIM_STREAM=1` forces 100%. Embeddings never stream. `./scripts/dev.sh` sets **`SIM_STREAM_FRACTION=0.33`** on the simulator by default.

**Docker** (with the stack already up via `./scripts/dev.sh mock` or `metal`):

```bash
docker compose --profile simulator up -d simulator
docker compose logs -f simulator
```

Defaults: **2s** interval, **5** teams, **`/v1/completions`** (`SIM_INTERVAL` / `SIM_TEAMS` / `SIM_PATH` in `docker-compose.yml`). On **Metal**, use chat completions for instruct models:

```bash
SIM_MODEL=mlx-community/Qwen2.5-0.5B-Instruct-4bit \
SIM_PATH=/v1/chat/completions \
  docker compose --profile simulator up -d simulator
```

Override or run locally:

```bash
python3 scripts/load_simulator.py --interval 2 --teams 5
python3 scripts/load_simulator.py -i 1 -n 3 --once   # one request per team, then exit
python3 scripts/load_simulator.py --path /v1/completions --instruct \
  --model mlx-community/Qwen2.5-0.5B-Instruct-4bit -i 2 -n 5
python3 scripts/load_simulator.py --stream --path /v1/chat/completions -i 2 -n 5
python3 scripts/load_simulator.py --stream-fraction 0.33 --rotate-paths -i 2 -n 5
python3 scripts/load_simulator.py --path /v1/embeddings -i 2 -n 5
python3 scripts/load_simulator.py --rotate-paths --instruct \
  --model mlx-community/Qwen2.5-0.5B-Instruct-4bit -i 2 -n 5
```

| Flag | Meaning |
|------|---------|
| `-i` / `--interval` | Seconds between requests |
| `-n` / `--teams` | Number of teams (`sim-team-1` … `sim-team-N`) |
| `--gateway-url` | Base URL (default `http://127.0.0.1:8080`, or `GATEWAY_URL` in Docker) |
| `--path` | Single path (default `/v1/completions`; ignored with `--rotate-paths`) |
| `--rotate-paths` | Cycle completions → chat → embeddings each request (`SIM_ROTATE_PATHS=1`) |
| `--instruct` | Wrap `/v1/completions` prompts in Qwen2 chat template (`SIM_INSTRUCT=1`) |
| `--stream` | All completion/chat requests stream (`SIM_STREAM=1`; embeddings excluded) |
| `--stream-fraction` | Per-request probability of `stream: true` on completion paths (`SIM_STREAM_FRACTION`, 0–1) |
| `--once` | One request per team, then exit |

Stop simulator only: `docker compose --profile simulator stop simulator`

### Grafana dashboards

With the core stack running (`docker compose up -d --build`, plus `--profile mock` or Metal on the host), open **http://127.0.0.1:3000** (login `admin` / `admin`).

Provisioned datasources:

| Name | Backend |
|------|---------|
| Prometheus | `gateway:8080/v1/metrics` and mock vLLM `/metrics` (via Prometheus) |
| ClickHouse | `vllm_attribution` database |

Dashboards (folder **vLLM Gateway**) — **Prometheus and ClickHouse are separate dashboards**. Each panel has a description (ⓘ) noting the exact source (counter, quantile stream, table) and dump/scrape interval.

| Dashboard | URL | Datasource |
|-----------|-----|------------|
| **Prometheus — Gateway & vLLM (live)** | http://127.0.0.1:3000/d/prometheus-gateway | Prometheus only (scrape **5s**) |
| **ClickHouse — Attribution & history** | http://127.0.0.1:3000/d/clickhouse-attribution | ClickHouse only (`request_events` per request; `request_metrics` + `vllm_system_metrics` every **15s**) |

#### Metrics mental model (what feeds what)

```
Per request   →  request_events          (immediate INSERT)
Every 15s     →  request_metrics         (in-memory window → ClickHouse)
Every 15s     →  vllm_system_metrics     (vLLM /metrics scrape → ClickHouse)
Continuous    →  Prometheus              (/v1/metrics + vLLM scrape every 5s)
```

**Per request → `request_events`** (SQL aggregates at query time in Grafana)

- Grafana: **Requests per team**
- Grafana: **Token usage by team**
- Grafana: **Total tokens in range (by team)**
- Grafana: **Recent request events** (includes `ttft_ms`; > 0 only for streaming)

**Every 15s → `request_metrics`** (`metrics.emit_interval`; `TotalCounter` window + `LatencyRegistry` quantile streams, then reset)

- Grafana: **Latency percentiles (emit windows)**
- Grafana: **TTFT percentiles (emit windows)** — `ttft_p50_ms` / `ttft_p95_ms` / `ttft_p99_ms`
- Grafana: **Requests per emit window**

**Every 15s → `vllm_system_metrics`** (`vllm.metrics_scrape_interval`; not per-team)

- Grafana: **Running vs queue depth**

**Continuous → Prometheus** (scrape **5s**; separate dashboard, not ClickHouse)

- Grafana: **Request rate by team** — `TotalCounter` → cumulative counter `gateway_total_requests`
- Grafana: **In-flight requests by team** — `ActiveCounter` → gauge `gateway_inflight_requests`
- Grafana: **Gateway latency percentiles** — `LatencyRegistry` → histogram `gateway_request_latency` (5m rate quantiles in panel)
- Grafana: **Time to first token (p95)** — TTFT `LatencyRegistry` → histogram `gateway_request_ttft` (empty until streaming)
- Grafana: **vLLM running requests** — `vllm:num_requests_running`
- Grafana: **vLLM queue depth (waiting)** — `vllm:num_requests_waiting`

If you still see old dashboards after an update, restart Grafana: `docker compose restart grafana` (or `docker compose down -v` to reset the Grafana volume).

Send traffic so panels populate (simulator above, or a one-off curl):

```bash
curl -s -X POST http://127.0.0.1:8080/v1/completions \
  -H "Content-Type: application/json" \
  -H "X-Team-ID: engineering" \
  -d '{"model":"mock-model","prompt":"hi","max_tokens":10}'
```

Prometheus UI: http://127.0.0.1:9090

### Gateway image only

Build:

```bash
docker build -t vllm-gateway:local .
```

Run (mock and ClickHouse on the **host**):

```bash
docker run --rm -p 8080:8080 vllm-gateway:local
```

Inside the container, `127.0.0.1` is the container itself. Use a config override for vLLM on the host, e.g.:

```yaml
vllm:
  url: "http://host.docker.internal:8000"
```

ClickHouse on the host: `clickhouse.addr: "host.docker.internal:9000"`.

## Health check

```bash
curl -i http://127.0.0.1:8080/health
```

**200** + `ok` when vLLM (or mock) responds on `/v1/models`.

## CI

GitHub Actions runs `go test ./...` on push/PR to `main` / `master` (see [`.github/workflows/ci.yml`](.github/workflows/ci.yml)). Integration tests (`-tags=integration`) are local/Docker only.

## Tests

**Unit tests** (no Docker):

```bash
go test ./...
```

**Integration tests** (starts ClickHouse, mock vLLM, and gateway via Compose; needs Docker):

```bash
go test -tags=integration -v ./internal/integration/...
```

Single streaming e2e:

```bash
go test -tags=integration -v -run TestGatewayStreamingE2E ./internal/integration/...
```

## Build binary

```bash
go build -o gateway ./cmd/gateway
./gateway --config=config.yaml
```

## License

Licensed under the Apache License, Version 2.0. See `LICENSE`.
