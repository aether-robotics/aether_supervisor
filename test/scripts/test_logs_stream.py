#!/usr/bin/env python3
"""Script to exercise the /v1/logs/stream SSE endpoint.

Each test connects to the stream, prints every event received, and then
disconnects (either when the server sends a 'done' event or when max_events
is reached).

Usage:
  python3 test_logs_stream.py

Requirements:
  - aether_supervisor running on 127.0.0.1:8080
  - Token matches TOKEN below (default: robotics)
  - An app matching APP_NAME is running (default: demo-app)
  - Docker daemon reachable
"""
from __future__ import annotations

import json
import urllib.error
import urllib.parse
import urllib.request
from collections.abc import Iterator

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------

BASE_URL = "http://127.0.0.1:8080"
TOKEN = "robotics"

APP_NAME = "demo-app"
SERVICE_NAME = ""        # leave empty to stream all services
STREAM_PATH = "/v1/logs/stream"

# ---------------------------------------------------------------------------
# HTTP / SSE helpers
# ---------------------------------------------------------------------------


def _auth_headers() -> dict:
    return {
        "Authorization": f"Bearer {TOKEN}",
        "Content-Type": "application/json",
        "Accept": "application/json",
    }


def join_url(path: str) -> str:
    return urllib.parse.urljoin(BASE_URL.rstrip("/") + "/", path.lstrip("/"))


def sse_stream(
    path: str,
    params: dict,
    *,
    max_events: int | None = None,
    timeout: int | None = 30,
) -> Iterator[tuple[str, str]]:
    """Yield ``(event_type, data)`` pairs from an SSE endpoint.

    Stops after *max_events* events if given, or when the server closes the
    connection.  Raises ``urllib.error.HTTPError`` for non-200 responses.
    """
    url = join_url(path)
    if params:
        url += "?" + urllib.parse.urlencode(
            {k: v for k, v in params.items() if v is not None}
        )

    req = urllib.request.Request(
        url,
        headers={**_auth_headers(), "Accept": "text/event-stream"},
        method="GET",
    )

    with urllib.request.urlopen(req, timeout=timeout) as resp:
        event_type: str | None = None
        data_parts: list[str] = []
        count = 0

        for raw in resp:
            line = raw.decode("utf-8", errors="replace").rstrip("\r\n")

            if line.startswith("event:"):
                event_type = line[6:].strip()
            elif line.startswith("data:"):
                data_parts.append(line[5:].strip())
            elif line == "":
                if data_parts:
                    yield (event_type or "message", "\n".join(data_parts))
                    count += 1
                    if max_events and count >= max_events:
                        break
                event_type = None
                data_parts = []


# ---------------------------------------------------------------------------
# Test runner
# ---------------------------------------------------------------------------


def stream_test(
    label: str,
    params: dict,
    *,
    max_events: int | None = None,
    timeout: int = 30,
) -> None:
    """Run one streaming scenario and print a summary."""
    print(f"=== {label} ===")
    try:
        data_count = 0
        for event_type, data in sse_stream(
            STREAM_PATH, params, max_events=max_events, timeout=timeout
        ):
            if event_type == "done":
                print("  [done] stream ended cleanly")
            elif event_type == "message":
                try:
                    obj = json.loads(data)
                    svc = obj.get("service") or obj.get("container", "?")
                    stream = obj.get("stream", "?")
                    line = obj.get("line", "")
                    print(f"  [{stream}] {svc} | {line}")
                    data_count += 1
                except json.JSONDecodeError:
                    print(f"  [raw] {data}")
                    data_count += 1
            else:
                print(f"  [event:{event_type}] {data}")

        print(f"  → {data_count} log line(s) received")

    except urllib.error.HTTPError as exc:
        body = exc.read().decode("utf-8", errors="replace")
        print(f"  HTTP {exc.code}: {body.strip()}")
    except TimeoutError:
        print("  (timed out — no events within timeout window)")

    print()


# ---------------------------------------------------------------------------
# Live tail (runs until Ctrl+C)
# ---------------------------------------------------------------------------


def stream_live(params: dict) -> None:
    """Stream logs indefinitely, printing each line as it arrives.

    Reconnects automatically if the server closes the connection unexpectedly.
    Press Ctrl+C to stop.
    """
    svc_label = params.get("service") or "all services"
    print(f"Tailing '{params.get('name')}' [{svc_label}] — press Ctrl+C to stop\n")

    while True:
        try:
            for event_type, data in sse_stream(STREAM_PATH, params, timeout=None):
                if event_type == "done":
                    print("[done] server closed stream — reconnecting...")
                    break
                if event_type != "message":
                    continue
                try:
                    obj = json.loads(data)
                    svc = obj.get("service") or obj.get("container", "?")
                    stream = obj.get("stream", "?")
                    line = obj.get("line", "")
                    print(f"[{stream}] {svc} | {line}")
                except json.JSONDecodeError:
                    print(f"[raw] {data}")

        except KeyboardInterrupt:
            print("\nStopped.")
            return
        except urllib.error.HTTPError as exc:
            body = exc.read().decode("utf-8", errors="replace")
            print(f"HTTP {exc.code}: {body.strip()}")
            return
        except Exception as exc:  # noqa: BLE001
            print(f"[error] {exc} — reconnecting in 2s...")
            import time
            time.sleep(2)


# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------


def main() -> None:
    # --- live tail: runs until Ctrl+C ---
    stream_live({"name": APP_NAME, "since": "1m"})

    # # --- happy-path: full app, all services ---
    # stream_test(
    #     f"Stream: full app '{APP_NAME}' (since=all, cap 20 events)",
    #     {"name": APP_NAME, "since": "all"},
    #     max_events=20,
    # )

    # # --- happy-path: single service filter ---
    # if SERVICE_NAME:
    #     stream_test(
    #         f"Stream: service '{SERVICE_NAME}' only (since=all, cap 10 events)",
    #         {"name": APP_NAME, "service": SERVICE_NAME, "since": "all"},
    #         max_events=10,
    #     )

    # # --- happy-path: default window (24 h) ---
    # stream_test(
    #     f"Stream: full app '{APP_NAME}' default window (cap 10 events)",
    #     {"name": APP_NAME},
    #     max_events=10,
    # )

    # # --- happy-path: short duration window ---
    # stream_test(
    #     f"Stream: full app '{APP_NAME}' last 10s (cap 10 events)",
    #     {"name": APP_NAME, "since": "10s"},
    #     max_events=10,
    # )

    # # --- error: missing required 'name' param → 400 ---
    # stream_test("Stream: missing 'name' (expect 400)", {})

    # # --- error: invalid 'since' value → 400 ---
    # stream_test(
    #     "Stream: invalid 'since' (expect 400)",
    #     {"name": APP_NAME, "since": "yesterday"},
    # )

    # # --- error: unknown app → stream connects but emits zero data events + done ---
    # stream_test(
    #     "Stream: unknown app (expect 0 events + done)",
    #     {"name": "nonexistent-app-xyz", "since": "all"},
    #     max_events=5,
    #     timeout=5,
    # )


if __name__ == "__main__":
    main()
