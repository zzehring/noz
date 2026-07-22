# AGENTS.md

Guidance for AI coding agents (and humans) working in this repo. Agent-neutral:
Claude Code and other agents that read `AGENTS.md` pick this up.

**Read `PRINCIPLES.md` first** — the tenets noz is built on (stateless,
lightweight, human-always-in-the-loop, respect the machine and data, safe by
construction, honest over clever, transparent). Do not break one without a
deliberate, documented reason.

## What noz is

A fast, stateless CLI for managing AI-agent sessions. Each task becomes a git
worktree plus a tmux session; one dashboard spans them all; an MCP server lets an
agent see, navigate, and (human-gated) create or destroy them. Everything is
derived live from git, tmux, and the filesystem. noz keeps no state of its own.

## Build and test

```sh
go build ./cmd/noz        # build the CLI
go test ./...             # run all tests
go vet ./...
gofmt -l .                # must print nothing (CI fails on unformatted files)
golangci-lint run ./...   # must be clean (CI runs this; config in .golangci.yml)
go install ./cmd/noz      # install to GOPATH/bin
```

Standard `go` commands, no Makefile. The lifecycle tests shell out to `git` and
`tmux`, so both must be installed to run the full suite. CI runs
`golangci-lint` too (see `.golangci.yml`), so run it locally before pushing —
its default linter set (errcheck, staticcheck, …) catches what `go vet` won't.

## Architecture

- `cmd/noz/cmd/` — Cobra command handlers, roughly one file per command (open,
  ls, spawn, close, rm, restore, reap, prune, mv, status, sw, back, path, mcp,
  profile, metrics, setup, gate, policy).
- `internal/gate/` — CEL policy engine; first-match-wins evaluation of agent
  tool-calls.
- `internal/shellparse/` — splits shell strings into command segments for the gate.
- `internal/toolcall/` — parses JSON tool-call input from agents.
- `internal/agent/` — registry of supported agents (launch + detect).
- `internal/config/` — config loading from `~/.config/noz/`.

Platform-specific code uses build tags (e.g. `birthtime_darwin.go` /
`birthtime_other.go`). macOS and Linux are both first-class; provide a path for
both rather than degrading on one.

## Session model

`noz open <slug>` creates a worktree at `$NOZ_ROOT/<repo>-<slug>` (default
`~/worktrees/`) plus a tmux session. `noz spawn` creates task-scoped "offshoots"
tagged with their parent session (`NOZ_PARENT`). `noz close` ends the session you
are in (hop to the parent, tear down; optional `--report` writes a back-report,
`--merge` does a local fast-forward). `noz rm` removes sessions by name. `noz ls`
is the dashboard.

## MCP surface

`noz mcp` runs an MCP server: read tools (`noz_sessions`, `noz_status`),
navigation (`noz_switch`, `noz_back`), and gated act tools (`noz_spawn`,
`noz_rm`, `noz_close`). Navigation is free; create and destroy are gated by the
agent harness's tool-call confirmation, so a human always approves them.

## Conventions

- Match the surrounding code's style and idiom.
- New behavior gets a test; lifecycle paths can be tested with a temp `git init`
  plus `git worktree` sandbox.
- The vocabulary leans a tree/growth metaphor (offshoot, branch, prune, reap,
  merge); keep it consistent.
- See `CONTRIBUTING.md` for the PR flow.
