#!/usr/bin/env python3
"""Fill completeness gaps: binding params + missing distinct invariants."""
from __future__ import annotations

import json
from copy import deepcopy
from pathlib import Path

BASE = Path(__file__).resolve().parents[1] / "internal" / "controlplane" / "presets" / "data"


def load(p: Path) -> dict:
    return json.loads(p.read_text(encoding="utf-8"))


def save(rel: str | Path, obj: dict) -> None:
    p = BASE / rel if not isinstance(rel, Path) else rel
    if not p.is_absolute():
        p = BASE / rel
    p.parent.mkdir(parents=True, exist_ok=True)
    p.write_text(json.dumps(obj, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print("wrote", p.relative_to(BASE))


def patch(path: Path, fn) -> None:
    obj = load(path)
    fn(obj)
    save(path, obj)


def ensure_params(obj: dict, fields: list[str]) -> None:
    # Optional operator overrides — not always required (defaults applied in materialize).
    notes = obj.setdefault("client_notes", {})
    notes["params"] = "optional bindings[].params overrides: " + ", ".join(fields)


# --- patch existing ---

def patch_shadowquic() -> None:
    for name in ("shadowquic_jls.json", "shadowquic_0rtt.json"):
        p = BASE / "shadowquic" / name

        def f(obj, _name=name):
            ensure_params(obj, ["jls_addr", "jls_server_name"])
            ib = obj["inbound_template"]
            jls = ib.setdefault("jls_upstream", {})
            jls["addr"] = "{{param.jls_addr}}"
            jls["server_name"] = "{{param.jls_server_name}}"
            if "quic_version_probe" not in jls and "jls" in _name:
                jls["quic_version_probe"] = True
            ob = obj["outbound_template"]
            ob["server_name"] = "{{param.jls_server_name}}"
            ob["sni"] = "{{param.jls_server_name}}"

        patch(p, f)


def patch_shadowtls() -> None:
    for name in ("shadowtls_v3.json", "shadowtls_v3_wildcard.json", "shadowtls_v3_wildcard_all.json"):
        p = BASE / "shadowtls" / name
        if not p.exists():
            continue

        def f(obj):
            ensure_params(obj, ["handshake_server"])
            ib = obj["inbound_template"]
            hs = ib.setdefault("handshake", {})
            hs["server"] = "{{param.handshake_server}}"
            hs.setdefault("server_port", 443)
            ob = obj["outbound_template"]
            tls = ob.setdefault("tls", {})
            tls["server_name"] = "{{param.handshake_server}}"

        patch(p, f)


def patch_ws_hosts() -> None:
    for proto in ("vless", "vmess", "trojan"):
        for p in (BASE / proto).glob("*ws*.json"):

            def f(obj):
                ensure_params(obj, ["ws_host", "ws_path"])
                for side in ("inbound_template", "outbound_template"):
                    tr = obj.get(side, {}).get("transport")
                    if not isinstance(tr, dict) or tr.get("type") != "ws":
                        continue
                    path = tr.get("path") or "/ws"
                    tr["path"] = "{{param.ws_path}}"
                    # stash default via client_notes; materialize fills defaults
                    notes = obj.setdefault("client_notes", {})
                    notes.setdefault("ws_path_default", path if path and not str(path).startswith("{{") else "/ws")
                    headers = tr.setdefault("headers", {})
                    headers["Host"] = ["{{param.ws_host}}"]

            patch(p, f)


def patch_httpupgrade() -> None:
    for proto in ("vless", "vmess", "trojan"):
        for p in (BASE / proto).glob("*httpupgrade*.json"):

            def f(obj):
                ensure_params(obj, ["hu_host", "hu_path"])
                for side in ("inbound_template", "outbound_template"):
                    tr = obj.get(side, {}).get("transport")
                    if not isinstance(tr, dict) or tr.get("type") != "httpupgrade":
                        continue
                    old_path = tr.get("path") or "/upgrade"
                    tr["path"] = "{{param.hu_path}}"
                    tr["host"] = "{{param.hu_host}}"
                    obj.setdefault("client_notes", {})["hu_path_default"] = (
                        old_path if not str(old_path).startswith("{{") else "/upgrade"
                    )

            patch(p, f)


def patch_http_transport() -> None:
    for proto in ("vless", "vmess", "trojan"):
        for p in (BASE / proto).glob("*http_*.json"):
            if "httpupgrade" in p.name:
                continue

            def f(obj):
                ensure_params(obj, ["http_host", "http_path"])
                for side in ("inbound_template", "outbound_template"):
                    tr = obj.get(side, {}).get("transport")
                    if not isinstance(tr, dict) or tr.get("type") != "http":
                        continue
                    old_path = tr.get("path") or "/http"
                    tr["path"] = "{{param.http_path}}"
                    tr["host"] = ["{{param.http_host}}"]
                    obj.setdefault("client_notes", {})["http_path_default"] = (
                        old_path if not str(old_path).startswith("{{") else "/http"
                    )

            patch(p, f)


def patch_hy2_masquerade_proxy() -> None:
    p = BASE / "hysteria2" / "hy2_masquerade_proxy.json"

    def f(obj):
        ensure_params(obj, ["masquerade_url"])
        m = obj["inbound_template"]["masquerade"]
        m["url"] = "{{param.masquerade_url}}"

    patch(p, f)


def patch_sudoku() -> None:
    for p in (BASE / "sudoku").glob("sudoku_*.json"):

        def f(obj):
            ensure_params(obj, ["fallback"])
            ib = obj["inbound_template"]
            if "fallback" in ib:
                ib["fallback"] = "{{param.fallback}}"

        patch(p, f)


def patch_snell() -> None:
    p = BASE / "snell" / "snell_v5.json"

    def f(obj):
        ensure_params(obj, ["obfs_host"])
        # put host on inbound too so outJson can copy
        obj["inbound_template"]["obfs_host"] = "{{param.obfs_host}}"
        obj["outbound_template"]["obfs_host"] = "{{param.obfs_host}}"

    patch(p, f)


def patch_derp() -> None:
    for p in (BASE / "derp").glob("derp_*.json"):

        def f(obj):
            ensure_params(obj, ["path"])
            obj["inbound_template"]["path"] = "{{param.path}}"
            obj["outbound_template"]["path"] = "{{param.path}}"

        patch(p, f)


def patch_ssh() -> None:
    for p in (BASE / "ssh").glob("ssh_*.json"):

        def f(obj):
            ensure_params(obj, ["server_version"])
            obj["inbound_template"]["server_version"] = "{{param.server_version}}"
            if "client_version" in obj.get("outbound_template", {}):
                obj["outbound_template"]["client_version"] = "{{param.server_version}}"

        patch(p, f)


def patch_mieru() -> None:
    for p in (BASE / "mieru").glob("mieru_*.json"):

        def f(obj):
            ensure_params(obj, ["traffic_pattern"])
            # inbound accepts traffic_pattern; outbound too
            obj["inbound_template"]["traffic_pattern"] = "{{param.traffic_pattern}}"
            obj["outbound_template"]["traffic_pattern"] = "{{param.traffic_pattern}}"

        patch(p, f)


# --- new presets ---

LIFECYCLE = {
    "enabled": True,
    "keepalive": True,
    "liveness_interval": "15s",
    "liveness_timeout": "5s",
    "liveness_failures": 3,
    "reconnect_backoff": "2s",
    "reconnect_backoff_max": "30s",
}


def add_shadowquic_uot() -> None:
    base = load(BASE / "shadowquic" / "shadowquic_jls.json")
    obj = deepcopy(base)
    obj["tag"] = "shadowquic_uot"
    obj["aliases"] = ["shadowquic-uot"]
    obj["short_name"] = "SQ UoS"
    obj["i18n"] = {
        "ru": {
            "title": "ShadowQUIC UDP-over-stream",
            "description": "ShadowQUIC JLS + outbound udp_over_stream (UDP поверх stream).",
        }
    }
    obj["traits"] = ["udp", "quic", "jls", "uot"]
    obj["outbound_template"]["udp_over_stream"] = True
    obj["inbound_template"]["jls_upstream"] = {
        "addr": "{{param.jls_addr}}",
        "server_name": "{{param.jls_server_name}}",
        "quic_version_probe": True,
    }
    obj["outbound_template"]["server_name"] = "{{param.jls_server_name}}"
    obj["outbound_template"]["sni"] = "{{param.jls_server_name}}"
    ensure_params(obj, ["jls_addr", "jls_server_name"])
    save("shadowquic/shadowquic_uot.json", obj)


def add_trusttunnel_auto() -> None:
    h3 = load(BASE / "trusttunnel" / "trusttunnel_h3.json")
    obj = deepcopy(h3)
    obj["tag"] = "trusttunnel_auto"
    obj["aliases"] = ["trusttunnel-auto"]
    obj["short_name"] = "TT auto"
    obj["i18n"] = {
        "ru": {
            "title": "TrustTunnel auto",
            "description": "TrustTunnel upstream_protocol=auto (H3→H2 fallback) + ICMP off.",
        }
    }
    obj["traits"] = ["tcp", "udp", "quic", "tls_custom", "http3", "auto"]
    for side in ("inbound_template", "outbound_template"):
        tr = obj[side]["transport"]
        tr["upstream_protocol"] = "auto"
        tr["enable_protocol_fallback"] = True
        tr["protocol_fallback_delay"] = "1s"
    save("trusttunnel/trusttunnel_auto.json", obj)


def add_carrier_jitsi_sei() -> None:
    for auth in ("shared", "users"):
        src = load(BASE / "carrier" / f"carrier_jitsi_{auth}.json")
        obj = deepcopy(src)
        obj["tag"] = f"carrier_jitsi_sei_{auth}"
        obj["aliases"] = [f"carrier-jitsi-sei-{auth}"]
        obj["short_name"] = f"Jitsi SEI {auth[:1]}"
        obj["i18n"] = {
            "ru": {
                "title": f"Carrier Jitsi SEI ({auth})",
                "description": "Jitsi SFU transport=seichannel; params.room обязателен.",
            }
        }
        obj["traits"] = [t for t in obj["traits"] if t != "jitsi"] + ["jitsi", "seichannel"]
        for side in ("inbound_template", "outbound_template"):
            obj[side]["link"]["transport"] = "seichannel"
        save(f"carrier/carrier_jitsi_sei_{auth}.json", obj)


def add_hy2_masquerade_file() -> None:
    src = load(BASE / "hysteria2" / "hy2_masquerade_proxy.json")
    obj = deepcopy(src)
    obj["tag"] = "hy2_masquerade_file"
    obj["aliases"] = ["hysteria2-masquerade-file"]
    obj["short_name"] = "Hy2 maskF"
    obj["i18n"] = {
        "ru": {
            "title": "Hysteria2 Masquerade File",
            "description": "Hy2 masquerade.type=file; params.masquerade_dir.",
        }
    }
    obj["inbound_template"]["masquerade"] = {
        "type": "file",
        "directory": "{{param.masquerade_dir}}",
    }
    obj["param_fields"] = ["masquerade_dir"]
    obj["client_notes"] = {"masquerade": "inbound-only file root", "params": "bindings[].params.masquerade_dir"}
    save("hysteria2/hy2_masquerade_file.json", obj)


def add_hy2_realm() -> None:
    hy = load(BASE / "hysteria2" / "hy2.json")
    obj = deepcopy(hy)
    obj["tag"] = "hy2_realm"
    obj["aliases"] = ["hysteria2-realm"]
    obj["short_name"] = "Hy2 realm"
    obj["i18n"] = {
        "ru": {
            "title": "Hysteria2 Realm",
            "description": "Hy2 inbound realm{} (server_url/realm_id/stun) + peer token.",
        }
    }
    obj["traits"] = ["udp", "quic", "tls", "realm"]
    obj["peer_secret_fields"] = {"realm_token": "password"}
    obj["param_fields"] = ["realm_server_url", "realm_id"]
    obj["inbound_template"]["realm"] = {
        "server_url": "{{param.realm_server_url}}",
        "token": "{{peer.realm_token}}",
        "realm_id": "{{param.realm_id}}",
        "stun_servers": ["stun.l.google.com:19302"],
    }
    obj["outbound_template"]["realm"] = {
        "server_url": "{{param.realm_server_url}}",
        "token": "{{peer.realm_token}}",
        "realm_id": "{{param.realm_id}}",
        "stun_servers": ["stun.l.google.com:19302"],
    }
    obj["client_notes"] = {
        "realm": "params.realm_server_url + realm_id; peer_secrets realm_token",
    }
    save("hysteria2/hy2_realm.json", obj)


def add_quic_transports() -> None:
    for proto, creds, variants, profiles in (
        ("vless", ["uuid"], ["flow-none"], ["pkt-xudp"]),
        ("vmess", ["uuid"], None, ["sec-auto"]),
        ("trojan", ["password"], None, None),
    ):
        tag = f"{proto}_quic_tls"
        ib = {
            "type": proto,
            "tag": "{{tag}}",
            "listen": "{{listen}}",
            "listen_port": 0,
            "users": [],
            "tls": {"enabled": True, "server_name": "{{server}}", "alpn": ["h3"]},
            "transport": {"type": "quic"},
        }
        ob = {
            "type": proto,
            "tag": "{{tag}}",
            "server": "{{server}}",
            "server_port": 0,
            "tls": {
                "enabled": True,
                "server_name": "{{server}}",
                "alpn": ["h3"],
                "utls": {"enabled": True, "fingerprint": "chrome"},
            },
            "transport": {"type": "quic"},
        }
        if proto == "vless":
            ob["uuid"] = "{{user.uuid}}"
            ob["packet_encoding"] = "xudp"
        elif proto == "vmess":
            ob["uuid"] = "{{user.uuid}}"
            ob["security"] = "auto"
        else:
            ob["password"] = "{{user.password}}"
        obj = {
            "schema_version": 1,
            "tag": tag,
            "protocol": proto,
            "aliases": [tag.replace("_", "-")],
            "short_name": f"{proto.upper() if proto != 'trojan' else 'Trojan'} QUIC",
            "status": "lab",
            "i18n": {
                "ru": {
                    "title": f"{proto} QUIC TLS",
                    "description": f"{proto} + TLS + transport.type=quic (UDP).",
                }
            },
            "traits": ["udp", "quic", "tls"],
            "demux_hints": {
                "network": ["udp"],
                "looks_like": "quic",
                "alpn": ["h3"],
                "sni_required": True,
                "first_bytes": "QUIC ClientHello",
                "compatible_with_demux": True,
            },
            "scores": {"dpi": 7, "speed": 8, "mobile": 5, "setup": 5},
            "cred_fields": creds,
            "inbound_template": ib,
            "outbound_template": ob,
            "requirements": {"tls_profile": True, "udp": True, "quic": True},
        }
        if variants:
            obj["default_user_variants"] = variants
        if profiles:
            obj["default_client_profiles"] = profiles
        save(f"{proto}/{tag}.json", obj)


def main() -> None:
    patch_shadowquic()
    patch_shadowtls()
    patch_ws_hosts()
    patch_httpupgrade()
    patch_http_transport()
    patch_hy2_masquerade_proxy()
    patch_sudoku()
    patch_snell()
    patch_derp()
    patch_ssh()
    patch_mieru()
    add_shadowquic_uot()
    # sudoku httpmask stream/ws: lx v1 requires CDN tunnel — skip
    add_trusttunnel_auto()
    add_carrier_jitsi_sei()
    add_hy2_masquerade_file()
    add_hy2_realm()
    add_quic_transports()


if __name__ == "__main__":
    main()
