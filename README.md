# Jedi Academy Pterodactyl (TaystJK)

Pterodactyl Docker image and egg for running a **TaystJK** dedicated server without redistributing copyrighted **Jedi Academy** game assets.

## What this repo contains

- `docker/Dockerfile` — source-built runtime image for TaystJK
- `egg/egg-taystjk-pterodactyl.json` — importable Pterodactyl egg
- `scripts/entrypoint.sh` — runtime preparation and launch helper
- `scripts/install_taystjk.sh` — standalone install helper
- `docs/source-analysis.md` — source-code findings that drove the project design
- `docs/notes.md` — final revision notes

## Key behavior

- Builds the dedicated server from TaystJK source
- Does **not** bundle `assets*.pk3` or other copyrighted base game files
- Supports asset provisioning through `manual`, `url`, or `none`
- Uses `FS_GAME_MOD=taystjk` by default
- Allows switching to manually installed mod folders such as `base`, `japlus`, `japro`, or `mbii`

## Release process

1. Copy release notes from `RELEASE_NOTES_v1.0.0.md` into a GitHub release.
2. Create a tag and release named `v1.0.0` with title `TaystJK Pterodactyl v1.0.0`.
3. (Optional) Publish image to GHCR and update the egg image reference.

## CI / GitHub Actions

This repository includes a workflow at `.github/workflows/ci.yml` that:

- validates shell scripts and egg JSON on push/PR,
- performs a Docker build test on push/PR,
- publishes to GHCR automatically when pushing a tag like `v1.0.0`.
