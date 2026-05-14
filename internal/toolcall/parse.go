package toolcall

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/zzehring/nozey/internal/gate"
)

// Parse converts a JSON string into a CommandRequest.
// Rejects any input containing shell metacharacters in cmd or args.
func Parse(input string) (*gate.CommandRequest, error) {
	var req gate.CommandRequest
	if err := json.Unmarshal([]byte(input), &req); err != nil {
		return nil, fmt.Errorf("parsing JSON: %w", err)
	}

	if req.Cmd == "" {
		return nil, fmt.Errorf("cmd is required")
	}

	if err := validateNoShell(req.Cmd); err != nil {
		return nil, fmt.Errorf("cmd: %w", err)
	}

	for i, arg := range req.Args {
		if err := validateNoShell(arg); err != nil {
			return nil, fmt.Errorf("args[%d]: %w", i, err)
		}
	}

	// Default mode to autonomous if not set
	if req.Mode == "" {
		req.Mode = "autonomous"
	}

	return &req, nil
}

// shellMeta are characters that could enable shell injection if passed to a shell.
// Since we use execve (no shell), these are blocked as a defense-in-depth measure.
var shellMeta = []string{";", "&&", "||", "|", "`", "$(", "${", ">", "<", "\n"}

func validateNoShell(s string) error {
	for _, meta := range shellMeta {
		if strings.Contains(s, meta) {
			return fmt.Errorf("shell metacharacter %q not allowed", meta)
		}
	}
	return nil
}
