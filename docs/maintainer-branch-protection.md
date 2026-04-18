# Main branch protection

This repository should keep `main` protected with a lightweight GitHub **ruleset**.

The goal is not heavy process. The goal is to protect the branch that drives:

- the main integration branch
- pull request merges
- CI validation
- GHCR image publishing from `main`

## Recommended baseline for this repository

Target:

- default branch: `main`

Enable these protections:

1. **Require a pull request before merging**
2. **Require status checks to pass**
3. **Block direct pushes**
4. **Block force pushes**
5. **Block branch deletion**

Keep it intentionally minimal for now:

- do **not** require multiple approvals yet
- do **not** require CODEOWNERS yet
- do **not** require signed commits yet
- do **not** require merge queue yet

## Status checks

Require only stable checks that are expected for the normal PR flow.

For this repository, the safest baseline is to require the `Validate scripts and egg` check.

Do not make `Build Docker image (no push)` required right now because it is conditional and does not run for every pull request.

If you add more required checks later, make sure they are always present for the PR types you expect to merge.

## Why this matters here

`main` is not just a collaboration branch in this repository.

It is also the source branch for:

- validation on push
- release/tag-related automation
- image publishing behavior from the main branch workflow

That makes accidental direct pushes more expensive than in a docs-only repository.

## Suggested GitHub ruleset shape

In GitHub, create a **branch ruleset** that targets `main`.

Recommended settings:

- **Enforcement status:** Active
- **Target branches:** `main`
- **Restrict updates:** enabled
- **Require pull request before merging:** enabled
- **Require status checks to pass:** enabled
- **Require approvals:** disabled for now
- **Allow force pushes:** disabled
- **Allow deletions:** disabled

If GitHub asks about bypass actors, keep that list as small as possible.

## When to tighten it later

Consider adding stricter controls when:

- more maintainers start merging regularly
- reviews are consistently done by more than one person
- CODEOWNERS becomes useful
- release risk grows

Good next steps later:

- require 1 approval
- require review from code owners
- require conversation resolution
- require merge queue for busier repositories
