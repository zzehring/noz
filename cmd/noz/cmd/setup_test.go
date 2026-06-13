package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
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
	json.Unmarshal(bak, &bakGot)
	if bakGot["v"] != "1" {
		t.Fatalf("backup should hold v=1, got %v", bakGot["v"])
	}
}
