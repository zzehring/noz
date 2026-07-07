package cmd

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestBrainWritePathsStayInNozDirs locks noz's side of the ownership contract:
// every path noz writes a session artifact to must live under context/ or
// reports/, and none may land inside the user-owned brain/. If someone reroutes
// an artifact into brain/ (or outside the brain entirely), this fails.
func TestBrainWritePathsStayInNozDirs(t *testing.T) {
	root := t.TempDir()
	t.Setenv("NOZ_ROOT", root)

	repo, slug := "myrepo", "feature-x"
	brainRoot := filepath.Join(root, ".noz", repo)

	cases := []struct {
		name string
		got  string
		sub  string
	}{
		{"context", contextFilePath(repo, slug), "context"},
		{"report", reportFilePath(repo, slug), "reports"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rel, err := filepath.Rel(brainRoot, c.got)
			if err != nil {
				t.Fatalf("path %q not under brain root %q: %v", c.got, brainRoot, err)
			}
			if strings.HasPrefix(rel, "..") {
				t.Fatalf("path %q escapes the brain root %q", c.got, brainRoot)
			}
			if !strings.HasPrefix(rel, c.sub+string(filepath.Separator)) {
				t.Fatalf("path %q must live under %s/, got rel %q", c.got, c.sub, rel)
			}
			if strings.HasPrefix(rel, "brain"+string(filepath.Separator)) {
				t.Fatalf("path %q writes into the user-owned brain/", c.got)
			}
		})
	}
}

func TestExtractTaskLine(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"basic", "# Session: x\n\n## Task\n\nFix the flaky test\n", "Fix the flaky test"},
		{"skips blank lines", "## Task\n\n\n   \nDo the thing\n", "Do the thing"},
		{"stops at next heading", "## Task\n\n## Notes\nirrelevant\n", ""},
		{"no task section", "# Session\n\nsome prose\n", ""},
		{"empty", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := extractTaskLine(c.in); got != c.want {
				t.Fatalf("extractTaskLine(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
