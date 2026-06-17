package cmd

import (
	"context"

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
		Description: "List noz pairing sessions (git worktree + tmux) across all repos, with " +
			"each one's live/idle state, the coding agent running in it, working/waiting " +
			"status, last activity, and repo. Use this to see what else is in progress.",
	}, mcpSessions)

	mcp.AddTool(s, &mcp.Tool{
		Name: "noz_status",
		Description: "Report the current session's context: slug, repo, branch, the agent " +
			"running, and working/waiting state. Use this to know where you are.",
	}, mcpStatus)

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
