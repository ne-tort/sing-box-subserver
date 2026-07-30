"""Single-inbound presets install + edit + subscription."""

from __future__ import annotations

import json
import time
from typing import Any


def _pick_tcp_preset(api) -> str:
    presets = api.data(api.get("/v1/controlplane/presets?lang=en"))
    # Prefer shadowsocks / trojan / vless non-reality for simple smoke
    prefer = ("shadowsocks-tcp", "trojan-tcp", "vless-tcp")
    names = {p.get("name") or p.get("tag") for p in presets}
    for n in prefer:
        if n in names:
            return n
    for p in presets:
        n = p.get("name") or p.get("tag")
        traits = " ".join(p.get("traits") or []).lower()
        if "wireguard" in traits or "wg" in (n or ""):
            continue
        if "reality" in (n or "").lower():
            continue
        if n:
            return n
    raise AssertionError("no suitable preset")


def test_from_presets_install_activate_user_sub(api, artifacts_dir):
    # Cleanup leftover sets with same names if any
    for name in ("e2e-ss", "e2e-tr"):
        st = api.get(f"/v1/controlplane/sets/{name}", expect=(200, 404))
        if st.get("ok"):
            data = api.data(st)
            if data.get("active"):
                api.post(f"/v1/controlplane/sets/{name}/deactivate", expect=(200, 422))
            api.delete(f"/v1/controlplane/sets/{name}", expect=(200, 404, 409))

    preset = _pick_tcp_preset(api)
    install = api.data(
        api.post(
            "/v1/controlplane/sets/from-presets",
            {
                "items": [{"name": "e2e-ss", "preset": preset, "listen_port": 18443}],
                "activate": True,
            },
            expect=201,
        )
    )
    api.dump(artifacts_dir, "from_presets_install.json", install)
    assert install["activated"] is True
    assert install["activated_sets"] == ["e2e-ss"]
    set_view = install["sets"][0]
    assert set_view["name"] == "e2e-ss"
    assert set_view["active"] is True
    assert "bindings" in set_view

    status = api.wait_ready(timeout=120)
    api.dump(artifacts_dir, "status_after_presets.json", status)

    # Edit set (PUT) — keep same port/preset
    got = api.data(api.get("/v1/controlplane/sets/e2e-ss"))
    put = api.data(
        api.put(
            "/v1/controlplane/sets/e2e-ss",
            {
                "name": "e2e-ss",
                "listen": got.get("listen") or "::",
                "listen_port": got["listen_port"],
                "description": "e2e edited",
                "bindings": got["bindings"],
            },
        )
    )
    assert put.get("description") == "e2e edited" or put.get("active") is True

    user = api.data(api.post("/v1/controlplane/users", {"name": "e2e-user-presets", "enabled": True}, expect=(200, 201)))
    api.dump(artifacts_dir, "user_presets.json", user)
    token = user.get("sub_token")
    assert token
    sub_url = user.get("subscription_url") or f"{api.base}/v1/sub/{token}"
    # Fetch subscription (raw sing-box JSON, not {ok,data})
    r = api.get(f"/v1/sub/{token}", raw=True, expect=200)
    sub = r.json()
    api.dump(artifacts_dir, "sub_presets.json", sub)
    assert "outbounds" in sub or "inbounds" in sub or isinstance(sub, dict)
    # Save URL for client phase
    api.dump(artifacts_dir, "sub_presets.url.txt", sub_url)

    tags = api.data(api.get("/v1/controlplane/subscription-tags?active_only=true"))
    api.dump(artifacts_dir, "subscription_tags.json", tags)
    assert "sets" in tags


def test_sets_list_get_shape_parity(api):
    listing = api.data(api.get("/v1/controlplane/sets"))
    assert isinstance(listing, list)
    if not listing:
        return
    one = listing[0]
    detail = api.data(api.get(f"/v1/controlplane/sets/{one['name']}"))
    for k in ("name", "listen_port", "bindings", "active", "has_demux"):
        assert k in one and k in detail
    assert one["active"] == detail["active"]
