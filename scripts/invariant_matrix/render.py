#!/usr/bin/env python3
"""Render sing-box server/client configs from controlplane preset JSON templates."""
from __future__ import annotations

import base64
import copy
import json
import os
import re
import secrets
import subprocess
from pathlib import Path
from typing import Any

FIXED_UUID = "b831381d-6324-4d53-ad4f-8cda48b30811"
FIXED_PASSWORD = "matrix-pass-1"
SERVER_DNS = "inv-server"
TLS_SNI = "matrix.local"
REALITY_SNI = "www.microsoft.com"
REALITY_HANDSHAKE_SERVER = "inv-handshake"
REALITY_HANDSHAKE_PORT = 443
REALITY_SHORT_ID = "0123456789abcdef"
BASE_PORT = 12000
USER_NAME = "u1"

LEFTOVER_RE = re.compile(r"\{\{[^{}]+\}\}")

# scripts/invariant_matrix -> ../../internal/controlplane/presets/data
HERE = Path(__file__).resolve().parent
DEFAULT_PRESETS = HERE / ".." / ".." / "internal" / "controlplane" / "presets" / "data"
DEFAULT_LX_BIN = Path(os.environ.get("LX_BIN", r"c:\Users\qwerty\git\sui\.tools\lx-client\sing-box"))
IMAGE = os.environ.get("INVMATRIX_IMAGE", "sui-lx-iperf:local")

PARAM_DEFAULTS: dict[str, str] = {
    "jls_addr": "www.cloudflare.com:443",
    "jls_server_name": "www.cloudflare.com",
    "handshake_server": "www.microsoft.com",
    "ws_host": TLS_SNI,
    "ws_path": "/ws",
    "hu_host": TLS_SNI,
    "hu_path": "/upgrade",
    "http_host": TLS_SNI,
    "http_path": "/http",
    "masquerade_url": "https://www.cloudflare.com",
    "masquerade_dir": "/work/masquerade",
    "realm_server_url": "",
    "realm_id": "",
    "fallback": "http://127.0.0.1:80",
    "obfs_host": "www.bing.com",
    "path": "/derp",
    "server_version": "SSH-2.0-OpenSSH_8.9",
    "traffic_pattern": "",
    "httpmask_path": "/sudoku",
    "httpmask_host": TLS_SNI,
    "room": "matrix-room",
    "token": "matrix-token-placeholder",
}


def docker_path(p: Path) -> str:
    return str(p.resolve()).replace("\\", "/")


def load_preset(presets_root: Path, tag: str) -> dict[str, Any]:
    for path in presets_root.rglob(f"{tag}.json"):
        if path.name == "protocol.json" or path.parent.name == "_schema":
            continue
        data = json.loads(path.read_text(encoding="utf-8"))
        if data.get("tag") == tag:
            return data
    raise FileNotFoundError(f"preset tag not found: {tag} under {presets_root}")


def rand_hex(n_bytes: int = 16) -> str:
    return secrets.token_hex(n_bytes)


def rand_b64(n_bytes: int = 16) -> str:
    return base64.b64encode(secrets.token_bytes(n_bytes)).decode("ascii")


def clone(obj: Any) -> Any:
    return copy.deepcopy(obj)


def substitute(obj: Any, vars_map: dict[str, str]) -> Any:
    raw = json.dumps(obj, ensure_ascii=False)
    for k, v in vars_map.items():
        esc = json.dumps(v, ensure_ascii=False)
        # JSON string contents without surrounding quotes
        raw = raw.replace(k, esc[1:-1])
    leftover = LEFTOVER_RE.findall(raw)
    if leftover:
        raise ValueError(f"unresolved template tokens: {sorted(set(leftover))}")
    return json.loads(raw)


def param_defaults_from_notes(notes: dict[str, Any] | None) -> dict[str, str]:
    out: dict[str, str] = {}
    if not notes:
        return out
    mapping = {
        "ws_path_default": "ws_path",
        "hu_path_default": "hu_path",
        "http_path_default": "http_path",
    }
    for note_key, param_key in mapping.items():
        v = notes.get(note_key)
        if isinstance(v, str) and v.strip():
            out[param_key] = v.strip()
    return out


