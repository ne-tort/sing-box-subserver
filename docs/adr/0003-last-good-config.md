# ADR 0003 — Last-good config and safe apply

## Status

Accepted

## Context

Naive panel flows stop the core then start a new config; a failed start leaves the node offline with no automatic restore. Edge agents cannot afford that.

Exclusive listen ports prevent a true dual-stack “start new then close old” swap for typical server inbounds.

## Decision

1. Persist **last-good** config + meta on disk (atomic rename).
2. On apply: **validate** (unmarshal) first without binding ports.
3. Write **staged** artifacts.
4. Stop current box → start staged.
5. If start/probe fails → **immediately** start from last-good; return structured error; do not promote staged.
6. If start/probe succeeds → promote staged to last-good; bump revision.
7. On unexpected box death → restart from last-good with backoff.

## Consequences

- Brief dataplane gap during apply (accepted for v1 exclusive binds).
- Failed apply does not leave the node permanently down if last-good exists.
- Process and management API stay up across apply failures.
- Future improvement (SO_REUSEPORT / drain) can reduce the gap without changing the last-good contract.
