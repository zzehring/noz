<div align="center">

<!-- Logo/banner: drop a centered banner image here once one exists, e.g.
     <img src="docs/banner.png" alt="noz" width="600"> -->

# noz

**A fast, stateless CLI for managing AI-agent sessions.**

<a href="https://github.com/zzehring/noz/releases"><img src="https://img.shields.io/github/v/release/zzehring/noz?color=blue" alt="Latest release"></a>
<a href="https://github.com/zzehring/noz/actions/workflows/ci.yml"><img src="https://github.com/zzehring/noz/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
<a href="https://goreportcard.com/report/github.com/zzehring/noz"><img src="https://goreportcard.com/badge/github.com/zzehring/noz" alt="Go Report Card"></a>
<a href="go.mod"><img src="https://img.shields.io/github/go-mod/go-version/zzehring/noz" alt="Go version"></a>
<a href="LICENSE"><img src="https://img.shields.io/badge/license-Apache%202.0-blue.svg" alt="License: Apache 2.0"></a>

</div>

**What it is.** `noz` turns each task into a git worktree + tmux session and
gives you one live dashboard across all of them — which are working, which are
waiting, which agent is running where. It keeps **no state of its own**:
everything is derived live from the filesystem, git, and tmux, so it can't
drift, deletes clean, and survives reboots (`noz restore` brings recently-active
sessions back).

**Why you'd want it.** Running many agents at once means juggling contexts. noz
puts you in a place to succeed agentically — each task isolated in its own
worktree so work can't collide, the blast radius contained, and **you always in
the loop**: the agent can see and navigate your sessions, but every create and
destroy is human-gated. It composes tmux and git rather than replacing them, so
it's a static binary that's trivial to adopt or drop — an extension of the
terminal you already live in, not another app to learn.

### 30-second quickstart

```bash
go install github.com/zzehring/noz/cmd/noz@latest   # needs git + tmux

noz open feature-auth                  # task → isolated git worktree + tmux session
noz ls                                 # live dashboard across all your sessions
noz spawn fix-flaky --task "..."       # fan out a contained agent offshoot
noz close                              # finish up and hop back to where you came from
```

## Demo

<!-- DEMO PLACEHOLDER — record the asset and drop it in here. The single
     highest-value asset is `noz ls` plus a spawn-and-return loop.

     asciinema:  <a href="https://asciinema.org/a/REPLACE"><img src="https://asciinema.org/a/REPLACE.svg" alt="noz demo" width="600"></a>
     or GIF:     <img src="docs/demo.gif" alt="noz demo" width="600">

     Record with asciinema (→ agg for the GIF):
       asciinema rec noz-demo.cast --cols 100 --rows 30
       # run the script below inside the recording, then:
       agg noz-demo.cast docs/demo.gif

     Exact commands to run on camera (the spawn-and-return loop is the money shot):
       noz open feature-auth                                # task → isolated worktree + tmux session
       noz ls                                               # the dashboard: working / waiting / idle
       noz spawn fix-flaky --task "tidy up flaky retries"   # contained offshoot on its own branch
       noz ls                                               # parent + offshoot, side by side
       noz close --report "fixed the retry helper"          # stream context back, hop to parent
       noz ls                                               # back where we started, report saved
-->

## Install

```bash
go install github.com/zzehring/noz/cmd/noz@latest
```

**Requires:** `git` (2.22+) and `tmux` (3.2+). Sessions
live under `$NOZ_ROOT` (default `~/worktrees/`).

### Homebrew

<!-- PLACEHOLDER — not wired up yet. A Homebrew tap is planned; once the release
     pipeline publishes the cask this section becomes the recommended install. -->

Coming soon:

```bash
brew install zzehring/tap/noz   # not available yet — tap is planned
```

### Prebuilt binaries

