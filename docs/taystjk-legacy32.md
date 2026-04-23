# TaystJK legacy32 image

`taystjk-legacy32` is a 32-bit (i386) TaystJK runtime family published
alongside:

- `taystjk-modern64`
- `openjk-modern64`
- `openjk-legacy32`
- `ybeproxy-legacy32`
- `vanilla-legacy32`

## Scope

- MP dedicated server runtime built from `taysta/TaystJK` (`master`)
- Engine binary: `/opt/jka/engine/taystjkded.i386`
- Image-managed payload: `/opt/jka/engine-payload/taystjk/`
- Default mode in egg: `fs_game=base` (switch to `taystjk` to run the bundled payload)

## Auto-update policy

`taystjk-legacy32` is one of the two TaystJK tracks that receives
scheduled upstream master checks and automatic rebuild/publish when
upstream changes:

- `taystjk-modern64`
- `taystjk-legacy32`

## Base assets are operator-supplied

The image ships no Jedi Academy base PK3s. Operators must provide their
own legally obtained base assets in `/home/container/base`.

## Shared baseline

Like the other runtime families, this image ships:

- `jka-antivpn`
- bundled addon docs/examples/defaults
- `/opt/jka` layout
- runtime manifest (`/opt/jka/runtime.json`)

## Tags

See [`image-strategy.md`](image-strategy.md) for the full policy.
This runtime publishes one canonical mutable tag:

- `taystjk-legacy32`

No extra `latest-*`, SHA/immutable, or release image tags are
published.

