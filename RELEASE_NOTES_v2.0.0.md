# v2.0.0

## Highlights

This release recenters the project's public identity around Pterodactyl, introduces an explicit tag policy, adds optional Docker Hub mirroring, and ships a series of runtime hardening improvements across the anti-VPN supervisor, the chatlogger addon, and the CI pipeline.

---

## Breaking changes

### Image renamed

The Docker image has been renamed from `ghcr.io/akiondev/jedi-academy-taystjk` to `ghcr.io/akiondev/jedi-academy-pterodactyl`.

The TaystJK runtime inside the image is **unchanged**. To migrate an existing Pterodactyl server, edit the server's docker image field in the panel:

```
Before: ghcr.io/akiondev/jedi-academy-taystjk:<tag>
After:  ghcr.io/akiondev/jedi-academy-pterodactyl:<tag>
```

The legacy GHCR package remains reachable for an interim period and is not deleted.

---

## What's new

### Pterodactyl-centered image identity

- Repository identity, GHCR package, and Docker Hub repository all share the name `jedi-academy-pterodactyl`
- The egg's `docker_images` list now exposes four selectable entries: GHCR `latest`, GHCR `taystjk`, Docker Hub `latest`, and Docker Hub `taystjk`
- The first entry (`ghcr.io/akiondev/jedi-academy-pterodactyl:latest`) is the default

### Explicit tag policy

| Tag | Meaning | Stability |
|-----|---------|-----------|
| `latest` | Current platform default (TaystJK master) | Mutable |
| `taystjk` | Latest TaystJK master build | Mutable |
| `taystjk-master-<short_sha>` | TaystJK master pinned to an upstream commit | Immutable |
| `v<semver>` | Repository release tag | Immutable |

Operators that want to be insulated from a future runtime change should use an explicit `taystjk*` tag rather than `latest`.

### Optional Docker Hub mirror

The image can now be published to `docker.io/akiondev/jedi-academy-pterodactyl` in addition to GHCR. This is opt-in: set the `PUBLISH_DOCKERHUB` repository variable and configure the `DOCKERHUB_USERNAME` / `DOCKERHUB_TOKEN` secrets. GHCR-only deployments are not affected.

### Anti-VPN supervisor hardening

- Improved userinfo key parsing to handle edge-case client strings
- Fixed trailing color code normalization in connect log lines
- Hardened broadcast player name sanitization
- Improved connection event parsing for multi-part log lines

### Chatlogger addon improvements

- Added runtime server log path support — the chatlogger now follows the resolved active log path instead of a fixed path, matching the entrypoint's own log path resolution
- Hardened `sleep` calls and extended the self-test to cover the generic fallback path
- Fixed timestamp parsing and prefix handling

### CI and workflow improvements

- Hardened and optimized Docker image build workflows
- Fixed TaystJK commit-based Docker builds for edge-case upstream states
- Safer upstream TaystJK master sync with improved error handling

---

## Migration

1. In your Pterodactyl panel, update each server's **Docker image** field to `ghcr.io/akiondev/jedi-academy-pterodactyl:latest` (or `:taystjk` to pin the runtime explicitly).
2. Restart the server. No server data or configuration changes are required.

---

## Important

This project does **not** redistribute copyrighted Jedi Academy game assets. Users must provide their own legally owned base game files.
