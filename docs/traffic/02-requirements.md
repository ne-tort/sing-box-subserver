# 02 — Requirements (traffic)

Applies when built with `with_traffic`.

## Functional

| ID | Requirement |
|----|-------------|
| FR-T-1 | Attach stats + rate-limit trackers to sing-box router on every box Start. |
| FR-T-2 | Count bytes for inbound / outbound / dataplane_user (`metadata.User`). |
| FR-T-3 | Persist cumulative counters and time-series under `{data_dir}/traffic/`. |
| FR-T-4 | Flush interval configurable (default 10s); retention configurable (default 30d). |
| FR-T-5 | RegisterSubjectManifest / PollSubjectUsage / SetLimits in-process API. |
| FR-T-6 | Management REST `/v1/traffic/*` (agent Bearer). |
| FR-T-7 | With controlplane: bridge maps users → subjects, syncs `traffic_used_bytes`, applies `speed_*` limits. |
| FR-T-8 | Without `with_traffic`: stubs; agent and CP behave as today. |

## Non-goals (v1)

- Clash API / v2ray_api stats servers.
- Billing engine / invoices.
- TUN-mode flow accounting (same limitation as s-ui trackers).
- Shared Go module with s-ui panel (copy/adapt; extract later if needed).

## NFR

- Module disable via build tag must not break CI default tags.
- Flush before box stop so deltas are not lost on Apply swap.
- Disk growth bounded by retention purge.
