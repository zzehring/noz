package cmd

import (
	"strings"
	"testing"
)

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
