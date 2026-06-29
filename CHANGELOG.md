# Changelog

Per-release notes for `noz` are **generated automatically** from
[Conventional Commits](https://www.conventionalcommits.org) and published on the
[GitHub Releases page](https://github.com/zzehring/noz/releases) — that page is
the canonical changelog.

This file is a pointer rather than a hand-maintained list on purpose: a second
copy of the history in the tree would only drift (PRINCIPLES.md — *honest over
clever*, store nothing that can't be regenerated).

## How it works

- Tagging a release (`vX.Y.Z`) runs [GoReleaser](https://goreleaser.com), which
  groups every commit since the previous tag into **Features** (`feat:`) and
  **Bug Fixes** (`fix:`) and drops the noise (`docs:`, `chore:`, `ci:`,
  `test:`, `style:`, `refactor:`, merges).
- The changelog is therefore only as good as the commit messages. Write them as
  Conventional Commits — see
  [CONTRIBUTING.md](CONTRIBUTING.md#commit-messages).

## Unreleased

Changes merged to `main` since the last tag live in the
[commit history](https://github.com/zzehring/noz/commits/main); they roll into
the next release's generated notes.
