// Package shellparse extracts command names and arguments from shell command strings.
//
// It handles the subset of shell syntax that coding agents typically produce:
//   - Simple commands: "kubectl get pods -n prod"
//   - Quoted args: "echo 'hello world'" or "grep \"pattern\" file"
//   - Pipelines: "kubectl get pods | grep web"
//   - Chains: "cd /tmp && ls -la"
//   - Semicolons: "echo foo; echo bar"
//   - Environment prefixes: "KUBECONFIG=/path kubectl get pods"
//   - Subshells/command substitution: detected and flagged
//
// It does NOT try to be a full shell parser. The goal is to extract the binary
// names so the CEL gate can vet them. The actual execution still goes through
// the real shell.
package shellparse

import (
	"fmt"
	"strings"
	"unicode"
)

// Command represents a single command extracted from a shell string.
type Command struct {
	Name string   // the binary name (e.g., "kubectl")
	Args []string // arguments (e.g., ["get", "pods"])
}

// Parse splits a shell command string into one or more Commands.
// Each pipeline segment and chain segment becomes a separate Command.
func Parse(input string) ([]Command, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, nil
	}

	// Split on chain operators and pipes to get individual command segments
	segments := splitSegments(input)

	var commands []Command
	for _, seg := range segments {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}

		cmd, err := parseSegment(seg)
		if err != nil {
			return nil, err
		}
		if cmd != nil {
			commands = append(commands, *cmd)
		}
	}

	return commands, nil
}

// splitSegments splits on |, &&, ||, ; while respecting quotes.
func splitSegments(input string) []string {
	var segments []string
	var current strings.Builder
	inSingle := false
	inDouble := false
	escaped := false

	runes := []rune(input)
	for i := 0; i < len(runes); i++ {
		r := runes[i]

		if escaped {
			current.WriteRune(r)
			escaped = false
			continue
		}

		if r == '\\' && !inSingle {
			escaped = true
			current.WriteRune(r)
			continue
		}

		if r == '\'' && !inDouble {
			inSingle = !inSingle
			current.WriteRune(r)
			continue
		}

		if r == '"' && !inSingle {
			inDouble = !inDouble
			current.WriteRune(r)
			continue
		}

		if inSingle || inDouble {
			current.WriteRune(r)
			continue
		}

		// Check for operators
		if r == '|' {
			if i+1 < len(runes) && runes[i+1] == '|' {
				// ||
				segments = append(segments, current.String())
				current.Reset()
				i++ // skip second |
				continue
			}
			// |
			segments = append(segments, current.String())
			current.Reset()
			continue
		}

		if r == '&' {
			if i+1 < len(runes) && runes[i+1] == '&' {
				// &&
				segments = append(segments, current.String())
				current.Reset()
				i++ // skip second &
				continue
			}
			// A lone & backgrounds the left side and runs whatever follows,
			// so it separates commands just like ';'. Exclude the redirection
			// forms &> (next is '>') and >& / N>& (previous non-space is '>'),
			// where '&' is part of a redirect, not a separator.
			nextRedirect := i+1 < len(runes) && runes[i+1] == '>'
			prevRedirect := lastNonSpace(current.String()) == '>'
			if !nextRedirect && !prevRedirect {
				segments = append(segments, current.String())
				current.Reset()
				continue
			}
		}

		if r == ';' || r == '\n' {
			segments = append(segments, current.String())
			current.Reset()
			continue
		}

		current.WriteRune(r)
	}

	if s := current.String(); strings.TrimSpace(s) != "" {
		segments = append(segments, s)
	}

	return segments
}

// lastNonSpace returns the last non-whitespace rune of s, or 0 if none.
func lastNonSpace(s string) rune {
	runes := []rune(s)
	for i := len(runes) - 1; i >= 0; i-- {
		if !unicode.IsSpace(runes[i]) {
			return runes[i]
		}
	}
	return 0
}

// parseSegment parses a single command segment into a Command.
func parseSegment(seg string) (*Command, error) {
	seg = strings.TrimSpace(seg)

	// Skip env var prefixes (KEY=VALUE before command)
	tokens := tokenize(seg)
	startIdx := 0
	for startIdx < len(tokens) {
		if isEnvAssignment(tokens[startIdx]) {
			startIdx++
		} else {
			break
		}
	}

	if startIdx >= len(tokens) {
		return nil, nil // env-only segment (e.g., "FOO=bar")
	}

	name := tokens[startIdx]
	args := tokens[startIdx+1:]

	// Reject command and process substitution anywhere in the command or its
	// arguments — the gate parses before the real shell runs, so $(…), `…`,
	// and <(…) / >(…) would each execute a command unseen by any rule.
	for _, tok := range append([]string{name}, args...) {
		switch {
		case strings.Contains(tok, "$(") || strings.Contains(tok, "`"):
			return nil, fmt.Errorf("command substitution not allowed: %s", tok)
		case strings.Contains(tok, "<(") || strings.Contains(tok, ">("):
			return nil, fmt.Errorf("process substitution not allowed: %s", tok)
		}
	}

	return &Command{
		Name: name,
		Args: args,
	}, nil
}

// tokenize splits a command string into tokens respecting quotes.
func tokenize(input string) []string {
	var tokens []string
	var current strings.Builder
	inSingle := false
	inDouble := false
	escaped := false

	for _, r := range input {
		if escaped {
			current.WriteRune(r)
			escaped = false
			continue
		}

		if r == '\\' && !inSingle {
			escaped = true
			continue
		}

		if r == '\'' && !inDouble {
			inSingle = !inSingle
			continue
		}

		if r == '"' && !inSingle {
			inDouble = !inDouble
			continue
		}

		if unicode.IsSpace(r) && !inSingle && !inDouble {
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
			continue
		}

		current.WriteRune(r)
	}

	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}

	return tokens
}

func isEnvAssignment(s string) bool {
	idx := strings.Index(s, "=")
	if idx <= 0 {
		return false
	}
	// Check that everything before = is a valid env var name
	prefix := s[:idx]
	for i, r := range prefix {
		if i == 0 && !unicode.IsLetter(r) && r != '_' {
			return false
		}
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
			return false
		}
	}
	return true
}
