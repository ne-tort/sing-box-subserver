"""Live controlplane tests against a deployed subserver (VPS).

Env:
  CP_BASE      https://163.5.180.181:8080
  CP_TOKEN     bearer token from agent.yaml
  CP_DOMAIN    wiki.ai-qwerty.ru (ACME / SNI)
  CP_INSECURE  1 to skip TLS verify (self-signed mgmt)
  CP_ARTIFACTS ./artifacts (subscription dumps)
"""

from __future__ import annotations

import json
import os
import time
from pathlib import Path
from typing import Any

import pytest
import requests
import urllib3

urllib3.disable_warnings(urllib3.exceptions.InsecureRequestWarning)

ROOT = Path(__file__).resolve().parent


def env(name: str, default: str = "") -> str:
    return os.environ.get(name, default).strip()


@pytest.fixture(scope="session")
def base_url() -> str:
    return env("CP_BASE", "https://163.5.180.181:8080").rstrip("/")


@pytest.fixture(scope="session")
def token() -> str:
    t = env("CP_TOKEN", "vps-cp-token-dev-only")
    if not t:
        pytest.skip("CP_TOKEN required")
    return t


@pytest.fixture(scope="session")
def domain() -> str:
    return env("CP_DOMAIN", "wiki.ai-qwerty.ru")


@pytest.fixture(scope="session")
def insecure() -> bool:
    return env("CP_INSECURE", "1") not in ("0", "false", "no")


@pytest.fixture(scope="session")
def artifacts_dir() -> Path:
    d = Path(env("CP_ARTIFACTS", str(ROOT / "artifacts")))
    d.mkdir(parents=True, exist_ok=True)
    return d


@pytest.fixture(scope="session")
def api(base_url: str, token: str, insecure: bool) -> "CPClient":
    c = CPClient(base_url, token, verify=not insecure)
    c.get("/v1/health")
    return c


class CPError(AssertionError):
    def __init__(self, method: str, path: str, status: int, body: Any):
        self.method = method
        self.path = path
        self.status = status
        self.body = body
        super().__init__(f"{method} {path} -> {status}: {body}")


class CPClient:
    def __init__(self, base: str, token: str, verify: bool = True):
        self.base = base.rstrip("/")
        self.session = requests.Session()
        self.session.headers.update(
            {
                "Authorization": f"Bearer {token}",
                "Content-Type": "application/json",
                "Accept": "application/json",
            }
        )
        self.verify = verify

    def request(
        self,
        method: str,
        path: str,
        *,
        json_body: Any | None = None,
        params: dict | None = None,
        expect: int | tuple[int, ...] | None = 200,
        raw: bool = False,
        timeout: float = 60,
    ) -> Any:
        url = path if path.startswith("http") else f"{self.base}{path}"
        r = self.session.request(
            method,
            url,
            json=json_body,
            params=params,
            verify=self.verify,
            timeout=timeout,
        )
        if raw:
            if expect is not None:
                wanted = expect if isinstance(expect, tuple) else (expect,)
                if r.status_code not in wanted:
                    raise CPError(method, path, r.status_code, r.text[:2000])
            return r
        try:
            body = r.json()
        except Exception:
            body = r.text
        if expect is not None:
            wanted = expect if isinstance(expect, tuple) else (expect,)
            if r.status_code not in wanted:
                raise CPError(method, path, r.status_code, body)
        return body

    def get(self, path: str, **kw: Any) -> Any:
        return self.request("GET", path, **kw)

    def post(self, path: str, json_body: Any = None, **kw: Any) -> Any:
        return self.request("POST", path, json_body=json_body, **kw)

    def put(self, path: str, json_body: Any = None, **kw: Any) -> Any:
        return self.request("PUT", path, json_body=json_body, **kw)

    def patch(self, path: str, json_body: Any = None, **kw: Any) -> Any:
        return self.request("PATCH", path, json_body=json_body, **kw)

    def delete(self, path: str, **kw: Any) -> Any:
        return self.request("DELETE", path, **kw)

    def data(self, body: Any) -> Any:
        assert isinstance(body, dict), body
        assert body.get("ok") is True, body
        return body["data"]

    def wait_ready(self, timeout: float = 180, interval: float = 3) -> dict:
        deadline = time.time() + timeout
        last: dict = {}
        while time.time() < deadline:
            payload = self.data(self.get("/v1/controlplane/status"))
            last = payload
            ready = payload.get("ready") or {}
            if ready.get("ok") is True:
                return payload
            time.sleep(interval)
        raise AssertionError(f"ready.ok timeout; last={json.dumps(last, ensure_ascii=False)[:2000]}")

    def dump(self, artifacts: Path, name: str, obj: Any) -> Path:
        path = artifacts / name
        path.parent.mkdir(parents=True, exist_ok=True)
        if isinstance(obj, (bytes, bytearray)):
            path.write_bytes(obj)
        elif isinstance(obj, str):
            path.write_text(obj, encoding="utf-8")
        else:
            path.write_text(json.dumps(obj, ensure_ascii=False, indent=2), encoding="utf-8")
        return path
