#!/usr/bin/env python3
"""Generate self-signed TLS material for invariant_matrix (CN/SAN = matrix.local)."""
from __future__ import annotations

import argparse
import shutil
import subprocess
import sys
from pathlib import Path

DEFAULT_CN = "matrix.local"


def ensure_certs(certs_dir: Path, cn: str = DEFAULT_CN, force: bool = False) -> tuple[Path, Path]:
    certs_dir.mkdir(parents=True, exist_ok=True)
    cert = certs_dir / "server.crt"
    key = certs_dir / "server.key"
    if cert.is_file() and key.is_file() and not force and _cert_lifetime_days(cert) > 350:
        # Old matrix certs used 3650d; Cronet rejects long-lived leaves.
        force = True
    if not (cert.is_file() and key.is_file()) or force:
        _write_pair(certs_dir, cert, key, cn, extra_dns=["inv-server", "localhost"])
    # Local Reality handshake target (Docker often cannot reach www.microsoft.com:443).
    hs_cert = certs_dir / "reality-hs.crt"
    hs_key = certs_dir / "reality-hs.key"
    if not (hs_cert.is_file() and hs_key.is_file()) or force:
        _write_pair(
            certs_dir,
            hs_cert,
            hs_key,
            "www.microsoft.com",
            extra_dns=["inv-handshake", "www.microsoft.com"],
            conf_name="openssl-reality-hs.cnf",
        )
    return cert, key


def _cert_lifetime_days(cert: Path) -> int:
    openssl = shutil.which("openssl")
    if not openssl:
        return 0
    try:
        out = subprocess.check_output(
            [openssl, "x509", "-in", str(cert), "-noout", "-dates"],
            text=True,
        )
    except subprocess.CalledProcessError:
        return 0
    # notBefore=... / notAfter=... — approximate via openssl -checkend if needed
    import datetime as _dt

    def parse(line: str) -> _dt.datetime | None:
        # notBefore=Jul 30 11:22:41 2026 GMT
        try:
            raw = line.split("=", 1)[1].strip()
            return _dt.datetime.strptime(raw, "%b %d %H:%M:%S %Y %Z")
        except Exception:
            return None

    before = after = None
    for line in out.splitlines():
        if line.startswith("notBefore="):
            before = parse(line)
        elif line.startswith("notAfter="):
            after = parse(line)
    if before and after:
        return max(0, (after - before).days)
    return 0


def _write_pair(
    certs_dir: Path,
    cert: Path,
    key: Path,
    cn: str,
    *,
    extra_dns: list[str],
    conf_name: str = "openssl.cnf",
) -> None:
    openssl = shutil.which("openssl")
    if not openssl:
        raise RuntimeError("openssl not found on PATH")

    conf = certs_dir / conf_name
    dns_lines = [f"DNS.1 = {cn}"]
    i = 2
    for d in extra_dns:
        if d == cn:
            continue
        dns_lines.append(f"DNS.{i} = {d}")
        i += 1
    conf.write_text(
        "\n".join(
            [
                "[req]",
                "distinguished_name = req_distinguished_name",
                "x509_extensions = v3_req",
                "prompt = no",
                "[req_distinguished_name]",
                f"CN = {cn}",
                "[v3_req]",
                "basicConstraints = critical, CA:TRUE, pathlen:0",
                "keyUsage = critical, digitalSignature, keyEncipherment, keyCertSign",
                "extendedKeyUsage = serverAuth",
                "subjectAltName = @alt_names",
                "[alt_names]",
                *dns_lines,
                "",
            ]
        ),
        encoding="utf-8",
    )
    subprocess.run(
        [
            openssl,
            "req",
            "-x509",
            "-newkey",
            "rsa:2048",
            "-nodes",
            "-keyout",
            str(key),
            "-out",
            str(cert),
            "-days",
            # Chrome/Cronet leaf max ~398d; keep matrix leaves short.
            "100",
            "-config",
            str(conf),
            "-extensions",
            "v3_req",
        ],
        check=True,
        capture_output=True,
        text=True,
    )


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--dir", type=Path, required=True, help="Output certs directory")
    ap.add_argument("--cn", default=DEFAULT_CN)
    ap.add_argument("--force", action="store_true")
    args = ap.parse_args()
    try:
        cert, key = ensure_certs(args.dir, cn=args.cn, force=args.force)
    except Exception as e:
        print(f"gen_certs failed: {e}", file=sys.stderr)
        return 1
    print(f"wrote {cert}")
    print(f"wrote {key}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
