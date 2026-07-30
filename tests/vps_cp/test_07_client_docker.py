"""Windows/local Docker client probes — optional; prefer test_07_client_remote.py on VPS.

Docker Desktop NAT toward public VPS is flaky for VLESS/Hy2/Reality.
Set CP_WIN_DOCKER=1 to enable.
"""

from __future__ import annotations

import os
import sys
from pathlib import Path

import pytest

if os.environ.get("CP_WIN_DOCKER", "0") not in ("1", "true", "yes"):
    pytest.skip("set CP_WIN_DOCKER=1 to run local Docker Desktop client probes", allow_module_level=True)

_HERE = Path(__file__).resolve().parent
if str(_HERE) not in sys.path:
    sys.path.insert(0, str(_HERE))

from client_harness import DEFAULT_IMAGE, run_outbound_probe  # noqa: E402

CLIENT_DIR = _HERE / "artifacts" / "client"


def test_client_image_available():
    import subprocess

    r = subprocess.run(["docker", "image", "inspect", DEFAULT_IMAGE], capture_output=True, text=True)
    if r.returncode != 0:
        pull = subprocess.run(["docker", "pull", DEFAULT_IMAGE], capture_output=True, text=True)
        assert pull.returncode == 0, pull.stderr


def test_probe_filtered_preset(api, artifacts_dir):
    user = api.data(
        api.post("/v1/controlplane/users", {"name": "win-docker-ss", "enabled": True}, expect=(200, 201))
    )
    r = api.session.get(
        f"{api.base}/v1/sub/{user['sub_token']}",
        params={"set": "e2e-ss"},
        verify=api.verify,
        timeout=60,
    )
    assert r.status_code == 200
    sub = r.json()
    assert sub.get("outbounds")
    res = run_outbound_probe(
        sub["outbounds"][0],
        work_dir=CLIENT_DIR / "win",
        name="cp-win-docker-ss",
        mixed_port=11080,
    )
    api.dump(artifacts_dir, "win_docker_ss_probe.json", res)
    assert res["ok"], res
