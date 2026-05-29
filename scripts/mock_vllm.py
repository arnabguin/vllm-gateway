#!/usr/bin/env python3
"""
Minimal OpenAI-compatible mock for gateway dev on Apple Silicon (no real vLLM).

  python3 scripts/mock_vllm.py
  # listens on http://127.0.0.1:8000

Endpoints:
  GET  /v1/models            — health checks
  POST /v1/completions       — legacy completion + usage
  POST /v1/chat/completions  — chat completion + usage

Streaming (stream: true in JSON body):
  MOCK_STREAM_TTFT_MS         — delay before first content chunk (default 50)
  MOCK_STREAM_CHUNK_DELAY_MS  — delay between subsequent chunks (default 10)
"""

from http.server import HTTPServer, BaseHTTPRequestHandler
from socketserver import ThreadingMixIn
import json
import os
import sys
import time

# 127.0.0.1 for local dev; use MOCK_BIND=0.0.0.0 in Docker Compose.
HOST = os.environ.get("MOCK_BIND", "127.0.0.1")
PORT = int(os.environ.get("MOCK_PORT", "8000"))
MODEL_ID = "mock-model"
MOCK_DELAY_MS = int(os.environ.get("MOCK_DELAY_MS", "0"))
MOCK_THREADED = os.environ.get("MOCK_THREADED", "") == "1"
MOCK_STREAM_TTFT_MS = int(os.environ.get("MOCK_STREAM_TTFT_MS", "50"))
MOCK_STREAM_CHUNK_DELAY_MS = int(os.environ.get("MOCK_STREAM_CHUNK_DELAY_MS", "10"))

# Non-stream token counts (match integration e2e constants).
COMPLETION_PROMPT_TOKENS = 12
COMPLETION_COMPLETION_TOKENS = 7
CHAT_PROMPT_TOKENS = 15
CHAT_COMPLETION_TOKENS = 8


class ThreadingHTTPServer(ThreadingMixIn, HTTPServer):
    daemon_threads = True


