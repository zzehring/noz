package gate

// CommandRequest is the structured representation of a tool call from an agent.
// Every field is available in CEL policy expressions via the "request" variable.
type CommandRequest struct {
	Cmd       string            `json:"cmd"`
	Args      []string          `json:"args"`
	Env       map[string]string `json:"env,omitempty"`
	WorkDir   string            `json:"workdir,omitempty"`
	Agent     string            `json:"agent,omitempty"`
	SessionID string            `json:"session,omitempty"`
	Mode      string            `json:"mode,omitempty"` // "autonomous" or "interactive"
}

// Verdict is the result of a CEL policy evaluation.
type Verdict string

const (
	Allow Verdict = "ALLOW"
	Deny  Verdict = "DENY"
	Pause Verdict = "PAUSE"
)

// GateResult contains the evaluation outcome plus which rule matched.
type GateResult struct {
	Verdict Verdict `json:"verdict"`
	Rule    string  `json:"rule"`
	Reason  string  `json:"reason,omitempty"`
}
