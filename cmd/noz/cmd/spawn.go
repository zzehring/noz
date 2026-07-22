package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zzehring/noz/internal/agent"
)

// spawnSpec describes one offshoot session to create.
type spawnSpec struct {
	Slug   string
	Task   string
	Source string // base branch to start from; "" = fresh from current HEAD
}

func newSpawnCmd() *cobra.Command {
	var task, source, agentName string
	var launch bool

	cmd := &cobra.Command{
		Use:   "spawn <slug>",
		Short: "Create an offshoot session (worktree + tmux) seeded with a task",
		Long: `Creates a task-scoped "offshoot" session in the current repo: a worktree,
a detached tmux session, and a seeded context file in the .noz brain (never the
repo tree). If spawned from inside a session, that session is recorded as the
parent, so 'noz close' returns you there when the work is done.

By default the agent is NOT started (the session is staged); pass --launch to
start it reading the seeded context immediately.

  noz spawn fix-flaky --task "Fix the flaky TestFoo retries"
  noz spawn review-1234 --source main --task "Review PR 1234" --launch`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			parent := currentTmuxSession()
			dir, err := spawnOffshoot(spawnSpec{Slug: args[0], Task: task, Source: source}, parent, agentName, launch)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "noz: spawned %s at %s\n", args[0], dir)
			if parent != "" {
				fmt.Fprintf(os.Stderr, "noz: parent = %s (noz close returns there)\n", parent)
			}
			if launch {
				fmt.Fprintf(os.Stderr, "noz: enter with `noz open %s`\n", args[0])
			} else {
				fmt.Fprintf(os.Stderr, "noz: staged (no agent). Start it with `noz open %s --agent %s`\n", args[0], agentName)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&task, "task", "", "what this offshoot should work on (seeds its brief)")
	cmd.Flags().StringVar(&source, "source", "", "base branch to start from (default: current HEAD)")
	cmd.Flags().StringVar(&agentName, "agent", "claude", "agent to launch with --launch")
	_ = cmd.RegisterFlagCompletionFunc("agent", completeAgents)
	cmd.Flags().BoolVar(&launch, "launch", false, "start the agent immediately, reading the seeded brief")

	return cmd
}

// spawnOffshoot creates a detached, context-seeded offshoot session in the
// current repo, tagged with its parent for return-on-close. Returns the worktree
// dir. When launch is true and agentName is set, window 0 runs that agent with a
// directive to read the seeded context; otherwise window 0 is a plain shell.
// Idempotent: an existing worktree/session is reused, not clobbered.
func spawnOffshoot(spec spawnSpec, parent, agentName string, launch bool) (string, error) {
	if err := validNewSlug(spec.Slug); err != nil {
		return "", err
	}
	repo, err := repoName()
	if err != nil {
		return "", err
	}
	root := nozRoot()
	wtDir := filepath.Join(root, repo+"-"+spec.Slug)
	if err := os.MkdirAll(root, 0755); err != nil {
		return "", fmt.Errorf("creating root dir: %w", err)
	}

	if !dirExists(wtDir) {
		var args []string
		if branchExists(spec.Slug) {
			args = []string{"worktree", "add", wtDir, spec.Slug}
		} else {
			args = []string{"worktree", "add", wtDir, "-b", spec.Slug}
			if spec.Source != "" {
				args = append(args, spec.Source)
			}
		}
		if err := runGit(args...); err != nil {
			return "", fmt.Errorf("creating worktree: %w", err)
		}
	}

	linkNozDir(root, repo, wtDir)

	if err := writeOffshootBrief(repo, spec.Slug, spec.Task, parent); err != nil {
		return "", err
	}
	// Only grant now if we're launching Claude into the offshoot immediately;
	// otherwise the later `noz open <slug>` grants for whatever agent opens it.
	grantAgent := ""
	if launch {
		grantAgent = agentName
	}
	grantBrainAccess(wtDir, repo, grantAgent) // Claude-only: read+write the brain without prompting

	tmuxBin, err := exec.LookPath("tmux")
	if err != nil {
		return "", fmt.Errorf("tmux not found")
	}
	if tmuxHasSession(spec.Slug) {
		return wtDir, nil // already live — leave it as-is
	}

	createArgs := []string{"new", "-d", "-s", spec.Slug, "-c", wtDir}
	if launch && agentName != "" {
		a, ok := agent.Lookup(agentName)
		if !ok {
			return "", fmt.Errorf("unknown agent %q (known: %s)", agentName, strings.Join(agent.Names(), ", "))
		}
		directive := fmt.Sprintf("Read %s for this session's context, then help me with it.", briefRef(spec.Slug))
		createArgs = append(createArgs, "-n", a.Name, shellJoin(a.LaunchWith(directive)))
	}
	c := exec.Command(tmuxBin, createArgs...)
	c.Env = append(os.Environ(), "NOZ_SLUG="+spec.Slug, "NOZ_REPO="+repo)
	if err := c.Run(); err != nil {
		return "", fmt.Errorf("creating session: %w", err)
	}
	tagNozSession(tmuxBin, spec.Slug, spec.Slug, repo)
	if parent != "" {
		_ = exec.Command(tmuxBin, "set-environment", "-t", sessionTarget(spec.Slug), "NOZ_PARENT", parent).Run()
	}
	return wtDir, nil
}

// writeSessionBrief seeds a session's task note into the .noz brain.
// Simpler than writeOffshootBrief — no parent/return boilerplate.
func writeSessionBrief(repo, slug, task string) error {
	var b strings.Builder
	fmt.Fprintf(&b, "# Session: %s\n\n", slug)
	fmt.Fprintf(&b, "## Task\n\n%s\n", strings.TrimSpace(task))
	ctxPath := briefPath(repo, slug)
	if err := os.MkdirAll(filepath.Dir(ctxPath), 0755); err != nil {
		return fmt.Errorf("writing session context: %w", err)
	}
	return os.WriteFile(ctxPath, []byte(b.String()), 0644)
}

// writeOffshootBrief seeds an offshoot's marching orders into the .noz brain:
// its task plus, when it has a parent, how to return when done.
func writeOffshootBrief(repo, slug, task, parent string) error {
	var b strings.Builder
	fmt.Fprintf(&b, "# Offshoot: %s\n\n", slug)
	if parent != "" {
		fmt.Fprintf(&b, "You are an offshoot session spawned from `%s`. When this task is\n", parent)
		fmt.Fprintf(&b, "complete, run `noz close` to tear it down and return to `%s`\n", parent)
		fmt.Fprintf(&b, "(or `noz back` to just hop back without removing it).\n\n")
	}
	fmt.Fprintf(&b, "## Task\n\n%s\n", strings.TrimSpace(task))

	ctxPath := briefPath(repo, slug)
	if err := os.MkdirAll(filepath.Dir(ctxPath), 0755); err != nil {
		return fmt.Errorf("writing offshoot context: %w", err)
	}
	return os.WriteFile(ctxPath, []byte(b.String()), 0644)
}
