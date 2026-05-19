# Jedi Outcast / JK2MV runtime

This document describes the **Jedi Outcast / JK2MV** Pterodactyl runtime
shipped by this repository. It is fully isolated from the
[TaystJK Jedi Academy runtime](../README.md) - no scripts, env vars, Docker
images, or eggs are shared between them.

## Summary

| Item | Value |
|------|-------|
| Upstream | [`mvdevs/jk2mv` v1.4.1](https://github.com/mvdevs/jk2mv/releases/tag/1.4.1) |
| Engine zip | `jk2mv-v1.4.1-dedicated.zip` |
| Binary used | `linux-amd64/jk2mvded` |
| Docker target | `docker/jk2mv/Dockerfile` |
| Image tag | `ghcr.io/akiondev/jedi-academy-pterodactyl:jk2mv` |
| Default port | `28070` |
| Default config | `/home/container/base/server.cfg` |
| Pterodactyl egg | `egg/egg-jk2mv-pterodactyl.json` |
| Runtime manifest | `image/jk2mv/runtime.json` |
| Entrypoint | `scripts/jk2mv/entrypoint.sh` |
| Start command | `scripts/jk2mv/start_jk2mv.sh` |

## Image layout

The JK2MV image is built from `debian:12-slim` and contains only the
minimum tools needed to fetch, unpack and run JK2MV:

```
/opt/jk2mv/
├── jk2mvded                # linux-amd64 dedicated server binary
├── base/
│   ├── assetsmv.pk3        # JK2MV's own (free) base assets
│   └── assetsmv2.pk3
├── runtime.json            # see image/jk2mv/runtime.json
└── .upstream-version       # JK2MV upstream version pinned at build time
/entrypoint.sh              # scripts/jk2mv/entrypoint.sh
/usr/local/bin/start_jk2mv.sh
```

The image runs as the unprivileged `container` user with
`HOME=/home/container` and `WORKDIR=/home/container`, matching
Pterodactyl's runtime expectations.

## Asset policy

The runtime ships **only** JK2MV's own free assets
(`assetsmv.pk3`, `assetsmv2.pk3`). The operator must upload their own
legally owned Raven Jedi Outcast base assets to
`/home/container/base/`:

| Asset | Required for `JK2MV_SERVER_VERSION=1.04` |
|-------|------------------------------------------|
| `assets0.pk3` | Yes (always required) |
| `assets1.pk3` | Yes (always required) |
| `assets2.pk3` | Yes |
| `assets5.pk3` | Yes |

For `1.02`, `1.03` and `auto` the runtime starts with only `assets0.pk3`
and `assets1.pk3`, but warns when `assets2.pk3` / `assets5.pk3` are
missing (some clients will fail to connect).

The runtime never overwrites operator-uploaded assets:
* `assetsmv.pk3` / `assetsmv2.pk3` are copied with `cp -n`.
* `assets[0125].pk3` are never touched.

## Environment variables

All JK2MV-specific variables use the `JK2MV_*` prefix to keep them
disjoint from the TaystJK runtime. The egg exposes:

| Variable | Default | Purpose |
|----------|---------|---------|
| `COPYRIGHT_ACKNOWLEDGED` | `false` | Must be `true` for the install script to proceed. |
| `SERVER_CONFIG` | `server.cfg` | File under `base/` passed to `+exec`. |
| `JK2MV_SERVER_VERSION` | `1.04` | Allowed: `auto`, `1.02`, `1.03`, `1.04`. |
| `EXTRA_STARTUP_ARGS` | empty | Additional arguments appended to the server command. |
| `SERVER_PORT` | `28070` | Provided by Pterodactyl; passed as `+set net_port`. |

## Start command

The container CMD invokes `start_jk2mv.sh`, which boils down to:

```
./jk2mvded \
    +set dedicated 2 \
    +set net_port "${SERVER_PORT}" \
    +set mv_serverversion "${JK2MV_SERVER_VERSION}" \
    +exec "${SERVER_CONFIG}" \
    ${EXTRA_STARTUP_ARGS}
```

`SERVER_CONFIG` is passed bare (without `base/`) because the Quake3 /
JK2 engine resolves config files relative to the active `fs_game`
directory.

## Isolation from the TaystJK runtime

* No `TAYSTJK_*` or `JKA_*` env vars are read.
* No code under `cmd/` or `internal/` is included in the JK2MV image
  (the Go anti-VPN supervisor is TaystJK-only).
* The TaystJK Dockerfile, entrypoint, runtime manifest and egg are
  unchanged.
* The runtime manifest lives under `image/jk2mv/runtime.json` with its
  own `schema_version: 1` and `runtime: jk2mv`. CI's
  TaystJK-specific schema check only runs against
  `image/taystjk-modern64/runtime.json`.
* CI builds / publishes the JK2MV image as a separate job using its
  own cache scope (`jk2mv-image`).
