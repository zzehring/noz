package cmd

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var defaultKeys = tmuxKeys{repo: "g", all: "G", children: "C-g"}

// nozBinds is what a correctly sourced snippet looks like in `list-keys`.
func nozBinds() map[string]string {
	return map[string]string{
		"g":   `run-shell "tmux set-option -g @noz_pick \"$(noz pick repo --filter)\"" ; choose-tree -Zs`,
		"G":   `run-shell "tmux set-option -g @noz_pick \"$(noz pick all --filter)\"" ; choose-tree -Zs`,
		"C-g": `run-shell "tmux set-option -g @noz_pick \"$(noz pick children --filter)\"" ; choose-tree -Zs`,
	}
}

func findingFor(findings []tmuxFinding, label string) (tmuxFinding, bool) {
	for _, f := range findings {
		if f.label == label {
			return f, true
		}
	}
	return tmuxFinding{}, false
}

func TestParseTmuxBinds(t *testing.T) {
	// Real `tmux list-keys -T prefix` output: space-padded columns, optional
	// flags before -T, and keys that are themselves quoted.
	out := strings.Join([]string{
		`bind-key    -T prefix       g                         run-shell "noz pick repo --filter"`,
		`bind-key    -T prefix       C-g                       run-shell "noz pick children"`,
		`bind-key -r -T prefix       Up                        select-pane -U`,
		`bind-key    -T prefix       '"'                       split-window`,
		`bind-key    -T copy-mode-vi y                         send -X copy-selection`,
		``,
		`garbage line that is not a binding`,
	}, "\n")

	binds := parseTmuxBinds(out)

	for key, want := range map[string]string{
		"g":   `run-shell "noz pick repo --filter"`,
		"C-g": `run-shell "noz pick children"`,
		"Up":  `select-pane -U`,
		`'"'`: `split-window`,
	} {
		if got := binds[key]; got != want {
			t.Errorf("binds[%q] = %q, want %q", key, got, want)
		}
	}
	if len(binds) != 5 { // the four above plus copy-mode-vi's y
		t.Errorf("parsed %d binds, want 5: %#v", len(binds), binds)
	}
}

// TestCheckTmuxConfigFreshDefersToEvidence is the property that keeps the
// mtime heuristic honest: a live binding proves the config was sourced, so a
// stale mtime must not be reported as "never sourced".
func TestCheckTmuxConfigFresh(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	conf := "/home/u/.tmux.conf"

	probe := func(mod time.Time) tmuxProbe {
		return tmuxProbe{
			serverUp:  true,
			startTime: start,
			configs:   []string{conf},
			configMod: map[string]time.Time{conf: mod},
		}
	}

	cases := []struct {
		name    string
		mod     time.Time
		nozLive bool
		want    checkStatus
	}{
		{"stale config, noz not live: the real bug", start.Add(20 * time.Minute), false, checkFail},
		{"stale config but noz live: sourced by hand", start.Add(20 * time.Minute), true, checkOK},
		{"current config, noz live", start.Add(-time.Hour), true, checkOK},
		{"current config, noz not live: block absent", start.Add(-time.Hour), false, checkOK},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := checkTmuxConfigFresh(probe(c.mod), c.nozLive)
			if f.status != c.want {
				t.Errorf("status = %q, want %q (detail: %s)", f.status, c.want, f.detail)
			}
			if c.want == checkFail && !strings.Contains(f.hint, "source-file") {
				t.Errorf("a failure must tell the user how to fix it, got hint %q", f.hint)
			}
		})
	}

	t.Run("unknown start time is a skip, not a pass", func(t *testing.T) {
		f := checkTmuxConfigFresh(tmuxProbe{serverUp: true}, false)
		if f.status != checkSkip {
			t.Errorf("status = %q, want %q", f.status, checkSkip)
		}
	})
}

