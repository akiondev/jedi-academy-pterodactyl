# Pterodactyl panel testing

Practical, family-by-family instructions for verifying that each
runtime image actually starts in a real Pterodactyl panel.

This document is written to be followed step by step in the panel.
It assumes:

- A working Pterodactyl panel + Wings node with internet egress.
- You have your **own legally obtained** Jedi Academy MP base assets
  (`assets0.pk3`, `assets1.pk3`, `assets2.pk3`, `assets3.pk3`,
  `mp_chars.pk3`, …).
- For `vanilla-legacy32` and `ybeproxy-legacy32` you also have your
  **own** legally obtained 32-bit Linux dedicated server binary
  (`linuxjampded`, `openjkded.i386` or equivalent).

All families share the same image entrypoint (`/entrypoint.sh`) and
the same panel startup contract (`--panel-startup`). The egg's
"Stop Command" is `quit` and the "Done" string Wings uses to mark the
server as **online** is `Resolving master.jkhub.org`.

---

## What every family has in common

Files the installer creates in the server volume on first install:

- `/home/container/base/` (empty — operator uploads PK3s here)
- `/home/container/base/server.cfg` (auto-generated default if not
  already present; safe to edit/replace)
- `/home/container/logs/`
- `/home/container/addons/`, `addons/docs/`, `addons/examples/`,
  `addons/defaults/`

Variables you must set in the panel before the **first start**:

- `COPYRIGHT_ACKNOWLEDGED = true` (the entrypoint refuses to launch
  otherwise)

What a **godkänd start** looks like in the panel console for every
family (these lines must all appear, in this order):

1. `============================================================`
   header with the family / image name.
2. A `SERVER` block with `Mode: Dedicated server`, the configured
   `Mod`, `Config`, `Port` and `Binary`.
3. A `CHECKS` block with at least:
   - `[ OK ] Runtime files synced from image`
   - `[ OK ] Server binary found`
   - `[ OK ] Container home prepared`
4. `[ OK ] Base assets found`
5. A `LAUNCH` block ending with
   `Launching configured dedicated server now...` (or
   `…under anti-VPN supervision…` if anti-VPN is on).
6. The dedicated server's own boot output, ending with
   `Resolving master.jkhub.org` — at this point Wings flips the
   server to **Running**.

If steps 1–6 all happen and the server stays "Running" without an
immediate restart loop, the runtime start is **godkänd**.

Common failure signatures (apply to all families):

| Log line | Means |
| --- | --- |
| `[ERROR] COPYRIGHT_ACKNOWLEDGED must be true` | You forgot to set the variable. |
| `[ERROR] Missing /home/container/base/assets0.pk3` | You did not upload base PK3s. |
| `[ERROR] Configured manual server binary … was not found under /home/container` | You did not upload the engine binary, or the filename does not match `SERVER_BINARY`. |
| `[ERROR] Configured manual mod directory … was not found` | `FS_GAME_MOD` points at a folder you have not uploaded. |
| `[ERROR] Configured server config … was not found` | `SERVER_CONFIG` does not match a real file inside the active mod folder. |
| `exec format error` from the engine | Wrong architecture (uploaded a 64-bit binary into a legacy32 image, or vice versa). |
| Container exits within ~1s of `Launching…` | Almost always a missing PK3, wrong binary architecture, or bad `server.cfg`. |

---

## Family: `vanilla-legacy32`

### A. Egg

Import `egg/egg-jka-vanilla-legacy32-pterodactyl.json`. Pterodactyl
will offer two image options (pick one — both are the same image):

- `ghcr.io/akiondev/jedi-academy-pterodactyl:vanilla-legacy32`
- `docker.io/akiondev/jedi-academy-pterodactyl:vanilla-legacy32`

### B. Files you must upload yourself

| Destination in server volume | What |
| --- | --- |
| `/home/container/linuxjampded` | Your legally obtained **32-bit (i386)** Linux vanilla `linuxjampded` (or whatever name matches `SERVER_BINARY`). |
| `/home/container/base/assets0.pk3` | Vanilla base assets. |
| `/home/container/base/assets1.pk3` | Vanilla base assets. |
| `/home/container/base/assets2.pk3` | Vanilla base assets. |
| `/home/container/base/assets3.pk3` | Vanilla base assets. |
| `/home/container/base/mp_chars.pk3` | Vanilla base assets. |

Verify the binary is i386 from the panel **Console → File Manager** or
SSH on the node:

```
file /home/container/linuxjampded
# expected: ELF 32-bit LSB executable, Intel 80386
```

### C. Panel variables

