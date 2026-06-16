package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func newReapCmd() *cobra.Command {
	var idle string
	var force bool

	cmd := &cobra.Command{
		Use:   "reap [filter]",
		Short: "Kill idle agents to reclaim memory (keeps worktree + tmux)",
		Long: `Frees memory held by coding agents that are idle — waiting for input and
quiet for a while — without tearing down the session. The worktree and tmux
session stay; only the agent process is killed (SIGTERM, so it can checkpoint
its transcript). Resume later with 'claude --continue' in the worktree.

Never touches a session that's attached or actively working. Dry-run by
default; pass --force to actually kill.

  noz reap                 # dry-run: show idle agents + reclaimable memory
  noz reap --force         # kill them
  noz reap --idle 2h       # only agents idle >= 2h
  noz reap cf --force      # only cf-* sessions`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			filter := ""
			if len(args) > 0 {
				filter = args[0]
			}
			return runReap(cmd, filter, idle, force)
		},
	}

	cmd.Flags().StringVar(&idle, "idle", "30m", "only reap agents idle at least this long (e.g. 30m, 2h, 1d)")
	cmd.Flags().BoolVar(&force, "force", false, "actually kill (default is a dry-run)")
	return cmd
}

func runReap(cmd *cobra.Command, filter, idleStr string, force bool) error {
	initColors()
	w := cmd.OutOrStdout()

	threshold, err := parseAge(idleStr)
	if err != nil {
		return fmt.Errorf("invalid --idle %q: %w", idleStr, err)
	}

	sessions, err := discoverSessions()
	if err != nil {
		return err
	}
	sessions = filterSessions(sessions, filter, true, false) // live sessions only

	procs := readProcInfo()
	tmuxBin, err := exec.LookPath("tmux")
	if err != nil {
		return fmt.Errorf("tmux not found")
	}

	action := "would reap"
	if force {
		action = "reaping"
	}

	reaped, reclaimed := 0, 0
	for _, s := range sessions {
		// Safety: never reap an attached session, a working one, or one with
		// no detected agent. Only idle-long-enough waiting agents.
		if s.attached || s.state != "waiting" || s.agent == "" {
			continue
		}
		if s.lastActive.IsZero() || time.Since(s.lastActive) < threshold {
			continue
		}

		pid := agentProcPID(tmuxBin, s.slug, s.agent, procs)
		if pid == "" {
			continue
		}
		mib := footprintMiB(pid)

		memStr := "?"
		if mib > 0 {
			memStr = fmt.Sprintf("~%dMiB", mib)
		}
		fmt.Fprintf(w, "  %s %-40s %s%s idle %s, %s%s\n",
			action, s.slug, cGray, s.agent, relativeTime(s.lastActive), memStr, cReset)

		if force {
			if err := exec.Command("kill", "-TERM", pid).Run(); err != nil {
				fmt.Fprintf(os.Stderr, "noz: warning: could not kill %s (pid %s): %v\n", s.slug, pid, err)
				continue
			}
		}
		reaped++
		reclaimed += mib
	}

	if reaped == 0 {
		fmt.Fprintf(w, "%snothing to reap (no idle agents past %s)%s\n", cGray, idleStr, cReset)
		return nil
	}

	verb := "would free"
	if force {
		verb = "freed"
	}
	fmt.Fprintf(w, "\n%s%s ~%dMiB across %d agent(s)%s\n", cGray, verb, reclaimed, reaped, cReset)
	if !force {
		fmt.Fprintf(w, "%srun with --force to kill; resume later with 'claude --continue'%s\n", cGray, cReset)
	} else {
		fmt.Fprintf(w, "%ssessions kept — resume an agent with 'claude --continue' in its worktree%s\n", cGray, cReset)
	}
	return nil
}

// procInfo is a snapshot of the process tree with command lines.
type procInfo struct {
	cmd      map[string]string
	children map[string][]string
}

func readProcInfo() procInfo {
	pi := procInfo{cmd: map[string]string{}, children: map[string][]string{}}
	out, err := exec.Command("ps", "-axo", "pid=,ppid=,command=").Output()
	if err != nil {
		return pi
	}
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		pid, ppid := f[0], f[1]
		if len(f) > 2 {
			pi.cmd[pid] = strings.Join(f[2:], " ")
		}
		pi.children[ppid] = append(pi.children[ppid], pid)
	}
	return pi
}

// agentProcPID finds the PID of the agent process running in a session by
// walking each pane's process tree for a command line mentioning the agent.
func agentProcPID(tmuxBin, session, agentName string, pi procInfo) string {
	out, err := exec.Command(tmuxBin, "list-panes", "-t", session, "-F", "#{pane_pid}").Output()
	if err != nil {
		return ""
	}
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		panePID := strings.TrimSpace(line)
		if panePID == "" {
			continue
		}
		if pid := pi.findAgent(panePID, agentName, map[string]bool{}); pid != "" {
			return pid
		}
	}
	return ""
}

func (pi procInfo) findAgent(pid, agentName string, seen map[string]bool) string {
	if seen[pid] {
		return ""
	}
	seen[pid] = true
	if strings.Contains(pi.cmd[pid], agentName) {
		return pid
	}
	for _, c := range pi.children[pid] {
		if found := pi.findAgent(c, agentName, seen); found != "" {
			return found
		}
	}
	return ""
}

// footprintMiB returns a process's physical footprint (resident + compressed)
// in MiB via macOS `footprint`, or 0 if unavailable. Slow (~1s) — call only on
// reap candidates, never on the ls hot path.
func footprintMiB(pid string) int {
	out, err := exec.Command("footprint", pid).Output()
	if err != nil {
		return 0
	}
	return parseFootprintMiB(string(out))
}

// parseFootprintMiB extracts the phys_footprint value from `footprint` output
// and normalizes it to MiB. Returns 0 when no value is found.
func parseFootprintMiB(out string) int {
	for line := range strings.SplitSeq(out, "\n") {
		// e.g. "    phys_footprint: 53 MB"
		_, after, ok := strings.Cut(line, "phys_footprint:")
		if !ok {
			continue
		}
		f := strings.Fields(after)
		if len(f) == 0 {
			continue
		}
		val, err := strconv.ParseFloat(f[0], 64)
		if err != nil {
			continue
		}
		unit := ""
		if len(f) > 1 {
			unit = f[1]
		}
		switch unit {
		case "GB", "G":
			return int(val * 1024)
		case "KB", "K":
			return int(val / 1024)
		default: // MB / M / bytes-ish
			return int(val)
		}
	}
	return 0
}
