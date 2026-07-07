// Package gate evaluates an agent's proposed tool calls against a CEL policy:
// first match wins, default deny.
package gate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

// Rule is a compiled CEL expression with a name for audit logging.
type Rule struct {
	Name    string
	Program cel.Program
	Source  string
}

// Gate evaluates CommandRequests against a chain of CEL rules.
type Gate struct {
	rules []Rule
}

// New creates a Gate from a policy file path. Supports both YAML (.yaml/.yml)
// and legacy CEL (.cel) formats. YAML is preferred.
func New(policyPath string) (*Gate, error) {
	ext := filepath.Ext(policyPath)
	switch ext {
	case ".yaml", ".yml":
		return LoadYAML(policyPath)
	case ".cel":
		data, err := os.ReadFile(policyPath)
		if err != nil {
			return nil, fmt.Errorf("reading policy %s: %w", policyPath, err)
		}
		return NewFromSource(string(data))
	default:
		// Try YAML first, fall back to CEL
		if g, err := LoadYAML(policyPath); err == nil {
			return g, nil
		}
		data, err := os.ReadFile(policyPath)
		if err != nil {
			return nil, fmt.Errorf("reading policy %s: %w", policyPath, err)
		}
		return NewFromSource(string(data))
	}
}

// NewFromSource creates a Gate from raw policy source text.
func NewFromSource(source string) (*Gate, error) {
	env, err := newCELEnv()
	if err != nil {
		return nil, fmt.Errorf("creating CEL env: %w", err)
	}

	blocks := splitRules(source)
	rules := make([]Rule, 0, len(blocks))

	for i, block := range blocks {
		expr := block.expr
		if expr == "" {
			continue
		}

		ast, issues := env.Compile(expr)
		if issues != nil && issues.Err() != nil {
			return nil, fmt.Errorf("compiling rule %d (%s): %w", i+1, block.name, issues.Err())
		}

		prg, err := env.Program(ast)
		if err != nil {
			return nil, fmt.Errorf("creating program for rule %d (%s): %w", i+1, block.name, err)
		}

		rules = append(rules, Rule{
			Name:    block.name,
			Program: prg,
			Source:  expr,
		})
	}

	return &Gate{rules: rules}, nil
}

// Evaluate runs the request through the rule chain. First match wins.
func (g *Gate) Evaluate(req *CommandRequest) GateResult {
	activation := map[string]interface{}{
		"request": map[string]interface{}{
			"tool":    req.Tool,
			"cmd":     req.Cmd,
			"args":    req.Args,
			"path":    req.Path,
			"content": req.Content,
			"env":     req.Env,
			"workdir": req.WorkDir,
			"agent":   req.Agent,
			"session": req.SessionID,
			"mode":    req.Mode,
		},
	}

	for _, rule := range g.rules {
		out, _, err := rule.Program.Eval(activation)
		if err != nil {
			// Fail closed: a rule that errors at eval time (e.g. an index
			// past the end of request.args on crafted input) must not be
			// silently skipped — that would let a later ALLOW rule match a
			// request an earlier DENY rule was written to catch. Deny.
			return GateResult{
				Verdict: Deny,
				Rule:    rule.Name,
				Reason:  fmt.Sprintf("rule evaluation error: %v", err),
			}
		}

		verdict := evalToVerdict(out)
		if verdict != "" {
			return GateResult{
				Verdict: verdict,
				Rule:    rule.Name,
				Reason:  fmt.Sprintf("matched rule: %s", rule.Name),
			}
		}
	}

	// Default deny if no rule matched
	return GateResult{
		Verdict: Deny,
		Rule:    "default-deny",
		Reason:  "no rule matched",
	}
}

// Rules returns the loaded rules (for introspection/testing).
func (g *Gate) Rules() []Rule {
	return g.rules
}

func evalToVerdict(out ref.Val) Verdict {
	if out.Type() == types.StringType {
		s := out.Value().(string)
		switch Verdict(s) {
		case Allow, Deny, Pause:
			return Verdict(s)
		case "":
			return "" // no match
		}
	}
	return ""
}

func newCELEnv() (*cel.Env, error) {
	return cel.NewEnv(
		cel.Variable("request", cel.MapType(cel.StringType, cel.DynType)),
	)
}

type ruleBlock struct {
	name string
	expr string
}

// splitRules splits a policy file into rule blocks separated by "---" lines.
// Lines starting with "//" are comments. A comment immediately before a rule
// block becomes the rule's name.
func splitRules(source string) []ruleBlock {
	var blocks []ruleBlock
	var currentLines []string
	var currentName string

	lines := strings.Split(source, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if trimmed == "---" {
			expr := strings.TrimSpace(strings.Join(currentLines, "\n"))
			if expr != "" {
				if currentName == "" {
					currentName = fmt.Sprintf("rule-%d", len(blocks)+1)
				}
				blocks = append(blocks, ruleBlock{name: currentName, expr: expr})
			}
			currentLines = nil
			currentName = ""
			continue
		}

		if strings.HasPrefix(trimmed, "//") {
			// Use comment as rule name if it's the first line of a block
			if len(currentLines) == 0 {
				currentName = strings.TrimSpace(strings.TrimPrefix(trimmed, "//"))
			}
			continue
		}

		if trimmed != "" {
			currentLines = append(currentLines, line)
		}
	}

	// Last block
	expr := strings.TrimSpace(strings.Join(currentLines, "\n"))
	if expr != "" {
		if currentName == "" {
			currentName = fmt.Sprintf("rule-%d", len(blocks)+1)
		}
		blocks = append(blocks, ruleBlock{name: currentName, expr: expr})
	}

	return blocks
}

// ListPolicies returns the names of policy files (.yaml, .yml, .cel) in a directory.
func ListPolicies(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading policies dir %s: %w", dir, err)
	}
	seen := make(map[string]bool)
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := filepath.Ext(e.Name())
		if ext == ".yaml" || ext == ".yml" || ext == ".cel" {
			name := strings.TrimSuffix(e.Name(), ext)
			if !seen[name] {
				seen[name] = true
				names = append(names, name)
			}
		}
	}
	return names, nil
}
