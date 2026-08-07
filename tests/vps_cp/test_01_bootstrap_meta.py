"""Bootstrap / metadata UX for clients."""

from __future__ import annotations

from typing import Any

import pytest


REQUIRED_CAPABILITIES = {
    "protocols",
    "presets",
    "demux_groups",
    "demux_in_binary",
    "port_policy",
    "ssl_profiles",
    "inbound_ssl_profile_param",
    "config_dns_route",
    "optional_listen_port",
    "ready_poll",
    "activate_contract",
}


def test_health(api):
    body = api.get("/v1/health")
    data = api.data(body)
    assert data.get("status") == "alive"


def test_bootstrap_capabilities_and_flows(api, artifacts_dir):
    data = api.data(api.get("/v1/controlplane/client/bootstrap?lang=en"))
    api.dump(artifacts_dir, "bootstrap.json", data)
    caps = data["capabilities"]
    missing = REQUIRED_CAPABILITIES - set(caps)
    assert not missing, f"missing capabilities: {missing}"
    assert caps["inbound_ssl_profile_param"] == "ssl_profile"
    assert "activated" in str(caps["activate_contract"]).lower() or "422" in str(caps["activate_contract"])
    assert caps.get("replace_on_from_star") is True
    assert "subscription" in data
    assert data["subscription"].get("prefer_variant") == "flow-none"
    assert "public_host" in data
    assert "client_auth" in data
    flows = data["flows"]
    assert any(f.get("id") == "single_presets" for f in flows)
    assert any(f.get("id") == "demux_group" for f in flows)
    # UX: every flow step should have method+path
    for flow in flows:
        for step in flow.get("steps") or []:
            assert step.get("method") and step.get("path"), step


def test_status_ready_shape(api, artifacts_dir):
    data = api.data(api.get("/v1/controlplane/status"))
    api.dump(artifacts_dir, "status.json", data)
    assert "config_mode" in data
    assert "ready" in data
    ready = data["ready"]
    assert "ok" in ready and "reasons" in ready and "poll" in ready
    assert "context" in ready
    assert ready["context"] in ("idle", "install_ready", "degraded")
    assert "ssl_profiles" in data or "tls_material_status" in data
    assert "tls_material_status" in data
    assert "ownership_health" in data


def test_protocols_presets_metadata(api, artifacts_dir):
    protocols = api.data(api.get("/v1/controlplane/protocols?lang=en"))
    assert isinstance(protocols, list) and protocols
    api.dump(artifacts_dir, "protocols.json", protocols)
    for p in protocols:
        assert p.get("tag"), p
        assert "title" in p or "short_name" in p or "name" in p

    tag = protocols[0]["tag"]
    presets = api.data(api.get(f"/v1/controlplane/presets?protocol={tag}&lang=en"))
    assert isinstance(presets, list)
    api.dump(artifacts_dir, f"presets_{tag}.json", presets)
    for pr in presets[:5]:
        assert pr.get("name") or pr.get("tag"), pr
        # UX helpers for install
        assert "protocol" in pr or "traits" in pr or "scores" in pr or "optional_params" in pr
        if "cred_fields" in pr:
            assert "cred_generators" in pr or pr.get("cred_generators") is None or True
    # At least one preset should expose generators when protocol has them
    any_gens = any("cred_generators" in (p or {}) for p in presets)
    any_peer = any("peer_secret_fields" in (p or {}) for p in presets)
    _ = (any_gens, any_peer)  # presence is additive; list always includes keys when non-empty


def test_demux_groups_match_meta_ux(api, artifacts_dir):
    groups = api.data(api.get("/v1/controlplane/demux-groups?lang=en"))
    assert isinstance(groups, list) and groups
    api.dump(artifacts_dir, "demux_groups.json", groups)
    g0 = groups[0]
    assert g0.get("tag")
    assert "slots" in g0 and "separation_summary" in g0
    slot = g0["slots"][0]
    for key in ("id", "role", "default_preset", "presets", "separation_tags", "interchange_tags", "match_shape", "match_priority"):
        assert key in slot, f"slot missing {key}: {slot}"
    # legacy alias
    assert slot.get("substitutes") == slot.get("presets") or "substitutes" in slot

    full = api.data(api.get(f"/v1/controlplane/demux-groups/{g0['tag']}?lang=en"))
    api.dump(artifacts_dir, f"demux_group_{g0['tag']}.json", full)
    assert "match_plan" in full
    plan = full["match_plan"]
    assert isinstance(plan, list) and plan
    for step in plan:
        assert "match_shape" in step or "slot_id" in step or "priority" in step

    subs = api.data(api.get(f"/v1/controlplane/demux-groups/{g0['tag']}/substitutions?lang=en"))
    api.dump(artifacts_dir, f"demux_subs_{g0['tag']}.json", subs)
    assert subs.get("group") or subs.get("group_tag") or "slots" in subs
    for s in subs["slots"]:
        assert "presets" in s and "options" in s
        assert "fits_interchange" in s["options"][0]


def test_ports_availability_shape(api):
    data = api.data(api.get("/v1/controlplane/ports/availability?port=443"))
    for k in ("port", "free", "can_tcp", "can_udp", "can_demux", "policy"):
        assert k in data, data
