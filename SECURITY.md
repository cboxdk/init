# Security Policy

Cbox Init runs as **PID 1** inside containers, often as root, and can start,
stop, and change ownership of files for the processes it supervises. We take its
security seriously and appreciate responsible disclosure.

## Supported Versions

Security fixes are released for the latest minor of the current major version.

| Version | Supported |
|---------|-----------|
| 2.3.x   | ✅        |
| < 2.3   | ❌ (upgrade) |

## Reporting a Vulnerability

**Do not open a public GitHub issue for security vulnerabilities.**

Report privately using either:

- GitHub's [private vulnerability reporting](https://github.com/cboxdk/init/security/advisories/new) (preferred), or
- Email **security@cbox.dk** with the details below.

Please include:

- A description of the issue and its impact (e.g. privilege escalation, RCE,
  info disclosure, denial of service).
- Affected version(s) and configuration (management API, TLS, ACL, hooks,
  scheduler, PUID/PGID remap, etc.).
- Reproduction steps or a proof of concept.
- Any suggested remediation.

## Response Targets

- **Acknowledgement:** within 3 business days.
- **Triage & severity assessment:** within 7 business days.
- **Fix or mitigation plan:** communicated after triage, prioritised by severity.

We will keep you informed throughout and credit you in the release notes unless
you prefer to remain anonymous.

## Scope & Hardening Notes

The following are known, documented trade-offs rather than vulnerabilities, but
we welcome reports that improve them:

- The management API and metrics endpoints are **disabled by default**. When
  enabled, protect them with a bearer token (`api_auth`), an IP ACL, and/or TLS,
  and bind them to a trusted interface. Never expose an unauthenticated API.
- The Unix control socket grants full process control to anyone who can access
  it; keep its group distinct from your application-runtime user.
- When cbox-init remaps ownership (PUID/PGID), point it only at directories you
  trust.

See `docs/features/` and `docs/observability/` for the full hardening guidance.
