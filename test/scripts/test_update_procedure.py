#!/usr/bin/env python3
from __future__ import annotations

import json
import time
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path

BASE_URL = "http://127.0.0.1:8080"
TOKEN = "robotics"

CONTAINER_NAME = "aether-test"
IMAGE = "dkhoanguyen/aether_base:latest"

COMPOSE_FILE = Path("tmp/aether-test.compose.yaml")

DEPLOY_PATH = "/v1/services"
CHECK_PATH = "/v1/check"
DOWNLOAD_PATH = "/v1/download"
STOP_PATH = "/v1/services/stop"
REMOVE_PATH = "/v1/services/remove"

WAIT_AFTER_DEPLOY_SECONDS = 3
WAIT_AFTER_STOP_SECONDS = 3

STOP_BODY = {"name": CONTAINER_NAME}
REMOVE_BODY = {"name": CONTAINER_NAME}


def build_compose_yaml() -> str:
    return f"""name: {CONTAINER_NAME}
services:
  {CONTAINER_NAME}:
    container_name: {CONTAINER_NAME}
    image: {IMAGE}
    command:
      - bash
      - -c
      - while true; do sleep 1; done
    privileged: true
    network_mode: host
    restart: always
    labels:
      com.centurylinklabs.watchtower.enable: "true"
"""


def build_app_spec() -> dict:
    return {
        "name": CONTAINER_NAME,
        "services": {
            CONTAINER_NAME: {
                "container_name": CONTAINER_NAME,
                "image": IMAGE,
                "command": ["bash", "-c", "while true; do sleep 1; done"],
                "privileged": True,
                "network_mode": "host",
                "restart": "always",
                "labels": {
                    "com.centurylinklabs.watchtower.enable": "true",
                },
            }
        },
    }


def join_url(path: str) -> str:
    return urllib.parse.urljoin(BASE_URL.rstrip("/") + "/", path.lstrip("/"))


def build_headers(content_type: str = "application/json") -> dict[str, str]:
    headers = {
        "Accept": "application/json, text/plain, */*",
        "Content-Type": content_type,
    }
    if TOKEN:
        headers["Authorization"] = f"Bearer {TOKEN}"
        headers["X-API-Token"] = TOKEN

    return headers


def http_post_json(url: str, payload: dict) -> tuple[int, str]:
    req = urllib.request.Request(
        url,
        data=json.dumps(payload).encode("utf-8"),
        headers=build_headers(),
        method="POST",
    )

    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            return resp.getcode(), resp.read().decode("utf-8", errors="replace")
    except urllib.error.HTTPError as exc:
        return exc.code, exc.read().decode("utf-8", errors="replace")


def http_post(url: str) -> tuple[int, str]:
    req = urllib.request.Request(
        url,
        data=b"",
        headers=build_headers(),
        method="POST",
    )

    try:
        with urllib.request.urlopen(req, timeout=60) as resp:
            return resp.getcode(), resp.read().decode("utf-8", errors="replace")
    except urllib.error.HTTPError as exc:
        return exc.code, exc.read().decode("utf-8", errors="replace")


def print_step(step: str, status: int, body: str) -> None:
    print(f"[{step}] HTTP {status}")
    if body.strip():
        print(body.strip())
    print("")


def require_success(step: str, status: int, body: str) -> None:
    if status not in {200, 201, 202, 204}:
        raise RuntimeError(f"{step} failed with HTTP {status}\n{body or '<empty>'}")


def decode_json(step: str, body: str) -> dict:
    try:
        return json.loads(body)
    except json.JSONDecodeError as exc:
        raise RuntimeError(f"{step} returned invalid JSON\n{body}") from exc


def construct_compose_yaml() -> str:
    compose_yaml = build_compose_yaml()
    COMPOSE_FILE.parent.mkdir(parents=True, exist_ok=True)
    COMPOSE_FILE.write_text(compose_yaml, encoding="utf-8")

    print(f"[construct] wrote compose yaml to {COMPOSE_FILE}")
    print("")
    print(compose_yaml.rstrip())
    print("")
    return compose_yaml


def deploy_from_compose_yaml() -> None:
    url = join_url(DEPLOY_PATH)
    payload = build_app_spec()

    status, body = http_post_json(url, payload)
    print_step("deploy", status, body)
    require_success("deploy", status, body)


def run_download_check() -> None:
    query = urllib.parse.urlencode({"image": IMAGE})
    url = f"{join_url(DOWNLOAD_PATH)}?{query}"

    status, body = http_post(url)
    print_step("download", status, body)
    require_success("download", status, body)

    response = decode_json("download", body)
    summary = response.get("summary", {})

    requested = summary.get("requested")
    downloaded = summary.get("downloaded")
    failed = summary.get("failed")

    if requested != 1:
        raise RuntimeError(f"expected requested=1, got {requested!r}")
    if downloaded != 1:
        raise RuntimeError(f"expected downloaded=1, got {downloaded!r}")
    if failed != 0:
        raise RuntimeError(f"expected failed=0, got {failed!r}")


def run_upstream_check() -> None:
    query = urllib.parse.urlencode({"image": IMAGE})
    url = f"{join_url(CHECK_PATH)}?{query}"

    status, body = http_post(url)
    print_step("check", status, body)
    require_success("check", status, body)

    response = decode_json("check", body)
    summary = response.get("summary", {})
    services = response.get("services") or []

    scanned = summary.get("scanned")
    updatable = summary.get("updatable")
    failed = summary.get("failed")

    if scanned != 1:
        raise RuntimeError(f"expected scanned=1, got {scanned!r}")
    if failed != 0:
        raise RuntimeError(f"expected failed=0, got {failed!r}")
    if updatable != len(services):
        raise RuntimeError(
            f"expected updatable to match number of services, got updatable={updatable!r}, services={len(services)}"
        )

    for service in services:
        if service.get("image") != IMAGE:
            raise RuntimeError(f"unexpected image in check response: {service!r}")
        if service.get("name") != CONTAINER_NAME:
            raise RuntimeError(f"unexpected service name in check response: {service!r}")
        if not service.get("upstream_digest"):
            raise RuntimeError(f"missing upstream_digest in check response: {service!r}")


def stop_container() -> None:
    url = join_url(STOP_PATH)
    status, body = http_post_json(url, STOP_BODY)
    print_step("stop", status, body)
    require_success("stop", status, body)


def remove_container() -> None:
    url = join_url(REMOVE_PATH)
    status, body = http_post_json(url, REMOVE_BODY)
    print_step("remove", status, body)
    require_success("remove", status, body)


def main() -> None:
    construct_compose_yaml()

    try:
        deploy_from_compose_yaml()
        time.sleep(WAIT_AFTER_DEPLOY_SECONDS)

        run_upstream_check()
        run_download_check()
    finally:
        try:
            stop_container()
            time.sleep(WAIT_AFTER_STOP_SECONDS)
        finally:
            remove_container()


if __name__ == "__main__":
    main()