Download for your platform from the
[releases page](https://github.com/zzehring/noz/releases), then move `noz` onto
your `$PATH`.

## Quick start

```bash
noz open feature-auth            # git worktree + tmux session
noz open --pr 456                # review a PR (shallow clone, review profile)
noz open bug-123 --agent claude  # ...and open claude in the first window

noz ls                           # dashboard for the current repo
noz ls -A                        # ...across all repos
noz status                       # where am I? (slug, repo, branch, agent, state)

noz spawn fix-flaky --task "..." # create a task-scoped offshoot (seeds context)
noz close                        # end the session you're in; hop back to parent
noz mv bug-123 bug-124           # rename across worktree + tmux + branch
noz rm feature-auth bug-123      # tear down one or more sessions
```

**Agents:** noz launches and detects `claude`, `opencode`, `codex`, `gemini`,
and `pi` (via `--agent` or profile windows). Command-gating hooks exist for
Claude today; the others are launch/detect only.

## The dashboard

`noz ls` cross-references your worktrees with tmux and groups sessions by slug
prefix (`feature-`, `review-`, `i-`, …):

```
                                     state      win  last
feature (2/3)
  ▶ feature-auth                     working    3    now     webapp
  ● feature-checkout                 waiting    1    2h      webapp
  ○ feature-search                                           webapp
```

- `▶` attached · `●` live (detached) · `○` idle (worktree only, no tmux)
- **state** — `working` / `waiting`, inferred from recent tmux activity (no
  setup needed); a blocked session shows a red `! needs you` once agent hooks
  are wired.
- Filter by substring or `^prefix`: `noz ls feature`, `noz ls ^review`. Scope with
  `--active` / `--idle`, `-A` (all repos), `-g` (group size).

## Profiles

A profile is a markdown file with optional YAML frontmatter that shapes a new
session — the body becomes the session's **context** (written to the `.noz`
brain, never the repo tree; the agent is launched with a directive to read it),
and `windows:` open tmux windows alongside your shell.

```bash
noz open incident-42 --profile troubleshoot   # opens k9s + an agent window
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
                         #   - prefix + g   native picker: sessions in THIS repo
                         #   - prefix + G   native picker: every noz session
                         #   - prefix + C-g native picker: offshoots of THIS session
```

`noz` never edits your tmux config for you — it prints an append-safe snippet so
it can't clobber your status bar or keybindings.

### Native session picker

The `g` / `G` / `C-g` bindings open tmux's **own** `choose-tree` — full-screen,
preview pane, type-to-search — just pre-filtered to a **view** over your live
sessions. No fzf, no external deps.

| Key | View | Shows |
|-----|------|-------|
| `prefix + g`   | repo     | sessions sharing the current session's repo |
| `prefix + G`   | all      | every live noz session, across all repos |
| `prefix + C-g` | children | offshoots spawned from the current session |

The default `prefix + s` (tmux's unfiltered tree) is left untouched. The keys
are just defaults — rebind any of them, or drop one entirely, without editing
the snippet by hand:

```bash
noz setup tmux --repo-key C-s --all-key C-a   # use your own keys
noz setup tmux --children-key ""              # omit the children binding
```

**How it works (and why noz, not raw `choose-tree -f`):** the binding asks `noz
pick <view> --filter` for the matching sessions, then `choose-tree` filters on
those. The filtering happens in noz on purpose. tmux's format filter falls back
to the *server's global environment* for any session whose own env lacks a
`NOZ_*` tag — so filtering `choose-tree` directly on `#{NOZ_REPO}` leaks
non-noz sessions in and drops untagged ones. noz reads each session's env
correctly in Go (same source as `noz ls`) and emits a filter that matches the
resolved names by `session_name`, a per-row primitive that's immune to that
fallback.

`noz pick` also has `--json` and a plain tab-separated form for scripting or
your own picker:

```bash
noz pick repo --json     # [{ "session": "...", "repo": "...", "state": "..." }, ...]
noz pick children        # "<session>\t<label>" lines, one per matching session
noz pick all --filter    # the tmux choose-tree filter the binding uses
```

## Shell setup

Running `noz` with no arguments shows the dashboard (same as `noz ls`).

Tab-completion (commands, session slugs, profiles, agents):

```bash
# zsh — add to ~/.zshrc (or drop into your fpath)
source <(noz completion zsh)
# bash:  source <(noz completion bash)   ·   fish: noz completion fish | source
```

Jump to a worktree by slug (with completion):

```bash
nzcd() { cd "$(noz path "$1")"; }
```

Show the current session's state in your prompt via `noz status --json`, e.g. a
p10k segment that surfaces `working` / `waiting` next to your prompt.

## Agent integration (MCP)

`noz mcp` runs an MCP server over stdio so a coding agent can *see, navigate,
and spawn* your sessions — know what else is in progress, move you between
contexts, and fan out task-scoped offshoots. It's stateless (just reads
fs/tmux), so the agent spawns it as a subprocess; no daemon, ports, or auth.
**Navigation is free; every create/destroy is gated** — your agent's tool-call
confirmation is the human-in-the-loop checkpoint.

```bash
noz setup mcp      # prints how to register it
```

Register it with Claude Code either way:

```jsonc
// .mcp.json in your repo (project scope)
{ "mcpServers": { "noz": { "command": "noz", "args": ["mcp"] } } }
```
```bash
claude mcp add noz -- noz mcp     # user scope
```

Tools:
- **See** (read-only): `noz_sessions`, `noz_status`
- **Navigate** (free): `noz_switch`, `noz_back`
- **Act** (gated — you confirm): `noz_spawn`, `noz_rm`, `noz_close`

### Agentic offshoots

