# Vanilla legacy32 image

`vanilla-legacy32` is a separate runtime family published alongside the
default TaystJK image and the `openjk-modern64` / `openjk-legacy32`
images. It is a **manual runtime family**: the image deliberately does
not build, ship, fetch, auto-update or otherwise distribute the vanilla
Jedi Academy MP dedicated server engine. Operators must upload their
own legally obtained 32-bit (i386) Linux dedicated server binary.

It does **not** replace the TaystJK image, the `openjk-modern64` image
or the `openjk-legacy32` image. It is an additive runtime family for
operators who specifically want to run the stock vanilla Raven engine
and need a clean i386-capable runtime environment for it.

## Scope

- **Manual runtime family.** No engine source build, no engine binary
  baked into the image, no auto-updates of any kind for the engine.
- **MP dedicated server only.** The image is intended for an
  operator-supplied vanilla Jedi Academy MP dedicated server binary
  (commonly named `linuxjampded`, `jampDed.i386` or similar). The
  default `SERVER_BINARY` value in the egg is `./linuxjampded`.
- **Architecture:** 32-bit (i386). The image enables the i386
  architecture in Debian and installs the i386 dynamic loader plus the
  i386 versions of every shared library the stock vanilla engine
  typically links against (`libc6:i386`, `libstdc++6:i386`,
  `libjpeg62-turbo:i386`, `libpng16-16:i386`, `zlib1g:i386`).
- **No image-managed mode payload.** Unlike the OpenJK / TaystJK
  families, the vanilla image does not stage any `jampgame*.so` into
  the live game tree. The mode-defining payload is whatever the
  operator-supplied engine loads from `base/` on disk.
- **Default mode:** `fs_game=base`. Other mod directories are
  user-managed: upload them yourself before startup.

The on-disk layout inside the image is:

- `/opt/jka/engine/` — empty for vanilla; no image-managed engine.
- `/opt/jka/engine-payload/` — empty for vanilla; no image-managed mode
  payload.

## Base assets and engine binary are operator-supplied

The image ships **no Jedi Academy assets** and **no vanilla engine
binary**. Operators must mount or upload:

- A legally obtained 32-bit Linux vanilla dedicated server binary at
  `/home/container/<SERVER_BINARY>` (default `./linuxjampded`).
- Their own legally obtained `base/` PK3s
  (`assets0.pk3`, `assets1.pk3`, `assets2.pk3`, `assets3.pk3`,
  `mp_chars.pk3`, …).

This is a licensing requirement, not a technical one.

## Other mods stay user-managed

Vanilla legacy32 has no image-managed payload at all. Any mod (custom
maps, gameplay mods, alternative `jampgame*.so` builds, etc.) remains
**user-managed**: operators install them into their own server volume
and select them through the usual `fs_game` / mod-loading variables.

## What is the same as the other runtime images

- The **anti-VPN supervisor** (`/usr/local/bin/jka-antivpn`) is built
  and installed identically.
- The **addon loader** under `/opt/jka/bundled-addons/` and the
  operator-provided `addons.d/` drop-in directory work the same way and
  follow the same execution conventions.
- The `/opt/jka/` runtime layout and `runtime.json` manifest model
  (`schema_version: 2`) are shared. The vanilla legacy32 image declares
  its own `image/vanilla-legacy32/runtime.json` so engine and payload
  paths are resolved consistently across all runtime families even when
  the directories are intentionally empty.

## Tags

See [`image-strategy.md`](image-strategy.md) for the full tag policy.
The vanilla legacy32 family currently publishes:

- `vanilla-legacy32` — latest vanilla legacy32 runtime image (mutable).
- `vanilla-legacy32-master-<short_sha>` — pinned to a repository commit
  (immutable). There is no upstream engine commit for this image
  because the engine itself is operator-supplied; the SHA reflects the
  state of the runtime image (Debian base, libraries, anti-VPN
  supervisor, addon loader, runtime helpers).

The `latest`, `taystjk*`, `openjk-modern64*` and `openjk-legacy32*`
tags continue to point at their respective runtimes and are unaffected
by this image.