def build_vars(
    *,
    tag: str,
    listen: str,
    port: int,
    server_host: str,
    server_name: str,
    user_creds: dict[str, str],
    peer_secrets: dict[str, str],
    params: dict[str, str],
) -> dict[str, str]:
    vars_map = {
        "{{tag}}": tag,
        "{{listen}}": listen,
        "{{listen_port}}": str(port),
        "{{server}}": server_name,
        "{{user.name}}": USER_NAME,
    }
    for k, v in user_creds.items():
        vars_map[f"{{{{user.{k}}}}}"] = v
    for k in ("password", "uuid", "username", "auth_str", "userkey", "private_key", "public_key", "device_id", "secret", "key"):
        vars_map.setdefault(f"{{{{user.{k}}}}}", "")
    for k, v in peer_secrets.items():
        vars_map[f"{{{{peer.{k}}}}}"] = v
    for k, v in params.items():
        vars_map[f"{{{{param.{k}}}}}"] = v.replace("{{server}}", server_name)
    # outbound uses {{server}} as dial host — override after SNI fill
    vars_map["{{server}}"] = server_host
    # re-apply SNI-specific via separate keys already baked into params where needed
    return vars_map


def generate_peer_secrets(preset: dict[str, Any]) -> dict[str, str]:
    fields = preset.get("peer_secret_fields") or {}
    out: dict[str, str] = {}
    for field, gen in fields.items():
        g = str(gen)
        if g == "ss2022_32":
            out[field] = rand_b64(32)
        elif g == "ss2022_16":
            out[field] = rand_b64(16)
        elif field in ("hy_auth", "obfs_password", "psk") or g in ("password", "hex"):
            # password peer for SS2022 handled above; plain password → hex unless ss
            if field == "password" and str((preset.get("inbound_template") or {}).get("method") or "").startswith("2022-"):
                out[field] = rand_b64(_ss2022_bytes(preset) or 16)
            elif g == "password" and field != "password":
                out[field] = rand_hex(16)
            elif field == "password":
                out[field] = FIXED_PASSWORD
            else:
                out[field] = rand_hex(16)
        else:
            out[field] = rand_hex(16)
    ib = preset.get("inbound_template") or {}
    method = str(ib.get("method") or "")
    if method.startswith("2022-") and "password" not in out:
        out["password"] = rand_b64(_ss2022_bytes(preset) or 16)
    return out


def _ss2022_bytes(preset: dict[str, Any]) -> int:
    method = str((preset.get("inbound_template") or {}).get("method") or "")
    if not method.startswith("2022-"):
        return 0
    gens = preset.get("cred_generators") or {}
    peer_gens = preset.get("peer_secret_fields") or {}
    for src in (gens, peer_gens):
        g = str(src.get("password") or "")
        if g == "ss2022_32":
            return 32
        if g == "ss2022_16":
            return 16
    if "aes-256" in method or "chacha" in method:
        return 32
    return 16


def generate_user_creds(preset: dict[str, Any], peer: dict[str, str]) -> dict[str, str]:
    protocol = str(preset.get("protocol") or "")
    fields = list(preset.get("cred_fields") or [])
    method = str((preset.get("inbound_template") or {}).get("method") or "")
    creds: dict[str, str] = {}
    ss_n = _ss2022_bytes(preset)

    if "uuid" in fields or protocol in ("vless", "vmess", "tuic"):
        creds["uuid"] = FIXED_UUID
    if "password" in fields or protocol in ("trojan", "anytls", "hysteria2", "shadowsocks", "shadowtls", "tuic"):
        if ss_n:
            creds["password"] = rand_b64(ss_n)
        else:
            creds["password"] = FIXED_PASSWORD
    if "auth_str" in fields or protocol == "hysteria":
        creds["auth_str"] = FIXED_PASSWORD
    if "userkey" in fields or protocol == "snell":
        creds["userkey"] = FIXED_PASSWORD
    if "username" in fields or protocol in ("http", "socks", "mixed", "naive", "ssh", "shadowquic", "trusttunnel"):
        creds["username"] = USER_NAME
        if "password" not in creds:
            creds["password"] = FIXED_PASSWORD
    for f in fields:
        if f not in creds:
            if "uuid" in f:
                creds[f] = FIXED_UUID
            elif f == "password" and ss_n:
                creds[f] = rand_b64(ss_n)
            else:
                creds[f] = FIXED_PASSWORD
    _ = peer
    _ = method
    return creds


