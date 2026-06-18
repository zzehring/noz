package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
)

func newMcpCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Run noz as an MCP server (agent-facing session awareness)",
		Long: `Runs an MCP server over stdio so an agent (e.g. Claude Code) can see your
noz sessions — list them and read the current one — and know what else you're
working on across all your contexts.

Point your agent at it, e.g. Claude Code .mcp.json:

  { "mcpServers": { "noz": { "command": "noz", "args": ["mcp"] } } }

This is Layer 1: read-only awareness. Navigation/spawn tools come later.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMCP(cmd.Context())
		},
	}
}

func runMCP(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	s := mcp.NewServer(&mcp.Implementation{Name: "noz", Version: "0.1.0"}, nil)

	mcp.AddTool(s, &mcp.Tool{
		Name: "noz_sessions",
		Description: "List noz sessions (git worktree + tmux) across all repos, with " +
			"each one's live/idle state, the coding agent running in it, working/waiting " +
			"status, last activity, and repo. Use this to see what else is in progress.",
	}, mcpSessions)

	mcp.AddTool(s, &mcp.Tool{
		Name: "noz_status",
		Description: "Report the current session's context: slug, repo, branch, the agent " +
			"running, and working/waiting state. Use this to know where you are.",
	}, mcpStatus)

	mcp.AddTool(s, &mcp.Tool{
		Name: "noz_switch",
		Description: "Switch the user's tmux client to the live noz session with the given " +
			"slug — i.e. move them to that context. Use after noz_sessions to navigate the " +
			"user between their workstreams. Only works when running inside tmux.",
	}, mcpSwitch)

	mcp.AddTool(s, &mcp.Tool{
		Name: "noz_back",
		Description: "Hop the user's tmux client back to the previous session (their last " +
			"hop). Use to return them where they came from. noz_status reports the last " +
			"session under \"last\".",
	}, mcpBack)

	mcp.AddTool(s, &mcp.Tool{
		Name: "noz_spawn",
		Description: "Create one or more task-scoped 'offshoot' sessions (git worktree + " +
			"tmux) in the current repo, each seeded with its task as context and tagged " +
			"with the current session as its parent (so `noz close` returns the user there). " +
			"This CREATES worktrees/sessions — a gated action; the user confirms it. Set " +
			"launch=true to also start the coding agent in each (reading its seeded context) " +
			"when the user wants to begin working immediately; leave it false to just stage " +
			"them. Fill slug/task/source yourself from the user's intent (issues, PRs, the " +
			"conversation).",
	}, mcpSpawn)

	mcp.AddTool(s, &mcp.Tool{
		Name: "noz_rm",
		Description: "Tear down one or more offshoot sessions by slug — removes each one's git " +
			"worktree, tmux session, and seeded context file. The destroy bookend to noz_spawn. " +
			"This DESTROYS worktrees/sessions — a gated action; the user confirms it. Refuses to " +
			"remove the current session (use noz_back/`noz close` to leave it first). A dirty " +
			"worktree is left untouched unless force=true (discards uncommitted changes). Set " +
			"delete_branch=true to also drop each local branch (safe delete — keeps unmerged work).",
	}, mcpRm)

	return s.Run(ctx, &mcp.StdioTransport{})
}

// --- tool I/O types --------------------------------------------------------

type mcpSessionsInput struct {
	Filter string `json:"filter,omitempty" jsonschema:"optional substring, or ^prefix, to narrow the list"`
}

type mcpSession struct {
	Slug       string `json:"slug"`
	Repo       string `json:"repo,omitempty"`
	Category   string `json:"category,omitempty"`
	Live       bool   `json:"live"`
	Attached   bool   `json:"attached"`
	Agent      string `json:"agent,omitempty"`
	State      string `json:"state,omitempty"`
	LastActive string `json:"last_active,omitempty"`
	Windows    int    `json:"windows,omitempty"`
	Dir        string `json:"dir"`
}

type mcpSessionsOutput struct {
	Sessions []mcpSession `json:"sessions"`
}

type mcpEmptyInput struct{}

// --- handlers --------------------------------------------------------------

func mcpSessions(ctx context.Context, req *mcp.CallToolRequest, in mcpSessionsInput) (*mcp.CallToolResult, mcpSessionsOutput, error) {
	sessions, err := discoverSessions()
	if err != nil {
		return nil, mcpSessionsOutput{}, err
	}
	sessions = filterSessions(sessions, in.Filter, false, false)

	out := mcpSessionsOutput{Sessions: []mcpSession{}}
	for _, s := range sessions {
		m := mcpSession{
			Slug:     s.slug,
			Repo:     s.repo,
			Category: s.category,
			Live:     s.hasTmux,
			Attached: s.attached,
			Agent:    s.agent,
			State:    s.state,
			Windows:  s.windows,
			Dir:      s.dir,
		}
		if !s.lastActive.IsZero() {
			m.LastActive = relativeTime(s.lastActive)
		}
		out.Sessions = append(out.Sessions, m)
	}
	return nil, out, nil
}

func mcpStatus(ctx context.Context, req *mcp.CallToolRequest, in mcpEmptyInput) (*mcp.CallToolResult, statusInfo, error) {
	return nil, gatherStatus(), nil
}

type mcpSwitchInput struct {
	Slug string `json:"slug" jsonschema:"the live session slug to switch the tmux client to (see noz_sessions)"`
}

type mcpSwitchOutput struct {
	Switched bool   `json:"switched"`
	Message  string `json:"message"`
}

func mcpSwitch(ctx context.Context, req *mcp.CallToolRequest, in mcpSwitchInput) (*mcp.CallToolResult, mcpSwitchOutput, error) {
	if err := validSlug(in.Slug); err != nil {
		return nil, mcpSwitchOutput{Message: err.Error()}, nil
	}
	if !tmuxHasSession(in.Slug) {
		return nil, mcpSwitchOutput{Message: fmt.Sprintf("no live session %q — list with noz_sessions; if it's idle, it needs `noz restore`/`noz open` first", in.Slug)}, nil
	}
	if os.Getenv("TMUX") == "" {
		return nil, mcpSwitchOutput{Message: "not running inside tmux — can't switch the client from here"}, nil
	}
	tmuxBin, err := exec.LookPath("tmux")
	if err != nil {
		return nil, mcpSwitchOutput{Message: "tmux not found"}, nil
	}
	if err := exec.Command(tmuxBin, "switch-client", "-t", in.Slug).Run(); err != nil {
		return nil, mcpSwitchOutput{Message: fmt.Sprintf("switch failed: %v", err)}, nil
	}
	return nil, mcpSwitchOutput{Switched: true, Message: "switched to " + in.Slug}, nil
}

// --- noz_spawn ---

type mcpSpawnSpec struct {
	Slug   string `json:"slug" jsonschema:"short kebab-case session name, e.g. fix-flaky or review-1234"`
	Task   string `json:"task" jsonschema:"what this offshoot should work on; becomes its seeded context"`
	Source string `json:"source,omitempty" jsonschema:"base branch to start from; empty means fresh from the current HEAD"`
}

type mcpSpawnInput struct {
	Sessions []mcpSpawnSpec `json:"sessions" jsonschema:"the offshoot sessions to create"`
	Launch   bool           `json:"launch,omitempty" jsonschema:"also start the coding agent in each, reading its context (default false: stage only)"`
	Agent    string         `json:"agent,omitempty" jsonschema:"agent to launch when launch=true (default claude)"`
}

type mcpSpawnResult struct {
	Slug  string `json:"slug"`
	Dir   string `json:"dir,omitempty"`
	Error string `json:"error,omitempty"`
}

type mcpSpawnOutput struct {
	Created []mcpSpawnResult `json:"created"`
	Parent  string           `json:"parent,omitempty"`
	Message string           `json:"message"`
}

func mcpSpawn(ctx context.Context, req *mcp.CallToolRequest, in mcpSpawnInput) (*mcp.CallToolResult, mcpSpawnOutput, error) {
	parent := currentTmuxSession()
	agentName := in.Agent
	if agentName == "" {
		agentName = "claude"
	}

	out := mcpSpawnOutput{Parent: parent}
	created, failed := 0, 0
	for _, s := range in.Sessions {
		dir, err := spawnOffshoot(spawnSpec{Slug: s.Slug, Task: s.Task, Source: s.Source}, parent, agentName, in.Launch)
		r := mcpSpawnResult{Slug: s.Slug, Dir: dir}
		if err != nil {
			r.Error = err.Error()
			failed++
		} else {
			created++
		}
		out.Created = append(out.Created, r)
	}

	verb := "staged"
	if in.Launch {
		verb = "launched"
	}
	out.Message = fmt.Sprintf("%s %d offshoot session(s)", verb, created)
	if failed > 0 {
		out.Message += fmt.Sprintf(", %d failed", failed)
	}
	if created > 0 {
		out.Message += ". Use noz_switch to move the user into one."
	}
	return nil, out, nil
}

// --- noz_rm ---

type mcpRmInput struct {
	Slugs        []string `json:"slugs" jsonschema:"the offshoot session slugs to tear down"`
	Force        bool     `json:"force,omitempty" jsonschema:"discard a dirty worktree instead of leaving it untouched (default false)"`
	DeleteBranch bool     `json:"delete_branch,omitempty" jsonschema:"also delete each local branch with a safe delete (default false)"`
}

type mcpRmResult struct {
	Slug  string `json:"slug"`
	Error string `json:"error,omitempty"`
}

type mcpRmOutput struct {
	Removed []mcpRmResult `json:"removed"`
	Message string        `json:"message"`
}

func mcpRm(ctx context.Context, req *mcp.CallToolRequest, in mcpRmInput) (*mcp.CallToolResult, mcpRmOutput, error) {
	var out mcpRmOutput
	removed, failed := 0, 0
	for _, slug := range in.Slugs {
		// runRm guards against removing the current session and reports nothing
		// found — reuse it so the MCP path and `noz rm` stay identical.
		err := runRm(slug, in.Force, false, in.DeleteBranch)
		r := mcpRmResult{Slug: slug}
		if err != nil {
			r.Error = err.Error()
			failed++
		} else {
			removed++
		}
		out.Removed = append(out.Removed, r)
	}

	out.Message = fmt.Sprintf("removed %d offshoot session(s)", removed)
	if failed > 0 {
		out.Message += fmt.Sprintf(", %d failed", failed)
	}
	return nil, out, nil
}

func mcpBack(ctx context.Context, req *mcp.CallToolRequest, in mcpEmptyInput) (*mcp.CallToolResult, mcpSwitchOutput, error) {
	last := lastTmuxSession()
	if last == "" {
		return nil, mcpSwitchOutput{Message: "no previous session to hop back to"}, nil
	}
	if os.Getenv("TMUX") == "" {
		return nil, mcpSwitchOutput{Message: "not running inside tmux — can't switch the client from here"}, nil
	}
	tmuxBin, err := exec.LookPath("tmux")
	if err != nil {
		return nil, mcpSwitchOutput{Message: "tmux not found"}, nil
	}
	if err := exec.Command(tmuxBin, "switch-client", "-l").Run(); err != nil {
		return nil, mcpSwitchOutput{Message: fmt.Sprintf("back failed: %v", err)}, nil
	}
	return nil, mcpSwitchOutput{Switched: true, Message: "hopped back to " + last}, nil
}
