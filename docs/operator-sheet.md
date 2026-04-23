# Operator sheet — legacy32 families

Short, panel-only crib sheet for the four shipping legacy32 runtime
families. Canonical runtime order (with vanilla last where ordering
matters):

1. `taystjk-legacy32`
2. `openjk-legacy32`
3. `ybeproxy-legacy32`
4. `vanilla-legacy32`

For full background, error tables and the complete
step-by-step panel walkthrough, see [`docs/panel-testing.md`](panel-testing.md).

All four families share:

- Same image entrypoint (`/entrypoint.sh`).
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

## vanilla-legacy32

- **Egg:** `egg/egg-jka-vanilla-legacy32-pterodactyl.json`
- **Image:** `ghcr.io/akiondev/jedi-academy-pterodactyl:vanilla-legacy32`
  (or the same tag on Docker Hub).
- **You upload:** 32-bit `linuxjampded` to `/home/container/`,
  base PK3s to `/home/container/base/`.
- **Image ships:** nothing executable; vanilla is fully manual.
- **Variables:** `COPYRIGHT_ACKNOWLEDGED=true`,
  `SERVER_BINARY=./linuxjampded`, `FS_GAME_MOD=base`,
  `SERVER_CONFIG=server.cfg`.
- **Log markers for godkänd start:** `Binary mode  manual user-supplied`,
  `[ OK ] Base assets found`, Raven JAmp banner ending at
  `Resolving master.jkhub.org`.
- **Most common failure:** `exec format error` → uploaded a 64-bit
  `linuxjampded` into a 32-bit image.

## openjk-legacy32

- **Egg:** `egg/egg-jka-openjk-legacy32-pterodactyl.json`
- **Image:** `ghcr.io/akiondev/jedi-academy-pterodactyl:openjk-legacy32`
- **You upload:** base PK3s only.
- **Image ships:** `openjkded.i386` (synced into `/home/container/`)
  and `base/jampgamei386.so` (staged into `/home/container/base/`).
- **Variables:** `COPYRIGHT_ACKNOWLEDGED=true`,
  `SERVER_BINARY=./openjkded.i386` (default — leave alone),
  `FS_GAME_MOD=base`, `SERVER_CONFIG=server.cfg`.
- **Log markers for godkänd start:** `Binary mode  image-managed`,
  OpenJK i386 banner, `Sys_LoadDll(jampgamei386)` succeeding from
  `base/`, `Resolving master.jkhub.org`.
- **Most common failure:**
  `Configured image-managed server binary openjkded.i386 was not found in the image-managed runtime`
  → wrong image tag pulled.

## ybeproxy-legacy32

- **Egg:** `egg/egg-jka-ybeproxy-legacy32-pterodactyl.json`
- **Image:** `ghcr.io/akiondev/jedi-academy-pterodactyl:ybeproxy-legacy32`
- **You upload:** 32-bit `linuxjampded` (or any compatible 32-bit
  dedicated server) to `/home/container/`, base PK3s to
  `/home/container/base/`.
- **Image ships:** `base/jampgamei386.so` only (the YBEProxy game
  module). It is staged into `/home/container/base/` on every start.
  YBEProxy ships **no engine binary** — this is intentional, it is a
  game-module proxy.
- **Variables:** `COPYRIGHT_ACKNOWLEDGED=true`,
  `SERVER_BINARY=./linuxjampded` (match whatever 32-bit binary you
  uploaded), `FS_GAME_MOD=base` (only `base` exercises the proxy),
  `SERVER_CONFIG=server.cfg`.
- **Log markers for godkänd start:** `Binary mode  manual user-supplied`,
  engine loads `jampgamei386.so` from `base/` (typically also a
  YBEProxy banner from the module), `Resolving master.jkhub.org`.
- **Most common failure:** the YBEProxy banner is missing → you set
  `FS_GAME_MOD` to something other than `base`, so the
  image-managed `base/jampgamei386.so` was bypassed.

## taystjk-legacy32

- **Egg:** `egg/egg-jka-taystjk-legacy32-pterodactyl.json`
- **Image:** `ghcr.io/akiondev/jedi-academy-pterodactyl:taystjk-legacy32`
  (or the same tag on Docker Hub).
- **You upload:** base PK3s only.
- **Image ships:** `taystjkded.i386` (synced into
  `/home/container/`) and the bundled `taystjk/` mod payload (staged
  into `/home/container/taystjk/`).
- **Variables:** `COPYRIGHT_ACKNOWLEDGED=true`,
  `SERVER_BINARY=./taystjkded.i386` (default — leave alone),
  `FS_GAME_MOD=base` (default; switch to `taystjk` to actually
  exercise the bundled payload), `SERVER_CONFIG=server.cfg`.
- **Log markers for godkänd start:** `Binary mode  image-managed TaystJK`,
  `[ OK ] Bundled TaystJK files found`, TaystJK i386 engine banner,
  `Resolving master.jkhub.org`.
- **Most common failure:** `exec format error` immediately after
  `Launching…` → the Wings host kernel cannot execute i386 ELFs.
  Verify on the node with `file /home/container/taystjkded.i386`
  and try running an i386 binary directly.
