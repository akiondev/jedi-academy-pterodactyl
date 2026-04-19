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

TaystJK is the **current default and only automatically managed runtime**.
Future runtimes (for example OpenJK) may be added later. When that happens
they will be exposed through additional tags and, if needed, additional
parallel eggs — not by replacing the platform identity.

## Tag policy

| Tag                           | Meaning                                                                         | Stability                                      |
| ----------------------------- | ------------------------------------------------------------------------------- | ---------------------------------------------- |
| `latest`                      | Current platform default. Today this points at the latest TaystJK master build. | Mutable. May change runtime in the future.     |
| `taystjk`                     | Latest TaystJK master build. Today identical to `latest`.                       | Mutable. Always a TaystJK build.               |
| `taystjk-master-<short_sha>`  | TaystJK master, pinned to an upstream commit.                                   | Immutable per upstream commit.                 |
| `master-<short_sha>`          | Legacy alias of `taystjk-master-<short_sha>`. Kept for backward compatibility.  | Immutable per upstream commit.                 |
| `v<semver>`                   | Repository release tags.                                                        | Immutable.                                     |
| `openjk` *(future)*           | Reserved for a future OpenJK runtime build.                                     | Not yet published.                             |
| `openjk-<ref>` *(future)*     | Reserved for OpenJK upstream-pinned builds.                                     | Not yet published.                             |

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
- Image-internal paths under `/opt/taystjk-*` describe the current runtime
  layout and stay TaystJK-prefixed.
- `scripts/install_taystjk.sh` and `scripts/entrypoint.sh` encode
  TaystJK-specific defaults (`taystjkded.*`, `FS_GAME_MOD=taystjk`, default
  `server.cfg` template).
- `cmd/taystjk-antivpn` is the in-image runtime supervisor. The Go module
  path stays platform-named (`github.com/akiondev/jedi-academy-pterodactyl`)
  while the binary keeps its TaystJK-prefixed name.
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