func TestCheckTmuxBindings(t *testing.T) {
	t.Run("all bound to noz", func(t *testing.T) {
		got := checkTmuxBindings(tmuxProbe{binds: nozBinds()}, defaultKeys)
		if len(got) != 3 {
			t.Fatalf("got %d findings, want 3", len(got))
		}
		for _, f := range got {
			if f.status != checkOK {
				t.Errorf("%s: status = %q, want ok (%s)", f.label, f.status, f.detail)
			}
		}
	})

	t.Run("unbound is a failure", func(t *testing.T) {
		got := checkTmuxBindings(tmuxProbe{binds: map[string]string{}}, defaultKeys)
		for _, f := range got {
			if f.status != checkFail {
				t.Errorf("%s: status = %q, want fail", f.label, f.status)
			}
		}
	})

	// A key bound to someone else's command is a collision, not a missing
	// paste: different cause, different fix, so it must not read as "fail".
	t.Run("collision is a warning naming the escape hatch", func(t *testing.T) {
		binds := nozBinds()
		binds["g"] = "next-window"
		got := checkTmuxBindings(tmuxProbe{binds: binds}, defaultKeys)

		f, ok := findingFor(got, "prefix+g")
		if !ok {
			t.Fatal("no finding for prefix+g")
		}
		if f.status != checkWarn {
			t.Errorf("status = %q, want warn", f.status)
		}
		if !strings.Contains(f.detail, "next-window") {
			t.Errorf("detail should name the conflicting command, got %q", f.detail)
		}
		if !strings.Contains(f.hint, "--repo-key") {
			t.Errorf("hint should point at --repo-key, got %q", f.hint)
		}
	})

	t.Run("dropped keys are not checked", func(t *testing.T) {
		got := checkTmuxBindings(tmuxProbe{binds: map[string]string{}}, tmuxKeys{repo: "g"})
		if len(got) != 1 || got[0].label != "prefix+g" {
			t.Errorf("only the configured key should be checked, got %#v", got)
		}
	})
}

func TestCheckTmuxStatusRight(t *testing.T) {
	live := checkTmuxStatusRight(tmuxProbe{statusRight: `#[fg=yellow]#{?NOZ_REPO,#{NOZ_REPO} ,}`})
	if live.status != checkOK {
		t.Errorf("status = %q, want ok", live.status)
	}

	// The `-ga` append protects the paste, not the aftermath: a plugin that
	// runs `set -g status-right` later silently eats the noz segment.
	clobbered := checkTmuxStatusRight(tmuxProbe{statusRight: `#(continuum_save.sh) %H:%M`})
	if clobbered.status != checkWarn {
		t.Errorf("status = %q, want warn", clobbered.status)
	}
	if !strings.Contains(clobbered.hint, "status-right") {
		t.Errorf("hint should explain the clobber, got %q", clobbered.hint)
	}
}

func TestCheckTmuxNozOnPath(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "noz")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Run("found", func(t *testing.T) {
		f := checkTmuxNozOnPath(tmuxProbe{envPath: strings.Join([]string{"/nonexistent", dir}, string(os.PathListSeparator))})
		if f.status != checkOK {
			t.Errorf("status = %q, want ok (%s)", f.status, f.detail)
		}
	})

	// A silent run-shell failure looks exactly like an unbound key, so this
	// must be reported rather than left for the user to guess at.
	t.Run("absent is a warning", func(t *testing.T) {
		f := checkTmuxNozOnPath(tmuxProbe{envPath: "/nonexistent"})
		if f.status != checkWarn {
			t.Errorf("status = %q, want warn", f.status)
		}
	})

	t.Run("non-executable does not count", func(t *testing.T) {
		plain := t.TempDir()
		if err := os.WriteFile(filepath.Join(plain, "noz"), []byte("data"), 0o644); err != nil {
			t.Fatal(err)
		}
		if f := checkTmuxNozOnPath(tmuxProbe{envPath: plain}); f.status != checkWarn {
			t.Errorf("status = %q, want warn", f.status)
		}
	})

	t.Run("unreadable PATH is a skip", func(t *testing.T) {
		if f := checkTmuxNozOnPath(tmuxProbe{}); f.status != checkSkip {
			t.Errorf("status = %q, want skip", f.status)
		}
	})
}

// TestCheckTmuxSourcedEvidence pins how checkTmux infers "the block was
// sourced". It's ANY live marker, not all of them: partial evidence still rules
// out "never sourced", and claiming otherwise sends the user to re-source a
// config that was already read.
func TestCheckTmuxSourcedEvidence(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	conf := "/home/u/.tmux.conf"

	// A config edited after the server started — the only case where the mtime
	// heuristic has anything to say.
	probe := func(binds map[string]string, statusRight string) tmuxProbe {
		return tmuxProbe{
			serverUp:    true,
			startTime:   start,
			configs:     []string{conf},
			configMod:   map[string]time.Time{conf: start.Add(20 * time.Minute)},
			binds:       binds,
			statusRight: statusRight,
		}
	}

	cases := []struct {
		name        string
		binds       map[string]string
		statusRight string
		keys        tmuxKeys
		want        checkStatus
	}{
		{"no evidence at all: genuinely never sourced", map[string]string{}, "", defaultKeys, checkFail},
		{"all bindings live", nozBinds(), "", defaultKeys, checkOK},
		{
			// One key absent, two live: the file was plainly read.
			name:  "one key missing is still proof of sourcing",
			binds: map[string]string{"G": nozBinds()["G"], "C-g": nozBinds()["C-g"]},
			keys:  defaultKeys,
			want:  checkOK,
		},
		{
			// Every picker key dropped, so bindings can't be evidence.
			name:        "status-right alone is proof when all keys are dropped",
			binds:       map[string]string{},
			statusRight: "#{?NOZ_REPO,#{NOZ_REPO} ,}",
			keys:        tmuxKeys{},
			want:        checkOK,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f, ok := findingFor(checkTmux(probe(c.binds, c.statusRight), c.keys), "config sourced")
			if !ok {
				t.Fatal("no 'config sourced' finding")
			}
			if f.status != c.want {
				t.Errorf("status = %q, want %q (detail: %s)", f.status, c.want, f.detail)
			}
		})
	}
}