class MockVLLMHandler(BaseHTTPRequestHandler):
    def do_GET(self) -> None:
        if self.path == "/v1/models":
            self._json_response(
                200,
                {"object": "list", "data": [{"id": MODEL_ID, "object": "model"}]},
            )
            return
        if self.path == "/health":
            self._json_response(200, {"status": "ok"})
            return
        if self.path == "/metrics":
            self.send_response(200)
            self.send_header("Content-Type", "text/plain; version=0.0.4")
            self.end_headers()
            body = """# HELP vllm:num_requests_running Number of running requests
# TYPE vllm:num_requests_running gauge
vllm:num_requests_running 2
# HELP vllm:num_requests_waiting Number of waiting requests
# TYPE vllm:num_requests_waiting gauge
vllm:num_requests_waiting 5
"""
            self.wfile.write(body.encode())
            return

    def _read_request_body(self) -> bytes:
        length = int(self.headers.get("Content-Length", 0))
        if length:
            return self.rfile.read(length)
        return b""

    def do_POST(self) -> None:
        if MOCK_DELAY_MS > 0:
            time.sleep(MOCK_DELAY_MS / 1000.0)

        body = self._read_request_body()
        stream = False
        if body:
            try:
                stream = bool(json.loads(body).get("stream"))
            except json.JSONDecodeError:
                self.send_error(400, "invalid JSON")
                return

        if self.path == "/v1/completions":
            if stream:
                self._stream_completions()
            else:
                self._json_response(
                    200,
                    {
                        "id": "cmpl-mock",
                        "object": "text_completion",
                        "model": MODEL_ID,
                        "choices": [
                            {
                                "text": "Hello from mock vLLM (Apple Silicon dev).",
                                "index": 0,
                                "finish_reason": "stop",
                            }
                        ],
                        "usage": {
                            "prompt_tokens": COMPLETION_PROMPT_TOKENS,
                            "completion_tokens": COMPLETION_COMPLETION_TOKENS,
                            "total_tokens": COMPLETION_PROMPT_TOKENS
                            + COMPLETION_COMPLETION_TOKENS,
                        },
                    },
                )
            return

        if self.path == "/v1/chat/completions":
            if stream:
                self._stream_chat_completions()
            else:
                self._json_response(
                    200,
                    {
                        "id": "chatcmpl-mock",
                        "object": "chat.completion",
                        "model": MODEL_ID,
                        "choices": [
                            {
                                "index": 0,
                                "message": {
                                    "role": "assistant",
                                    "content": "Hello from mock vLLM chat (Apple Silicon dev).",
                                },
                                "finish_reason": "stop",
                            }
                        ],
                        "usage": {
                            "prompt_tokens": CHAT_PROMPT_TOKENS,
                            "completion_tokens": CHAT_COMPLETION_TOKENS,
                            "total_tokens": CHAT_PROMPT_TOKENS + CHAT_COMPLETION_TOKENS,
                        },
                    },
                )
            return

        self.send_error(404)

    def _sse_write(self, data: str) -> None:
        self.wfile.write(f"data: {data}\n\n".encode())
        self.wfile.flush()

    def _begin_sse(self) -> None:
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.send_header("Cache-Control", "no-cache")
        # Close after stream so http.client / Go http.Client see body EOF (stdlib
        # keep-alive + manual wfile writes do not always emit the final chunk).
        self.send_header("Connection", "close")
        self.end_headers()

    def _stream_completions(self) -> None:
        self._begin_sse()
        if MOCK_STREAM_TTFT_MS > 0:
            time.sleep(MOCK_STREAM_TTFT_MS / 1000.0)
        self._sse_write(json.dumps({"choices": [{"text": "Hel", "index": 0}]}))
        if MOCK_STREAM_CHUNK_DELAY_MS > 0:
            time.sleep(MOCK_STREAM_CHUNK_DELAY_MS / 1000.0)
        self._sse_write(json.dumps({"choices": [{"text": "lo", "index": 0}]}))
        if MOCK_STREAM_CHUNK_DELAY_MS > 0:
            time.sleep(MOCK_STREAM_CHUNK_DELAY_MS / 1000.0)
        self._sse_write(
            json.dumps(
                {
                    "model": MODEL_ID,
                    "usage": {
                        "prompt_tokens": COMPLETION_PROMPT_TOKENS,
                        "completion_tokens": COMPLETION_COMPLETION_TOKENS,
                    },
                }
            )
        )
        self._sse_write("[DONE]")

    def _stream_chat_completions(self) -> None:
        self._begin_sse()
        # Role-only first event (should not trigger TTFT).
        self._sse_write(
            json.dumps({"choices": [{"delta": {"role": "assistant"}, "index": 0}]})
        )
        if MOCK_STREAM_TTFT_MS > 0:
            time.sleep(MOCK_STREAM_TTFT_MS / 1000.0)
        self._sse_write(
            json.dumps({"choices": [{"delta": {"content": "Hi"}, "index": 0}]})
        )
        if MOCK_STREAM_CHUNK_DELAY_MS > 0:
            time.sleep(MOCK_STREAM_CHUNK_DELAY_MS / 1000.0)
        self._sse_write(
            json.dumps({"choices": [{"delta": {"content": " there"}, "index": 0}]})
        )
        if MOCK_STREAM_CHUNK_DELAY_MS > 0:
            time.sleep(MOCK_STREAM_CHUNK_DELAY_MS / 1000.0)
        self._sse_write(
            json.dumps(
                {
                    "model": MODEL_ID,
                    "usage": {
                        "prompt_tokens": CHAT_PROMPT_TOKENS,
                        "completion_tokens": CHAT_COMPLETION_TOKENS,
                    },
                }
            )
        )
        self._sse_write("[DONE]")

    def _json_response(self, status: int, body: dict) -> None:
        data = json.dumps(body).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def log_message(self, fmt: str, *args) -> None:
        print(f"[mock-vllm] {self.address_string()} - {fmt % args}")


def main() -> None:
    handler = MockVLLMHandler
    if MOCK_THREADED:
        server = ThreadingHTTPServer((HOST, PORT), handler)
    else:
        server = HTTPServer((HOST, PORT), handler)
    print(f"Mock vLLM listening on http://{HOST}:{PORT}")
    print(f"Model id: {MODEL_ID}")
    print(f"Stream TTFT delay: {MOCK_STREAM_TTFT_MS}ms chunk delay: {MOCK_STREAM_CHUNK_DELAY_MS}ms")
    print("Press Ctrl+C to stop.")
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        print("\nShutting down.")
        server.server_close()
        sys.exit(0)


if __name__ == "__main__":
    main()
