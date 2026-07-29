#!/usr/bin/env python3
"""Smoke: traffic module status endpoint when agent built with with_traffic.

Usage (agent already running with traffic+controlplane tags):
  python scripts/smoke_traffic_status.py --base https://127.0.0.1:8080 --token SECRET

Exits 0 if GET /v1/traffic/status returns enabled=true.
"""

from __future__ import annotations

import argparse
import json
import ssl
import sys
import urllib.error
import urllib.request


def main() -> int:
    p = argparse.ArgumentParser()
    p.add_argument("--base", required=True, help="Agent base URL")
    p.add_argument("--token", required=True, help="Agent bearer token")
    p.add_argument("--insecure", action="store_true", help="Skip TLS verify")
    args = p.parse_args()
    url = args.base.rstrip("/") + "/v1/traffic/status"
    req = urllib.request.Request(url, headers={"Authorization": f"Bearer {args.token}"})
    ctx = None
    if args.insecure:
        ctx = ssl._create_unverified_context()
    try:
        with urllib.request.urlopen(req, context=ctx, timeout=10) as resp:
            body = json.loads(resp.read().decode())
    except urllib.error.HTTPError as e:
        print(f"HTTP {e.code}: {e.read()!r}", file=sys.stderr)
        return 1
    except Exception as e:
        print(f"request failed: {e}", file=sys.stderr)
        return 1
    data = body.get("data") or {}
    if not body.get("ok") or not data.get("enabled"):
        print(f"unexpected: {body}", file=sys.stderr)
        return 1
    print(json.dumps(data, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
