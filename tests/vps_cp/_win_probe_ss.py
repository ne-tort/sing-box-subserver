import json
import os
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from client_harness import run_outbound_probe  # noqa: E402
from conftest import CPClient  # noqa: E402

api = CPClient(os.environ["CP_BASE"], os.environ["CP_TOKEN"], verify=False)
u = api.data(api.post("/v1/controlplane/users", {"name": "win-probe-ss2", "enabled": True}, expect=(200, 201)))
tok = u["sub_token"]
r = api.session.get(f"{api.base}/v1/sub/{tok}", params={"set": "e2e-ss"}, verify=False, timeout=60)
print("sub", r.status_code)
sub = r.json()
print("outs", [(o.get("tag"), o.get("type"), o.get("server_port")) for o in sub.get("outbounds") or []])
ob = sub["outbounds"][0]
res = run_outbound_probe(
    ob,
    work_dir=Path("tests/vps_cp/artifacts/client/win-ss"),
    name="cp-win-ss",
    mixed_port=11080,
)
print(json.dumps({k: res[k] for k in ("ok", "tag", "detail")}, ensure_ascii=False, indent=2))
print("logs:\n", res["logs"][-600:])