// TestCheckTmuxNoServer pins the honest limit: with no server, --check reports
// that it cannot tell rather than implying a clean bill of health.
func TestCheckTmuxNoServer(t *testing.T) {
	got := checkTmux(tmuxProbe{}, defaultKeys)
	if len(got) != 1 || got[0].status != checkSkip {
		t.Fatalf("want a single skip finding, got %#v", got)
	}

	var buf bytes.Buffer
	if err := reportTmuxCheck(&buf, got); err != nil {
		t.Errorf("a skip must not be reported as failure: %v", err)
	}
	if strings.Contains(buf.String(), "looks good") {
		t.Errorf("must not claim success when nothing was inspected:\n%s", buf.String())
	}
}

func TestReportTmuxCheckExitStatus(t *testing.T) {
	cases := []struct {
		name    string
		status  checkStatus
		wantErr bool
	}{
		{"ok", checkOK, false},
		{"warn does not fail the gate", checkWarn, false},
		{"fail exits non-zero", checkFail, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := reportTmuxCheck(&buf, []tmuxFinding{{status: c.status, label: "l", detail: "d"}})
			if (err != nil) != c.wantErr {
				t.Errorf("err = %v, wantErr = %v", err, c.wantErr)
			}
		})
	}
}

// TestCheckTmuxAgainstRealServer runs the probe against a real tmux server on a
// private socket, so the formats and list-keys parsing are verified against the
// installed tmux rather than a fixture of what we think it prints.
func TestCheckTmuxAgainstRealServer(t *testing.T) {
	tmuxBin, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("tmux not installed")
	}

	sock := "noz-check-test"
	tm := func(args ...string) (string, error) {
		out, err := exec.Command(tmuxBin, append([]string{"-L", sock}, args...)...).CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}
	conf := filepath.Join(t.TempDir(), "tmux.conf")
	if err := os.WriteFile(conf, []byte("set -g status off\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := tm("-f", conf, "new-session", "-d", "-s", "probe"); err != nil {
		t.Skipf("could not start a tmux server (%v): %s", err, out)
	}
	t.Cleanup(func() { _, _ = tm("kill-server") })

	// The probe helpers target the default socket, so drive this server
	// directly and assert on the parsing/judging that follows.
	startOut, err := tm("display-message", "-p", "#{start_time}\t#{config_files}")
	if err != nil {
		t.Fatalf("display-message: %v", startOut)
	}
	if !strings.Contains(startOut, "\t") {
		t.Fatalf("expected a tab-separated start_time and config_files, got %q", startOut)
	}
	if !strings.Contains(startOut, conf) {
		t.Errorf("#{config_files} should list the sourced config %q, got %q", conf, startOut)
	}

	// Bind the real snippet, then confirm the parser sees it as noz's.
	if out, err := tm("bind-key", "g", "run-shell", `tmux set-option -g @noz_pick "$(noz pick repo --filter)"`); err != nil {
		t.Fatalf("bind-key: %v: %s", err, out)
	}
	keysOut, err := tm("list-keys", "-T", "prefix")
	if err != nil {
		t.Fatalf("list-keys: %v", keysOut)
	}
	binds := parseTmuxBinds(keysOut)
	if got := binds["g"]; !strings.Contains(got, "noz pick repo") {
		t.Fatalf("parseTmuxBinds missed the real binding; got %q", got)
	}

	f, ok := findingFor(checkTmuxBindings(tmuxProbe{binds: binds}, tmuxKeys{repo: "g"}), "prefix+g")
	if !ok || f.status != checkOK {
		t.Errorf("real binding should check ok, got %#v", f)
	}
}
