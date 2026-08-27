<div align="center">

<!-- Logo/banner: drop a centered banner image here once one exists, e.g.
     <img src="docs/banner.png" alt="noz" width="600"> -->

# noz

**Context-first session management for multi-agent work.**

<a href="https://github.com/zzehring/noz/releases"><img src="https://img.shields.io/github/v/release/zzehring/noz?color=blue" alt="Latest release"></a>
<a href="https://github.com/zzehring/noz/actions/workflows/ci.yml"><img src="https://github.com/zzehring/noz/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
<a href="go.mod"><img src="https://img.shields.io/github/go-mod/go-version/zzehring/noz" alt="Go version"></a>
<a href="LICENSE"><img src="https://img.shields.io/badge/license-Apache%202.0-blue.svg" alt="License: Apache 2.0"></a>

</div>

**What it is.** When you're working across multiple tasks with AI agents, context
is the bottleneck — knowing what each agent is working on, what you left off,
and how to get back to it cleanly. `noz` gives each task its own git worktree
and tmux session, keeps the working context (task notes, back-reports, history)
alongside the workspace, and gives you one dashboard across everything. To switch
tasks you pick a session and the context is already there.

It keeps **no session manifest**: which sessions exist and what they're doing is
derived live from the filesystem, git, and tmux. Sessions can't drift, deletions
are clean, and after a reboot `noz restore` brings everything back — nothing to
maintain. The only bytes noz persists are the shared `.noz` brain (your task
briefs and back-reports), which you own and can delete at any time.

It's built for engineers who live in the terminal and run several agents at
once. A single static binary that builds on the tmux and git you already have.
No IDE, no daemon.

**Why it's useful.** Running several agents in parallel, each needs an isolated
workspace so work doesn't collide, and you need to know what's happening without
jumping into every session. noz isolates each task in its own worktree, surfaces
what's working and what's waiting on one screen, and keeps **you in control**:
the agent can see and navigate sessions, but creating and destroying is always
your call. Adopt it for one repo, drop it cleanly if it doesn't fit.

### 30-second quickstart

```bash
go install github.com/zzehring/noz/cmd/noz@latest   # needs git + tmux

noz open feature-auth            # new task → isolated git worktree + tmux session
noz open bug-123 --agent claude  # open a session and launch an agent in it
noz ls                           # live dashboard: what's running, working, waiting
noz close                        # finish up and hop back to where you came from
```

<!-- DEMO PLACEHOLDER: drop an asciinema/GIF here. Money shot: noz ls + spawn-and-return loop. -->

## Install

```bash
go install github.com/zzehring/noz/cmd/noz@latest
```

