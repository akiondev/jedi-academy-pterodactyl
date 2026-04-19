# v0.2.0 (beta)

## ⚠️ Breaking — Image renamed

The Docker image moved from `ghcr.io/akiondev/jedi-academy-taystjk` to **`ghcr.io/akiondev/jedi-academy-pterodactyl`**.

The game server inside is unchanged — only the image name is different.

---

## Quick migration

> Do **both** steps. The egg update gives your panel the new image names; the per-server edit points each running server at the new image.

### 1. Re-import the egg

1. Download the latest [`egg/egg-taystjk-pterodactyl.json`](egg/egg-taystjk-pterodactyl.json) from this release.
2. In Pterodactyl go to **Admin → Nests → your nest → Import Egg**.
3. Upload the file and confirm the import (it will update the existing egg in place).

### 2. Update each server's Docker image

1. Open each server in **Admin → Servers → \<server\> → Startup**.
2. Change the Docker image to one of:
   - `ghcr.io/akiondev/jedi-academy-pterodactyl:latest`
   - `ghcr.io/akiondev/jedi-academy-pterodactyl:taystjk`
3. Restart the server. No data or config changes needed.

The old `ghcr.io/akiondev/jedi-academy-taystjk` image stays available for a limited time but will not receive updates.

---

## What's new

### Image & registry

- Image renamed to `jedi-academy-pterodactyl` on both GHCR and Docker Hub ([`643d661`](https://github.com/akiondev/jedi-academy-pterodactyl/commit/643d661820017e6b3d79adca924cda192faab2ca))
- Optional Docker Hub mirror — opt-in via `PUBLISH_DOCKERHUB` repo variable ([`643d661`](https://github.com/akiondev/jedi-academy-pterodactyl/commit/643d661820017e6b3d79adca924cda192faab2ca))
- Egg now lists four selectable images: GHCR latest/taystjk + Docker Hub latest/taystjk ([`643d661`](https://github.com/akiondev/jedi-academy-pterodactyl/commit/643d661820017e6b3d79adca924cda192faab2ca))

### Anti-VPN hardening

- Tighter userinfo key parsing ([`64dfd1b`](https://github.com/akiondev/jedi-academy-pterodactyl/commit/64dfd1b31dd2c34449760ad947bb3e0e44b8511f))
- Fixed trailing color code normalization in connect logs ([`646c996`](https://github.com/akiondev/jedi-academy-pterodactyl/commit/646c9969227e24733d72421b1f69c08f4b27780b))
- Hardened broadcast player name sanitization ([`bc592de`](https://github.com/akiondev/jedi-academy-pterodactyl/commit/bc592deab3636f171898ff77587116dedcc3ecb4))
- Improved connection event parsing ([`e066b5d`](https://github.com/akiondev/jedi-academy-pterodactyl/commit/e066b5d13738e3268e2b00f3923016d43c7db8bf))

### Chatlogger addon

- Chatlogger now follows the runtime-resolved log path instead of a hardcoded one ([`a0f5c1c`](https://github.com/akiondev/jedi-academy-pterodactyl/commit/a0f5c1cec51eee3ef1609e1ce4be54911b117ed1), [`1cb651a`](https://github.com/akiondev/jedi-academy-pterodactyl/commit/1cb651a59c594f1837a61df9ab4e2f4d4febe11c))
- Hardened sleep calls and extended self-test coverage ([`b11fa56`](https://github.com/akiondev/jedi-academy-pterodactyl/commit/b11fa5620d38663caf43f21f43dabe83ececb30a))
- Fixed timestamp parsing and prefix handling ([`d397e97`](https://github.com/akiondev/jedi-academy-pterodactyl/commit/d397e974771e4f8fd67343b589d4ad68ef0ca4f4), [`3cd6fb1`](https://github.com/akiondev/jedi-academy-pterodactyl/commit/3cd6fb1018ef994464e16e1f5b7bd15a53359281), [`0dda3c3`](https://github.com/akiondev/jedi-academy-pterodactyl/commit/0dda3c375b8d325d99670e7eb329911b0b82ee0c))

### CI & workflows

- Optimized and hardened Docker image build workflows ([`d419ffc`](https://github.com/akiondev/jedi-academy-pterodactyl/commit/d419ffcd32efc1f4cbcbfdfa795cedd6729bf924))
- Fixed commit-based Docker builds for edge-case upstream states ([`764112f`](https://github.com/akiondev/jedi-academy-pterodactyl/commit/764112faf0e755aaf2ea8d79da85c835ae84de1a))
- Added safe upstream TaystJK master sync ([`6a0b3dd`](https://github.com/akiondev/jedi-academy-pterodactyl/commit/6a0b3dd43988256ae1d5d71253d6fbf3fdb6c06d))

### Docs & repo hygiene

- Added CONTRIBUTING.md and SECURITY.md ([`da75ba2`](https://github.com/akiondev/jedi-academy-pterodactyl/commit/da75ba2ae680479e5f64b48a90233123b97259cd))
- Added SUPPORT.md ([`68c94a4`](https://github.com/akiondev/jedi-academy-pterodactyl/commit/68c94a43d045a9fbdd90d2e01bf17264a7bc7183))
- Expanded README with Pterodactyl context, console screenshot, and quick-start links ([`890c358`](https://github.com/akiondev/jedi-academy-pterodactyl/commit/890c358835f18b8326199a31793d1d8d33dfb4be), [`601900e`](https://github.com/akiondev/jedi-academy-pterodactyl/commit/601900ef228acb9dbfeeddf1ead94ee20cc6200f))
- Removed tracked Python cache files ([`2373748`](https://github.com/akiondev/jedi-academy-pterodactyl/commit/237374805a1780f57c33a53731e56b69cfc53e86), [`20cc8ff`](https://github.com/akiondev/jedi-academy-pterodactyl/commit/20cc8ff30a5b876583ca07e123ae9674e6c1e350))

---

## Important

This project does **not** redistribute copyrighted Jedi Academy game assets. You must provide your own legally owned base game files.