def inbound_user_entry(protocol: str, creds: dict[str, str]) -> dict[str, Any]:
    if protocol == "ssh":
        entry: dict[str, Any] = {"user": creds.get("username") or USER_NAME}
        if "password" in creds:
            entry["password"] = creds["password"]
        if "public_key" in creds:
            entry["public_key"] = creds["public_key"]
        return entry
    if protocol == "derp":
        return {
            "name": USER_NAME,
            "public_key": creds.get("public_key") or "",
        }
    if protocol in ("socks", "http", "naive", "mixed", "shadowquic", "trusttunnel"):
        return {
            "username": creds.get("username") or USER_NAME,
            "password": creds.get("password") or FIXED_PASSWORD,
        }
    if protocol == "hysteria":
        return {"name": USER_NAME, "auth_str": creds.get("auth_str") or FIXED_PASSWORD}
    if protocol == "snell":
        return {"name": USER_NAME, "userkey": creds.get("userkey") or FIXED_PASSWORD}
    if protocol == "mieru":
        return {"name": USER_NAME, "password": creds.get("password") or FIXED_PASSWORD}
    # vless/vmess/trojan/hy2/tuic/anytls/ss/shadowtls/...
    entry = {"name": USER_NAME}
    for k, v in creds.items():
        if k.startswith("uuid_") and k != "uuid":
            continue
        entry[k] = v
    # vless: uuid only, no flow
    if protocol == "vless":
        return {"name": USER_NAME, "uuid": creds.get("uuid") or FIXED_UUID}
    if protocol == "vmess":
        return {"name": USER_NAME, "uuid": creds.get("uuid") or FIXED_UUID}
    if protocol == "trojan":
        return {"name": USER_NAME, "password": creds.get("password") or FIXED_PASSWORD}
    if protocol == "tuic":
        return {
            "name": USER_NAME,
            "uuid": creds.get("uuid") or FIXED_UUID,
            "password": creds.get("password") or FIXED_PASSWORD,
        }
    if protocol == "hysteria2":
        return {"name": USER_NAME, "password": creds.get("password") or FIXED_PASSWORD}
    if protocol == "anytls":
        return {"name": USER_NAME, "password": creds.get("password") or FIXED_PASSWORD}
    if protocol == "shadowsocks":
        return {"name": USER_NAME, "password": creds.get("password") or FIXED_PASSWORD}
    if protocol == "shadowtls":
        return {"name": USER_NAME, "password": creds.get("password") or FIXED_PASSWORD}
    return entry


def traits_of(preset: dict[str, Any]) -> set[str]:
    return {str(t) for t in (preset.get("traits") or [])}


def needs_reality(preset: dict[str, Any]) -> bool:
    return "reality" in traits_of(preset) or bool((preset.get("requirements") or {}).get("reality_assignment"))


def needs_tls_certs(preset: dict[str, Any]) -> bool:
    if needs_reality(preset):
        return False
    tr = traits_of(preset)
    if "tls" in tr:
        return True
    return bool((preset.get("requirements") or {}).get("tls_profile"))


