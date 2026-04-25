# Operator sheet — TaystJK modern64

Short, panel-only crib sheet for the only shipping runtime family,
`taystjk-modern64`. For the full step-by-step Pterodactyl panel
walkthrough, see [`docs/panel-testing.md`](panel-testing.md).

## Shared facts

- Image entrypoint: `/entrypoint.sh`.
- Pterodactyl "Startup Command" field is the literal token
  `--panel-startup`.
- Egg "Stop Command" is `quit`.
- Wings "Done" string flipping the server to **Running** is
  `Resolving master.jkhub.org`.
- `COPYRIGHT_ACKNOWLEDGED=true` must be set before the first start.
- Base assets must be uploaded to `/home/container/base/`
  (`assets0.pk3` … `mp_chars.pk3`).

A start is **godkänd** when the console reaches
`Resolving master.jkhub.org` and the panel state stays **Running** for
≥ 30 s without a restart loop.

---

## taystjk-modern64

- **Egg:** `egg/egg-jka-taystjk-modern64-pterodactyl.json`
- **Image:** `ghcr.io/akiondev/jedi-academy-pterodactyl:taystjk-modern64`
  (or the same tag on Docker Hub).
- **You upload:** base PK3s only. If `TAYSTJK_AUTO_UPDATE_BINARY=false`
  (the default), you must also upload your own `taystjkded.x86_64` (or
  any compatible binary) to `/home/container/`.
- **Image ships:** `taystjkded.x86_64` under `/opt/jka/engine/` and the
  bundled `taystjk/` mod payload under `/opt/jka/engine-payload/`.
  These are only copied into `/home/container/` when the operator opts
  in (see below).
- **Panel variables (the only four):**
  - `COPYRIGHT_ACKNOWLEDGED=true`
  - `EXTRA_STARTUP_ARGS=` (optional)
  - `SERVER_BINARY=./taystjkded.x86_64`
  - `TAYSTJK_AUTO_UPDATE_BINARY=false` (set to `true` to let the image
    overwrite `/home/container/taystjkded.x86_64` on every start)
- **Behavior config:** all other behavior lives in
  `/home/container/config/jka-runtime.json`. The runtime creates this
  file from the shipped template on first start and refreshes
  `jka-runtime.example.json` next to it on every start. The
  user-owned file is never overwritten. Edit it to enable the anti-VPN
  supervisor, the RCON guard, addons, the `chatlogger`/`checkserverstatus`
  helpers, the live-output mirror, debug startup, and the optional
  `server.fs_game`/`server.config`/`server.log_filename` overrides.
- **server.cfg ownership:** the runtime never writes managed cvars
  (`sv_hostname`, `g_motd`, `sv_maxclients`, `g_gametype`, `rconpassword`)
  into `server.cfg` from panel variables. Set them in your own
  `server.cfg`. The runtime reads `net_port` from the config; if it is
  missing it falls back to `server.port_fallback` from
  `jka-runtime.json`.
- **Log markers for godkänd start:**
  `Binary mode  image-managed TaystJK (auto-update enabled)` (when
  auto-update is on) or `Binary mode  manual user-supplied` (default),
  `[ OK ] Bundled TaystJK files found`, TaystJK x86_64 engine banner,
  `Resolving master.jkhub.org`.
- **Most common failures:**
  - `Configured manual server binary taystjkded.x86_64 was not found
    under /home/container.` → set `TAYSTJK_AUTO_UPDATE_BINARY=true` or
    upload your own binary.
  - `Runtime config at /home/container/config/jka-runtime.json is not
    valid JSON` → fix the JSON file (use the `.example.json` next to it
    as a reference).