**Requires:** `git` (2.22+) and `tmux` (3.2+). Sessions live under `$NOZ_ROOT`
(default `~/worktrees/`). `noz open --pr` additionally requires the
[GitHub CLI](https://cli.github.com) (`gh`).

### Homebrew

A Homebrew tap is planned. Check the [releases page](https://github.com/zzehring/noz/releases) in the meantime.

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

**Agents:** the sessions, worktrees, and dashboard are agent-agnostic — noz
launches and detects `claude`, `opencode`, `codex`, `gemini`, and `pi` (via
`--agent` or profile windows), and none of that assumes a particular agent.
Two conveniences are Claude-specific today and only kick in for Claude: the CEL
command-gating hooks, and the brain auto-grant (a `.claude/settings.local.json`
so the agent can read its brief without a prompt). For any other agent noz
writes no agent config into your worktree.

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
- Sessions cluster by the slug prefix before the first hyphen, so naming your
  work `feature-auth` and `feature-search` lands it under one `feature` header.
  The `(2/3)` count is how many are live out of the group total. A one-off
  prefix folds into `other`; `-g` sets how many sessions a prefix needs to get
  its own group.
- Filter by substring or `^prefix`: `noz ls feature`, `noz ls ^review`. Scope with
  `--active`/`-a`, `--idle`/`-i`, `-A` (all repos), `-g` (group size).

## Profiles

A profile is a markdown file with optional YAML frontmatter that shapes a new
session — the body becomes the session's **context** (written to `.noz/<repo>/`,
a shared brain directory symlinked into each worktree; the agent is given read
access to it on open), and `windows:` open tmux windows alongside your shell.

```bash
noz open incident-42 --profile troubleshoot   # opens k9s + an agent window
noz profile list                               # see built-ins + your own
noz profile create tf-review                   # scaffold one in $EDITOR
```

Built-ins: `investigate`, `review`, `troubleshoot`, and `profilesmith` (a meta
profile that helps you write new ones). Custom profiles live in
`~/.config/noz/profiles/`.

## Lifecycle management

Many long-lived agent sessions add up. noz manages the full lifecycle:

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
it can checkpoint — noz reminds you when you re-enter a session that has prior
history, so you can resume (e.g. `claude --continue`). Memory estimates are
measured only on idle candidates (macOS: `phys_footprint`; Linux: RSS).

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

Pasting the snippet isn't the same as it being live — a running tmux server won't
re-read `~/.tmux.conf` on its own, so the usual reason "nothing happens" is simply
that the config was never sourced. `--check` inspects the running server and says
which parts are actually in effect:

```bash
noz setup tmux --check   # sourced? bound? not clobbered? noz on tmux's PATH?
```

It exits non-zero when a check fails, so it works as a dotfiles smoke test. It
reads only — noz still never edits your tmux config.

`noz pick` also has `--json` and a plain tab-separated form for scripting or
your own picker. The filtering happens in noz (not raw `choose-tree -f`) because
tmux's format filter falls back to the server's global env for untagged sessions,
making direct `#{NOZ_REPO}` filtering unreliable — see
[docs/session-picker.md](docs/session-picker.md) for the full design.

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
**Navigation is free; create and destroy are gated.** The gating is enforced by
your MCP client's tool-approval prompt — the agent must *request* `noz_spawn` /
`noz_rm` / `noz_close`, and you approve the call before it runs. noz doesn't add
its own confirmation on top, so this holds only as long as your client actually
prompts: if you auto-approve tool calls, these run unattended. Since `noz mcp`
is just a subprocess that reads fs/tmux, the trust boundary is "any process that
can run `noz mcp` can propose these" — bounded by that approval prompt.

```bash
noz setup mcp      # prints how to register it
```

`noz mcp` works with any MCP-compatible client. Claude Code examples:

```jsonc
// .mcp.json in your repo (project scope — any MCP client)
{ "mcpServers": { "noz": { "command": "noz", "args": ["mcp"] } } }
```
```bash
claude mcp add noz -- noz mcp     # Claude Code user scope
```

Tools:
- **See** (read-only): `noz_sessions`, `noz_status`, `noz_peek`
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
is done. The agent works in its own isolated worktree directory — it can't
accidentally modify your main checkout's working tree. When it finishes,
`noz close --report "..."` (or the `report` field on `noz_close`) saves a
back-report to `.noz/<repo>/reports/<slug>.md`, so you read what it did without
re-entering the session. `noz close --merge` fast-forwards the branch into your
main checkout on the way out (local; PR merges stay manual and gated by your
forge).

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
- **Recovery** (`noz restore`) brings back recently-active sessions after a
  reboot. Since sessions are just worktrees + tmux, recovery means re-attaching
  tmux to what already exists — noz reads agent transcripts and worktree mtimes
  to decide what was active, with no manifest to maintain.

## What noz touches (and how to remove it)

noz is stateless, but it does create a few files. All of them are yours to
inspect or delete:

| Path | What | When |
|------|------|------|
| `$NOZ_ROOT/` (default `~/worktrees/`) | one worktree dir per session | `noz open` / `spawn` |
| `$NOZ_ROOT/.noz/<repo>/` | the shared brain: `brief/`, `reports/`, `brain/` | first `open` per repo |
| `~/.config/noz/` | your profiles and gate policies | `noz profile` / `setup` |
| `<worktree>/.claude/settings.local.json` | Claude brain-grant (Claude sessions only) | `open` with `--agent claude` |
| `<repo>/.claude/settings.json` | CEL gate hooks | only if you run `noz setup claude` |
| `<file>.noz.bak` | a backup before noz edits a file | `noz setup` |
| `.git/info/exclude` entries | keeps `.noz` / `.claude/settings.local.json` out of git | first `open` per repo |

noz never edits your tmux config or your tracked repo files. To remove it
cleanly:

```bash
noz rm <slug>...              # tear down sessions (worktrees + tmux)
noz setup claude --remove     # undo the gate hooks (if you installed them)
rm -rf "$NOZ_ROOT/.noz"       # drop briefs/reports/brain (keep it if you want the notes)
rm "$(command -v noz)"        # remove the binary
```

The per-worktree `.claude/settings.local.json` files are gitignored and
harmless; they vanish when you `noz rm` the session.

## Commands

| Command | Description |
|---------|-------------|
| `noz open <slug>` | Start/attach a session (worktree + tmux); `--pr`, `--profile`, `--agent`, `--detach` |
| `noz spawn <slug>` | Create a task-scoped offshoot (worktree + seeded context); `--task`, `--source`, `--launch` |
| `noz ls [filter]` | Session dashboard (`-A` all repos, `--active`/`-a`, `--idle`/`-i`) |
| `noz pick <repo\|children\|all>` | Resolve sessions for a view; backs the `choose-tree` picker (`--json`, `--filter`) |
| `noz status` | Current session context (`--json` for a prompt segment) |
| `noz peek <slug>` | Show another session's recent agent output without switching (`-n` lines) |
| `noz path <slug>` | Print a session's worktree dir (`cd "$(noz path x)"`) |
| `noz mv <old> <new>` | Rename a session across worktree + tmux + branch |
| `noz close` | End the session you're in; hop to parent/last, then tear down (`--report`, `--merge`) |
| `noz back` | Hop to the previous tmux session |
| `noz rm <slug>...` | Remove one or more sessions (`--force`/`-f`, `--keep-worktree`, `--delete-branch`) |
| `noz reap [filter]` | Preview/kill idle agents to reclaim memory (dry-run by default) |
| `noz prune` | Remove stale worktrees with no live session (`--force`, `--age`, `--all`) |
| `noz restore [filter]` | Re-create sessions that were live before a reboot |
| `noz profile …` | Manage session profiles (`list`/`create`/`edit`/`show`) |
| `noz setup tmux` | Print the tmux status-bar + jump-key snippet (`--check` verifies it's live) |
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