def attach_inbound_tls(ib: dict[str, Any], cert_path: str, key_path: str, server_name: str) -> None:
    tls = ib.get("tls")
    if not isinstance(tls, dict):
        tls = {}
    tls["enabled"] = True
    tls["server_name"] = server_name
    for k in ("certificate_provider", "certificate", "key", "reality"):
        tls.pop(k, None)
    tls["certificate_path"] = cert_path
    tls["key_path"] = key_path
    ib["tls"] = tls


def attach_outbound_tls_insecure(
    ob: dict[str, Any],
    server_name: str,
    *,
    cert_in_container: str | None = None,
    cert_host: Path | None = None,
) -> None:
    tls = ob.get("tls")
    if not isinstance(tls, dict):
        tls = {"enabled": True}
    tls["enabled"] = True
    tls["server_name"] = server_name
    tls.pop("reality", None)
    # NaiveProxy rejects insecure=true; pin the matrix CA/cert instead.
    # Also rejects alpn / utls / many other TLS knobs.
    # Client container only mounts clients/<tag>, so embed PEM (not certificate_path).
    if str(ob.get("type") or "") == "naive":
        tls.pop("insecure", None)
        tls.pop("utls", None)
        tls.pop("alpn", None)
        tls.pop("certificate_path", None)
        if cert_host is not None and cert_host.is_file():
            pem = cert_host.read_text(encoding="utf-8").strip()
            tls["certificate"] = pem.splitlines()
        elif cert_in_container:
            tls["certificate_path"] = cert_in_container
    else:
        tls["insecure"] = True
    # uTLS is incompatible with QUIC / hysteria V2Ray transports.
    tr = ob.get("transport")
    tr_type = ""
    if isinstance(tr, dict):
        tr_type = str(tr.get("type") or "")
    if tr_type in ("quic", "hysteria"):
        tls.pop("utls", None)
    ob["tls"] = tls


def attach_inbound_trusttunnel_tls(ib: dict[str, Any], cert_host: Path, key_host: Path, server_name: str) -> None:
    # TrustTunnel uses PEM strings, not certificate_path / enabled.
    ib["hostname"] = server_name
    ib["tls"] = {
        "server_name": server_name,
        "certificate": cert_host.read_text(encoding="utf-8"),
        "private_key": key_host.read_text(encoding="utf-8"),
    }


def attach_outbound_trusttunnel_tls(ob: dict[str, Any], server_name: str) -> None:
    ob["hostname"] = server_name
    ob["tls"] = {
        "server_name": server_name,
        "skip_verification": True,
    }


def generate_ssh_ed25519_keypair(lx_bin: Path, image: str = IMAGE) -> tuple[str, str]:
    """Return (private_pem, authorized_keys_line) via host ssh-keygen (OpenSSH)."""
    import tempfile

    _ = lx_bin
    _ = image
    with tempfile.TemporaryDirectory() as td:
        key_path = Path(td) / "matrix_ssh"
        proc = subprocess.run(
            ["ssh-keygen", "-t", "ed25519", "-N", "", "-f", str(key_path), "-q"],
            capture_output=True,
            text=True,
            timeout=30,
            check=False,
        )
        if proc.returncode != 0 or not key_path.is_file():
            err = (proc.stdout or "") + (proc.stderr or "")
            raise RuntimeError(f"ssh-keygen failed: {err}")
        priv = key_path.read_text(encoding="utf-8")
        pub = (key_path.with_suffix(key_path.suffix + ".pub")).read_text(encoding="utf-8").strip()
        # Windows OpenSSH writes path.pub not path + .pub for files without extension...
        pub_path = Path(str(key_path) + ".pub")
        if pub_path.is_file():
            pub = pub_path.read_text(encoding="utf-8").strip()
        return priv if priv.endswith("\n") else priv + "\n", pub


def ensure_ssh_creds(
    preset: dict[str, Any],
    creds: dict[str, str],
    *,
    lx_bin: Path,
    image: str,
) -> None:
    gens = preset.get("cred_generators") or {}
    fields = list(preset.get("cred_fields") or [])
    need = str(gens.get("private_key") or "") == "ssh_ed25519" or (
        str(preset.get("protocol") or "") == "ssh" and "private_key" in fields
    )
    if not need:
        return
    priv, pub = generate_ssh_ed25519_keypair(lx_bin, image=image)
    creds["private_key"] = priv
    creds["public_key"] = pub


