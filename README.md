<h1 align="center">Jedi Academy Pterodactyl (TaystJK)</h1>

<p align="center"><strong>Production-oriented Pterodactyl image and egg for TaystJK, with asset-safe delivery, lightweight Bash/Python addon loading, and optional anti-VPN runtime protection.</strong></p>

<p align="center">
  <a href="https://github.com/akiondev/jedi-academy-pterodactyl/actions/workflows/ci.yml">
    <img src="https://img.shields.io/github/actions/workflow/status/akiondev/jedi-academy-pterodactyl/ci.yml?branch=main&style=for-the-badge&label=build&logo=githubactions&logoColor=white" alt="Build status" />
  </a>
  <a href="https://github.com/akiondev/jedi-academy-pterodactyl/releases">
    <img src="https://img.shields.io/badge/release-ready_for_tag-2ea043?style=for-the-badge&logo=github&logoColor=white" alt="Release ready for tag" />
  </a>
  <a href="https://github.com/akiondev/jedi-academy-pterodactyl/pkgs/container/jedi-academy-taystjk">
    <img src="https://img.shields.io/badge/ghcr-container-blue?style=for-the-badge&logo=github&logoColor=white" alt="GHCR container" />
  </a>
</p>

<p align="center">
  <a href="https://github.com/akiondev/jedi-academy-pterodactyl/blob/main/docs/anti-vpn.md">
    <img src="https://img.shields.io/badge/anti--vpn-documented-6f42c1?style=for-the-badge" alt="Anti-VPN documented" />
  </a>
  <a href="https://github.com/akiondev/jedi-academy-pterodactyl/blob/main/egg/egg-taystjk-pterodactyl.json">
    <img src="https://img.shields.io/static/v1?label=pterodactyl&message=egg%20ready&color=1f6feb&style=for-the-badge" alt="Pterodactyl egg ready" />
  </a>
  <a href="https://github.com/akiondev/jedi-academy-pterodactyl#key-behavior">
    <img src="https://img.shields.io/badge/game_assets-not_included-b22222?style=for-the-badge" alt="Game assets not included" />
  </a>
</p>

Pterodactyl Docker image and egg for running a **TaystJK** dedicated server without redistributing copyrighted **Jedi Academy** game assets.

## What this repo contains

- `docker/Dockerfile` — source-built runtime image for TaystJK
- `egg/egg-taystjk-pterodactyl.json` — importable Pterodactyl egg
- `scripts/entrypoint.sh` — runtime preparation and launch helper
- `scripts/install_taystjk.sh` — standalone install helper
- `cmd/taystjk-antivpn` — Go-based anti-VPN supervisor for runtime join checks
- `docs/addon_readme.md` — compact addon usage guide with quick examples
- `docs/source-analysis.md` — source-code findings that drove the project design
- `docs/notes.md` — final revision notes
- `docs/anti-vpn.md` — anti-VPN design, variables, scoring and operating notes

## Key behavior

- Builds the dedicated server from TaystJK source
- Does **not** bundle `assets*.pk3` or other copyrighted base game files
- Supports asset provisioning through `manual`, `url`, or `none`
- Uses `FS_GAME_MOD=taystjk` by default
- Allows switching to manually installed mod folders such as `base`, `japlus`, `japro`, or `mbii`
- Allows switching to a manually uploaded alternative dedicated server binary through `SERVER_BINARY`
- Supports lightweight runtime addons from `/home/container/addons` using top-level `.sh` and `.py` scripts executed alphabetically before normal startup
- Syncs `ADDON_README.md` automatically into `/home/container/addons/docs`
- Ships a managed `checkserverstatus` helper that is always refreshed from the image and can be run from the Pterodactyl console
- Ships addon examples in `/home/container/addons/examples`, including a Python RCON announcer template that server owners can copy into `/home/container/addons` when they want to enable it
- Ships a stronger addon baseline in the runtime image with `python3`, `pip`, `venv`, `sqlite3`, `curl`, `wget`, `jq`, `git`, `rsync`, `procps`, `tar`, and `unzip`
- Optional anti-VPN supervision using online API checks with cache, allowlist, structured logging and weighted decisions

