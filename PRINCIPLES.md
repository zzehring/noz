# noz principles

The tenets noz is built on. They are defaults with strong gravity — break one
only *deliberately*, when the value is clear, and write down why. An amendment
is a choice; a violation is an accident. Avoid the latter.

## 1. Stateless
Derive everything from the filesystem, git, and tmux. Store nothing that can't be
thrown away — the only persisted bytes are disposable caches that regenerate.
→ can't drift, can't corrupt, deletes clean.

## 2. Lightweight
One static binary, no daemon, instant. Compose proven primitives (tmux, git)
rather than reinvent them. "Lightweight" is footprint and speed, not command count.
→ fast, trustworthy, trivial to adopt or remove.

## 3. The human is always in the loop
The agent sees, suggests, and navigates — but **create and destroy are gated**,
and noz never hands the agent free-form command execution.
→ the core value *and* the safety: every token is spent under human judgment.

## 4. Respect the machine and the data
Print config to paste rather than editing the user's files; back up before any
write; detect and adapt, don't impose. Emit nothing without opt-in. Expose
metadata, never the contents of files or conversations.
→ respect the machine you run on; your data is yours.

## 5. Safe and resilient by construction
Destructive paths are scoped so they can't reach your work — validated names,
guarded to the worktree root, teardown that can't touch a worktree, prune that
skips dirty, deletes that never force. And a missing dependency or partial
failure degrades gracefully: noz does less, never damage (atomic writes,
fallbacks, errors over corruption).
→ safety is a property of the design, not a scary prompt.

## 6. Honest over clever
Don't claim "stateless" if there's state, or "10x" because it sounds good.
Surface the caveats.
→ an accurate mental model is worth more than a tagline.

## 7. Transparent
What noz does is visible and auditable — it announces its actions, and
destructive ops leave a trail you can read. (An append-only log is still
throwaway-able, so this stays compatible with #1.)
→ you can always answer "what did it do, and when?"

---

_What noz is: an extension of tmux + git worktrees, made agentic-workflow-native,
with the human always at the controls. It's for terminal users who've recognized
that the terminal is the best place to run agents and juggle many workstreams at
once — not a bloated IDE. It should make a newcomer a power user without making
them learn a pile of arcana: the agent is the friendly UI over a sharp substrate.
The launcher is table stakes; the cockpit — see, know, go, return, reclaim,
recover, all human-gated — is the point._
