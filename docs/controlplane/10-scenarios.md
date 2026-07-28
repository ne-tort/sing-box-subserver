# 10 — Operator scenarios (controlplane)

Happy-path flows. Assumes binary built with `with_controlplane` (+ `with_demux` if using demux sets).

## A. Standalone edge with two ports

1. Set agent YAML `controlplane.public_host` to the VPS public IP/DNS.
2. `POST /v1/controlplane/users` `{ "name": "alice" }` → save `subscription_url` / `sub_token`.
3. `POST /v1/controlplane/sets` — set `ss-443` with preset `shadowsocks-tcp`, port 443 (no demux).
4. `POST /v1/controlplane/sets` — set `vless-8443` with preset `vless-tcp`, port 8443.
5. Activate both → `config_mode=controlplane`; materialize merges both ports.
6. Client `GET /v1/sub/{token}` → outbounds for both sets.
7. Optional: demux set with `trojan-tcp` + `vless-tcp` behind one port (requires `with_demux` + TLS profile — default self-signed PEM under `data_dir/controlplane/tls`).
8. Optional: `PUT /v1/controlplane/tls` with `acme_domain` / `acme_ip` → materialize emits `certificate_providers` tag `cp-tls` (needs `with_acme` in the binary).


## B. User expiry

1. `PATCH /users/{id}` `{ "expires_at": "<past or soon>" }`.
2. After tick (or immediately on PATCH): user omitted from inbound `users[]`; Apply hot-reloads.
3. Sub fetch → `403` `cp_user_ineligible`.

## C. Panel takes over

1. While CP active, `PUT /v1/config` with panel JSON → `config_mode=direct`; CP `active_sets` cleared; users/sets files **remain** on disk.
2. Or `POST /v1/subscribe` → `config_mode=subscribed`; same CP clear.
3. Re-activate a set later → Claim(controlplane) again and Apply CP materialize.

## D. Deactivate last set

1. Deactivate every active set → `config_mode=idle`; last-good unchanged (box may still serve previous JSON until next Apply from another owner).
