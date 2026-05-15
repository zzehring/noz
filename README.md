# nozey

**Sovereign & lightweight agent supervisor harness.**

noz is a policy-enforced, provider-agnostic CLI for running AI coding agents safely. It gates every command and file access through [CEL](https://github.com/google/cel-go) policies, integrates with agent hook systems (Claude Code, Codex, Gemini CLI), and provides a tmux-native pairing workflow.

## Install

```bash
go install github.com/zzehring/nozey/cmd/noz@latest
```

## What it does

- **CEL policy gate** — every agent command evaluated against named rules. ALLOW, DENY, or PAUSE.
- **Agent hooks** — integrates via PreToolUse hooks. Agent doesn't know about noz, it just sees commands blocked.
- **File access control** — gates Read, Write, Edit tools. Blocks sensitive files (.env, .pem, .ssh/, .aws/).
- **Pairing sessions** — `noz pair` creates git worktrees + tmux sessions (replaces manual tmux/worktree management).
- **Provider-agnostic** — local process today, MicroVM (SmolVM/Firecracker) tomorrow. Same CLI, same policies.

## Quick start

```bash
# Gate your Claude Code session with readonly policy
noz setup claude --policy readonly --project-only

# Now every command Claude runs is CEL-evaluated:
#   kubectl get pods    → ALLOW
#   curl example.com    → DENY
#   git push            → PAUSE
#   cat .env            → DENY

# Start a pairing session
noz pair feature-auth           # git worktree + tmux
noz pair --pr 456               # PR review mode
noz pair scratch --no-repo      # repo-less workspace

# Manage sessions
noz ls                          # list active sessions
noz rm feature-auth             # cleanup worktree + tmux

# Undo hooks
noz setup claude --remove --project-only
```

## Policies

Policies are YAML files with CEL expressions. Three shipped:

| Policy | Description |
|--------|-------------|
| `readonly` | Read-only CLI tools, blocks writes, blocks sensitive files |
| `dev` | Allows code changes, pauses on git push and PR creation |
| `sre` | Read-only infra, pauses on any k8s/git mutation |

Example rule:

```yaml
rules:
  - name: block-sensitive-files
    description: Never read secrets, keys, credentials
    cel: |
      request.tool in ["read", "write", "edit"] &&
        request.path.matches(".*\\.(env|pem|key|credentials)$")
    verdict: DENY
```

## Commands

| Command | Description |
|---------|-------------|
| `noz setup <agent>` | Configure agent hooks (claude, codex, gemini) |
| `noz pair <slug>` | Start a pairing session (worktree + tmux) |
| `noz ls` | List active sessions |
| `noz rm <slug>` | Remove session (worktree + tmux) |
| `noz gate` | Policy evaluation endpoint (called by hooks) |
| `noz policy check` | Dry-run a command against policy |
| `noz policy validate` | Validate a policy file |
| `noz policy list` | List available policies |

## How it works

```
Agent (Claude Code, Codex, Gemini CLI)
  │
  ├─ Bash tool call: "kubectl delete pod web-1"
  │   └─ PreToolUse hook fires
  │       └─ echo $CLAUDE_TOOL_INPUT | noz gate --tool bash --policy readonly.yaml
  │           └─ shellparse → CEL evaluate → DENY (exit 2)
  │               └─ Agent sees: "command blocked"
  │
  ├─ Read tool call: "/home/user/.ssh/id_rsa"
  │   └─ PreToolUse hook fires
  │       └─ noz gate --tool read → DENY (block-sensitive-dirs)
  │
  └─ Read tool call: "src/main.go"
      └─ PreToolUse hook fires
          └─ noz gate --tool read → ALLOW
```

## Roadmap

- [x] CEL policy gate with YAML format
- [x] Agent hook integration (Claude Code, Codex, Gemini)
- [x] File access control (Read, Write, Edit)
- [x] Pairing sessions (worktree + tmux)
- [x] Session management (ls, rm)
- [ ] MicroVM providers (SmolVM, Firecracker)
- [ ] Credential proxy (short-lived tokens)
- [ ] Guard tower (live audit log pane)
- [ ] `noz run` (autonomous agent dispatch)
- [ ] K8s hosted mode (Argo Events + Workflows)
