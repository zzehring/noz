# nozey

**A fast, stateless CLI for managing AI-agent sessions.**

`noz` turns each task into a git worktree + tmux session and gives you a live
dashboard across all of them. It keeps **no state of its own** — everything is
derived live from the filesystem, git, and tmux — so it can't drift, and it
survives reboots (the worktrees do; `noz restore` brings recently-active
sessions back).

One command per task spins up an isolated worktree + tmux session; one
dashboard shows which are live, which agent is running, and which are idle.

## Install

```bash
go install github.com/zzehring/noz/cmd/noz@latest
```

**Requires:** `git` and `tmux` (plus `fzf` for `noz sw`). Sessions live under
`$NOZ_ROOT` (default `~/worktrees/`).

## Quick start

```bash
noz open feature-auth            # git worktree + tmux session
noz open --pr 456                # review a PR (shallow clone, review profile)
noz open bug-123 --agent claude  # ...and open claude in the first window

noz ls                           # dashboard for the current repo
noz ls -A                        # ...across all repos
noz sw                           # fzf-pick a live session and jump to it
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
                         #   - prefix + j to fuzzy-jump sessions (noz sw)
```

`noz` never edits your tmux config for you — it prints an append-safe snippet so
it can't clobber your status bar or keybindings.

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
- **Act** (gated — you confirm): `noz_spawn`, `noz_rm`

### Agentic offshoots

The payoff of the act-tools is a human-gated fan-out loop:

```
noz_spawn (gated) → agent works on its own isolated branch → reports back
                  → you review and merge
```

`noz spawn` (or `noz_spawn`) creates a task-scoped **offshoot** — its own
worktree, tmux session, and a seeded context file — tagged with the session it
was spawned from (`NOZ_PARENT`), so `noz close` returns you there when the work
is done. The agent works *contained* on its branch (it can't touch `main` or
its siblings), and you bring the work back deliberately. The gated **merge**
bookend that closes this loop is in progress.

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
| `noz sw [filter]` | Fuzzy-pick a live session and switch to it |
| `noz status` | Current session context (`--json` for a prompt segment) |
| `noz path <slug>` | Print a session's worktree dir (`cd "$(noz path x)"`) |
| `noz mv <old> <new>` | Rename a session across worktree + tmux + branch |
| `noz close` | End the session you're in; hop to parent/last, then tear down |
| `noz rm <slug>...` | Remove one or more sessions (`-y`/`--force`, `--keep-worktree`, `--delete-branch`) |
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
- [x] MCP surface — agent can see / navigate / spawn / tear down sessions (create + destroy gated)
- [x] Agentic offshoots — `spawn` task-scoped sessions, `NOZ_PARENT` lineage, `close` to return
- [ ] Gated `merge` bookend — a fast review surface + PR/local merge to close the loop _(in progress)_
- [ ] Observability — live "what's the agent doing right now" view (`noz top`), built on the gate
- [ ] Isolation providers (v2) — run agents in microVMs ([`grafana/umm`](https://github.com/grafana/umm))
      for hard memory caps + isolation; `noz` stays the orchestrator
