# Security policy

`agent-gate` is a single-user, local-only audit tool. It TLS-intercepts your
own outbound HTTPS, persists captured flows to your local disk, and serves a
local dashboard for review. There is no backend, no telemetry, and no
multi-tenant deployment — but that doesn't mean it has no attack surface.

## Reporting a vulnerability

Please report suspected vulnerabilities **privately**, not via a public issue.

- **Preferred:** open a private security advisory at
  <https://github.com/WZ/agent-gate/security/advisories/new>.
- **Alternative:** email **lwz812@gmail.com** with `[agent-gate security]` in
  the subject.

Include enough detail to reproduce: version (`agent-gate version`), platform,
the steps you took, what you observed, and what you expected. A proof-of-concept
script or recording is appreciated but not required.

## What I'll do

This is a personal, single-maintainer project. I'll do my best on the
following timeline:

| Step                          | Target          |
| ----------------------------- | --------------- |
| Acknowledge receipt           | within 72 hours |
| Initial assessment + severity | within 7 days   |
| Patch + coordinated release   | best-effort     |

If a report turns out to be valid and you're willing to be credited, I'll
mention you in the release notes for the fix.

## In scope

- The TLS-intercepting proxy (`internal/proxy`) and its host-list policy logic
  (`internal/{allowlist,denylist,passthrough}`).
- The local CA mint + leaf signer (`internal/ca`) and truststore install
  (`internal/ca/truststore.go`).
- The audit-store writers (`internal/store`) — JSONL + SQLite — and any path
  that could let captured data leak across boundaries.
- The launcher / sandbox jails (`internal/launcher`) — anything that could let
  a child process bypass the proxy capture.
- The dashboard (`internal/dashboard`) — XSS, CSRF on policy mutations,
  anything that opens loopback exposure to non-loopback callers.
- The redactor / secret-detection regexes (`internal/redactor`,
  `internal/secrets`) — false negatives that leak credentials into rendered
  views.

## Out of scope

- Filesystem exfiltration. `agent-gate` audits **network** traffic. If an
  agent reads `.env` and copies it somewhere on disk, that's not visible here.
  This is documented in `CLAUDE.md`'s "What we explicitly don't do" section.
- Cert-pinned upstreams that refuse our MITM cert. We tunnel TCP raw via
  `passthrough.txt` and audit only connection metadata. This is by design.
- Issues that require an attacker who already has full local user-account
  access (the threat model assumes the local user is trusted).
- Reports based purely on outdated dependencies with no demonstrated impact —
  please open a regular issue or PR for those.

## Coordinated disclosure

If you'd like to publish your findings, please give me a reasonable window
(usually 30–90 days from confirmed report) before going public so a fix can
ship first. I'll do the same in the other direction — I won't sit on a
confirmed report indefinitely.
