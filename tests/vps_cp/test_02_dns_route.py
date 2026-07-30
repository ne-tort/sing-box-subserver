"""DNS / route template fragments via API."""

from __future__ import annotations


def test_dns_route_roundtrip(api, artifacts_dir):
    before_dns = api.data(api.get("/v1/controlplane/config/dns"))
    before_route = api.data(api.get("/v1/controlplane/config/route"))
    api.dump(artifacts_dir, "dns_before.json", before_dns)
    api.dump(artifacts_dir, "route_before.json", before_route)
    assert "dns" in before_dns and "config_mode" in before_dns
    assert "route" in before_route

    dns_body = {
        "dns": {
            "servers": [
                {"tag": "local", "type": "local"},
                {"tag": "google", "type": "udp", "server": "8.8.8.8"},
            ],
            "final": "local",
        }
    }
    put_dns = api.data(api.put("/v1/controlplane/config/dns", dns_body))
    assert put_dns.get("persisted") is True
    assert "rematerialized" in put_dns
    assert put_dns["dns"]["final"] == "local"

    route_body = {
        "route": {
            "rules": [],
            "final": "direct",
            "auto_detect_interface": True,
        }
    }
    put_route = api.data(api.put("/v1/controlplane/config/route", route_body))
    assert put_route.get("persisted") is True
    assert put_route["route"]["final"] == "direct"

    got_dns = api.data(api.get("/v1/controlplane/config/dns"))
    got_route = api.data(api.get("/v1/controlplane/config/route"))
    assert got_dns["dns"]["final"] == "local"
    assert got_route["route"]["final"] == "direct"
    api.dump(artifacts_dir, "dns_after.json", got_dns)
    api.dump(artifacts_dir, "route_after.json", got_route)


def test_dns_invalid_rejected(api):
    body = api.put(
        "/v1/controlplane/config/dns",
        {"dns": ["not-an-object"]},
        expect=400,
    )
    assert body.get("ok") is False
    assert body["error"]["code"] == "cp_invalid_config"
