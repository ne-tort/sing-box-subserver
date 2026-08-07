# ADR: SSL Profiles only (no legacy TLS/ACME/ECH adapters)
#
# Status: Accepted (v1)
#
# Context: TLS leaf selection was split across `/tls` (self-signed),
# `/cert-manager` (ACME domains), binding `params.sni` / `self_signed_sni` /
# handshake knobs / ECH bank. Operators had to coordinate three APIs.
#
# Decision: First-class SSL Profile (`ssl_profiles.json` + `controlplane/ssl/<id>/`)
# owns leaf + handshake + ECH. Bindings reference `params.ssl_profile` only
# (optional → `default`). Legacy routes and binding params are removed with
# hard rejection — no backward-compat migrate adapters.
#
# Consequences: Materialize emits per-profile `cp-ssl-<id>` ACME providers or
# profile PEM paths. Mgmt HTTPS uses Default SSL profile PEMs. Client hub tile
# is SSL; ECH bank / cert-manager /tls APIs retired.