## Managed vs manual paths

This repository is intentionally **TaystJK-first**:

- the Docker image automatically builds and syncs the TaystJK dedicated server runtime
- the default managed dedicated server binary is `taystjkded.*`
- the default managed mod directory is `taystjk`

Manual alternatives are still allowed, but they are **not** automatically managed:

- `SERVER_BINARY` may point at a manually uploaded alternative binary under `/home/container`
- `FS_GAME_MOD` may point at a manually uploaded mod directory under `/home/container`
- manual alternatives must already exist and contain their own required files before startup
- only the default `taystjk` path gets automatic mod-directory preparation and default `server.cfg` generation

Practical rule:

- `taystjkded.*` and `taystjk/` are image-managed TaystJK namespaces
- other binaries and mod folders are user-owned namespaces

## Release process

1. Copy release notes from `RELEASE_NOTES_v1.0.0.md` into a GitHub release.
2. Create a tag and release named `v1.0.0` with title `TaystJK Pterodactyl v1.0.0`.
3. (Optional) Publish image to GHCR and update the egg image reference.

## CI / GitHub Actions

This repository includes a workflow at `.github/workflows/ci.yml` that:

- validates the Go anti-VPN component with `go test ./...`,
- validates shell scripts and egg JSON on push/PR,
- performs a Docker build test on push/PR,
- publishes to GHCR automatically when pushing a tag like `v1.0.0`.

## Anti-VPN overview

The anti-VPN feature is designed specifically for VPN / hosting / non-residential detection. It does not use offline proxy lists, Tor blocklists, or generic abuse feeds.

- Runtime component: compiled Go binary inside the Docker image
- Detection inputs: `proxycheck.io`, `ipapi.is`, `IPQualityScore`, `IPLocate`, `IPHub`, and optionally `vpnapi.io`
- Runtime behavior: captures join events from live server stdout while also watching `server.log`, caches decisions locally, writes a dedicated audit trail, and can log, broadcast, or block based on score
- Safety defaults: external API failures do not stop server startup and do not hard-block players by themselves

Read [docs/anti-vpn.md](docs/anti-vpn.md) for the full operating guide.

## Addon support overview

This repository also includes a lightweight addon loader for self-hosted Pterodactyl users.

- Addon directory: `/home/container/addons`
- User addons: only top-level `.sh` and `.py` files in `/home/container/addons` are executed by the addon loader
- Built-in addon docs: synced into `/home/container/addons/docs`
- Bundled examples: synced into `/home/container/addons/examples` and kept up to date by the image, but not executed until copied into the top-level addon directory
- Managed defaults: synced into `/home/container/addons/defaults`; the built-in `checkserverstatus` helper is installed from there automatically on managed startup
- Managed server settings: the runtime can publish effective server values into `/home/container/.runtime/taystjk-effective.env` and selected non-sensitive values into `.json` for addons and admin utilities
- Optional server.cfg overrides: when enabled, non-empty egg override fields can write selected values such as `rconpassword` into the active `server.cfg`; otherwise addons fall back to the current config and runtime defaults
- Supported file types: `.sh` for Bash and `.py` for Python 3
- Support files: `.md`, `.json`, and `.txt` are ignored if they are placed beside top-level addon scripts, but the recommended place for image-managed docs and examples is the dedicated `docs/`, `examples/`, and `defaults/` subdirectories
- Execution order: alphabetical by filename across top-level user-owned addon scripts only
- Runtime behavior: each addon is executed before normal managed server startup
- Safety model: best-effort by default, with optional strict mode and per-addon timeouts
- Scope: addons affect only the current server container and are fully owned by the server operator

Read [docs/addon_readme.md](docs/addon_readme.md) for the compact addon guide.
