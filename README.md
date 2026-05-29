# vllm-gateway

A lightweight Go reverse proxy in front of [vLLM](https://github.com/vllm-project/vllm) (or a dev mock) that attributes inference usage and latency to internal teams, persists history in ClickHouse, and exposes live metrics via Prometheus and Grafana.

## What it does

- Proxies OpenAI-style **`POST /v1/completions`** and **`POST /v1/chat/completions`** (buffered and **`stream: true`** with time-to-first-token)
- Requires **`X-Team-ID`** (optional `X-Project`, `X-User-ID`)
- Records per-request token usage, latency, and **`ttft_ms`** in **`request_events`**
- Flushes per-team latency and TTFT percentiles to **`request_metrics`** every **`metrics.emit_interval`** (default 15s)
- Scrapes vLLM **`/metrics`** into **`vllm_system_metrics`** on the same interval
- Exposes **`GET /v1/metrics`** (Prometheus) and **`GET /health`** (checks vLLM `/v1/models`)

## Features

| Area | Details |
|------|---------|
| Proxy paths | `/v1/completions`, `/v1/chat/completions` |
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

## Quick start (Docker)

```bash
docker compose up -d --build
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

python3 scripts/load_simulator.py --stream --interval 2 --teams 5
```

Stop: `docker compose down`

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
scripts/mock_vllm.py       # dev mock (no GPU)
scripts/load_simulator.py  # steady-rate traffic for dashboards
config.yaml
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

### 5. Streaming and TTFT (optional)

Streaming records **`ttft_ms`** on the first content token (role-only chat deltas do not count). Non-streaming requests keep `ttft_ms = 0`.

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

Sample configs:

| File | Use |
|------|-----|
| `config.yaml` | Local dev (host ClickHouse + mock vLLM) |
| `config.docker.yaml` | Docker Compose service DNS |
| `config.example.yaml` | Template; copy and customize for other environments |

**Why two configs?** The gateway always reads the same keys; only the hostnames change. On your machine, vLLM and ClickHouse are `127.0.0.1`. Inside a Compose container, `127.0.0.1` is the gateway itself, so you must use service names (`mock-vllm`, `clickhouse`). Compose mounts `config.docker.yaml` over `/config.yaml` in the gateway container.

`config.yaml` (or `--config=path`):

> **Dev credentials:** Compose and sample configs use `devpassword` (ClickHouse) and Grafana `admin` / `admin`. These are for local development only — change them before any shared or production deployment.

| Key | Purpose |
|-----|---------|
| `gateway.listen_addr` | Gateway bind address (`:8080`) |
| `vllm.url` | vLLM OpenAI base URL |
| `vllm.metrics_scrape_interval` | Scrape period (`15s`) |
| `clickhouse.addr` | Native protocol host:port (`9000`) |
| `clickhouse.database` | Database name |
| `clickhouse.username` / `password` | Auth |
| `metrics.emit_interval` | Flush `request_metrics` window (`15s`; `0` disables) |

If ClickHouse is unreachable at startup, the gateway logs a warning and uses **noop storage** (requests still proxy; nothing persisted).

## Docker

### Docker Compose (full stack)

Runs ClickHouse, mock vLLM, and the gateway on one network:

```bash
docker compose up --build
```

Uses `config.docker.yaml` (service DNS: `mock-vllm`, `clickhouse`). Test:

```bash
curl -s -X POST http://127.0.0.1:8080/v1/completions \
  -H "Content-Type: application/json" \
  -H "X-Team-ID: engineering" \
  -d '{"model":"mock-model","prompt":"hi","max_tokens":10}'
```

Stop: `docker compose down`

### Load simulator (optional sidecar)

`scripts/load_simulator.py` sends `POST /v1/completions` (or chat) on a fixed interval, rotating across teams (`sim-team-1`, `sim-team-2`, …). Use `--stream` / `SIM_STREAM=1` for SSE (`stream: true`) to populate TTFT metrics in Grafana.

**Docker** (with the stack already up):

```bash
docker compose up -d --build
docker compose --profile simulator up -d simulator
docker compose logs -f simulator
```

Defaults: **2s** interval, **5** teams (`SIM_INTERVAL` / `SIM_TEAMS` in `docker-compose.yml`). Override or run locally:

```bash
python3 scripts/load_simulator.py --interval 2 --teams 5
python3 scripts/load_simulator.py -i 1 -n 3 --once   # one request per team, then exit
python3 scripts/load_simulator.py --stream --path /v1/chat/completions -i 2 -n 5
```

| Flag | Meaning |
|------|---------|
| `-i` / `--interval` | Seconds between requests |
| `-n` / `--teams` | Number of teams (`sim-team-1` … `sim-team-N`) |
| `--gateway-url` | Base URL (default `http://127.0.0.1:8080`, or `GATEWAY_URL` in Docker) |
| `--path` | `/v1/completions` or `/v1/chat/completions` |
| `--stream` | `stream: true` + drain SSE; logs client-side `ttft=` (or `SIM_STREAM=1`) |
| `--once` | One request per team, then exit |

Stop simulator only: `docker compose --profile simulator stop simulator`

### Grafana dashboards

With the full stack running (`docker compose up --build`), open **http://127.0.0.1:3000** (login `admin` / `admin`).

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
