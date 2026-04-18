# Contributing

Thanks for your interest in contributing.

This repository is the Pterodactyl integration project around TaystJK. Contributions here should focus on the Docker image, egg, startup flow, addon system, documentation, and related admin tooling.

## What belongs here

Changes that fit this repository include:

- the Docker image build
- the Pterodactyl egg
- startup and runtime scripts
- addon loading and managed helpers
- anti-VPN integration
- documentation and examples

## What belongs upstream

This project is built around [taysta/TaystJK](https://github.com/taysta/TaystJK).

If your change belongs to the game runtime itself, it should usually go upstream instead. That includes:

- engine or game-code changes
- gameplay or runtime behavior inside TaystJK itself
- fixes that should live in TaystJK outside this Pterodactyl integration

## Contribution flow

1. [Fork this repository](https://github.com/akiondev/jedi-academy-pterodactyl/fork).
2. Create a new branch in your fork.
3. Make focused changes.
4. Open a pull request back to this repository.

If your change belongs upstream, use [taysta/TaystJK](https://github.com/taysta/TaystJK) instead.

## Guidelines

- Keep changes focused and easy to review.
- Preserve the TaystJK-first direction of this project.
- Do not add copyrighted Jedi Academy game assets to the repository, image, or egg flow.
- If you change addon behavior, update the addon docs too.
- If you change startup, install, or egg behavior, update the README or relevant docs so the operator-facing behavior stays accurate.

## Local checks

Before opening a pull request, it is a good idea to run the same kinds of checks this repository expects:

- shell validation for startup and install scripts
- JSON validation for the egg
- Python syntax validation for addon helpers when relevant

GitHub Actions will also run the repository CI for validation, image build checks, and publishing logic where applicable.
