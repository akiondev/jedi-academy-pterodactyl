# Pterodactyl panel testing

Practical instructions for verifying that the `taystjk-modern64`
runtime image actually starts in a real Pterodactyl panel.

This document is written to be followed step by step in the panel.

It assumes:

- A working Pterodactyl panel + Wings node with internet egress.
- You have your **own legally obtained** Jedi Academy MP base assets
  (`assets0.pk3`, `assets1.pk3`, `assets2.pk3`, `assets3.pk3`,
  `mp_chars.pk3`, …).
- If `TAYSTJK_AUTO_UPDATE_BINARY=false` (the default), you also have
  your **own** dedicated server binary uploaded to
  `/home/container/`. Set it to `true` if you want the runtime to
  install the image-managed `taystjkded.x86_64` for you.

The image entrypoint is `/entrypoint.sh` and the panel startup
contract is `--panel-startup`. The egg's "Stop Command" is `quit` and
the "Done" string Wings uses to mark the server as **online** is
`Resolving master.jkhub.org`.

---

## Files the installer creates on first install

- `/home/container/base/` (empty — operator uploads PK3s here)
- `/home/container/logs/`
- `/home/container/addons/`, `addons/docs/`, `addons/defaults/`,
  `addons/defaults/events/` (managed default addons + their
  `*.config.json` files; user-owned configs are preserved on update)
- `/home/container/config/jka-runtime.json` (default template; never
  overwritten on subsequent starts)
- `/home/container/config/jka-runtime.example.json` (refreshed on
  every start; safe to diff against your own config)

## Panel variables

The egg exposes only four variables:

- `COPYRIGHT_ACKNOWLEDGED = true` (required; the entrypoint refuses
  to launch otherwise)
- `EXTRA_STARTUP_ARGS` (optional)
- `SERVER_BINARY` (default `./taystjkded.x86_64`)
- `TAYSTJK_AUTO_UPDATE_BINARY` (default `false`)

All other behavior — anti-VPN, RCON guard, addons, event bus,
hostname/MOTD/maxclients/gametype/rconpassword, debug startup,
live-output mirror, `server.fs_game`, `server.config`,
`server.log_filename`, `server.port_fallback`,
`server.sync_managed_taystjk_payload` — is configured in
`/home/container/config/jka-runtime.json`.

## What a godkänd start looks like

In the panel console these lines must appear, in order:

1. `============================================================`
   header with the family / image name.
2. A `SERVER` block with `Mode: Dedicated server`, the configured
   `Mod`, `Config`, `Port`, `Binary`, `Binary mode`, and `Auto-update`.
3. A `CHECKS` block with at least:
   - either `[ OK ] Image-managed runtime binaries synced from image`
     (when `TAYSTJK_AUTO_UPDATE_BINARY=true`) or an explicit
     manual-engine note (when `false`)
   - `[ OK ] Server binary found`
   - `[ OK ] Container home prepared`
4. A `PATHS` block listing the resolved binary, mod, addons,
   runtime-env, server-log, anti-VPN log, chatlog, audit log, and
   cache paths.
5. The TaystJK x86_64 engine banner.
6. `Resolving master.jkhub.org` — at this point Wings flips the
   server state to **Running**.

A start is **godkänd** when the console reaches `Resolving
master.jkhub.org` and the panel state stays **Running** for at least
30 s without entering a restart loop.

## Step-by-step verification

1. Import `egg/egg-jka-taystjk-modern64-pterodactyl.json` into
   Pterodactyl.
2. Create a server using
   `ghcr.io/akiondev/jedi-academy-pterodactyl:taystjk-modern64` (or
   the Docker Hub mirror).
3. Set `COPYRIGHT_ACKNOWLEDGED=true`.
4. Decide whether you want image-managed binary updates:
   - `TAYSTJK_AUTO_UPDATE_BINARY=true`: the image binary is synced
     into `/home/container/taystjkded.x86_64` on every start.
   - `TAYSTJK_AUTO_UPDATE_BINARY=false` (default): upload your own
     binary to `/home/container/` and confirm `SERVER_BINARY` matches
     the file name.
5. Upload your legally owned base assets to `/home/container/base/`.
6. Start the server. The runtime creates
   `/home/container/config/jka-runtime.json` from the shipped template
   on first start.
7. To enable anti-VPN, RCON guard, addons, the chatlogger, etc., edit
   `/home/container/config/jka-runtime.json` and restart the server.
   Provider API keys go into the `anti_vpn.providers.*` keys in that
   file, never into the panel.
8. Verify the console reaches `Resolving master.jkhub.org` and stays
   **Running**.

## Common failure modes

- `COPYRIGHT_ACKNOWLEDGED must be true`: set it in the panel.
- `Configured manual server binary taystjkded.x86_64 was not found
  under /home/container.`: either set
  `TAYSTJK_AUTO_UPDATE_BINARY=true`, or upload your own binary.
- `Runtime config at /home/container/config/jka-runtime.json is not
  valid JSON`: fix the JSON file (use the `.example.json` template as
  a reference).
- `Configured manual TaystJK mod directory taystjk was not found
  under /home/container.`: set
  `server.sync_managed_taystjk_payload=true` in `jka-runtime.json` or
  upload the mod folder yourself.
