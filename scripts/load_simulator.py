#!/usr/bin/env python3
"""
Steady-rate gateway load simulator for dashboard / e2e dev.

  python3 scripts/load_simulator.py --interval 2 --teams 5
  python3 scripts/load_simulator.py --gateway-url http://127.0.0.1:8080 -i 1 -n 3
  python3 scripts/load_simulator.py --stream --path /v1/chat/completions
  python3 scripts/load_simulator.py --stream-fraction 0.33 --rotate-paths
  python3 scripts/load_simulator.py --path /v1/embeddings -i 2 -n 5
  python3 scripts/load_simulator.py --rotate-paths --instruct -i 2 -n 5

In Docker Compose (profile simulator):
  docker compose --profile simulator up -d simulator
  SIM_STREAM=1 docker compose --profile simulator up -d simulator
  SIM_STREAM_FRACTION=0.33 docker compose --profile simulator up -d simulator
  SIM_ROTATE_PATHS=1 docker compose --profile simulator up -d simulator
"""

from __future__ import annotations

import argparse
import json
import os
import random
import sys
import time
import urllib.error
import urllib.request

DEFAULT_GATEWAY = os.environ.get("GATEWAY_URL", "http://127.0.0.1:8080")
DEFAULT_MODEL = os.environ.get("SIM_MODEL", "mock-model")
DEFAULT_EMBEDDING_MODEL = os.environ.get(
    "SIM_EMBEDDING_MODEL", "mlx-community/Qwen3-Embedding-0.6B-8bit"
)
DEFAULT_PREFIX = os.environ.get("SIM_TEAM_PREFIX", "sim-team")
ROTATE_PATHS = ("/v1/completions", "/v1/chat/completions", "/v1/embeddings")


def env_flag(name: str) -> bool:
    return os.environ.get(name, "").strip().lower() in ("1", "true", "yes", "on")


def clamp_stream_fraction(value: float) -> float:
    if not 0.0 <= value <= 1.0:
        raise SystemExit("stream fraction must be between 0 and 1")
    return value


def resolve_stream_fraction(stream_flag: bool, cli_fraction: float | None) -> float:
    """Fraction of completion-path requests that use stream=true (embeddings never stream)."""
    if stream_flag:
        return 1.0
    if cli_fraction is not None:
        return clamp_stream_fraction(cli_fraction)
    raw = os.environ.get("SIM_STREAM_FRACTION", "").strip()
    if raw:
        return clamp_stream_fraction(float(raw))
    return 0.0


def pick_stream(path: str, force_stream: bool, fraction: float) -> bool:
    if path == "/v1/embeddings":
        return False
    if force_stream:
        return True
    if fraction <= 0.0:
        return False
    if fraction >= 1.0:
        return True
    return random.random() < fraction


