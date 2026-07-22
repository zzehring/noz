package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestNozHooksRoundTrip is the key safety property: installing noz hooks and
// then stripping them must leave the user's settings byte-for-byte as they
// were — no leftover keys, no clobbered values.
func TestNozHooksRoundTrip(t *testing.T) {
	userHook := func() map[string]any {
		return map[string]any{
			"matcher": "Bash",
			"hooks": []any{
				map[string]any{"type": "command", "command": "echo not-noz"},
			},
		}
	}

	cases := map[string]func() map[string]any{
		"empty settings": func() map[string]any {
			return map[string]any{}
		},
		"settings with unrelated keys": func() map[string]any {
			return map[string]any{"model": "opus", "theme": "dark"}
		},
		"settings with a pre-existing user hook": func() map[string]any {
			return map[string]any{
				"model": "opus",
				"hooks": map[string]any{"PreToolUse": []any{userHook()}},
			}
		},
	}

	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			orig := build()
			work := build()

			installNozHooks(work, "/usr/local/bin/noz", "readonly")
			stripNozHooks(work)

			if !reflect.DeepEqual(work, orig) {
				t.Fatalf("round-trip not clean\n got: %#v\nwant: %#v", work, orig)
			}
		})
	}
}

// TestInstallNozHooksIdempotent ensures re-running install doesn't duplicate.
func TestInstallNozHooksIdempotent(t *testing.T) {
	s := map[string]any{}
	installNozHooks(s, "noz", "readonly")
	installNozHooks(s, "noz", "readonly")

	pre := s["hooks"].(map[string]any)["PreToolUse"].([]any)
	if len(pre) != 4 {
		t.Fatalf("expected 4 hooks after double install, got %d", len(pre))
	}
}

// TestWriteJSONFileAtomicAndBackup verifies the write produces valid JSON and
// backs up any existing file before overwriting.
func TestWriteJSONFileAtomicAndBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	if err := writeJSONFile(path, map[string]any{"v": "1"}); err != nil {
		t.Fatal(err)
	}
	// No backup should exist on first write.
	if _, err := os.Stat(path + ".noz.bak"); !os.IsNotExist(err) {
		t.Fatal("backup created on first write")
	}

	if err := writeJSONFile(path, map[string]any{"v": "2"}); err != nil {
		t.Fatal(err)
	}

	// Current file holds v2, valid JSON.
	var got map[string]any
	data, _ := os.ReadFile(path)
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}
	if got["v"] != "2" {
		t.Fatalf("want v=2, got %v", got["v"])
	}
	// Backup holds the prior v1.
	bak, err := os.ReadFile(path + ".noz.bak")
	if err != nil {
		t.Fatalf("backup not written: %v", err)
	}
	var bakGot map[string]any
	if err := json.Unmarshal(bak, &bakGot); err != nil {
		t.Fatalf("backup is not valid JSON: %v", err)
	}
	if bakGot["v"] != "1" {
		t.Fatalf("backup should hold v=1, got %v", bakGot["v"])
	}
}

// TestNozTmuxSnippet covers the picker/jump bindings the snippet emits and the
// per-key opt-out, so we never silently clobber a user's own macros.
func TestNozTmuxSnippet(t *testing.T) {
	full := nozTmuxSnippet(tmuxKeys{repo: "g", all: "G", children: "C-g"})
	for _, want := range []string{
		"bind-key g run-shell",
		"noz pick repo --filter",
		"bind-key G run-shell",
		"noz pick all --filter",
		"bind-key C-g run-shell",
		"noz pick children --filter",
		"choose-tree -Zs -f \"#{E:#{@noz_pick}}\"", // native, double-expanded
	} {
		if !strings.Contains(full, want) {
			t.Errorf("snippet missing %q\n%s", want, full)
		}
	}
	// The default prefix+s tree must never be rebound.
	if strings.Contains(full, "bind-key s ") {
		t.Errorf("snippet must not touch prefix+s\n%s", full)
	}

	// Empty keys drop their bindings entirely.
	trimmed := nozTmuxSnippet(tmuxKeys{repo: "g"})
	if strings.Contains(trimmed, "pick all") || strings.Contains(trimmed, "pick children") {
		t.Errorf("empty keys should drop their bindings\n%s", trimmed)
	}
	if !strings.Contains(trimmed, "bind-key g run-shell") || !strings.Contains(trimmed, "noz pick repo --filter") {
		t.Errorf("repo binding should survive\n%s", trimmed)
	}
}
