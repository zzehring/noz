# Contributing to noz

Thanks for your interest in noz.

## Before you start
For anything beyond a small fix, please open an issue first so we can agree on
the approach before you write code. noz has strong design principles (see
PRINCIPLES.md); a quick issue saves a rewrite.

## Development
Requires Go 1.26+, plus `git` 2.22+ and `tmux` 3.2+ to exercise sessions locally.

```sh
go build ./cmd/noz      # build the binary
go test ./...           # run the test suite
go vet ./...            # vet
gofmt -l .              # must print nothing; CI fails on unformatted files
```

## Commit messages
noz uses [Conventional Commits](https://www.conventionalcommits.org). The type
drives the auto-generated changelog (see [CHANGELOG.md](CHANGELOG.md)):

- `feat:` — a new feature (listed under **Features**)
- `fix:` — a bug fix (listed under **Bug Fixes**)
- `docs:`, `chore:`, `ci:`, `test:`, `style:`, `refactor:` — excluded from the changelog
- a scope is encouraged: `feat(open): …`, `fix(restore): …`
- breaking changes: add `!` (`feat!:`) or a `BREAKING CHANGE:` footer

## Pull requests
1. Branch off `main`.
2. Add or update tests for behavior changes.
3. Make sure `go test ./...`, `go vet ./...`, and `gofmt -l .` are clean.
4. Open the PR with a clear description; reference the issue with `Fixes #NN`.

## Legal
By contributing, you agree that your contributions are licensed under the
project's Apache-2.0 License and that you have the right to submit them.
