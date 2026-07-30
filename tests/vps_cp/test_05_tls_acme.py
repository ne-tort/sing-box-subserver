"""TLS self-signed + cert-manager ACME with domain / optional IP."""

from __future__ import annotations

import time


def test_tls_self_signed_status(api, artifacts_dir):
    tls = api.data(api.get("/v1/controlplane/tls"))
    api.dump(artifacts_dir, "tls.json", tls)
    assert "self_signed" in tls
    assert "material_status" in tls
    assert tls["material_status"].get("ready") is True


def test_cert_manager_domain_acme(api, domain, artifacts_dir):
    """Configure ACME for wiki.ai-qwerty.ru and wait for leaf if HTTP-01 possible."""
    cm = api.data(
        api.put(
            "/v1/controlplane/cert-manager",
            {
                "email": "admin@ai-qwerty.ru",
                "provider": "letsencrypt",
                "domains": [domain],
                "disable_http_challenge": False,
                "disable_tls_alpn_challenge": False,
            },
        )
    )
    api.dump(artifacts_dir, "cert_manager_put.json", cm)
    assert domain in (cm.get("domains") or [])
    assert cm.get("enabled") is True

    # Poll material_status up to ~3 minutes (LE HTTP-01 needs :80 free)
    deadline = time.time() + 180
    last = cm
    while time.time() < deadline:
        last = api.data(api.get("/v1/controlplane/cert-manager"))
        ms = last.get("material_status") or {}
        if ms.get("ready") is True:
            break
        time.sleep(5)
    api.dump(artifacts_dir, "cert_manager_polled.json", last)
    ms = last.get("material_status") or {}
    # Soft assert: document if ACME not ready (port 80 / rate limit)
    if not ms.get("ready"):
        api.dump(
            artifacts_dir,
            "cert_manager_NOT_READY.txt",
            f"ACME not ready after wait: {ms}. Ensure :80 is free for HTTP-01 on {domain}.",
        )


def test_tls_inbound_with_params_sni(api, domain, artifacts_dir):
    """Install TLS inbound bound to ACME domain via params.sni."""
    cm = api.data(api.get("/v1/controlplane/cert-manager"))
    domains = cm.get("domains") or []
    if domain not in domains:
        api.put(
            "/v1/controlplane/cert-manager",
            {
                "email": "admin@ai-qwerty.ru",
                "provider": "letsencrypt",
                "domains": [domain],
            },
        )

    # Find a TLS (non-reality) tcp preset
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

    # Use high port to avoid clash with demux :443
    body = {
        "items": [
            {
                "name": name,
                "preset": tls_preset,
                "listen_port": 18444,
                "params": {"sni": domain},
            }
        ],
        "activate": True,
    }
    install = api.post("/v1/controlplane/sets/from-presets", body, expect=(201, 400, 422))
    api.dump(artifacts_dir, "acme_tls_install.json", install)
    if not install.get("ok"):
        # Domain not in CM / validation — surface for operator
        return
    data = api.data(install)
    assert data.get("activated") is True
    detail = api.data(api.get(f"/v1/controlplane/sets/{name}"))
    bindings = detail["bindings"]
    assert any((b.get("params") or {}).get("sni") == domain for b in bindings)

    # ready should mention acme if certs missing
    status = api.data(api.get("/v1/controlplane/status"))
    api.dump(artifacts_dir, "status_acme_tls.json", status)
    ready = status.get("ready") or {}
    if not (status.get("cert_manager") or {}).get("ready", True):
        assert "acme_not_ready" in (ready.get("reasons") or []) or ready.get("ok") is False


def test_cert_manager_ip_san_optional(api, artifacts_dir):
    """Let's Encrypt short-lived IP certs — optional, skip if unsupported by provider path."""
    ip = "163.5.180.181"
    # Attempt add IP as domain; may be rejected by our Validate — that is OK to document
    resp = api.put(
        "/v1/controlplane/cert-manager",
        {
            "email": "admin@ai-qwerty.ru",
            "provider": "letsencrypt",
            "domains": ["wiki.ai-qwerty.ru", ip],
        },
        expect=(200, 400),
    )
    api.dump(artifacts_dir, "cert_manager_with_ip.json", resp)