def ensure_curve25519_creds(
    preset: dict[str, Any],
    peer: dict[str, str],
    creds: dict[str, str],
    *,
    lx_bin: Path,
    image: str,
) -> None:
    """Replace placeholder curve25519 fields with wg-keypair material (RawURL)."""
    peer_fields = preset.get("peer_secret_fields") or {}
    gens = preset.get("cred_generators") or {}
    need_peer = any(str(g) == "curve25519" for g in peer_fields.values())
    need_user = any(str(gens.get(f) or "") == "curve25519" for f in (preset.get("cred_fields") or []))
    need_user = need_user or (
        str(preset.get("protocol") or "") == "derp"
        and ("private_key" in (preset.get("cred_fields") or []) or "public_key" in (preset.get("cred_fields") or []))
    )
    if need_peer:
        priv, pub = generate_wg_keypair(lx_bin, image=image)
        if "private_key" in peer_fields:
            peer["private_key"] = priv
        if "public_key" in peer_fields:
            peer["public_key"] = pub
    if need_user:
        priv, pub = generate_wg_keypair(lx_bin, image=image)
        creds["private_key"] = priv
        creds["public_key"] = pub


def attach_inbound_reality(ib: dict[str, Any], private_key: str) -> None:
    tls = ib.get("tls")
    if not isinstance(tls, dict):
        tls = {}
    tls["enabled"] = True
    tls["server_name"] = REALITY_SNI
    for k in ("certificate_path", "key_path", "certificate_provider", "certificate", "key"):
        tls.pop(k, None)
    tls["reality"] = {
        "enabled": True,
        "private_key": private_key,
        "short_id": [REALITY_SHORT_ID],
        "handshake": {
            "server": REALITY_HANDSHAKE_SERVER,
            "server_port": REALITY_HANDSHAKE_PORT,
        },
    }
    ib["tls"] = tls


def attach_outbound_reality(ob: dict[str, Any], public_key: str) -> None:
    tls = ob.get("tls")
    if not isinstance(tls, dict):
        tls = {}
    tls["enabled"] = True
    tls["server_name"] = REALITY_SNI
    tls.pop("insecure", None)
    if "utls" not in tls:
        tls["utls"] = {"enabled": True, "fingerprint": "chrome"}
    tls["reality"] = {
        "enabled": True,
        "public_key": public_key,
        "short_id": REALITY_SHORT_ID,
    }
    ob["tls"] = tls


def drop_empty_optional_fields(obj: dict[str, Any]) -> None:
    for k in ("traffic_pattern", "path", "host", "mode"):
        if k in obj and obj[k] == "":
            del obj[k]


def apply_ss2022_outbound_password(
    ob: dict[str, Any],
    peer: dict[str, str],
    creds: dict[str, str],
    *,
    shared: bool = False,
) -> None:
    method = str(ob.get("method") or "")
    if not method.startswith("2022-"):
        return
    if shared:
        # PSK-only (e.g. chacha2022): never SIP022 combine.
        if peer.get("password"):
            ob["password"] = peer["password"]
        return
    sp = peer.get("password") or ""
    up = creds.get("password") or ""
    if sp and up:
        ob["password"] = f"{sp}:{up}"


def _parse_keypair_output(out: str, label: str) -> tuple[str, str]:
    priv = pub = ""
    for line in out.splitlines():
        line = line.strip()
        low = line.lower()
        if low.startswith("privatekey:"):
            priv = line.split(":", 1)[1].strip()
        elif low.startswith("publickey:"):
            pub = line.split(":", 1)[1].strip()
    if not priv or not pub:
        raise RuntimeError(f"could not parse {label} keypair from: {out}")
    return priv, pub