The payoff of the act-tools is a human-gated fan-out loop:

```
noz_spawn (gated) → agent works on its own isolated branch
                  → noz close --report (context streams back to the brain)
                  → you read the report and bring the work back
```

`noz spawn` (or `noz_spawn`) creates a task-scoped **offshoot**: its own
worktree, tmux session, and a seeded context file, tagged with the session it
was spawned from (`NOZ_PARENT`), so `noz close` returns you there when the work
is done. The agent works *contained* on its branch (it can't touch `main` or its
siblings). When it finishes, `noz close --report "..."` (or the `report` field on
`noz_close`) saves a back-report to `.noz/<repo>/reports/<slug>.md`, so you read
what it did without re-entering the session. `noz close --merge` fast-forwards
the branch into your main checkout on the way out (local; PR merges stay manual
and gated by your forge).

## Observability (opt-in)

`noz metrics` emits your session landscape as Prometheus text — counts by
state/agent/repo, last-activity per live session. noz is a CLI (no daemon), so
feed it to Prometheus/Mimir via the textfile collector:

```bash
noz metrics --textfile "$HOME/.local/state/noz/noz.prom"   # via cron/launchd/Alloy
```

A starter Grafana dashboard lives at `dashboards/noz.json` (Prometheus
datasource). Visualize your fleet of contexts and watch idle/memory trends over
time.

## Optional: command gating

Not the focus of the tool, but available: for agents with pre-tool hooks
(Claude Code today), evaluate every command and file access against a
[CEL](https://github.com/google/cel-go) policy before it runs.

```bash
noz setup claude --policy readonly --project-only   # install hooks (readonly|dev|sre)
noz setup claude --remove --project-only            # undo
```

## How it works

`noz` is stateless by design. It stores nothing of its own:

- **Sessions** are discovered by scanning `$NOZ_ROOT`, resolving each worktree's
  repo from its `.git` pointer, and cross-referencing `tmux`.
- **Identity** is tagged on the tmux session (`NOZ_SLUG`, `NOZ_REPO`), so
  same-named slugs in different repos don't collide.
- **State** (working/waiting) comes from tmux activity.
- **Recovery** (`noz restore`) brings back recently-active worktrees after a
  reboot by reading durable, reboot-surviving signals (agent transcript and
  worktree mtimes) — there's no manifest or cache to keep in sync.

## Commands

| Command | Description |
|---------|-------------|
| `noz open <slug>` | Start/attach a session (worktree + tmux); `--pr`, `--profile`, `--agent`, `--detach` |
| `noz spawn <slug>` | Create a task-scoped offshoot (worktree + seeded context); `--task`, `--source`, `--launch` |
| `noz ls [filter]` | Session dashboard (`-A` all repos, `--active`/`--idle`) |
| `noz pick [repo\|children\|all]` | Resolve sessions for a view; backs the `choose-tree` picker (`--json`, `--filter`) |
| `noz status` | Current session context (`--json` for a prompt segment) |
| `noz path <slug>` | Print a session's worktree dir (`cd "$(noz path x)"`) |
| `noz mv <old> <new>` | Rename a session across worktree + tmux + branch |
| `noz close` | End the session you're in; hop to parent/last, then tear down (`--report`, `--merge`) |
| `noz rm <slug>...` | Remove one or more sessions (`-y`/`--force`, `--keep-worktree`, `--delete-branch`) |
| `noz reap [filter]` | Kill idle agents to reclaim memory |
| `noz prune [filter]` | Remove stale worktrees with no live session |
| `noz restore [filter]` | Re-create sessions that were live before a reboot |
| `noz profile …` | Manage session profiles (`list`/`create`/`edit`/`show`) |
| `noz setup tmux` | Print the tmux status-bar + jump-key snippet |
| `noz setup claude` | Install CEL gate hooks (optional) |
| `noz gate` / `noz policy …` | Policy endpoint + introspection (optional) |

## Roadmap

- [x] Stateless session dashboard, native session picker, status
- [x] Profiles with tmux windows; agent registry + detection
- [x] Lifecycle: `reap` (memory), `prune`, `restore` (reboot recovery)
- [x] MCP surface — agent can see / navigate / spawn / tear down sessions (create + destroy gated)
- [x] Agentic offshoots — `spawn` task-scoped sessions, `NOZ_PARENT` lineage, `close` to return
- [x] Report-back on close — offshoots stream context to the brain (`close --report`); local `close --merge`
- [ ] Observability — surface `report ✓` + live "what's the agent doing right now" in `noz ls` / `noz top` (built on the gate)
- [ ] Isolation providers (v2) — run agents in microVMs ([`grafana/umm`](https://github.com/grafana/umm))
      for hard memory caps + isolation; `noz` stays the orchestrator
