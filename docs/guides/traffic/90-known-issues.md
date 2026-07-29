# 90 — Known issues / backlog

Operator-visible gaps and engineering debt. Implementation details stay in
[`docs/traffic/`](../../traffic/00-index.md); this list is for **ops expectations**
and **future work**.

Status legend: **fixed** (current tree) · **open** · **deferred**.

## Fixed recently

| Item | Notes |
|------|-------|
| Double flush (Service + Bridge) wiped `onlines` | Bridge tick only syncs usage; Service owns flush |
| Flush vs Zero/SetSubjectUsage race (counter resurrect) | Serialized via `flushMu` |
| Dead empty loop in `ReplaceSubjects` | Removed |
| Auto-discover subjects beside CP (double counting risk) | Auto cleared while controlplane manifest present |
| Rate Read/Write up/down orientation | Inbound Read=upload, Write=download |
| 1 MiB burst hid shaping on small transfers | Burst clamped 16–64 KiB |
| Manual PUT bare CP name missed VLESS variants | `SetManualLimits` expands bare → `name-*` keys |
| `SetSubjectUsage` skewed bare dataplane key | Absolute on `SeriesSubject`; `SubjectUsage` takes max(keys, series) |
| `SetInboundCaps` dead API | Removed |
| Empty VLESS/Trojan `users:[]` | Inert `cp-inert` user (uuid/password) like socks/http |

## Open (use carefully / investigate)

| Item | Impact | Suggested direction |
|------|--------|---------------------|
| JSONL history after zero/reset | Cumulative 0 but old series rows remain | Optional purge / tombstone |
| Min burst 16 KiB | Very low rates still allow short spike | Documented; optional smaller min |
| Upload throttle after Read | One buffer already accepted (token-bucket on inbound) | Accept or move earlier in stack |

## Deferred

| Item | Notes |
|------|-------|
| Soft quota / separate up vs down quota | Hard total only today |
| WireGuard peer Mbps / IpcGet | Phase 3+ |
| `granularity` query on stats | Not implemented |
| Split Service into Limits/Flush/Query packages | Structure cleanup |
| Concurrent stress tests for flush↔zero | Mutex added; still worth chaos test |

## Smoke notes

- Timed shaping uses HTTP blob via SOCKS→VLESS (iperf has no native SOCKS; iperf kept for dataplane reachability).
- CP smoke may PUT bare names; expansion covers `*-flow-*` keys.