def _lx_generate(lx_bin: Path, image: str, subcommand: str) -> str:
    if not lx_bin.is_file():
        raise FileNotFoundError(f"lx binary not found: {lx_bin}")
    script = (
        "cp /bin-ro/sing-box /tmp/sing-box && chmod +x /tmp/sing-box && "
        f"/tmp/sing-box generate {subcommand}"
    )
    proc = subprocess.run(
        [
            "docker",
            "run",
            "--rm",
            "-v",
            f"{docker_path(lx_bin)}:/bin-ro/sing-box:ro",
            image,
            "bash",
            "-c",
            script,
        ],
        capture_output=True,
        text=True,
        timeout=60,
        check=False,
    )
    out = (proc.stdout or "") + (proc.stderr or "")
    if proc.returncode != 0:
        raise RuntimeError(f"generate {subcommand} failed: {out}")
    return out


def generate_reality_keypair(lx_bin: Path, image: str = IMAGE) -> tuple[str, str]:
    out = _lx_generate(lx_bin, image, "reality-keypair")
    return _parse_keypair_output(out, "reality")


def generate_wg_keypair(lx_bin: Path, image: str = IMAGE) -> tuple[str, str]:
    out = _lx_generate(lx_bin, image, "wg-keypair")
    return _parse_keypair_output(out, "wg")


def render_cell(
    preset: dict[str, Any],
    *,
    port: int,
    cert_in_container: str,
    key_in_container: str,
    cert_host: Path | None,
    key_host: Path | None,
    reality_keys: tuple[str, str] | None,
    lx_bin: Path,
    image: str = IMAGE,
    server_host: str = SERVER_DNS,
    listen: str = "0.0.0.0",
) -> dict[str, Any]:
    tag = str(preset["tag"])
    protocol = str(preset["protocol"])
    peer = generate_peer_secrets(preset)
    creds = generate_user_creds(preset, peer)
    ensure_curve25519_creds(preset, peer, creds, lx_bin=lx_bin, image=image)
    ensure_ssh_creds(preset, creds, lx_bin=lx_bin, image=image)
    notes = preset.get("client_notes") if isinstance(preset.get("client_notes"), dict) else {}
    params = dict(PARAM_DEFAULTS)
    params.update(param_defaults_from_notes(notes))
    # ShadowTLS handshake must hit a reachable TLS host inside the matrix net.
    if "tls_mimic" in traits_of(preset) or protocol == "shadowtls":
        params["handshake_server"] = REALITY_HANDSHAKE_SERVER
    # For Reality cells, transport hosts should follow Reality SNI where useful
    if needs_reality(preset):
        params["ws_host"] = REALITY_SNI
        params["hu_host"] = REALITY_SNI
        params["http_host"] = REALITY_SNI

    # First pass: substitute with dial host = server_host, but tls server_name needs TLS_SNI.
    # Templates use {{server}} for both dial host and tls.server_name.
    # Strategy: substitute with TLS_SNI / Reality SNI first for templates, then fix dial fields.
    sni = REALITY_SNI if needs_reality(preset) else (TLS_SNI if needs_tls_certs(preset) else server_host)
    vars_map = build_vars(
        tag=tag,
        listen=listen,
        port=port,
        server_host=sni,
        server_name=sni,
        user_creds=creds,
        peer_secrets=peer,
        params=params,
    )
    ib = substitute(clone(preset["inbound_template"]), vars_map)
    ob = substitute(clone(preset["outbound_template"]), vars_map)

    ib["listen"] = listen
    ib["listen_port"] = port
    ib["tag"] = tag
    ob["server"] = server_host
    ob["server_port"] = port
    ob["tag"] = tag

    # Fill users unless shared-key / no_users traits
    tr = traits_of(preset)
    if "no_users" not in tr and "shared_key" not in tr and "shared_auth" not in tr:
        ib["users"] = [inbound_user_entry(protocol, creds)]
    else:
        ib.pop("users", None)

    if needs_reality(preset):
        if not reality_keys:
            raise ValueError(f"{tag}: reality keys required")
        priv, pub = reality_keys
        attach_inbound_reality(ib, priv)
        attach_outbound_reality(ob, pub)
    elif protocol == "trusttunnel":
        if not cert_host or not key_host:
            raise ValueError(f"{tag}: trusttunnel requires host cert/key paths")
        attach_inbound_trusttunnel_tls(ib, cert_host, key_host, TLS_SNI)
        attach_outbound_trusttunnel_tls(ob, TLS_SNI)
    elif needs_tls_certs(preset):
        attach_inbound_tls(ib, cert_in_container, key_in_container, TLS_SNI)
        attach_outbound_tls_insecure(
            ob,
            TLS_SNI,
            cert_in_container=cert_in_container,
            cert_host=cert_host,
        )
    elif protocol == "shadowtls":
        # Handshake host uses matrix self-signed TLS; skip verify on camouflage TLS.
        tls = ob.get("tls") if isinstance(ob.get("tls"), dict) else {}
        tls["enabled"] = True
        tls["server_name"] = params.get("handshake_server") or REALITY_HANDSHAKE_SERVER
        tls["insecure"] = True
        ob["tls"] = tls
        hs = ib.get("handshake")
        if isinstance(hs, dict):
            hs["server"] = params.get("handshake_server") or REALITY_HANDSHAKE_SERVER
            hs["server_port"] = 443


    apply_ss2022_outbound_password(
        ob,
        peer,
        creds,
        shared=("no_users" in tr or "shared_key" in tr or "shared_auth" in tr),
    )
    drop_empty_optional_fields(ib)
    drop_empty_optional_fields(ob)

    return {
        "tag": tag,
        "protocol": protocol,
        "port": port,
        "inbound": ib,
        "outbound": ob,
        "peer_secrets": peer,
        "user_creds": creds,
        "traits": sorted(tr),
    }


