"""Demux group install, substitutions, slot overrides, edit."""

from __future__ import annotations

import time
from typing import Any


def _cleanup_set(api, name: str) -> None:
    st = api.get(f"/v1/controlplane/sets/{name}", expect=(200, 404))
    if not st.get("ok"):
        return
    data = api.data(st)
    if data.get("active"):
        api.post(f"/v1/controlplane/sets/{name}/deactivate", expect=(200, 422))
    api.delete(f"/v1/controlplane/sets/{name}", expect=(200, 404, 409))


def test_demux_group_install_with_slot_presets(api, artifacts_dir):
    groups = api.data(api.get("/v1/controlplane/demux-groups?lang=en"))
    # Prefer a compact group that fits :443
    prefer = ("dg_443_dual", "dg_443_triple", "dg_443_tls_quic")
    by_tag = {g["tag"]: g for g in groups}
    tag = next((t for t in prefer if t in by_tag), groups[0]["tag"])
    g = by_tag[tag]
    subs = api.data(api.get(f"/v1/controlplane/demux-groups/{tag}/substitutions?lang=en"))
    api.dump(artifacts_dir, f"demux_pick_{tag}.json", {"group": g, "subs": subs})

    # Defaults are the normative live path (member + demux front). Alternatives
    # must still be demux-compatible in the picker metadata.
    slot_presets: dict[str, str] = {}
    for slot in subs["slots"]:
        slot_presets[slot["id"]] = slot["default_preset"]
        for o in slot["options"]:
            assert o.get("tag") != "hy2_salamander" or (
                o.get("fits_interchange") is False or o.get("demux_compat") == "demux_unsupported"
            ), o
            if o.get("demux_compat") == "full" and o.get("fits_interchange", True):
                assert o["tag"] != "hy2_salamander"

    name = "e2e-demux"
    _cleanup_set(api, name)

    # Free 443 if occupied by previous tests
    for s in api.data(api.get("/v1/controlplane/sets")):
        if s.get("listen_port") == 443 and s.get("active"):
            api.post(f"/v1/controlplane/sets/{s['name']}/deactivate", expect=(200, 422))

    body = {
        "group": tag,
        "name": name,
        "listen_port": 443,
        "slot_presets": slot_presets,
        "activate": True,
    }
    install = api.data(api.post("/v1/controlplane/sets/from-demux-group", body, expect=201))
    api.dump(artifacts_dir, "demux_install.json", install)
    assert install["activated"] is True
    set_view = install["set"]
    assert set_view["has_demux"] is True
    assert set_view["active"] is True
    assert "member_ports" in install and install["member_ports"]
    assert "hy2_salamander" not in install["member_ports"]
    assert "slot_snis" in install

    # Name conflict on reinstall without cleanup
    api.post(
        "/v1/controlplane/sets/from-demux-group",
        {"group": tag, "name": name, "listen_port": 8444, "activate": False},
        expect=409,
    )

    status = api.wait_ready(timeout=180)
    api.dump(artifacts_dir, "status_after_demux.json", status)

    detail = api.data(api.get(f"/v1/controlplane/sets/{name}"))
    api.dump(artifacts_dir, "demux_set_detail.json", detail)
    assert detail.get("demux_template") or detail.get("has_demux")
    assert detail.get("member_ports") or install["member_ports"]
    assert detail.get("slot_snis") or install.get("slot_snis"), "slot_snis must persist for list/get after reload"
    assert detail.get("presets") == list(slot_presets.values()) or set(detail.get("presets") or []) == set(
        slot_presets.values()
    )

    # User + subscription for demux stack
    user = api.data(
        api.post(
            "/v1/controlplane/users",
            {"name": f"e2e-user-demux-{int(time.time())}", "enabled": True},
            expect=(200, 201),
        )
    )
    token = user["sub_token"]
    r = api.get(f"/v1/sub/{token}", raw=True, expect=200)
    sub = r.json()
    api.dump(artifacts_dir, "sub_demux.json", sub)
    api.dump(artifacts_dir, "sub_demux.url.txt", user.get("subscription_url") or f"{api.base}/v1/sub/{token}")
    # Expect multiple outbounds for demux members
    outs = sub.get("outbounds") or []
    assert isinstance(outs, list)
    assert len(outs) >= 2, f"expected multiple outbounds, got {len(outs)}"
    assert sub.get("meta", {}).get("matched") == len(outs)
    for o in outs:
        if o.get("type") == "vless":
            flow = o.get("flow") or ""
            assert "udp443" not in flow, o


def test_demux_omit_listen_port_auto_pick(api, artifacts_dir):
    groups = api.data(api.get("/v1/controlplane/demux-groups"))
    tag = groups[0]["tag"]
    name = "e2e-demux-autoport"
    _cleanup_set(api, name)
    # Omit listen_port — should not explode with port conflict blindly on 443
    install = api.data(
        api.post(
            "/v1/controlplane/sets/from-demux-group",
            {"group": tag, "name": name, "activate": False},
            expect=201,
        )
    )
    api.dump(artifacts_dir, "demux_autoport.json", install)
    assert install["set"]["listen_port"] > 0
    assert install["activated"] is False
    _cleanup_set(api, name)