| Variable | Value |
| --- | --- |
| `COPYRIGHT_ACKNOWLEDGED` | `true` |
| `SERVER_BINARY` | `./linuxjampded` (default, change if your file has a different name) |
| `FS_GAME_MOD` | `base` (default) |
| `SERVER_CONFIG` | `server.cfg` (default; the installer auto-creates a stub) |
| Everything else | leave at default |

### D. Startup / default `SERVER_BINARY` / `FS_GAME_MOD`

- Egg's "Startup Command" field is the literal token `--panel-startup`
  (the image's entrypoint builds the real engine command line).
- Default `SERVER_BINARY` = `./linuxjampded`.
- Default `FS_GAME_MOD` = `base`. Vanilla legacy32 has **no
  image-managed mod payload** — `base` is the only sensible default.

### E. Expected log on godkänd start

In addition to the shared lines above, you should see:

- `Binary mode  manual user-supplied` (vanilla never has an
  image-managed engine).
- `[ OK ] Base assets found`
- The Raven engine's own banner (e.g. `JAmp: v1.0.1.0 linux-i386 …`)
  followed by the standard `Hitch warning: …` /
  `------ Server Initialization ------` block, ending with
  `Resolving master.jkhub.org`.

### F. Common failure signatures specific to vanilla

| Log line | Means |
| --- | --- |
| `Configured manual server binary linuxjampded was not found under /home/container` | The binary was not uploaded, or has a different filename than `SERVER_BINARY`. |
| `exec format error` immediately after `Launching…` | Uploaded the 64-bit `linuxjampded` instead of the 32-bit one. |
| `Couldn't load default.cfg` | The base PK3s were not uploaded. |

### G. Definition of godkänd runtime-start

`Resolving master.jkhub.org` reached AND the server stays in the
panel state **Running** for at least 30 seconds without restarting.

### H. Step-by-step panel checklist

- [ ] Import `egg/egg-jka-vanilla-legacy32-pterodactyl.json` into a
      nest in the panel.
- [ ] Create a new server using this egg. Pick the GHCR or Docker Hub
      image listed in the egg.
- [ ] Wait for the installer to finish in the panel **Console**.
- [ ] Open **File Manager** and upload `linuxjampded` to
      `/home/container/`.
- [ ] Upload all base PK3s (`assets0.pk3`, `assets1.pk3`,
      `assets2.pk3`, `assets3.pk3`, `mp_chars.pk3`) to
      `/home/container/base/`.
- [ ] In **Startup**, set `COPYRIGHT_ACKNOWLEDGED = true`. Save.
- [ ] Click **Start**.
- [ ] Wait for the `LAUNCH` block in the console. Watch for
      `Resolving master.jkhub.org`.
- [ ] Confirm the panel state turns **Running** and stays Running.
- [ ] Click **Stop**. The server should exit cleanly.

---

## Family: `ybeproxy-legacy32`

### A. Egg

Import `egg/egg-jka-ybeproxy-legacy32-pterodactyl.json`. Image
options:

- `ghcr.io/akiondev/jedi-academy-pterodactyl:ybeproxy-legacy32`
- `docker.io/akiondev/jedi-academy-pterodactyl:ybeproxy-legacy32`

### B. Files you must upload yourself

| Destination | What |
| --- | --- |
| `/home/container/linuxjampded` | Your legally obtained **32-bit (i386)** Linux Jedi Academy MP dedicated server binary. (`openjkded.i386` or `taystjkded.i386` also work — set `SERVER_BINARY` to match.) |
| `/home/container/base/assets0.pk3` … `mp_chars.pk3` | Base assets. |

You do **not** need to upload `base/jampgamei386.so` — the YBEProxy
image ships its own at `/opt/jka/engine-payload/base/jampgamei386.so`
and the entrypoint stages it into `/home/container/base/` on every
start.

### C. Panel variables

| Variable | Value |
| --- | --- |
| `COPYRIGHT_ACKNOWLEDGED` | `true` |
| `SERVER_BINARY` | `./linuxjampded` (default; match it to whatever 32-bit binary you uploaded) |
| `FS_GAME_MOD` | `base` (default — only `base` uses the YBEProxy `jampgamei386.so` payload) |
| `SERVER_CONFIG` | `server.cfg` (default) |
| Everything else | leave at default |

### D. Startup / default `SERVER_BINARY` / `FS_GAME_MOD`

- Startup command in the panel: `--panel-startup`.
- Default `SERVER_BINARY` = `./linuxjampded`.
- Default `FS_GAME_MOD` = `base`. YBEProxy is delivered as
  `base/jampgamei386.so`, so any other `fs_game` value bypasses the
  proxy.

### E. Expected log on godkänd start

Same shared lines as above, plus:

- `Binary mode  manual user-supplied`.
- The engine's own boot output should reference
  `jampgamei386.so` being loaded from `base/`. With YBEProxy active
  you typically also see a banner from the YBEProxy module itself
  (string varies by upstream commit).
- Boot ends at `Resolving master.jkhub.org`.

### F. Common failure signatures specific to ybeproxy

| Log line | Means |
| --- | --- |
| The proxy banner is missing | You set `FS_GAME_MOD` to something other than `base`, so the image-managed `base/jampgamei386.so` was never loaded. |
| `Sys_LoadDll(jampgamei386) failed` | A non-`base` mod folder was selected and that mod does not contain its own `jampgamei386.so`. |
| `exec format error` | Wrong architecture engine uploaded. |

### G. Definition of godkänd runtime-start

`Resolving master.jkhub.org` reached AND `base/jampgamei386.so` was
the one loaded by the engine (verify either via the YBEProxy banner
in the log, or by SSH:
`sha256sum /home/container/base/jampgamei386.so` matches
`sha256sum /opt/jka/engine-payload/base/jampgamei386.so` inside the
container).

### H. Step-by-step panel checklist

- [ ] Import `egg/egg-jka-ybeproxy-legacy32-pterodactyl.json`.
- [ ] Create a new server using this egg with one of the listed
      images.
- [ ] Wait for the installer to finish.
- [ ] Upload your 32-bit dedicated server binary to
      `/home/container/` (and update `SERVER_BINARY` if its name is
      not `linuxjampded`).
- [ ] Upload base PK3s to `/home/container/base/`.
- [ ] Set `COPYRIGHT_ACKNOWLEDGED = true`. Save.
- [ ] Click **Start**.
- [ ] Confirm `Resolving master.jkhub.org` appears and the panel
      state goes **Running**.
- [ ] Confirm in the engine's own log that the loaded
      `jampgamei386.so` is the YBEProxy one (banner / sha256 check).
- [ ] Click **Stop**.

---

## Family: `taystjk-legacy32` (experimental)

### A. Egg

Import `egg/egg-jka-taystjk-legacy32-pterodactyl.json`. Image
options:

- `ghcr.io/akiondev/jedi-academy-pterodactyl:taystjk-legacy32`
- `docker.io/akiondev/jedi-academy-pterodactyl:taystjk-legacy32`

This image is **experimental**. It is only published from CI when
either the workflow is run via `workflow_dispatch` or the repo
variable `PUBLISH_TAYSTJK_LEGACY32=true` is set; otherwise the
runtime image is built but not pushed. Verify the tag exists in your
registry before creating the panel server.

### B. Files you must upload yourself

| Destination | What |
| --- | --- |
| `/home/container/base/assets0.pk3` … `mp_chars.pk3` | Base assets. |

You do **not** upload an engine binary: the image ships
`taystjkded.i386` at `/opt/jka/engine/` and the entrypoint syncs it
into `/home/container/` on every start. You also do **not** upload
the `taystjk/` mod directory — it is image-managed and synced from
`/opt/jka/engine-payload/taystjk/`.

### C. Panel variables

| Variable | Value |
| --- | --- |
| `COPYRIGHT_ACKNOWLEDGED` | `true` |
| `SERVER_BINARY` | `./taystjkded.i386` (default — leave alone) |
| `FS_GAME_MOD` | `base` (default), or `taystjk` to actually exercise the bundled TaystJK payload |
| `SERVER_CONFIG` | `server.cfg` (default) |
| Everything else | leave at default |

### D. Startup / default `SERVER_BINARY` / `FS_GAME_MOD`

- Startup command in the panel: `--panel-startup`.
- Default `SERVER_BINARY` = `./taystjkded.i386` (image-managed,
  i386).
- Default `FS_GAME_MOD` = `base`. The image-managed `taystjk/`
  payload is staged into `/home/container/taystjk/` regardless, so
  switching `FS_GAME_MOD` to `taystjk` after the first start is
  enough to test the bundled payload.

### E. Expected log on godkänd start

Same shared lines as above, plus:

- `Binary mode  image-managed TaystJK`.
- `[ OK ] Bundled TaystJK files found`.
- Engine banner from `taystjkded.i386` (TaystJK build string),
  ending at `Resolving master.jkhub.org`.

### F. Common failure signatures specific to taystjk-legacy32

| Log line | Means |
| --- | --- |
| `Configured TaystJK server binary taystjkded.i386 was not found in the image-managed runtime` | You are running the wrong image tag. Re-pull `taystjk-legacy32`. |
| `Managed TaystJK mod directory taystjk was not found in the image-managed runtime` | You set `FS_GAME_MOD=taystjk` against an image where the payload is missing. Repull. |
| `exec format error` immediately after `Launching…` | The node kernel does not support `i386` ELF execution at all. Verify with `uname -m` and that `linux32`/`i386` ELFs run on the host. |

### G. Definition of godkänd runtime-start

`Resolving master.jkhub.org` reached AND the panel reports
**Running** for at least 30 seconds. Then, as a follow-up test,
change `FS_GAME_MOD` to `taystjk` and restart; confirm the engine
log says it loaded `taystjk/jampgamei386.so` (or equivalent).

### H. Step-by-step panel checklist

- [ ] Confirm the registry actually has the
      `taystjk-legacy32` tag (this image is publish-gated).
- [ ] Import `egg/egg-jka-taystjk-legacy32-pterodactyl.json`.
- [ ] Create a new server using this egg.
- [ ] Wait for the installer to finish.
- [ ] Upload base PK3s to `/home/container/base/`.
- [ ] Set `COPYRIGHT_ACKNOWLEDGED = true`. Save.
- [ ] Click **Start**. Confirm `Resolving master.jkhub.org` appears
      and panel state becomes **Running**.
- [ ] Click **Stop**.
- [ ] Optional: change `FS_GAME_MOD` to `taystjk`, **Save**,
      **Start** again, and confirm the engine loads the bundled
      `taystjk/` payload.

---

## Family: `openjk-legacy32`

### A. Egg

Import `egg/egg-jka-openjk-legacy32-pterodactyl.json`. Image options:

- `ghcr.io/akiondev/jedi-academy-pterodactyl:openjk-legacy32`
- `docker.io/akiondev/jedi-academy-pterodactyl:openjk-legacy32`

### B. Files you must upload yourself

| Destination | What |
| --- | --- |
| `/home/container/base/assets0.pk3` … `mp_chars.pk3` | Base assets. |

You do **not** upload an engine binary: the image ships
`openjkded.i386` at `/opt/jka/engine/` and syncs it into
`/home/container/` on every start. You also do **not** upload
`base/jampgamei386.so` — the image stages its own into
`/home/container/base/` from `/opt/jka/engine-payload/base/`.

### C. Panel variables

| Variable | Value |
| --- | --- |
| `COPYRIGHT_ACKNOWLEDGED` | `true` |
| `SERVER_BINARY` | `./openjkded.i386` (default — leave alone) |
| `FS_GAME_MOD` | `base` (default — the image-managed payload is in `base/`) |
| `SERVER_CONFIG` | `server.cfg` (default) |
| Everything else | leave at default |

### D. Startup / default `SERVER_BINARY` / `FS_GAME_MOD`

- Startup command in the panel: `--panel-startup`.
- Default `SERVER_BINARY` = `./openjkded.i386` (image-managed,
  i386).
- Default `FS_GAME_MOD` = `base`. Other mod folders are entirely
  user-managed and bypass the image's `base/jampgamei386.so`.

### E. Expected log on godkänd start

Same shared lines as above, plus:

- `Binary mode  image-managed`.
- The OpenJK engine's banner (e.g. `OpenJK: <git short sha> …`),
  followed by `Sys_LoadDll(jampgamei386)` succeeding from `base/`,
  ending at `Resolving master.jkhub.org`.

### F. Common failure signatures specific to openjk-legacy32

| Log line | Means |
| --- | --- |
| `Configured TaystJK server binary openjkded.i386 was not found in the image-managed runtime` | Wrong image tag pulled. Re-pull `openjk-legacy32`. (The "TaystJK" word in this error message is a known cosmetic legacy label — the actual binary check is correct.) |
| `Sys_LoadDll(jampgamei386) failed` | The image-managed payload was not synced (something deleted `/home/container/base/jampgamei386.so`). Restart to re-stage. |
| `exec format error` | Host kernel cannot execute i386 ELFs. |

### G. Definition of godkänd runtime-start

`Resolving master.jkhub.org` reached AND the panel reports
**Running** for at least 30 seconds AND the engine banner identifies
itself as OpenJK i386 (not vanilla, not TaystJK).

### H. Step-by-step panel checklist

- [ ] Import `egg/egg-jka-openjk-legacy32-pterodactyl.json`.
- [ ] Create a new server using this egg.
- [ ] Wait for the installer to finish.
- [ ] Upload base PK3s to `/home/container/base/`.
- [ ] Set `COPYRIGHT_ACKNOWLEDGED = true`. Save.
- [ ] Click **Start**.
- [ ] Confirm the OpenJK i386 banner appears in the console and that
      `Resolving master.jkhub.org` is reached.
- [ ] Confirm the panel state turns **Running** and stays Running.
- [ ] Click **Stop**.
