"""Reality profiles + UX metadata for assignments."""

from __future__ import annotations


def test_reality_get_and_put_partial(api, artifacts_dir):
    before = api.data(api.get("/v1/controlplane/reality"))
    api.dump(artifacts_dir, "reality_before.json", before)
    assert "effective_profiles" in before or "default_profiles" in before or "profiles" in before or "using_user_overrides" in before

    # Valid-looking profile
    put = api.data(
        api.put(
            "/v1/controlplane/reality",
            {
                "profiles": [
                    {
                        "sni": "www.cloudflare.com",
                        "handshake_server": "www.cloudflare.com",
                        "handshake_port": 443,
                    }
                ]
            },
        )
    )
    api.dump(artifacts_dir, "reality_put.json", put)
    assert "accepted" in put
    assert len(put["accepted"]) >= 1
    assert put.get("rejected") == [] or isinstance(put.get("rejected"), list)


def test_reality_all_rejected(api):
    body = api.put(
        "/v1/controlplane/reality",
        {"profiles": [{"sni": "", "handshake_server": ""}]},
        expect=400,
    )
    assert body.get("ok") is False
    assert body["error"]["code"] == "cp_invalid_reality"
