package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGrantBrainAccess(t *testing.T) {
	repo := "myrepo"
	want := []string{
		"additionalDirectories",
		"Read(.noz/**)", "Edit(.noz/**)", "Write(.noz/**)",
	}

	t.Run("writes RW brain permissions", func(t *testing.T) {
		root := t.TempDir()
		t.Setenv("NOZ_ROOT", root)
		wtDir := filepath.Join(root, repo+"-feature")
		if err := os.MkdirAll(wtDir, 0755); err != nil {
			t.Fatal(err)
		}

		grantBrainAccess(wtDir, repo, "claude")

		data, err := os.ReadFile(filepath.Join(wtDir, ".claude", "settings.local.json"))
		if err != nil {
			t.Fatalf("settings.local.json not written: %v", err)
		}
		s := string(data)
		for _, w := range want {
			if !strings.Contains(s, w) {
				t.Errorf("settings missing %q; got:\n%s", w, s)
			}
		}
	})

	t.Run("respects NOZ_NO_GRANT_CONTEXT", func(t *testing.T) {
		root := t.TempDir()
		t.Setenv("NOZ_ROOT", root)
		t.Setenv("NOZ_NO_GRANT_CONTEXT", "1")
		wtDir := filepath.Join(root, repo+"-feature")
		if err := os.MkdirAll(wtDir, 0755); err != nil {
			t.Fatal(err)
		}

		grantBrainAccess(wtDir, repo, "claude")

		if _, err := os.Stat(filepath.Join(wtDir, ".claude", "settings.local.json")); !os.IsNotExist(err) {
			t.Errorf("expected no settings file when gated off, err = %v", err)
		}
	})

	t.Run("skips non-Claude agents", func(t *testing.T) {
		root := t.TempDir()
		t.Setenv("NOZ_ROOT", root)
		wtDir := filepath.Join(root, repo+"-feature")
		if err := os.MkdirAll(wtDir, 0755); err != nil {
			t.Fatal(err)
		}

		grantBrainAccess(wtDir, repo, "gemini")

		if _, err := os.Stat(filepath.Join(wtDir, ".claude", "settings.local.json")); !os.IsNotExist(err) {
			t.Errorf("expected no .claude config for a non-Claude agent, err = %v", err)
		}
	})

	t.Run("does not clobber an unparseable settings file", func(t *testing.T) {
		root := t.TempDir()
		t.Setenv("NOZ_ROOT", root)
		wtDir := filepath.Join(root, repo+"-feature")
		claudeDir := filepath.Join(wtDir, ".claude")
		if err := os.MkdirAll(claudeDir, 0755); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(claudeDir, "settings.local.json")
		original := "// user's JSONC config\n{ \"foo\": true }\n"
		if err := os.WriteFile(path, []byte(original), 0644); err != nil {
			t.Fatal(err)
		}

		grantBrainAccess(wtDir, repo, "claude")

		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != original {
			t.Errorf("clobbered an unparseable settings file:\ngot:  %q\nwant: %q", string(data), original)
		}
	})

	t.Run("idempotent — no duplicate rules", func(t *testing.T) {
		root := t.TempDir()
		t.Setenv("NOZ_ROOT", root)
		wtDir := filepath.Join(root, repo+"-feature")
		if err := os.MkdirAll(wtDir, 0755); err != nil {
			t.Fatal(err)
		}

		grantBrainAccess(wtDir, repo, "claude")
		grantBrainAccess(wtDir, repo, "claude")

		data, err := os.ReadFile(filepath.Join(wtDir, ".claude", "settings.local.json"))
		if err != nil {
			t.Fatal(err)
		}
		if n := strings.Count(string(data), "Read(.noz/**)"); n != 1 {
			t.Errorf("Read(.noz/**) appears %d times, want 1 (not deduped)", n)
		}
	})
}

func TestSplitFrontmatter(t *testing.T) {
	t.Run("with frontmatter", func(t *testing.T) {
		front, body := splitFrontmatter("---\nwindows: []\n---\n# Body\ntext\n")
		if front != "windows: []" {
			t.Errorf("front = %q", front)
		}
		if body != "# Body\ntext\n" {
			t.Errorf("body = %q", body)
		}
	})
	t.Run("no frontmatter", func(t *testing.T) {
		src := "# Just a body\n"
		front, body := splitFrontmatter(src)
		if front != "" || body != src {
			t.Errorf("front=%q body=%q", front, body)
		}
	})
	t.Run("unterminated fence is treated as body", func(t *testing.T) {
		src := "---\nno closing fence\n"
		front, body := splitFrontmatter(src)
		if front != "" || body != src {
			t.Errorf("front=%q body=%q", front, body)
		}
	})
}

// TestResolveProfileBuiltinWindows checks a builtin profile renders its body
// template and parses its window frontmatter. HOME is isolated so a real user
// profile of the same name can't shadow the builtin.
func TestResolveProfileBuiltinWindows(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	body, windows, err := resolveProfile("troubleshoot", ProfileData{Slug: "incident-42", Repo: "infra"})
	if err != nil {
		t.Fatal(err)
	}
	if len(windows) != 2 || windows[0].Name != "k9s" || windows[1].Name != "agent" {
		t.Fatalf("windows = %+v", windows)
	}
	if !strings.Contains(body, "incident-42") || !strings.Contains(body, "infra") {
		t.Fatalf("body did not render template vars:\n%s", body)
	}
}

// TestProfilesmithEscapesTemplateVars guards the meta profile: its documented
// {{.Slug}} examples must render literally, not get expanded/error out.
func TestProfilesmithEscapesTemplateVars(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	body, _, err := resolveProfile("profilesmith", ProfileData{Slug: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "{{.Slug}}") {
		t.Fatal("profilesmith should render a literal {{.Slug}} in its docs")
	}
}
