# Security Policy

## Reporting a Vulnerability

If you discover a security vulnerability in godiam, please report it
**privately** so it can be addressed before public disclosure.

**Do NOT open a public GitHub issue for security vulnerabilities.**

Instead, please email the maintainer using the email address listed on the
repository owner's [GitHub profile](https://github.com/haoli000). Include:

- A description of the vulnerability
- Steps to reproduce
- Potential impact
- Suggested fix (if any)

You should receive an acknowledgment within 48 hours and a detailed response
within 7 days indicating next steps.

## Supported Versions

Only the latest release on the `main` branch is actively supported with
security fixes.

## Security Considerations

godiam implements the Diameter protocol (RFC 6733), which is typically deployed
in trusted telecom networks. When deploying, keep in mind:

- **TLS**: Always enable TLS for Diameter connections outside trusted networks.
- **Admin API**: The `admin_api` extension binds to `127.0.0.1` by default.
  Do not expose it on public interfaces without authentication.
- **Metrics**: The `prom_metrics` endpoint should be access-controlled in
  production.

## Disclosure Policy

We follow coordinated disclosure. Once a fix is available, we will:

1. Release a patched version
2. Publish a GitHub Security Advisory
3. Credit the reporter (unless they prefer anonymity)