def build_server_config(cells: list[dict[str, Any]]) -> dict[str, Any]:
    return {
        "log": {"level": "warn"},
        "inbounds": [c["inbound"] for c in cells],
        "outbounds": [{"type": "direct", "tag": "direct"}],
    }


def build_client_config(
    outbound: dict[str, Any],
    iperf_ip: str,
    *,
    traits: list[str] | None = None,
    iperf_mode: str = "tcp",
) -> dict[str, Any]:
    tag = outbound.get("tag") or "proxy"
    ob = clone(outbound)
    ob["tag"] = tag
    _ = traits
    inbound: dict[str, Any] = {
        "type": "direct",
        "tag": "iperf-in",
        "listen": "127.0.0.1",
        "listen_port": 15201,
        "override_address": iperf_ip,
        "override_port": 5201,
        # Payload probe: TCP by default. Transport may be QUIC/UDP underneath.
        "network": ["udp"] if iperf_mode == "udp" else ["tcp"],
    }
    return {
        "log": {"level": "warn"},
        "inbounds": [inbound],
        "outbounds": [ob, {"type": "direct", "tag": "direct"}],
        "route": {"final": tag},
    }


def write_json(path: Path, obj: Any) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(obj, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")


def render_protocol_workdir(
    *,
    tags: list[str],
    workdir: Path,
    presets_root: Path = DEFAULT_PRESETS,
    lx_bin: Path = DEFAULT_LX_BIN,
    base_port: int = BASE_PORT,
    image: str = IMAGE,
    iperf_modes: dict[str, str] | None = None,
) -> list[dict[str, Any]]:
    """Render server.json + clients/<tag>/client.json into workdir. Returns cell metas."""
    from gen_certs import ensure_certs

    certs_dir = workdir / "certs"
    cert, key = ensure_certs(certs_dir)
    cert_in = "/work/certs/server.crt"
    key_in = "/work/certs/server.key"

    # Static files for hy2_masquerade_file (mounted at /work)
    masq = workdir / "masquerade"
    masq.mkdir(parents=True, exist_ok=True)
    (masq / "index.html").write_text("<html><body>matrix</body></html>\n", encoding="utf-8")
    PARAM_DEFAULTS["masquerade_dir"] = "/work/masquerade"

    need_reality = False
    presets: list[dict[str, Any]] = []
    for tag in tags:
        p = load_preset(presets_root, tag)
        presets.append(p)
        if needs_reality(p):
            need_reality = True

    reality_keys = generate_reality_keypair(lx_bin, image=image) if need_reality else None
    modes = iperf_modes or {}

    cells: list[dict[str, Any]] = []
    for i, preset in enumerate(presets):
        port = base_port + i
        cell = render_cell(
            preset,
            port=port,
            cert_in_container=cert_in,
            key_in_container=key_in,
            cert_host=cert,
            key_host=key,
            reality_keys=reality_keys,
            lx_bin=lx_bin,
            image=image,
        )
        cell["iperf_mode"] = modes.get(cell["tag"]) or "tcp"
        cells.append(cell)
        client_dir = workdir / "clients" / cell["tag"]
        # Placeholder IP overwritten by run.py once iperf is up
        write_json(
            client_dir / "client.json",
            build_client_config(
                cell["outbound"],
                "127.0.0.1",
                traits=cell["traits"],
                iperf_mode=str(cell["iperf_mode"]),
            ),
        )
        write_json(
            client_dir / "meta.json",
            {
                "tag": cell["tag"],
                "port": cell["port"],
                "protocol": cell["protocol"],
                "peer_secrets": cell["peer_secrets"],
                "user_creds": cell["user_creds"],
                "traits": cell["traits"],
                "iperf_mode": cell["iperf_mode"],
            },
        )

    write_json(workdir / "server.json", build_server_config(cells))
    write_json(
        workdir / "cells.json",
        [
            {
                "tag": c["tag"],
                "port": c["port"],
                "protocol": c["protocol"],
                "traits": c["traits"],
                "iperf_mode": c["iperf_mode"],
            }
            for c in cells
        ],
    )
    # Keep host paths recorded for debugging
    write_json(
        workdir / "render_info.json",
        {
            "cert": str(cert),
            "key": str(key),
            "lx_bin": str(lx_bin),
            "reality": bool(reality_keys),
        },
    )
    return cells


def rewrite_client_iperf_ip(workdir: Path, tag: str, iperf_ip: str, server_ip: str | None = None) -> Path:
    meta_cells = {c["tag"]: c for c in json.loads((workdir / "cells.json").read_text(encoding="utf-8"))}
    if tag not in meta_cells:
        raise KeyError(tag)
    # Reload outbound from existing client.json and rewrite override
    client_path = workdir / "clients" / tag / "client.json"
    doc = json.loads(client_path.read_text(encoding="utf-8"))
    for ib in doc.get("inbounds") or []:
        if ib.get("tag") == "iperf-in" or ib.get("type") == "direct":
            ib["override_address"] = iperf_ip
            ib["override_port"] = 5201
    if server_ip:
        for ob in doc.get("outbounds") or []:
            if ob.get("type") == "direct":
                continue
            # mieru UDP underlay cannot resolve Docker DNS names
            if "server" in ob:
                ob["server"] = server_ip
    write_json(client_path, doc)
    return client_path


if __name__ == "__main__":
    import argparse

    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--tags", nargs="+", required=True)
    ap.add_argument("--workdir", type=Path, required=True)
    ap.add_argument("--presets", type=Path, default=DEFAULT_PRESETS)
    ap.add_argument("--lx-bin", type=Path, default=DEFAULT_LX_BIN)
    ap.add_argument("--base-port", type=int, default=BASE_PORT)
    args = ap.parse_args()
    cells = render_protocol_workdir(
        tags=args.tags,
        workdir=args.workdir,
        presets_root=args.presets,
        lx_bin=args.lx_bin,
        base_port=args.base_port,
    )
    print(json.dumps([{"tag": c["tag"], "port": c["port"]} for c in cells], indent=2))
