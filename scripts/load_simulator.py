#!/usr/bin/env python3
"""
Steady-rate gateway load simulator for dashboard / e2e dev.

  python3 scripts/load_simulator.py --interval 2 --teams 5
  python3 scripts/load_simulator.py --gateway-url http://127.0.0.1:8080 -i 1 -n 3
  python3 scripts/load_simulator.py --stream --path /v1/chat/completions

In Docker Compose (profile simulator):
  docker compose --profile simulator up -d simulator
  SIM_STREAM=1 docker compose --profile simulator up -d simulator
"""

from __future__ import annotations

import argparse
import json
import os
import sys
import time
import urllib.error
import urllib.request

DEFAULT_GATEWAY = os.environ.get("GATEWAY_URL", "http://127.0.0.1:8080")
DEFAULT_MODEL = os.environ.get("SIM_MODEL", "mock-model")
DEFAULT_PREFIX = os.environ.get("SIM_TEAM_PREFIX", "sim-team")


def env_flag(name: str) -> bool:
    return os.environ.get(name, "").strip().lower() in ("1", "true", "yes", "on")


def parse_args() -> argparse.Namespace:
    p = argparse.ArgumentParser(
        description="Send completion requests to the vLLM gateway on a fixed interval, rotating across teams.",
    )
    p.add_argument(
        "-i",
        "--interval",
        type=float,
        default=float(os.environ.get("SIM_INTERVAL", "2")),
        help="Seconds between requests (default: 2, or SIM_INTERVAL env)",
    )
    p.add_argument(
        "-n",
        "--teams",
        type=int,
        default=int(os.environ.get("SIM_TEAMS", "3")),
        metavar="N",
        help="Number of teams (sim-team-1 .. sim-team-N) (default: 3, or SIM_TEAMS env)",
    )
    p.add_argument(
        "--gateway-url",
        default=DEFAULT_GATEWAY,
        help=f"Gateway base URL (default: {DEFAULT_GATEWAY!r} or GATEWAY_URL env)",
    )
    p.add_argument(
        "--team-prefix",
        default=DEFAULT_PREFIX,
        help=f"Team id prefix (default: {DEFAULT_PREFIX!r})",
    )
    p.add_argument(
        "--model",
        default=DEFAULT_MODEL,
        help=f"Model field in JSON body (default: {DEFAULT_MODEL!r})",
    )
    p.add_argument(
        "--path",
        default="/v1/completions",
        choices=("/v1/completions", "/v1/chat/completions"),
        help="Upstream path to hit (default: /v1/completions)",
    )
    p.add_argument(
        "--stream",
        action="store_true",
        default=env_flag("SIM_STREAM"),
        help="Set stream=true and drain SSE (default: off, or SIM_STREAM=1)",
    )
    p.add_argument(
        "--warmup-timeout",
        type=float,
        default=120.0,
        help="Seconds to wait for gateway /health before starting (default: 120)",
    )
    p.add_argument(
        "--once",
        action="store_true",
        help="Send one request per team and exit",
    )
    return p.parse_args()


def team_ids(prefix: str, count: int) -> list[str]:
    if count < 1:
        raise SystemExit("--teams must be >= 1")
    return [f"{prefix}-{i}" for i in range(1, count + 1)]


def wait_for_gateway(base: str, timeout: float) -> None:
    url = base.rstrip("/") + "/health"
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        try:
            req = urllib.request.Request(url, method="GET")
            with urllib.request.urlopen(req, timeout=5) as resp:
                if 200 <= resp.status < 300:
                    print(f"[simulator] gateway ready: {url}")
                    return
        except (urllib.error.URLError, TimeoutError, OSError) as e:
            print(f"[simulator] waiting for gateway ({e!r})...", file=sys.stderr)
        time.sleep(2)
    raise SystemExit(f"gateway not healthy after {timeout}s: {url}")


def request_body(path: str, team_id: str, model: str, stream: bool) -> dict:
    if path == "/v1/chat/completions":
        body = {
            "model": model,
            "messages": [{"role": "user", "content": f"ping from {team_id}"}],
            "max_tokens": 10,
        }
    else:
        body = {
            "model": model,
            "prompt": f"ping from {team_id}",
            "max_tokens": 10,
        }
    if stream:
        body["stream"] = True
    return body


def chunk_has_first_content(path: str, chunk: bytes) -> bool:
    if path == "/v1/chat/completions":
        return b'"content":' in chunk and b'"content": ""' not in chunk
    return b'"text":' in chunk


def consume_sse_stream(resp, path: str) -> tuple[float | None, float]:
    """Drain SSE body; return (client-side ttft seconds or None, total seconds)."""
    start = time.perf_counter()
    ttft: float | None = None
    while True:
        chunk = resp.read(4096)
        if not chunk:
            break
        if ttft is None and chunk_has_first_content(path, chunk):
            ttft = time.perf_counter() - start
    return ttft, time.perf_counter() - start


def send_request(
    base: str, path: str, team_id: str, model: str, stream: bool
) -> tuple[int, float, float | None]:
    """Returns (status, total_elapsed_seconds, ttft_seconds or None)."""
    url = base.rstrip("/") + path
    body = request_body(path, team_id, model, stream)
    data = json.dumps(body).encode()
    headers = {
        "Content-Type": "application/json",
        "X-Team-ID": team_id,
    }
    if stream:
        headers["Accept"] = "text/event-stream"

    req = urllib.request.Request(url, data=data, method="POST", headers=headers)
    start = time.perf_counter()
    with urllib.request.urlopen(req, timeout=60) as resp:
        if stream:
            ttft, elapsed = consume_sse_stream(resp, path)
            return resp.status, elapsed, ttft
        return resp.status, time.perf_counter() - start, None


def main() -> None:
    args = parse_args()
    if args.interval <= 0:
        raise SystemExit("--interval must be > 0")

    teams = team_ids(args.team_prefix, args.teams)
    wait_for_gateway(args.gateway_url, args.warmup_timeout)

    print(
        f"[simulator] gateway={args.gateway_url} path={args.path} stream={args.stream} "
        f"interval={args.interval}s teams={teams}"
    )

    index = 0
    try:
        while True:
            team = teams[index % len(teams)]
            index += 1
            try:
                status, elapsed, ttft = send_request(
                    args.gateway_url, args.path, team, args.model, args.stream
                )
                line = (
                    f"[simulator] team={team} status={status} "
                    f"latency={elapsed * 1000:.0f}ms"
                )
                if ttft is not None:
                    line += f" ttft={ttft * 1000:.0f}ms"
                print(line)
            except urllib.error.HTTPError as e:
                print(f"[simulator] team={team} HTTP {e.code}: {e.reason}", file=sys.stderr)
            except (urllib.error.URLError, TimeoutError, OSError) as e:
                print(f"[simulator] team={team} error: {e}", file=sys.stderr)

            if args.once and index >= len(teams):
                print("[simulator] --once done")
                return

            time.sleep(args.interval)
    except KeyboardInterrupt:
        print("\n[simulator] stopped")


if __name__ == "__main__":
    main()
