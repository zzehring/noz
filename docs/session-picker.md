# Design note: repo/session-aware tmux picker

A repo- and offshoot-aware session switcher for noz, rendered with tmux's own
native `choose-tree` (full-screen, preview, type-to-search) — no fzf, no
external deps. This is the design behind `noz pick` and the `noz setup tmux`
picker bindings.

## Goal

Fast, keyboard-only jumps to *relevant* noz sessions, where "relevant" is one of
three first-class views:

| View | Definition | Default key |
|------|------------|-------------|
| **repo**     | sessions sharing the current session's `NOZ_REPO` | `prefix + g` |
| **children** | sessions whose `NOZ_PARENT` == current session name | `prefix + C-g` |
| **all**      | every live noz session | `prefix + G` |

No by-state filter — state is shown as a hint, not a filter axis.

## Constraints / decisions

- **Leave `prefix+s` alone.** tmux's built-in unfiltered tree stays as-is; the
  picker adds *new* keys (`g` / `G` / `C-g` by default), all rebindable.
- **tmux-native UI.** Use tmux's own `choose-tree -Zs` — the full-screen tree
  with a preview pane that people already know from `prefix+s`. Not fzf, not a
  flat `display-menu`.
- **Logic lives in noz.** A single cobra command owns the `NOZ_*` tag semantics
  and resolves which sessions match. The binding is thin.

## The trap: `choose-tree -f` can't be trusted to filter on `NOZ_*`

The obvious implementation is `choose-tree -Zs -f '#{==:#{NOZ_REPO},<repo>}'`.
It looks like it works — `tmux ls -f '#{==:#{NOZ_REPO},noz}'` filters correctly
in a quick test — but it's a trap.

tmux's format resolver, when a session's *own* environment lacks a variable,
**falls back to the server's global environment**. And the global environment is
seeded from whatever shell first started the tmux server — which, for a noz
user, often already has `NOZ_REPO` / `NOZ_SLUG` exported from a session. Observed
in the wild:

```
$ tmux show-environment -g | grep NOZ
NOZ_REPO=webapp
NOZ_SLUG=feature-auth
```

With that leak, any session whose own env is missing a tag (a hand-made session,
or one where tagging didn't stick) inherits these global values and **passes the
filter as a false positive**, while genuine sessions with tag gaps get dropped.
That's the "saw some non-noz sessions and was missing others" failure.

(The original locked design flagged exactly this risk — "choose-tree can't
filter on session-environment variables." The nuance learned here: the *format
filter* technically runs, but the global-env fallback makes it unreliable, so
the conclusion stands.)

## The fix: noz resolves names, choose-tree filters on `session_name`

Split the work along the reliability line:

1. **noz resolves names.** `noz pick <view>` reads each session's env correctly
   in Go (via the existing `discoverSessions` / `getTmuxDetails` helpers — the
   same source as the `noz ls` dashboard, with `NOZ_PARENT` now plumbed
   through) and filters to the matching sessions. The current session is kept —
   `choose-tree` highlights where you are, mirroring the native `prefix+s` tree.
2. **tmux filters on `session_name`.** `noz pick <view> --filter` emits a tmux
   format-filter that matches those names by `session_name` — a true per-row
   primitive that is **never** subject to the global-env fallback:

   ```
   #{||:#{==:#{session_name},dev},#{==:#{session_name},docs-guide}}
   ```

   An empty match set emits `0` (matches nothing; `choose-tree` shows an empty
   tree rather than erroring). The expression is space-free, so it survives the
   binding's unquoted command substitution.

## The binding

```tmux
bind-key g run-shell "tmux set-option -g @noz_pick \"\$(noz pick repo --filter)\"" \; \
           choose-tree -Zs -f "#{E:#{@noz_pick}}"
```

Two steps, chained with `\;`:

1. `run-shell` (synchronous — it blocks the command queue) stashes the resolved
   filter into the `@noz_pick` user option.
2. `choose-tree` runs **natively in the client** (not under `run-shell`, so there
   is no client-targeting ambiguity) and reads the filter back. `#{E:...}`
   forces a second format-expansion pass, because `#{@noz_pick}` expands to a
   *string that is itself a format* — without `#{E:}` tmux would use the literal
   filter text (always truthy) and show everything.

The `children` and `repo` views need "who am I" — `noz pick` detects the current
session via `tmux display-message` (overridable with `--current` / `--repo` for
tests and scripts).

## `noz pick` — the command

```
noz pick [repo|children|all] [--json|--filter] [--current NAME] [--repo REPO]
```

| Form | Use |
|------|-----|
| *(default)* `\<session\>\t\<label\>` lines | scripting / your own picker |
| `--json`   | structured: `[{session, repo, parent, agent, state}]` (also handy for MCP) |
| `--filter` | the `choose-tree -f` expression the binding consumes |

Only **live** (tmux-backed) sessions are listed — idle worktrees aren't
switchable. The **current** session is kept (choose-tree highlights it).

## tmux version

The picker needs `choose-tree -f`, `set -F` (none used here, but `set-option`
of a format string is), and `#{E:}` — all present since the tmux 3.x format
work. The repo's existing **tmux 3.2+** requirement covers it. Crucially, the
picker does *not* depend on session-environment variables resolving inside a
filter (the unreliable path above); it only relies on `session_name`.

## Keys are defaults, not assumptions

`noz setup tmux` is print-only — it never edits `~/.tmux.conf`. Every binding is
gated on its key being non-empty and overridable, so it can't silently clobber a
user's own macros:

```bash
noz setup tmux --repo-key C-s --all-key C-a   # rebind
noz setup tmux --children-key ""              # drop a binding
```

## On-brand

Stateless and derived-live: the picker reads the same git/tmux/filesystem
signals as the rest of noz and stores nothing of its own (the one `@noz_pick`
user option is a transient scratch value, overwritten on every keypress). The
vocabulary stays in the tree metaphor (offshoots / children). The binding is
thin; the correctness-critical semantics live in one well-tested cobra command.
