package gate

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// PolicyFile is the YAML policy format.
type PolicyFile struct {
	Name        string       `yaml:"name"`
	Description string       `yaml:"description,omitempty"`
	Rules       []PolicyRule `yaml:"rules"`
}

// PolicyRule is a single rule in a YAML policy.
type PolicyRule struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description,omitempty"`
	CEL         string `yaml:"cel,omitempty"` // CEL expression (omit for unconditional verdict)
	Verdict     string `yaml:"verdict"`       // ALLOW, DENY, or PAUSE
}

// LoadYAML creates a Gate from a YAML policy file.
func LoadYAML(path string) (*Gate, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading policy %s: %w", path, err)
	}
	return NewFromYAML(data)
}

// NewFromYAML creates a Gate from YAML policy bytes.
func NewFromYAML(data []byte) (*Gate, error) {
	var pf PolicyFile
	if err := yaml.Unmarshal(data, &pf); err != nil {
		return nil, fmt.Errorf("parsing YAML policy: %w", err)
	}

	if len(pf.Rules) == 0 {
		return nil, fmt.Errorf("policy has no rules")
	}

	// Convert YAML rules to CEL source that the existing engine can compile
	// Each rule becomes: <cel_expr> ? "<VERDICT>" : ""
	// Rules with no CEL expression are unconditional: "<VERDICT>"
	var celSource string
	for i, rule := range pf.Rules {
		verdict := strings.ToUpper(rule.Verdict)
		if verdict != "ALLOW" && verdict != "DENY" && verdict != "PAUSE" {
			return nil, fmt.Errorf("rule %d (%s): invalid verdict %q (must be ALLOW, DENY, or PAUSE)", i+1, rule.Name, rule.Verdict)
		}

		if i > 0 {
			celSource += "\n---\n"
		}

		celSource += fmt.Sprintf("// %s\n", rule.Name)
		if rule.CEL != "" {
			celSource += fmt.Sprintf("%s\n  ? %q\n  : \"\"", strings.TrimSpace(rule.CEL), verdict)
		} else {
			celSource += fmt.Sprintf("%q", verdict)
		}
	}

	return NewFromSource(celSource)
}
