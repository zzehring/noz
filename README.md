# nozey

**A fast, stateless CLI for managing AI-agent pairing sessions.**

`noz` turns each task into a git worktree + tmux session and gives you a live
dashboard across all of them. It holds **no state of its own** — everything is
derived on the fly from the filesystem, git, and tmux — so it can't drift, and
it survives reboots (the worktrees do; `noz restore` brings the sessions back).

One command per task spins up an isolated worktree + tmux session; one
dashboard shows which are live, which agent is running, and which are idle.

## Install

```bash
go install github.com/zzehring/nozey/cmd/noz@latest
```

Sessions live under `$NOZ_ROOT` (default `~/worktrees/`).

## Quick start

```bash
noz pair feature-auth            # git worktree + tmux session
noz pair --pr 456                # review a PR (shallow clone, review profile)
noz pair bug-123 --agent claude  # ...and open claude in the first window

noz ls                           # dashboard for the current repo
noz ls -A                        # ...across all repos
noz sw                           # fzf-pick a live session and jump to it
noz status                       # where am I? (slug, repo, branch, agent, state)

noz mv bug-123 bug-124           # rename across worktree + tmux + branch
noz rm feature-auth              # tear down worktree + tmux
```

## The dashboard

`noz ls` cross-references your worktrees with tmux and groups sessions by slug
prefix (`cf-`, `review-`, `i-`, …):

```
                                     state      win  last
cf (2/3)
  ▶ feature-auth             working    3    now     webapp
  ● feature-checkout           waiting    1    2h      webapp
  ○ feature-search                                            webapp
```

- `▶` attached · `●` live (detached) · `○` idle (worktree only, no tmux)
- **state** — `working` / `waiting`, inferred from recent tmux activity (no
  setup needed); a blocked session shows a red `! needs you` once agent hooks
  are wired.
- Filter by substring or `^prefix`: `noz ls cf`, `noz ls ^review`. Scope with
  `--active` / `--idle`, `-A` (all repos), `-g` (group size).

## Profiles

A profile is a markdown file with optional YAML frontmatter that shapes a new
session — the body becomes the session's `CLAUDE.md`, and `windows:` open tmux
windows alongside your shell.

```bash
noz pair incident-42 --profile troubleshoot   # opens k9s + an agent window
noz profile list                               # see built-ins + your own
noz profile create tf-review                   # scaffold one in $EDITOR
```

Built-ins: `investigate`, `review`, `troubleshoot`, and `profilesmith` (a meta
profile that helps you write new ones). Custom profiles live in
`~/.config/noz/profiles/`.

## Memory & restarts

Many long-lived agent sessions add up. noz manages the lifecycle:

```bash
noz reap                 # dry-run: idle agents + reclaimable memory
noz reap --force         # SIGTERM idle (waiting) agents, keep worktree + tmux
noz reap --idle 2h       # only agents idle >= 2h

noz prune                # dry-run: worktrees with no tmux, older than 7d
noz prune --force        # remove them

noz restore              # after a reboot: re-create the tmux sessions that
                         #   were live (worktrees survived; tmux didn't)
```

`reap` never touches an attached or working session, and SIGTERMs the agent so
it can checkpoint — resume later with `claude --continue` (noz reminds you when
you re-enter a session that has prior history). Reclaim estimates come from the
real `phys_footprint`, measured only on the candidates.

## tmux integration

```bash
noz setup tmux           # prints a snippet to paste into ~/.tmux.conf:
                         #   - NOZ_SLUG / NOZ_REPO + current command in the status bar
                         #   - prefix + j to fuzzy-jump sessions (noz sw)
```

`noz` never edits your tmux config for you — it prints an append-safe snippet so
it can't clobber your status bar or keybindings.

## Optional: command gating

A secondary feature for agents that support pre-tool hooks (Claude Code today):
evaluate every command and file access against a [CEL](https://github.com/google/cel-go)
policy before it runs.

```bash
noz setup claude --policy readonly --project-only   # install PreToolUse hooks
noz policy check '{"tool":"bash","cmd":"rm","args":["-rf","/"]}'
noz setup claude --remove --project-only            # undo
```

Shipped policies: `readonly`, `dev`, `sre`. This isn't the focus of the tool;
it's there if you want it.

## How it works

`noz` is stateless by design. It stores nothing of its own:

- **Sessions** are discovered by scanning `$NOZ_ROOT`, resolving each worktree's
  repo from its `.git` pointer, and cross-referencing `tmux`.
- **Identity** is tagged on the tmux session (`NOZ_SLUG`, `NOZ_REPO`), so
  same-named slugs in different repos don't collide.
- **State** (working/waiting) comes from tmux activity; an optional
  `~/.cache/noz/` cache holds the live-session set (for `restore`) and any
  hook-written agent state — both degrade gracefully if absent.

## Commands

| Command | Description |
|---------|-------------|
| `noz pair <slug>` | Start/attach a session (worktree + tmux); `--pr`, `--profile`, `--agent` |
| `noz ls [filter]` | Session dashboard (`-A` all repos, `--active`/`--idle`) |
| `noz sw [filter]` | Fuzzy-pick a live session and switch to it |
| `noz status` | Current session context (`--json` for a prompt segment) |
| `noz mv <old> <new>` | Rename a session across worktree + tmux + branch |
| `noz rm <slug>` | Remove a session (`--keep-worktree`, `--delete-branch`) |
| `noz reap [filter]` | Kill idle agents to reclaim memory |
| `noz prune [filter]` | Remove stale worktrees with no live session |
| `noz restore [filter]` | Re-create sessions that were live before a reboot |
| `noz profile …` | Manage session profiles (`list`/`create`/`edit`/`show`) |
| `noz setup tmux` | Print the tmux status-bar + jump-key snippet |
| `noz setup claude` | Install CEL gate hooks (optional) |
| `noz gate` / `noz policy …` | Policy endpoint + introspection (optional) |

## Roadmap

- [x] Stateless session dashboard, fuzzy switch, status
- [x] Profiles with tmux windows; agent registry + detection
- [x] Lifecycle: `reap` (memory), `prune`, `restore` (reboot recovery)
- [ ] Isolation providers (v2) — run agents in microVMs ([`grafana/umm`](https://github.com/grafana/umm))
      for hard memory caps + isolation; `noz` stays the orchestrator
- [ ] MCP surface so an agent can drive `noz` itself (list / switch / spawn)