def parse_args() -> argparse.Namespace:
    p = argparse.ArgumentParser(
        description="Send inference requests to the vLLM gateway on a fixed interval, rotating across teams.",
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
        default=os.environ.get("SIM_PATH", "/v1/completions"),
        choices=("/v1/completions", "/v1/chat/completions", "/v1/embeddings"),
        help="Upstream path to hit (default: /v1/completions, or SIM_PATH env; ignored with --rotate-paths)",
    )
    p.add_argument(
        "--rotate-paths",
        action="store_true",
        default=env_flag("SIM_ROTATE_PATHS"),
        help="Round-robin /v1/completions → /v1/chat/completions → /v1/embeddings each request",
    )
    p.add_argument(
        "--stream",
        action="store_true",
        default=env_flag("SIM_STREAM"),
        help="Every completion-path request streams (same as --stream-fraction 1; SIM_STREAM=1)",
    )
    stream_frac_default: float | None = None
    if os.environ.get("SIM_STREAM_FRACTION", "").strip():
        stream_frac_default = float(os.environ["SIM_STREAM_FRACTION"])
    p.add_argument(
        "--stream-fraction",
        type=float,
        default=stream_frac_default,
        metavar="F",
        help="Probability each completions/chat request uses stream=true (0–1; SIM_STREAM_FRACTION)",
    )
    p.add_argument(
        "--instruct",
        action="store_true",
        default=env_flag("SIM_INSTRUCT"),
        help="Wrap /v1/completions prompts in Qwen2 chat template (for *-Instruct models)",
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


def instruct_completion_prompt(user_text: str) -> str:
    """Wrap plain user text for Qwen2.x instruct models on /v1/completions."""
    return (
        "<|im_start|>user\n"
        f"{user_text}\n"
        "\n"
        "<|im_start|>assistant\n"
    )


def model_for_path(path: str, model: str) -> str:
    if path == "/v1/embeddings":
        return DEFAULT_EMBEDDING_MODEL
    return model


def request_body(path: str, team_id: str, model: str, stream: bool, instruct: bool) -> dict:
    if path == "/v1/chat/completions":
        body = {
            "model": model,
            "messages": [{"role": "user", "content": f"ping from {team_id}"}],
            "max_tokens": 10,
        }
    elif path == "/v1/completions":
        user_text = f"ping from {team_id}"
        prompt = instruct_completion_prompt(user_text) if instruct else user_text
        body = {
            "model": model,
            "prompt": prompt,
            "max_tokens": 10,
        }
    elif path == "/v1/embeddings":
        body = {
            "model": model_for_path(path, model),
            "input": f"hello from {team_id}",
        }
    else:
        raise ValueError(f"unsupported path: {path}")
    if stream:
        body["stream"] = True
        body["stream_options"] = {"include_usage": True}
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


def use_stream(path: str, stream: bool) -> bool:
    return stream and path != "/v1/embeddings"


def send_request(
    base: str, path: str, team_id: str, model: str, stream: bool, instruct: bool
) -> tuple[int, float, float | None]:
    """Returns (status, total_elapsed_seconds, ttft_seconds or None)."""
    url = base.rstrip("/") + path
    streaming = use_stream(path, stream)
    body = request_body(path, team_id, model, streaming, instruct)
    data = json.dumps(body).encode()
    headers = {
        "Content-Type": "application/json",
        "X-Team-ID": team_id,
    }
    if streaming:
        headers["Accept"] = "text/event-stream"

    req = urllib.request.Request(url, data=data, method="POST", headers=headers)
    start = time.perf_counter()
    with urllib.request.urlopen(req, timeout=60) as resp:
        if streaming:
            ttft, elapsed = consume_sse_stream(resp, path)
            return resp.status, elapsed, ttft
        return resp.status, time.perf_counter() - start, None


def path_for_request(rotate_paths: bool, fixed_path: str, index: int) -> str:
    if rotate_paths:
        return ROTATE_PATHS[index % len(ROTATE_PATHS)]
    return fixed_path


def main() -> None:
    args = parse_args()
    if args.interval <= 0:
        raise SystemExit("--interval must be > 0")

    teams = team_ids(args.team_prefix, args.teams)
    wait_for_gateway(args.gateway_url, args.warmup_timeout)

    if args.instruct and args.path != "/v1/completions" and not args.rotate_paths:
        print(
            "[simulator] warning: --instruct only applies to /v1/completions",
            file=sys.stderr,
        )

    stream_fraction = resolve_stream_fraction(args.stream, args.stream_fraction)

    paths_label = " → ".join(ROTATE_PATHS) if args.rotate_paths else args.path
    print(
        f"[simulator] gateway={args.gateway_url} paths={paths_label} "
        f"stream_fraction={stream_fraction} instruct={args.instruct} "
        f"interval={args.interval}s teams={teams}"
    )

    index = 0
    try:
        while True:
            team = teams[index % len(teams)]
            path = path_for_request(args.rotate_paths, args.path, index)
            streaming = pick_stream(path, args.stream, stream_fraction)
            index += 1
            try:
                status, elapsed, ttft = send_request(
                    args.gateway_url,
                    path,
                    team,
                    args.model,
                    streaming,
                    args.instruct,
                )
                line = (
                    f"[simulator] team={team} path={path} stream={streaming} "
                    f"status={status} latency={elapsed * 1000:.0f}ms"
                )
                if ttft is not None:
                    line += f" ttft={ttft * 1000:.0f}ms"
                print(line)
            except urllib.error.HTTPError as e:
                print(
                    f"[simulator] team={team} path={path} HTTP {e.code}: {e.reason}",
                    file=sys.stderr,
                )
            except (urllib.error.URLError, TimeoutError, OSError) as e:
                print(
                    f"[simulator] team={team} path={path} error: {e}",
                    file=sys.stderr,
                )

            if args.once and index >= len(teams):
                print("[simulator] --once done")
                return

            time.sleep(args.interval)
    except KeyboardInterrupt:
        print("\n[simulator] stopped")


if __name__ == "__main__":
    main()
