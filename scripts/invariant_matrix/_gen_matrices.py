#!/usr/bin/env python3
"""One-shot generator for matrices/*.yaml (run from invariant_matrix/)."""
from pathlib import Path

import yaml

M = Path(__file__).resolve().parent / "matrices"
M.mkdir(parents=True, exist_ok=True)


def dump(name: str, doc: dict) -> None:
    p = M / f"{name}.yaml"
    p.write_text(yaml.safe_dump(doc, sort_keys=False, allow_unicode=True), encoding="utf-8")
    print("wrote", p)


def main() -> None:
    dump(
        "vless",
        {
            "protocol": "vless",
            "mode": "render",
            "cells": [
                {"tag": "vless_tls", "stage": 1},
                {"tag": "vless_reality", "stage": 1},
                {"tag": "vless_ws_tls", "stage": 1},
                {"tag": "vless_grpc_tls", "stage": 1},
                {"tag": "vless_ws_reality", "stage": 2},
                {"tag": "vless_grpc_reality", "stage": 2},
                {"tag": "vless_http_tls", "stage": 2},
                {"tag": "vless_http_reality", "stage": 2},
                {"tag": "vless_httpupgrade_tls", "stage": 2},
                {"tag": "vless_httpupgrade_reality", "stage": 2},
                {"tag": "vless_tls_mux", "stage": 2},
                {"tag": "vless_quic_tls", "stage": 2},
                {"tag": "vless_hysteria_tls", "stage": 2},
                {"tag": "vless_tcp", "stage": 2},
            ],
        },
    )

    dump(
        "trojan",
        {
            "protocol": "trojan",
            "mode": "render",
            "cells": [
                {"tag": "trojan_tls", "stage": 1},
                {"tag": "trojan_reality", "stage": 1},
                {"tag": "trojan_ws_tls", "stage": 2},
                {"tag": "trojan_ws_reality", "stage": 2},
                {"tag": "trojan_grpc_tls", "stage": 2},
                {"tag": "trojan_grpc_reality", "stage": 2},
                {"tag": "trojan_http_tls", "stage": 2},
                {"tag": "trojan_http_reality", "stage": 2},
                {"tag": "trojan_httpupgrade_tls", "stage": 2},
                {"tag": "trojan_httpupgrade_reality", "stage": 2},
                {"tag": "trojan_tls_mux", "stage": 2},
                {"tag": "trojan_quic_tls", "stage": 2},
                {"tag": "trojan_tls_fallback", "stage": 2},
            ],
        },
    )

    dump(
        "vmess",
        {
            "protocol": "vmess",
            "mode": "render",
            "cells": [
                {"tag": "vmess_tls", "stage": 1},
                {"tag": "vmess_reality", "stage": 1},
                {"tag": "vmess_ws_tls", "stage": 2},
                {"tag": "vmess_ws_reality", "stage": 2},
                {"tag": "vmess_grpc_tls", "stage": 2},
                {"tag": "vmess_grpc_reality", "stage": 2},
                {"tag": "vmess_http_tls", "stage": 2},
                {"tag": "vmess_http_reality", "stage": 2},
                {"tag": "vmess_httpupgrade_tls", "stage": 2},
                {"tag": "vmess_httpupgrade_reality", "stage": 2},
                {"tag": "vmess_tls_mux", "stage": 2},
                {"tag": "vmess_quic_tls", "stage": 2},
                {"tag": "vmess_tcp", "stage": 2},
            ],
        },
    )

    dump(
        "shadowsocks",
        {
            "protocol": "shadowsocks",
            "mode": "render",
            "cells": [
                {"tag": "ss_aes128", "stage": 1},
                {"tag": "ss_2022_aes128", "stage": 1},
                {"tag": "ss_aes128_mux", "stage": 2},
                {"tag": "ss_aes128_uot", "stage": 2},
                {"tag": "ss_aes256", "stage": 2},
                {"tag": "ss_chacha20", "stage": 2},
                {"tag": "ss_2022_aes256", "stage": 2},
                {"tag": "ss_2022_chacha", "stage": 2},
                {"tag": "ss_2022_aes128_mux", "stage": 2},
            ],
        },
    )

    dump(
        "hysteria2",
        {
            "protocol": "hysteria2",
            "mode": "render",
            "cells": [
                {"tag": "hy2", "stage": 1},
                {"tag": "hy2_salamander", "stage": 1},
                {"tag": "hy2_gecko", "stage": 2},
                {"tag": "hy2_gecko_compact", "stage": 2},
                {"tag": "hy2_gecko_masquerade", "stage": 2},
                {"tag": "hy2_masquerade", "stage": 2},
                {"tag": "hy2_masquerade_proxy", "stage": 2},
                {"tag": "hy2_masquerade_file", "stage": 2},
                {"tag": "hy2_realm", "stage": 5},
            ],
        },
    )

    dump(
        "tuic",
        {
            "protocol": "tuic",
            "mode": "render",
            "cells": [
                {"tag": "tuic", "stage": 1},
                {"tag": "tuic_0rtt", "stage": 2},
            ],
        },
    )

    dump(
        "anytls",
        {
            "protocol": "anytls",
            "mode": "render",
            "cells": [
                {"tag": "anytls", "stage": 1},
                {"tag": "anytls_idle", "stage": 2},
            ],
        },
    )

    lx = "../../../sing-box-lx/lx-test"
    for proto, dirname, tags, stage in [
        ("shadowquic", "shadowquic-docker", ["shadowquic_jls", "shadowquic_0rtt", "shadowquic_uot"], 3),
        ("sudoku", "sudoku-docker", ["sudoku_pad", "sudoku_httpmask", "sudoku_aes"], 3),
        ("trusttunnel", "trusttunnel-docker", ["trusttunnel_h2", "trusttunnel_h3", "trusttunnel_auto"], 3),
        ("derp", "derp-docker", ["derp_tls", "derp_ws", "derp_uot"], 3),
    ]:
        dump(
            proto,
            {
                "protocol": proto,
                "mode": "reuse",
                "stage": stage,
                "reuse_dir": f"{lx}/{dirname}",
                "reuse_cmd": ["docker", "compose", "up", "--abort-on-container-exit"],
                "tags": tags,
                "cells": [{"tag": t, "stage": stage} for t in tags],
            },
        )

    dump(
        "mieru",
        {
            "protocol": "mieru",
            "mode": "render",
            "cells": [
                {"tag": "mieru_tcp", "stage": 3},
                {"tag": "mieru_udp", "stage": 3},
            ],
        },
    )

    dump(
        "wireguard",
        {
            "protocol": "wireguard",
            "mode": "reuse",
            "stage": 4,
            "reuse_dir": f"{lx}/awg-matrix-docker",
            "reuse_cmd": ["docker", "compose", "up", "--abort-on-container-exit"],
            "tags": ["wg", "wg_awg2", "wg_awg3"],
            "cells": [
                {"tag": "wg", "stage": 4},
                {"tag": "wg_awg2", "stage": 4},
                {"tag": "wg_awg3", "stage": 4},
            ],
        },
    )

    dump(
        "carrier",
        {
            "protocol": "carrier",
            "mode": "reuse",
            "stage": 4,
            "reuse_dir": f"{lx}/carrier-docker",
            "reuse_cmd": ["docker", "compose", "up", "--abort-on-container-exit"],
            "tags": [
                "carrier_peer_shared",
                "carrier_peer_users",
                "carrier_jitsi_shared",
                "carrier_jitsi_users",
                "carrier_jitsi_sei_shared",
                "carrier_jitsi_sei_users",
            ],
            "cells": [
                {"tag": "carrier_peer_shared", "stage": 4},
                {"tag": "carrier_peer_users", "stage": 4},
                {"tag": "carrier_jitsi_shared", "stage": 4},
                {"tag": "carrier_jitsi_users", "stage": 4},
                {"tag": "carrier_jitsi_sei_shared", "stage": 4},
                {"tag": "carrier_jitsi_sei_users", "stage": 4},
            ],
        },
    )

    dump(
        "hysteria",
        {
            "protocol": "hysteria",
            "mode": "render",
            "cells": [
                {"tag": "hy1", "stage": 5},
                {"tag": "hy1_obfs", "stage": 5},
            ],
        },
    )
    dump(
        "snell",
        {
            "protocol": "snell",
            "mode": "render",
            "cells": [
                {"tag": "snell_v5", "stage": 5},
                {"tag": "snell_v6", "stage": 5},
            ],
        },
    )
    dump(
        "ssh",
        {
            "protocol": "ssh",
            "mode": "render",
            "cells": [
                {"tag": "ssh_password", "stage": 5},
                {"tag": "ssh_uot", "stage": 5},
                {"tag": "ssh_pubkey", "stage": 5},
            ],
        },
    )
    dump(
        "naive",
        {
            "protocol": "naive",
            "mode": "render",
            "cells": [
                {"tag": "naive_tls", "stage": 5},
                {"tag": "naive_quic", "stage": 5},
            ],
        },
    )
    dump(
        "shadowtls",
        {
            "protocol": "shadowtls",
            "mode": "render",
            "cells": [
                {"tag": "shadowtls_v3", "stage": 5},
                {"tag": "shadowtls_v3_wildcard", "stage": 5},
                {"tag": "shadowtls_v3_wildcard_all", "stage": 5},
            ],
        },
    )
    dump(
        "http",
        {
            "protocol": "http",
            "mode": "render",
            "cells": [
                {"tag": "http", "stage": 5},
                {"tag": "http_tls", "stage": 5},
                {"tag": "http_tls_path", "stage": 5},
            ],
        },
    )
    dump(
        "socks",
        {
            "protocol": "socks",
            "mode": "render",
            "cells": [
                {"tag": "socks", "stage": 5},
                {"tag": "socks_uot", "stage": 5},
            ],
        },
    )
    dump(
        "mixed",
        {
            "protocol": "mixed",
            "mode": "render",
            "cells": [
                {"tag": "mixed_auth", "stage": 5},
                {"tag": "mixed_tls", "stage": 5},
            ],
        },
    )
    dump(
        "cloudflared",
        {
            "protocol": "cloudflared",
            "mode": "skip",
            "stage": 5,
            "skip_reason": "requires Cloudflare tunnel token / external network",
            "tags": ["cloudflared_token"],
            "cells": [{"tag": "cloudflared_token", "stage": 5}],
        },
    )
    print("done", len(list(M.glob("*.yaml"))), "manifests")


if __name__ == "__main__":
    main()
