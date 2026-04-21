<h1 align="center">Jedi Academy Pterodactyl (TaystJK)</h1>

<p align="center"><strong>Pterodactyl image and egg for TaystJK, with manual game asset handling, lightweight Bash/Python addons, and optional anti-VPN runtime protection.</strong></p>

<p align="center">
  <a href="https://github.com/akiondev/jedi-academy-pterodactyl/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/akiondev/jedi-academy-pterodactyl/ci.yml?branch=main&style=for-the-badge&label=build&logo=githubactions&logoColor=white" alt="Build status" /></a>
  <a href="https://github.com/akiondev/jedi-academy-pterodactyl/stargazers"><img src="https://img.shields.io/github/stars/akiondev/jedi-academy-pterodactyl?style=for-the-badge&label=stars" alt="GitHub stars" /></a>
  <a href="https://github.com/akiondev/jedi-academy-pterodactyl/issues"><img src="https://img.shields.io/github/issues/akiondev/jedi-academy-pterodactyl?style=for-the-badge&label=issues" alt="GitHub issues" /></a>
  <a href="https://github.com/akiondev/jedi-academy-pterodactyl/blob/main/LICENSE"><img src="https://img.shields.io/github/license/akiondev/jedi-academy-pterodactyl?style=for-the-badge&label=license" alt="GPL-3.0 license" /></a>
  <br>
  <a href="https://hub.docker.com/r/akiondev/jedi-academy-pterodactyl"><img src="https://img.shields.io/docker/pulls/akiondev/jedi-academy-pterodactyl?style=for-the-badge&logo=docker&logoColor=white&label=docker%20pulls" alt="Docker pulls" /></a>
  <a href="https://github.com/akiondev/jedi-academy-pterodactyl/pkgs/container/jedi-academy-pterodactyl"><img src="https://img.shields.io/static/v1?label=GHCR&message=package&color=0969da&style=for-the-badge&logo=github&logoColor=white" alt="GHCR package" /></a>
  <a href="https://github.com/akiondev/jedi-academy-pterodactyl#key-behavior"><img src="https://img.shields.io/badge/game_assets-not_included-b22222?style=for-the-badge" alt="Game assets not included" /></a>
</p>

<p align="center">
  <a href="https://github.com/akiondev/jedi-academy-pterodactyl/blob/main/egg/egg-jka-taystjk-modern64-pterodactyl.json"><strong>Download EGG</strong></a>
  &nbsp;•&nbsp;
  <a href="https://github.com/taysta/TaystJK"><strong>TaystJK</strong></a>
  &nbsp;•&nbsp;
  <a href="https://github.com/JACoders/OpenJK"><strong>OpenJK</strong></a>
  &nbsp;•&nbsp;
  <a href="https://jkhub.org"><strong>JKHub.org</strong></a>
  &nbsp;•&nbsp;
  <a href="https://github.com/akiondev/jedi-academy-pterodactyl/issues/new?title=BUG:%20"><strong>Report Bug</strong></a>
  &nbsp;•&nbsp;
  <a href="https://github.com/akiondev/jedi-academy-pterodactyl/issues/new?title=REQUEST:%20"><strong>Request Feature</strong></a>
</p>

Pterodactyl Docker image and egg for running a **TaystJK** dedicated server without redistributing copyrighted **Jedi Academy** game assets.

