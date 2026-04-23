# OpenJK modern64 image

`openjk-modern64` is the **first additional runtime family** published
alongside the default TaystJK image. It is built from the
[JACoders/OpenJK](https://github.com/JACoders/OpenJK) `master` branch and
ships as a separate set of tags under the same platform image name
(`ghcr.io/akiondev/jedi-academy-pterodactyl` and the optional Docker Hub
mirror).

It does **not** replace the TaystJK image. It is an additive runtime
family for operators who specifically want stock OpenJK behaviour.

## Scope

- **MP dedicated server only.** The image runs the multiplayer dedicated
  server. Single-player and the SDL client are out of scope.
- **Engine binary:** `openjkded.x86_64`, copied from the upstream
  `JediAcademy/openjkded.x86_64` build artifact and installed under the
  shared `/opt/jka/engine/` layout.
- **Image-managed mode payload:** `base/jampgamex86_64.so`. This is the
  only mod artifact baked into the image and is staged into the live
  game tree on startup using the same sha256 manifest mechanism as the
  TaystJK image.
- **Default mode:** `fs_game=base`. The server boots into stock
  multiplayer base.

## Base assets are still operator-supplied

The image deliberately does **not** ship any Jedi Academy base game
assets (`assets0.pk3`, `assets1.pk3`, `assets2.pk3`, `assets3.pk3`,
`mp_chars.pk3`, …). Operators must mount or upload their own legally
obtained `base/` PK3s, exactly as with the TaystJK image. This is a
licensing requirement, not a technical one.

## Other mods stay user-managed

Only the stock `base/jampgamex86_64.so` payload is image-managed. Any
other mod (custom maps, gameplay mods, alternative `jampgame*.so`
builds, etc.) remains **user-managed**: operators install them into
their own server volume and select them through the usual
`fs_game` / mod-loading variables. The image does not stage or clean
them up.

## What is the same as the TaystJK image

- The **anti-VPN supervisor** (`/usr/local/bin/jka-antivpn`) is built
  and installed identically.
- The **addon loader** under `/opt/jka/bundled-addons/` and the
  operator-provided `addons.d/` drop-in directory work the same way and
  follow the same execution conventions.
- The `/opt/jka/` runtime layout and `runtime.json` manifest model
  (`schema_version: 2`) are shared. The OpenJK image declares its own
  `image/openjk-modern64/runtime.json` so engine and payload paths are
  resolved consistently across both runtime families.

## Tags

See [`image-strategy.md`](image-strategy.md) for the full tag policy.
The OpenJK modern64 family publishes:

- `openjk-modern64` — canonical mutable tag for this runtime.

No extra `latest-*`, SHA/immutable, or release image tags are published.
