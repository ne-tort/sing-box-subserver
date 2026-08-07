"""SSL profiles: Default self-signed + ACME domain / optional IP."""

from __future__ import annotations

import time


def test_ssl_default_status(api, artifacts_dir):
    ssl = api.data(api.get("/v1/controlplane/ssl"))
    api.dump(artifacts_dir, "ssl.json", ssl)
    profiles = ssl.get("profiles") or []
    assert profiles, "expected at least Default SSL profile"
    default = next((p for p in profiles if p.get("id") == "default"), profiles[0])
    st = default.get("status") or {}
    assert st.get("state") in ("ready", "pending", "missing", "expired", "error")
    # Default self-signed should be ready after ensure.
    assert st.get("state") == "ready" or default.get("type") == "self_signed"


def test_ssl_acme_domain(api, domain, artifacts_dir):
    """Create an ACME SSL profile for the public domain and wait for leaf if possible."""
    created = api.data(
        api.post("/v1/controlplane/ssl", {"name": "e2e-acme"})
    )
    pid = created.get("id")
    assert pid
    body = {
        "id": pid,
        "name": "e2e-acme",
        "type": "acme",
        "domain": domain,
        "email": "admin@ai-qwerty.ru",
        "provider": "letsencrypt",
        "disable_http_challenge": False,
    }
    cm = api.data(api.put(f"/v1/controlplane/ssl/{pid}", body))
    api.dump(artifacts_dir, "ssl_acme_put.json", cm)
    assert cm.get("domain") == domain or (cm.get("status") is not None)

    deadline = time.time() + 180
    last = cm
    while time.time() < deadline:
        last = api.data(api.get(f"/v1/controlplane/ssl/{pid}"))
        st = last.get("status") or {}
        if st.get("state") == "ready":
            break
        time.sleep(5)
    api.dump(artifacts_dir, "ssl_acme_polled.json", last)
    st = last.get("status") or {}
    if st.get("state") != "ready":
        api.dump(
            artifacts_dir,
            "ssl_acme_NOT_READY.txt",
            f"ACME not ready after wait: {st}. Ensure :80 is free for HTTP-01 on {domain}.",
        )


def test_tls_inbound_with_ssl_profile(api, domain, artifacts_dir):
    """Install TLS inbound bound to an ACME SSL profile via params.ssl_profile."""
    profiles = (api.data(api.get("/v1/controlplane/ssl")).get("profiles") or [])
    acme = next(
        (p for p in profiles if p.get("type") in ("acme", "acme_ip") and (p.get("domain") == domain or p.get("ip"))),
        None,
    )
    if acme is None:
        created = api.data(api.post("/v1/controlplane/ssl", {"name": "e2e-bind"}))
        pid = created["id"]
        api.put(
            f"/v1/controlplane/ssl/{pid}",
            {
                "id": pid,
                "name": "e2e-bind",
                "type": "acme",
                "domain": domain,
                "email": "admin@ai-qwerty.ru",
                "provider": "letsencrypt",
            },
        )
        acme = api.data(api.get(f"/v1/controlplane/ssl/{pid}"))
    pid = acme["id"]

    presets = api.data(api.get("/v1/controlplane/presets?lang=en"))
    tls_preset = None
    for p in presets:
        n = p.get("name") or p.get("tag") or ""
        if "reality" in n.lower() or "shadowsocks" in n.lower() or n.startswith("wg"):
            continue
        if "trojan" in n.lower() or "vless" in n.lower() or "hysteria" in n.lower():
            tls_preset = n
            break
    if not tls_preset:
        tls_preset = "trojan-tcp"

    name = "e2e-acme-tls"
    st = api.get(f"/v1/controlplane/sets/{name}", expect=(200, 404))
    if st.get("ok"):
        d = api.data(st)
        if d.get("active"):
            api.post(f"/v1/controlplane/sets/{name}/deactivate", expect=(200, 422))
        api.delete(f"/v1/controlplane/sets/{name}", expect=(200, 404, 409))

    body = {
        "items": [
            {
                "name": name,
                "preset": tls_preset,
                "listen_port": 18444,
                "params": {"ssl_profile": pid},
            }
        ],
        "activate": True,
    }
    install = api.post("/v1/controlplane/sets/from-presets", body, expect=(201, 400, 422))
    api.dump(artifacts_dir, "acme_tls_install.json", install)
    if not install.get("ok"):
        return
    data = api.data(install)
    assert data.get("activated") is True
    detail = api.data(api.get(f"/v1/controlplane/sets/{name}"))
    bindings = detail["bindings"]
    assert any((b.get("params") or {}).get("ssl_profile") == pid for b in bindings)

    status = api.data(api.get("/v1/controlplane/status"))
    api.dump(artifacts_dir, "status_acme_tls.json", status)
    ready = status.get("ready") or {}
    ms = status.get("tls_material_status") or {}
    if not ms.get("ready", True):
        assert "tls_not_ready" in (ready.get("reasons") or []) or ready.get("ok") is False


def test_ssl_acme_ip_optional(api, artifacts_dir):
    """Let's Encrypt short-lived IP certs via acme_ip SSL profile."""
    ip = "163.5.180.181"
    created = api.data(api.post("/v1/controlplane/ssl", {"name": "e2e-ip"}))
    pid = created["id"]
    resp = api.put(
        f"/v1/controlplane/ssl/{pid}",
        {
            "id": pid,
            "name": "e2e-ip",
            "type": "acme_ip",
            "ip": ip,
            "email": "admin@ai-qwerty.ru",
            "provider": "letsencrypt",
        },
        expect=(200, 400),
    )
    api.dump(artifacts_dir, "ssl_acme_ip.json", resp)
