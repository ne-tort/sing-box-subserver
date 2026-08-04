#!/usr/bin/env python3
"""Controlplane docker smoke (curl-equivalent via urllib)."""
from __future__ import annotations

import json
import ssl
import time
import urllib.error
import urllib.request
from pathlib import Path

BASE = "https://127.0.0.1:18081"
TOKEN = "smoke-token-not-for-prod"
CTX = ssl._create_unverified_context()


def req(method: str, path: str, body=None, auth=True):
    data = None
    headers = {}
    if auth:
        headers["Authorization"] = f"Bearer {TOKEN}"
    if body is not None:
        data = json.dumps(body).encode()
        headers["Content-Type"] = "application/json"
    r = urllib.request.Request(BASE + path, data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(r, context=CTX, timeout=30) as resp:
            raw = resp.read()
            code = resp.status
    except urllib.error.HTTPError as e:
        raw = e.read()
        code = e.code
    text = raw.decode("utf-8", errors="replace")
    try:
        parsed = json.loads(text) if text else None
    except json.JSONDecodeError:
        parsed = text
    return code, parsed, text


def main() -> None:
    for _ in range(60):
        code, _, _ = req("GET", "/v1/health", auth=False)
        if code == 200:
            break
        time.sleep(2)
    else:
        raise SystemExit("health timeout")
    print("health OK")

    code, tls, _ = req("GET", "/v1/controlplane/tls")
    assert code == 200, tls
    assert tls["data"]["material_status"]["active_material"] == "self_signed_pem"
    assert "203.0.113.10" in tls["data"]["self_signed"]["ip_sans"]
    print("TLS OK")

    code, _, _ = req(
        "PUT",
        "/v1/controlplane/tls",
        {
            "mode": "acme_ip",
            "acme": {"email": "a@b.c", "domains": ["203.0.113.10"], "provider": "zerossl"},
        },
    )
    assert code == 400, code
    print("ACME reject OK")

    code, user, _ = req("POST", "/v1/controlplane/users", {"name": "alice"})
    assert code in (200, 201), user
    tok = user["data"]["sub_token"]
    assert tok

    code, vless, _ = req("GET", "/v1/controlplane/protocols/vless?lang=en")
    assert code == 200
    desc = vless["data"]["description"]
    assert not any("\u0400" <= c <= "\u04FF" for c in desc), desc
    code, vt, _ = req("GET", "/v1/controlplane/presets/vless_tcp?lang=en")
    fb = vt["data"]["demux_hints"]["first_bytes"]
    assert not any("\u0400" <= c <= "\u04FF" for c in fb), fb
    print("catalog EN OK")

    code, _, text = req(
        "POST",
        "/v1/controlplane/sets",
        {"name": "ss1", "listen": "0.0.0.0", "listen_port": 1080, "presets": ["shadowsocks-tcp"]},
    )
    assert code in (200, 201), text
    code, act, text = req("POST", "/v1/controlplane/sets/ss1/activate")
    assert code == 200, text
    assert act["data"]["config_mode"] == "controlplane"

    code, _, text = req(
        "POST",
        "/v1/controlplane/sets",
        {"name": "tr1", "listen": "0.0.0.0", "listen_port": 8443, "presets": ["trojan-tcp"]},
    )
    assert code in (200, 201), text
    code, _, text = req("POST", "/v1/controlplane/sets/tr1/activate")
    assert code == 200, text

    code, _, cfg = req("GET", "/v1/config")
    assert code == 200
    assert "certificate_path" in cfg
    assert "certificate_provider" not in cfg
    print("config OK")

    code, sub, text = req("GET", f"/v1/sub/{tok}", auth=False)
    assert code == 200, text[:200]
    if isinstance(sub, str):
        # may be plain JSON text already parsed fail — treat as text
        sub = json.loads(text)
    found = False
    for ob in sub.get("outbounds") or []:
        if ob.get("type") == "trojan" and (ob.get("tls") or {}).get("insecure") is True:
            found = True
    assert found, sub
    print("sub OK")

    import socket
    import hashlib
    from OpenSSL import crypto  # may not exist

    # handshake via stdlib
    import ssl as sslmod

    def thumb() -> str:
        raw = sslmod.get_server_certificate(("127.0.0.1", 18443), ssl_version=sslmod.PROTOCOL_TLS_CLIENT)
        # get_server_certificate validates — may fail on self-signed; use custom
        return raw

    # custom connect
    def peer_fp() -> bytes:
        ctx = sslmod.SSLContext(sslmod.PROTOCOL_TLS_CLIENT)
        ctx.check_hostname = False
        ctx.verify_mode = sslmod.CERT_NONE
        with socket.create_connection(("127.0.0.1", 18443), timeout=5) as sock:
            with ctx.wrap_socket(sock, server_hostname="203.0.113.10") as ssock:
                der = ssock.getpeercert(binary_form=True)
                return hashlib.sha256(der).digest()

    time.sleep(1)
    fp_a = peer_fp()
    print("handshake OK")

    import subprocess

    def crt_hash() -> str:
        out = subprocess.check_output(
            ["docker", "exec", "subserver-cp-smoke", "sha256sum", "/var/lib/subserver/controlplane/tls/server.crt"],
            text=True,
        )
        return out.strip()

    code, st1, _ = req("GET", "/v1/status")
    rev1 = int(st1["data"]["revision"])
    h1 = crt_hash()
    code, _, text = req("POST", "/v1/controlplane/tls/regenerate")
    assert code == 200, text
    h2 = crt_hash()
    assert h1 != h2, "cert hash unchanged"
    code, st2, _ = req("GET", "/v1/status")
    rev2 = int(st2["data"]["revision"])
    assert rev2 > rev1, (rev1, rev2)
    fp_b = peer_fp()
    assert fp_a != fp_b, "runtime still serves old cert"
    print("regenerate OK")
    print("== OK controlplane matrix ==")


if __name__ == "__main__":
    main()
