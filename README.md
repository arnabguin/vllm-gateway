# vllm-gateway

Go reverse proxy in front of [vLLM](https://github.com/vllm-project/vllm) (or a dev mock) that attributes inference usage and latency to teams, stores history in ClickHouse, and exposes live metrics via Prometheus and Grafana.

**Capabilities:** `POST /v1/completions`, `/v1/chat/completions`, `/v1/embeddings` (buffered + `stream: true` with TTFT) · requires `X-Team-ID` (optional `X-Project`, `X-User-ID`) · `GET /v1/metrics`, `GET /health` · tables `request_events` (per request), `request_metrics` + `vllm_system_metrics` (15s rollups) · scrapes vLLM `/metrics` every 15s.

| Area | Details |
|------|---------|
| Streaming | SSE relay; TTFT on first content token; gateway injects `stream_options.include_usage` so token counts appear in the final SSE chunk |
| Storage | `request_events`, `request_metrics`, `vllm_system_metrics` |
| Dev | `./scripts/dev.sh` mock \| metal, mock vLLM, load simulator |
| Tests | `go test ./...`; `-tags=integration` with Docker |

## Architecture

```text
Clients → gateway (:8080) → vLLM / mock (:8000, embeddings :8001)
              ├─ ClickHouse (events + 15s rollups)
              ├─ GET /v1/metrics → Prometheus (5s) → Grafana
              └─ scrape vLLM /metrics (15s)
```

| Path | Data | Interval |
|------|------|----------|
| Per request | `request_events` (tokens, latency, `ttft_ms` if streamed) | Immediate |
| Rollup | `request_metrics` (latency + TTFT percentiles per team) | `metrics.emit_interval` (default 15s) |
| System | `vllm_system_metrics` (queue, running) | `vllm.metrics_scrape_interval` (15s) |
| Live | Prometheus histograms/gauges | 5s scrape |

## Quick start

One config (`config.yaml`); Docker overrides hostnames via env (no separate docker configs).

```bash
cp .env.example .env          # optional: HF_TOKEN + Metal models
./scripts/dev.sh mock         # mock vLLM, no GPU
# ./scripts/dev.sh metal      # host vLLM on :8000/:8001 (Apple Silicon)
./scripts/dev.sh stop
```

| Service | URL |
|---------|-----|
| Gateway | http://127.0.0.1:8080 |
| Grafana | http://127.0.0.1:3000 (`admin` / `admin`) |
| Prometheus | http://127.0.0.1:9090 |
| Dashboards | [Live (Prom)](http://127.0.0.1:3000/d/prometheus-gateway) · [History (CH)](http://127.0.0.1:3000/d/clickhouse-attribution) |

```bash
curl -s -X POST http://127.0.0.1:8080/v1/completions \
  -H "Content-Type: application/json" -H "X-Team-ID: engineering" \
  -d '{"model":"mock-model","prompt":"hi","max_tokens":10}'
```

**Prerequisites:** Go 1.26+, Docker, Python 3 (mock / simulator).

**Layout:** `cmd/gateway/` · `internal/{config,proxy,scraper,storage}/` · `monitoring/` · `scripts/{dev.sh,mock_vllm.py,load_simulator.py,run_metal_*.sh}` · `config.yaml`

## `dev.sh` commands

| Command | What runs |
|---------|-----------|
| `./scripts/dev.sh mock` | clickhouse, gateway, prometheus, grafana, mock-vllm, simulator (`SIM_STREAM_FRACTION=0.33`) |
| `./scripts/dev.sh metal` | host vLLM :8000/:8001 (sequential start, `VLLM_METAL_MEMORY_FRACTION=0.40`), then same Docker stack + simulator |
| `./scripts/dev.sh stop` | Docker + host vLLM processes |
| `./scripts/dev.sh status` | Ports / compose |
| `./scripts/dev.sh logs [svc]` | Docker logs |
| `./scripts/dev.sh chat` / `embed` | Foreground Metal server (advanced) |

`DEV_SIMULATOR=0` skips the simulator. Do **not** run mock and Metal together (both use host `:8000`).

### Metal (Apple Silicon)

```bash
curl -fsSL https://raw.githubusercontent.com/vllm-project/vllm-metal/main/install.sh | bash
cp .env.example .env && ./scripts/dev.sh metal
```

Gateway uses `host.docker.internal` for host vLLM. Default models: `mlx-community/Qwen2.5-0.5B-Instruct-4bit` (chat), `mlx-community/Qwen3-Embedding-0.6B-8bit` (embeddings). Override via `.env` (`VLLM_MODEL`, `VLLM_EMBEDDING_MODEL`, `VLLM_METAL_DUAL_MEMORY_FRACTION`, `VLLM_MAX_MODEL_LEN`).

**Chat:** prefer `/v1/chat/completions` with `messages`. For `/v1/completions` on instruct models, use the chat template (bare `prompt` works but output is garbage; tokens still attributed). Simulator: `SIM_INSTRUCT=1` or `--instruct` wraps Qwen2 for completions.

```bash
# chat
curl -s -X POST http://127.0.0.1:8080/v1/chat/completions \
  -H "Content-Type: application/json" -H "X-Team-ID: engineering" \
  -d '{"model":"mlx-community/Qwen2.5-0.5B-Instruct-4bit","messages":[{"role":"user","content":"hi"}],"max_tokens":10}'

# embeddings
curl -s -X POST http://127.0.0.1:8080/v1/embeddings \
  -H "Content-Type: application/json" -H "X-Team-ID: engineering" \
  -d '{"model":"mlx-community/Qwen3-Embedding-0.6B-8bit","input":"gpu cost"}'
```

## Configuration

**`config.yaml`** — localhost defaults for `go run` on the host. Compose env overrides when the gateway runs in Docker:

| Env var | Mock | Metal |
|---------|------|-------|
| `VLLM_URL` | `http://mock-vllm:8000` | `http://host.docker.internal:8000` |
| `VLLM_EMBEDDINGS_URL` | *(empty → uses chat URL)* | `http://host.docker.internal:8001` |
| `CLICKHOUSE_ADDR` | `clickhouse:9000` | `clickhouse:9000` |

Also overridable: `GATEWAY_LISTEN_ADDR`, `VLLM_METRICS_SCRAPE_INTERVAL`, `CLICKHOUSE_DATABASE` / `USERNAME` / `PASSWORD`, `METRICS_EMIT_INTERVAL`.

| YAML key | Purpose |
|----------|---------|
| `gateway.listen_addr` | Bind (`:8080`) |
| `vllm.url` / `vllm.embeddings_url` | Completions/chat vs embeddings upstream |
| `vllm.metrics_scrape_interval` | vLLM scrape (`15s`) |
| `clickhouse.*` | Native protocol + auth |
| `metrics.emit_interval` | `request_metrics` flush (`15s`; `0` = off) |

ClickHouse down at startup → **noop storage** (proxy works; nothing persisted). Integration tests use `config.integration.yaml` + `docker-compose.integration.yml`.

> **Dev credentials:** `devpassword` (ClickHouse), Grafana `admin`/`admin` — change before any shared deployment.

## Streaming, TTFT, and tokens

- **`ttft_ms` > 0** only when `stream: true` (first content token; role-only chat deltas excluded).
- Non-streaming: `ttft_ms = 0`.
- Gateway sets **`stream_options.include_usage`** on streamed completion/chat requests so vLLM emits usage in the final SSE chunk (otherwise ClickHouse shows `prompt_tokens` / `completion_tokens` = 0).
- Embeddings: non-streaming only; `completion_tokens = 0`.

```bash
curl -N -s -X POST http://127.0.0.1:8080/v1/completions \
  -H "Content-Type: application/json" -H "X-Team-ID: engineering" \
  -H "Accept: text/event-stream" \
  -d '{"model":"mock-model","prompt":"hi","max_tokens":10,"stream":true}'
```

## Load simulator

`scripts/load_simulator.py` — steady `POST` traffic with rotating `sim-team-*` headers. Started by `dev.sh` unless `DEV_SIMULATOR=0`.

| Flag / env | Meaning |
|------------|---------|
| `-i` / `SIM_INTERVAL` | Seconds between requests (default 2) |
| `-n` / `SIM_TEAMS` | Team count (default 5) |
| `--path` / `SIM_PATH` | Single path (default `/v1/completions`) |
| `--rotate-paths` / `SIM_ROTATE_PATHS=1` | Cycle completions → chat → embeddings |
| `--instruct` / `SIM_INSTRUCT=1` | Qwen2 template for `/v1/completions` |
| `--stream` / `SIM_STREAM=1` | 100% streaming (completions/chat) |
| `--stream-fraction` / `SIM_STREAM_FRACTION` | Per-request stream probability (0–1); `dev.sh` defaults **0.33** |
| `--once` | One request per team, exit |
| `SIM_MODEL`, `SIM_EMBEDDING_MODEL`, `GATEWAY_URL` | Model and gateway base |

```bash
docker compose --profile simulator up -d simulator
docker compose logs -f simulator
python3 scripts/load_simulator.py --stream-fraction 0.33 --rotate-paths --instruct -i 2 -n 5
```

## Grafana

Login http://127.0.0.1:3000 (`admin` / `admin`). Datasources: **Prometheus** (gateway + vLLM scrape), **ClickHouse** (`vllm_attribution`). Panel tooltips (ⓘ) document sources and intervals.

**Data flow**

```text
Per request  → request_events           (immediate)
Every 15s    → request_metrics          (in-memory window → CH)
Every 15s    → vllm_system_metrics      (vLLM /metrics scrape)
Continuous   → Prometheus               (5s scrape)
```

| Panel (CH dashboard) | Source |
|----------------------|--------|
| Requests / tokens / recent events | `request_events` |
| Latency / TTFT percentiles / requests per window | `request_metrics` |
| Running vs queue | `vllm_system_metrics` |

| Panel (Prom dashboard) | Source |
|------------------------|--------|
| Request rate, in-flight | `gateway_total_requests`, `gateway_inflight_requests` |
| Gateway latency p50/p95/p99 | `gateway_request_latency` |
| TTFT p95 | `gateway_request_ttft` (needs streaming traffic) |
| vLLM running / queue | `vllm:num_requests_running`, `vllm:num_requests_waiting` |

Stale dashboards: `docker compose restart grafana` (or `down -v` to reset volume). Mock vLLM reports fixed `running=2`, `queue=5`; real vLLM is often 0 at scrape time under light load.

## Manual setup (without `dev.sh`)

For debugging components separately on macOS (real vLLM in Docker usually needs NVIDIA; use mock locally):

```bash
# ClickHouse
docker run -d --name clickhouse -p 9000:9000 -p 8123:8123 \
  -e CLICKHOUSE_USER=default -e CLICKHOUSE_PASSWORD=devpassword \
  clickhouse/clickhouse-server

python3 scripts/mock_vllm.py          # :8000, mock-model
go run ./cmd/gateway --config=config.yaml   # :8080
```

Sample CH query:

```bash
docker exec clickhouse clickhouse-client --user default --password devpassword -q \
  "SELECT timestamp, team_id, prompt_tokens, completion_tokens, latency_ms, ttft_ms
   FROM vllm_attribution.request_events ORDER BY timestamp DESC LIMIT 5"
```

Missing `X-Team-ID` → **400**. Gateway up, vLLM down → **503** on `/health`.

## Health, tests, build, CI

```bash
curl -i http://127.0.0.1:8080/health    # 200 + ok when vLLM /v1/models responds

go test ./...
go test -tags=integration -v ./internal/integration/...
go test -tags=integration -v -run TestGatewayStreamingE2E ./internal/integration/...

go build -o gateway ./cmd/gateway && ./gateway --config=config.yaml
docker build -t vllm-gateway:local .
```

CI: `go test ./...` on push/PR ([`.github/workflows/ci.yml`](.github/workflows/ci.yml)). Integration tests are local/Docker only.

Container-only gateway on host vLLM/CH: set `vllm.url` and `clickhouse.addr` to `host.docker.internal:…` in config.

## License

Apache License 2.0 — see `LICENSE`.