Built around [TaystJK](https://github.com/taysta/TaystJK) by [taysta](https://github.com/taysta). This repository packages that runtime for Pterodactyl and adds the surrounding image, egg, startup, addon, and admin tooling.

This image is designed to keep the default **`taystjkded.x86_64`** server binary up to date automatically, so you do not need to upload new TaystJK server files manually. If you choose to run a different server binary yourself, that manual binary is left untouched.

License: **GPL-3.0**

<p>
  <a href="SUPPORT.md"><img src="https://img.shields.io/badge/support-view%20guide-f2cc60?style=for-the-badge" alt="Support guide" /></a>
</p>

## What is Pterodactyl?

This repository is meant to be used with Pterodactyl.

[Pterodactyl](https://pterodactyl.io/) is an open-source game server management panel. It runs servers inside Docker containers and gives you a web interface for installing, starting, stopping, and managing them.

In Pterodactyl, an **egg** is a server template. It defines how a server should be installed, configured, and started inside the panel.

This repository provides the Docker image and Pterodactyl egg for running a TaystJK-based Jedi Academy server in that environment.

Official Pterodactyl documentation:
[pterodactyl.io/project/introduction.html](https://pterodactyl.io/project/introduction.html)

If you want the fastest way to get started with Pterodactyl itself, there is also an unofficial installer project:
[pterodactyl-installer/pterodactyl-installer](https://github.com/pterodactyl-installer/pterodactyl-installer)

Warning: this is not an official Pterodactyl installation method. Follow the linked project's instructions carefully; on some systems, you may need to already be logged in as `root`.

![Pterodactyl TaystJK example console](docs/assets/pterodactyl-console-example.png)

## Quick install

1. Import [egg/egg-jka-taystjk-modern64-pterodactyl.json](egg/egg-jka-taystjk-modern64-pterodactyl.json) into Pterodactyl.
2. Create a server with the default image `ghcr.io/akiondev/jedi-academy-pterodactyl:latest` (or pin the runtime explicitly with `:taystjk`). A Docker Hub mirror is published in parallel at `docker.io/akiondev/jedi-academy-pterodactyl` when configured.
3. Set `COPYRIGHT_ACKNOWLEDGED=true`.
4. Add your legally owned Jedi Academy base assets manually into `/home/container/base`.
5. Start the server and verify that `/home/container/base/assets0.pk3` exists.

> **Migration note (image rename)**
> Earlier builds were published as `ghcr.io/akiondev/jedi-academy-taystjk`. The platform image is now published under the Pterodactyl-centered name `ghcr.io/akiondev/jedi-academy-pterodactyl` (and mirrored to Docker Hub as `akiondev/jedi-academy-pterodactyl`). Existing servers can switch by editing the server's docker image to the new path; the underlying TaystJK runtime is identical. The legacy GHCR package will remain reachable for an interim period to avoid breaking existing deployments. See [docs/image-strategy.md](docs/image-strategy.md) for the full image and tag policy.

## Manual alternatives

1. Upload your own dedicated server binary into `/home/container`, then set `SERVER_BINARY` to that file, for example `./openjkded.x86_64`.
2. Upload your own mod folder into `/home/container/<modname>`, then set `FS_GAME_MOD` to that folder name, for example `japlus` or `mbii`.
3. Place the active config file inside that mod folder and set `SERVER_CONFIG` if you are not using `server.cfg`.
4. Start the server. The runtime will launch manual binaries and mod folders if they exist, but it will not install, sync, or manage them for you.

## What this repo contains

- `docker/taystjk-modern64/Dockerfile` — source-built runtime image for TaystJK
- `egg/egg-jka-taystjk-modern64-pterodactyl.json` — importable Pterodactyl egg
- `scripts/entrypoint.sh` — runtime preparation and launch helper
- `scripts/install_taystjk.sh` — standalone install helper
- `cmd/taystjk-antivpn` — Go-based anti-VPN supervisor for runtime join checks
- `docs/addon_readme.md` — compact addon usage guide with quick examples
- `docs/addon_readme_advanced.md` — full addon reference for developers and AI-guided scripting
- `docs/anti-vpn.md` — anti-VPN design, variables, scoring and operating notes
- `docs/operator-sheet.md` — short panel-only crib sheet per legacy32 family
- `docs/panel-testing.md` — full step-by-step Pterodactyl panel walkthrough

## Key behavior

- Builds the dedicated server from TaystJK source
- Tracks TaystJK `master` through image builds, and can automatically rebuild and publish the default image-managed runtime when upstream changes are detected
- Does **not** bundle `assets*.pk3` or other copyrighted base game files
- Requires server owners to provide their own legally owned Jedi Academy base assets manually in `/home/container/base`
- Uses `FS_GAME_MOD=taystjk` by default
- Allows switching to manually installed mod folders such as `base`, `japlus`, `japro`, or `mbii`
- Allows switching to a manually uploaded alternative dedicated server binary through `SERVER_BINARY`
- Supports lightweight runtime addons from `/home/container/addons` using top-level `.sh` and `.py` scripts executed alphabetically before normal startup
- Syncs `ADDON_README.md` and `ADDON_README_ADVANCED.md` automatically into `/home/container/addons/docs`
- Ships a managed `checkserverstatus` helper that is always refreshed from the image and can be run from the Pterodactyl console
- Ships addon examples in `/home/container/addons/examples`, including a Python RCON announcer template that server owners can copy into `/home/container/addons` when they want to enable it
- Optional anti-VPN supervision using online API checks with cache, allowlist, structured logging and weighted decisions

## Managed vs manual paths

This repository is intentionally **TaystJK-first**:

- the Docker image automatically builds and syncs the TaystJK dedicated server runtime
- the default managed dedicated server binary is `taystjkded.*`
- the default managed mod directory is `taystjk`
- when a newer image-managed TaystJK runtime is published, servers using the default `taystjkded.*` path receive the newer binary on the next start that uses the refreshed image

Manual alternatives are still allowed, but they are **not** automatically managed:

- `SERVER_BINARY` may point at a manually uploaded alternative binary under `/home/container`
- `FS_GAME_MOD` may point at a manually uploaded mod directory under `/home/container`
- manual alternatives must already exist and contain their own required files before startup
- manually selected binaries such as `openjkded.x86_64` are left untouched by image-managed TaystJK sync
- only the default `taystjk` path gets automatic mod-directory preparation and default `server.cfg` generation

Practical rule:

- `taystjkded.*` and `taystjk/` are image-managed TaystJK namespaces
- other binaries and mod folders are user-owned namespaces

## Anti-VPN overview

The anti-VPN feature is designed specifically for VPN / hosting / non-residential detection. It does not use offline proxy lists, Tor blocklists, or generic abuse feeds.

- Runtime component: compiled Go binary inside the Docker image
- Detection inputs: `proxycheck.io`, `ipapi.is`, `IPQualityScore`, `IPLocate`, `IPHub`, and optionally `vpnapi.io`
- Runtime behavior: captures join events from live server stdout while also watching the resolved active server log path, caches decisions locally, writes a dedicated audit trail, and can log, broadcast, or block based on score
- Safety defaults: external API failures do not stop server startup and do not hard-block players by themselves

Read [docs/anti-vpn.md](docs/anti-vpn.md) for the full operating guide.

## Addon support overview

This repository also includes a lightweight addon loader for self-hosted Pterodactyl users.

- Addon root: `/home/container/addons`
- Live user addons: only top-level `.sh` and `.py` files in `/home/container/addons` are executed by the addon loader
- Disable suffix: rename a top-level addon script to end with `.disable` if you want to keep it without running it
- Execution order: alphabetical by filename across top-level user-owned addon scripts only
- Runtime behavior: each top-level user addon runs before normal managed server startup and is wrapped in the configured addon timeout
- Safety model: best-effort by default, with optional strict mode and per-addon timeouts
- Built-in addon docs: synced into `/home/container/addons/docs`
- Bundled examples: synced into `/home/container/addons/examples`, kept up to date by the image, and not executed until copied into the top-level addon directory
- Managed defaults: synced into `/home/container/addons/defaults` and handled by dedicated runtime logic rather than the top-level user addon loader
- Managed `checkserverstatus`: refreshed from `/home/container/addons/defaults`, installed into `/home/container/bin`, available from the Pterodactyl console through the runtime bridge, and controlled by `ADDON_CHECKSERVERSTATUS_ENABLED`
- Managed `chatlogger`: refreshed from `/home/container/addons/defaults`, controlled by `ADDON_CHATLOGGER_ENABLED`, follows the resolved active server log path, and writes clean daily player chat logs into `/home/container/chatlogs`
- Managed server settings: the runtime publishes effective values into `/home/container/.runtime/taystjk-effective.env` and selected non-sensitive values into `.json` for addons and admin utilities
- Optional server.cfg overrides: when enabled, non-empty egg override fields can write selected values such as `rconpassword` into the active `server.cfg`; otherwise addons fall back to the current config and runtime defaults
- Runtime image addon baseline: the official image ships `python3`, `pip`, `venv`, `sqlite3`, `curl`, `wget`, `jq`, `git`, `rsync`, `procps`, `tar`, and `unzip`
- Support files: top-level `.md`, `.json`, and `.txt` files are treated as support files and not executed; image-managed docs, examples, and defaults belong in their dedicated subdirectories
- Scope: addons affect only the current server container and are fully owned by the server operator

Read [docs/addon_readme.md](docs/addon_readme.md) for the compact addon guide and [docs/addon_readme_advanced.md](docs/addon_readme_advanced.md) for the full advanced reference.

## Development

### Start here

- Start with [scripts/entrypoint.sh](scripts/entrypoint.sh) if you want to understand the managed runtime flow, startup preparation, addon execution, helper refresh, and server launch path.
- Use [scripts/install_taystjk.sh](scripts/install_taystjk.sh) to understand the standalone install flow and what the egg installer prepares automatically.
- Read [egg/egg-jka-taystjk-modern64-pterodactyl.json](egg/egg-jka-taystjk-modern64-pterodactyl.json) for the panel-facing variable contract and install/startup behavior exposed in Pterodactyl.
- Check [docker/taystjk-modern64/Dockerfile](docker/taystjk-modern64/Dockerfile) for the official runtime image build, source-built TaystJK packaging, and the current addon tool baseline shipped in the image.

### Project areas

- Anti-VPN behavior lives in [cmd/taystjk-antivpn](cmd/taystjk-antivpn) and is documented in [docs/anti-vpn.md](docs/anti-vpn.md).
- Addon authoring is documented in [docs/addon_readme.md](docs/addon_readme.md) and [docs/addon_readme_advanced.md](docs/addon_readme_advanced.md).
- CI behavior lives in [.github/workflows/ci.yml](.github/workflows/ci.yml).
- Scheduled upstream TaystJK master tracking lives in [.github/workflows/upstream-taystjk-master-sync.yml](.github/workflows/upstream-taystjk-master-sync.yml).

### Contribution and security

- Contribution guide: [CONTRIBUTING.md](CONTRIBUTING.md)
- Security policy: [SECURITY.md](SECURITY.md)

### Contributing upstream to TaystJK

- Upstream runtime credit goes to [taysta/TaystJK](https://github.com/taysta/TaystJK).
- If your change belongs to the game runtime itself, it should usually go upstream to TaystJK rather than this repository.
