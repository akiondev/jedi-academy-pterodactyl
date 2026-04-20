# Image and tag strategy

This document describes the image distribution and tag policy for the
**Jedi Academy Pterodactyl** project. It separates the **platform layer**
(public identity, registry naming) from the **runtime layer** (the actual
server binary distribution being shipped today).

## Platform identity

The project's public identity is Pterodactyl-centered:

- Repository: [`akiondev/jedi-academy-pterodactyl`](https://github.com/akiondev/jedi-academy-pterodactyl)
- GHCR package: `ghcr.io/akiondev/jedi-academy-pterodactyl`
- Docker Hub repository: `docker.io/akiondev/jedi-academy-pterodactyl`

Both registries publish the **same image content** under the **same name**.
Docker Hub publishing is opt-in via the `PUBLISH_DOCKERHUB` repository
variable plus the `DOCKERHUB_USERNAME` and `DOCKERHUB_TOKEN` secrets, so
the GHCR-only path keeps working unchanged when those are not configured.

## Runtime today

TaystJK is the **current default runtime** and is what `latest` points
at. A second runtime family, **`openjk-modern64`**, is now also built
and published from this repository as the first additional runtime
family (built from `JACoders/OpenJK` master, MP dedicated server only).
It is exposed through its own set of tags under the same platform image
name and does not replace TaystJK. See
[`openjk-modern64.md`](openjk-modern64.md) for details. A third runtime
family, **`openjk-legacy32`**, is also published from this repository
(built from `JACoders/OpenJK` master as a 32-bit i386 target, MP
dedicated server only) and is exposed through its own set of tags. See
[`openjk-legacy32.md`](openjk-legacy32.md) for details. A fourth
runtime family, **`vanilla-legacy32`**, is also published as a manual
runtime family: it provides a 32-bit (i386) Linux runtime environment
for an operator-supplied vanilla Jedi Academy MP dedicated server
binary and does not build, ship or auto-update any engine itself. See
[`vanilla-legacy32.md`](vanilla-legacy32.md) for details.

## Tag policy

| Tag                           | Meaning                                                                         | Stability                                      |
| ----------------------------- | ------------------------------------------------------------------------------- | ---------------------------------------------- |
| `latest`                      | Current platform default. Today this points at the latest TaystJK master build. | Mutable. May change runtime in the future.     |
| `taystjk`                     | Latest TaystJK master build. Today identical to `latest`.                       | Mutable. Always a TaystJK build.               |
| `taystjk-master-<short_sha>`  | TaystJK master, pinned to an upstream commit.                                   | Immutable per upstream commit.                 |
| `master-<short_sha>`          | Legacy alias of `taystjk-master-<short_sha>`. Kept for backward compatibility.  | Immutable per upstream commit.                 |
| `v<semver>`                   | Repository release tags.                                                        | Immutable.                                     |
| `openjk-modern64`                         | Latest OpenJK modern64 (`JACoders/OpenJK` master) build.        | Mutable. Always an OpenJK modern64 build.      |
| `latest-openjk-modern64`                  | Alias of `openjk-modern64`.                                     | Mutable. Always an OpenJK modern64 build.      |
| `openjk-modern64-master-<short_sha>`      | OpenJK modern64, pinned to an upstream commit.                  | Immutable per upstream commit.                 |
| `openjk-legacy32`                         | Latest OpenJK legacy32 (`JACoders/OpenJK` master, i386) build.  | Mutable. Always an OpenJK legacy32 build.      |
| `latest-openjk-legacy32`                  | Alias of `openjk-legacy32`.                                     | Mutable. Always an OpenJK legacy32 build.      |
| `openjk-legacy32-master-<short_sha>`      | OpenJK legacy32, pinned to an upstream commit.                  | Immutable per upstream commit.                 |
| `vanilla-legacy32`                        | Latest vanilla legacy32 runtime image (operator-supplied engine). | Mutable. Always a vanilla legacy32 runtime image. |
| `latest-vanilla-legacy32`                 | Alias of `vanilla-legacy32`.                                    | Mutable. Always a vanilla legacy32 runtime image. |
| `vanilla-legacy32-<short_sha>`            | Vanilla legacy32 runtime image, pinned to a repository commit.  | Immutable per repository commit.               |

### Defaults that operators see

The egg exposes the following entries in `docker_images` (Pterodactyl will
show the first as the default selection):

1. `Jedi Academy Pterodactyl (latest, GHCR)` → `ghcr.io/akiondev/jedi-academy-pterodactyl:latest`
2. `Jedi Academy Pterodactyl (TaystJK runtime, GHCR)` → `ghcr.io/akiondev/jedi-academy-pterodactyl:taystjk`
3. `Jedi Academy Pterodactyl (latest, Docker Hub mirror)` → `docker.io/akiondev/jedi-academy-pterodactyl:latest`
4. `Jedi Academy Pterodactyl (TaystJK runtime, Docker Hub mirror)` → `docker.io/akiondev/jedi-academy-pterodactyl:taystjk`

Operators that want to be insulated from a future runtime change should
choose an explicit `taystjk*` tag rather than `latest`.

## Multi-runtime roadmap

When a second runtime (for example OpenJK) is actually integrated, the
following additive changes are planned:

- Add a runtime-specific Dockerfile or build stage selected by build arg.
- Publish runtime-specific tags (`openjk`, `openjk-<ref>`) under the same
  platform image name.
- Ship a parallel egg, e.g. `egg/egg-openjk-pterodactyl.json`, alongside
  the existing TaystJK egg, instead of overloading a single egg.
- Decide at that point whether `latest` should remain TaystJK-aliased or
  be repointed. Until then, `latest == taystjk`.

The platform image name and the existing TaystJK tags do **not** need to
change when the second runtime is introduced.

## What is intentionally not generalized today

The repository keeps the following TaystJK-specific implementation details
because generalizing them before a second runtime exists would be premature:

- `docker/Dockerfile` is a single TaystJK-source build pipeline.
- Image-internal paths live under the neutral `/opt/jka/` prefix and are
  declared in `/opt/jka/runtime.json` (`schema_version: 1`, paths only).
  Legacy `/opt/taystjk-*` directory paths and `/usr/local/bin/taystjk-antivpn`
  remain available as deprecated symlinks for one beta release window for
  any external tooling that may have referenced them; they will be removed
  in a future PR. The stamp files inside the engine directory were renamed
  from `.taystjk-upstream-{commit,ref}` to `.upstream-{commit,ref}`.
- `scripts/install_taystjk.sh` and `scripts/entrypoint.sh` encode
  TaystJK-specific defaults (`taystjkded.*`, `FS_GAME_MOD=taystjk`, default
  `server.cfg` template).
- `cmd/taystjk-antivpn` is the in-image runtime supervisor source package.
  The Go module path stays platform-named
  (`github.com/akiondev/jedi-academy-pterodactyl`) and the source package
  retains its TaystJK-prefixed name; the built binary is installed as
  `/usr/local/bin/jka-antivpn` (with a deprecated `taystjk-antivpn` symlink).
- Image labels under the `io.akiondev.taystjk.*` namespace describe the
  TaystJK upstream that was built into the image. Future runtimes get their
  own `io.akiondev.<runtime>.*` namespace; the existing labels are not
  renamed.

## Migration note for operators

Before this strategy was adopted, the image was published as
`ghcr.io/akiondev/jedi-academy-taystjk`. After the rename, the same image
content is published as `ghcr.io/akiondev/jedi-academy-pterodactyl`.

To migrate an existing Pterodactyl server:

1. Edit the server's docker image in the Pterodactyl panel.
2. Replace `ghcr.io/akiondev/jedi-academy-taystjk:<tag>` with
   `ghcr.io/akiondev/jedi-academy-pterodactyl:<tag>`.
3. Restart the server. The TaystJK runtime is unchanged.

The legacy GHCR package remains reachable for an interim period; it is
not deleted as part of this rename.

## Versioning policy

The project follows [Semantic Versioning 2.0.0](https://semver.org/) and
uses the `0.x.y` version range to signal that the public interface (egg
variables, startup flags, addon loader conventions, image layout) is not
yet frozen.

### Current phase — beta (`0.x.y`)

- Minor bumps (`0.2.0` → `0.3.0`) may contain breaking changes to the egg
  format, startup variables, addon conventions, or image-internal paths.
- Patch bumps (`0.2.0` → `0.2.1`) are backward-compatible bug fixes.
- Users should expect migration steps between minor versions and should
  read the release notes before upgrading.

### Previous phase — alpha (`0.1.x`)

The `v0.1.0` tag marks the initial proof-of-concept snapshot. At that
stage the project was functional but offered no stability guarantees of
any kind. The project has moved past this phase.

### Graduation to `v1.0.0`

The project will move to `v1.0.0` when **all** of the following are true:

1. The egg variable contract is stable — no breaking changes planned for
   existing startup variables.
2. The Docker image name is final (`jedi-academy-pterodactyl` ✓).
3. The addon loader convention is documented and locked.
4. At least one external operator runs the project in production and
   confirms it works end-to-end.
5. A tested migration/upgrade path exists and is documented.

After `v1.0.0`, standard SemVer rules apply: breaking changes require a
major bump, new features bump minor, and fixes bump patch.
