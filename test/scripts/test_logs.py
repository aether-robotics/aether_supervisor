#!/usr/bin/env python3
"""Script to exercise the /v1/logs endpoint and print raw responses.

Lifecycle:
  1. Deploy a test app (nginx:alpine) via /v1/services
  2. Wait a moment so the container produces log output
  3. Query /v1/logs with various window and filter options, printing each response
  4. Stop and remove the app

Usage:
  python3 test_logs.py

Requirements:
  - aether_supervisor running on 127.0.0.1:8080 with --http-api-update
  - Token matches TOKEN below (default: robotics)
  - Docker daemon reachable
"""
from __future__ import annotations

import json
import time
import urllib.error
import urllib.parse
import urllib.request

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------

BASE_URL = "http://127.0.0.1:8080"
TOKEN = "robotics"

APP_NAME = "aether-logs-test"
SERVICE_NAME = "aether-test"
IMAGE = "nginx:alpine"

LOGS_PATH = "/v1/logs"
DEPLOY_PATH = "/v1/services"
STOP_PATH = "/v1/services/stop"
REMOVE_PATH = "/v1/services/remove"

WAIT_AFTER_DEPLOY_SECONDS = 3


# ---------------------------------------------------------------------------
# HTTP helpers
# ---------------------------------------------------------------------------

def _auth_headers() -> dict:
    return {
        "Authorization": f"Bearer {TOKEN}",
        "Content-Type": "application/json",
        "Accept": "application/json",
    }


def join_url(path: str) -> str:
    return urllib.parse.urljoin(BASE_URL.rstrip("/") + "/", path.lstrip("/"))


def http_get(path: str, params: dict | None = None) -> tuple[int, str]:
    url = join_url(path)
    if params:
        url += "?" + urllib.parse.urlencode({k: v for k, v in params.items() if v is not None})

    req = urllib.request.Request(url, headers=_auth_headers(), method="GET")
    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            return resp.getcode(), resp.read().decode("utf-8", errors="replace")
    except urllib.error.HTTPError as exc:
        return exc.code, exc.read().decode("utf-8", errors="replace")


def http_post_json(path: str, payload: dict) -> tuple[int, str]:
    url = join_url(path)
    req = urllib.request.Request(
        url,
        data=json.dumps(payload).encode("utf-8"),
        headers=_auth_headers(),
        method="POST",
    )
    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            return resp.getcode(), resp.read().decode("utf-8", errors="replace")
    except urllib.error.HTTPError as exc:
        return exc.code, exc.read().decode("utf-8", errors="replace")


# ---------------------------------------------------------------------------
# Print helper
# ---------------------------------------------------------------------------

def print_response(label: str, status: int, body: str) -> None:
    print(f"[{label}] HTTP {status}")
    try:
        print(json.dumps(json.loads(body), indent=2))
    except (json.JSONDecodeError, ValueError):
        print(body.strip())
    print()


# ---------------------------------------------------------------------------
# App lifecycle
# ---------------------------------------------------------------------------

def deploy_test_app() -> None:
    payload = {
        "name": APP_NAME,
        "services": {
            SERVICE_NAME: {
                "image": IMAGE,
                "restart": "no",
            }
        },
    }
    status, body = http_post_json(DEPLOY_PATH, payload)
    print_response("deploy", status, body)


def stop_test_app() -> None:
    status, body = http_post_json(STOP_PATH, {"name": APP_NAME})
    print_response("stop", status, body)


def remove_test_app() -> None:
    status, body = http_post_json(REMOVE_PATH, {"name": APP_NAME})
    print_response("remove", status, body)


# ---------------------------------------------------------------------------
# Log queries
# ---------------------------------------------------------------------------

def fetch_logs(label: str, params: dict) -> None:
    status, body = http_get(LOGS_PATH, params)
    print_response(label, status, body)


# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

def main() -> None:
    print("=== Setup ===")
    # deploy_test_app()
    print(f"Waiting {WAIT_AFTER_DEPLOY_SECONDS}s for log output...\n")
    time.sleep(WAIT_AFTER_DEPLOY_SECONDS)

    print("=== Logs: default window (24h) ===")
    fetch_logs("since=<omitted>", {"name": "demo-app", "since": "10s"})

    # print("=== Logs: explicit windows ===")
    # for window in ("24h", "7d", "all"):
    #     fetch_logs(f"since={window}", {"name": APP_NAME, "since": window})

    # print("=== Logs: arbitrary durations ===")
    # for window in ("2.5h", "5.6d", "90m", "0.5d"):
    #     fetch_logs(f"since={window}", {"name": APP_NAME, "since": window})

    # print("=== Logs: service filter ===")
    # fetch_logs(f"name={APP_NAME}&service={SERVICE_NAME}", {"name": APP_NAME, "service": SERVICE_NAME})

    # print("=== Logs: missing name (expect 400) ===")
    # fetch_logs("name=<missing>", {})

    # print("=== Logs: invalid since (expect 400) ===")
    # fetch_logs("since=yesterday", {"name": APP_NAME, "since": "yesterday"})

    # print("=== Logs: unknown app (expect empty) ===")
    # fetch_logs("name=nonexistent-app", {"name": "nonexistent-app"})

    # print("=== Teardown ===")
    # stop_test_app()
    # remove_test_app()


if __name__ == "__main__":
    main()
