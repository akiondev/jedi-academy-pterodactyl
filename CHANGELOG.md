# Changelog

All notable changes to the **jedi-academy-pterodactyl** project are documented
here. Each release also has a dedicated notes file under [`releases/`](releases/).

This project follows [Semantic Versioning](https://semver.org/). While the
version is below `1.0.0`, minor bumps (`0.x.0`) may include breaking changes
and patch bumps (`0.x.y`) are backwards-compatible fixes.

---

## [v0.2.0](releases/v0.2.0.md) (beta)

**Breaking** — Docker image renamed from `jedi-academy-taystjk` to
`jedi-academy-pterodactyl`.

Highlights:

- Image available on both GHCR and Docker Hub (opt-in)
- Egg lists four selectable images (GHCR + Docker Hub × latest/taystjk)
- Anti-VPN hardening (userinfo parsing, name sanitization, connection events)
- Chatlogger addon improvements (runtime log path, timestamp parsing, self-tests)
- CI workflow optimizations and safe upstream TaystJK sync
- Added CONTRIBUTING.md, SECURITY.md, SUPPORT.md
- Expanded README with quick-start links and console screenshot

## [v0.1.0](releases/v0.1.0.md) (alpha)

First public snapshot — proof-of-concept release.

- Source-built Docker image for TaystJK dedicated server
- Importable Pterodactyl egg
- Runtime entrypoint and installation helper scripts
