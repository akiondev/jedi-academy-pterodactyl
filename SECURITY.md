# Security Policy

## Scope

This repository mainly covers:

- the Docker image
- the Pterodactyl egg
- startup and runtime scripts
- addon loading and managed helpers
- anti-VPN integration
- documentation for those parts

## Supported focus

The main supported focus is the current state of the repository on `main`.

Security-sensitive issues are most relevant when they affect:

- container startup and runtime behavior
- privilege or command execution boundaries
- secret handling
- addon execution safety
- image packaging
- egg or panel-facing configuration that could introduce unsafe behavior

## Reporting a vulnerability

If you believe you found a real security issue in this repository, please avoid opening a public issue first.

Instead:

- contact the maintainer privately first if possible
- or use GitHub security reporting features if they are enabled for this repository

This gives time to confirm the issue and prepare a fix before public disclosure.

## What should go to normal issues

Please use the normal GitHub issue tracker for:

- general bugs
- documentation mistakes
- feature requests
- non-sensitive addon problems
- normal runtime misconfiguration questions

If you are unsure whether something is a real security issue or just a normal bug, start with a private report when possible.
