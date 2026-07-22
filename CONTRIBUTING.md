# Contributing to noz

Thanks for your interest in noz.

## Code of conduct

This project follows the [Contributor Covenant](CODE_OF_CONDUCT.md). By
participating, you're expected to uphold it.

## Before you start

For anything beyond a small fix, please open an issue first so we can agree on
the approach before you write code. noz has strong design principles (see
[PRINCIPLES.md](PRINCIPLES.md)); a quick issue saves a rewrite.

## Development setup

Requires Go 1.25+, plus `git` 2.22+ and `tmux` 3.2+ to exercise sessions
locally.

```sh
go build ./cmd/noz      # build the binary
go test ./...           # run the full test suite
go vet ./...            # vet
gofmt -l .              # must print nothing; CI fails on unformatted files
golangci-lint run ./... # must be clean; CI runs this (config in .golangci.yml)
go install ./cmd/noz    # install to $GOPATH/bin for manual testing
```

The lifecycle tests (`cmd/noz/cmd/`) shell out to `git` and `tmux` — both must
be installed and on `$PATH` to run them. Unit tests in `internal/` have no
external deps.

## Making a change

1. **Fork** the repo and clone your fork.
2. Create a branch off `main`: `git checkout -b feat/your-thing`.
3. Make your changes; add or update tests for any behavior change.
4. Run `go test ./...`, `go vet ./...`, `gofmt -l .`, and `golangci-lint run ./...` — all must be clean.
5. Push your branch to your fork and open a pull request against `main` here.
6. Reference the issue in the PR description with `Fixes #NN`.

If you're a maintainer with write access you can branch directly in this repo,
but fork-based PRs are equally welcome and the preferred path for new contributors.

## Commit messages

noz uses [Conventional Commits](https://www.conventionalcommits.org). The type
drives the auto-generated changelog (see [CHANGELOG.md](CHANGELOG.md)):

- `feat:` — a new feature (listed under **Features**)
- `fix:` — a bug fix (listed under **Bug Fixes**)
- `docs:`, `chore:`, `ci:`, `test:`, `style:`, `refactor:` — excluded from the changelog
- a scope is encouraged: `feat(open): …`, `fix(restore): …`
- breaking changes: add `!` (`feat!:`) or a `BREAKING CHANGE:` footer

## Legal

By contributing, you agree that your contributions are licensed under the
project's [Apache-2.0 License](LICENSE) and that you have the right to submit
them.
