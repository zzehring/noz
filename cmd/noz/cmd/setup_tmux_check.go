package cmd

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// `noz setup tmux` prints advice and then goes blind: it can't tell you whether
// the snippet was ever sourced. --check closes that loop by inspecting the live
// tmux server. It reads only — no state, no writes — so it stays print-only in
// spirit (PRINCIPLES #4) while being honest about what's actually in effect (#6).
//
// Gathering (probeTmux) is deliberately separated from judging (checkTmux) so
// the whole diagnosis is a pure function, testable without a running server.

// tmuxProbe is the live tmux server state that --check inspects.
type tmuxProbe struct {
	serverUp bool

	startTime time.Time            // when the server started
	configs   []string             // #{config_files}, in source order
	configMod map[string]time.Time // sourced config -> mtime (absent if unreadable)

	statusRight string            // global status-right, as it stands post-plugin
	binds       map[string]string // prefix-table key -> bound command
	envPath     string            // tmux's PATH — what run-shell bindings inherit
}

// checkStatus is a finding's severity. checkFail means the integration is
// definitely not working; checkWarn means it may be degraded; checkSkip means
// noz couldn't tell — reported rather than silently implying a clean bill of
// health.
type checkStatus string

const (
	checkOK   checkStatus = "ok"
	checkWarn checkStatus = "warn"
	checkFail checkStatus = "fail"
	checkSkip checkStatus = "skip"
)

type tmuxFinding struct {
	status checkStatus
	label  string
	detail string
	hint   string
}

// bindLine matches a `tmux list-keys -T <table>` row: `bind-key`, optional
// flags, `-T <table>`, the key, then the command. tmux pads with runs of spaces
// and may quote the key (e.g. '"'), so the key is taken as one non-space token.
var bindLine = regexp.MustCompile(`^bind-key\s+(?:-\S+\s+)*?-T\s+\S+\s+(\S+)\s+(.*)$`)

// parseTmuxBinds maps key -> bound command for one key table.
func parseTmuxBinds(out string) map[string]string {
	binds := make(map[string]string)
	for _, line := range strings.Split(out, "\n") {
		m := bindLine.FindStringSubmatch(strings.TrimRight(line, "\r"))
		if m == nil {
			continue
		}
		binds[m[1]] = strings.TrimSpace(m[2])
	}
	return binds
}

// probeTmux collects live server state, best-effort: any piece that can't be
// read is left zero and reported as a skip rather than guessed at.
func probeTmux() tmuxProbe {
	p := tmuxProbe{configMod: map[string]time.Time{}, binds: map[string]string{}}

	tmuxBin, err := exec.LookPath("tmux")
	if err != nil {
		return p
	}
	// One round-trip for the scalars. If this fails there's no server to inspect.
	out, err := exec.Command(tmuxBin, "display-message", "-p", "#{start_time}\t#{config_files}").Output()
	if err != nil {
		return p
	}
	p.serverUp = true

	fields := strings.SplitN(strings.TrimSpace(string(out)), "\t", 2)
	if secs, err := strconv.ParseInt(strings.TrimSpace(fields[0]), 10, 64); err == nil && secs > 0 {
		p.startTime = time.Unix(secs, 0)
	}
	if len(fields) > 1 {
		for _, c := range strings.Split(fields[1], ",") {
			if c = strings.TrimSpace(c); c == "" {
				continue
			}
			p.configs = append(p.configs, c)
			if st, err := os.Stat(c); err == nil {
				p.configMod[c] = st.ModTime()
			}
		}
	}

	if out, err := exec.Command(tmuxBin, "show-options", "-gv", "status-right").Output(); err == nil {
		p.statusRight = strings.TrimSpace(string(out))
	}
	if out, err := exec.Command(tmuxBin, "list-keys", "-T", "prefix").Output(); err == nil {
		p.binds = parseTmuxBinds(string(out))
	}
	if out, err := exec.Command(tmuxBin, "show-environment", "-g", "PATH").Output(); err == nil {
		p.envPath = strings.TrimPrefix(strings.TrimSpace(string(out)), "PATH=")
	}
	return p
}

// checkTmux judges a probe against the keys the snippet was generated for,
// ordered most-likely-to-bite first.
func checkTmux(p tmuxProbe, keys tmuxKeys) []tmuxFinding {
	if !p.serverUp {
		// The honest limit: --check inspects a running server, so it cannot
		// verify a config that is correct but never loaded.
		return []tmuxFinding{{
			status: checkSkip,
			label:  "tmux server",
			detail: "no server running — nothing to inspect",
			hint:   "start tmux (or `noz open <slug>`), then re-run --check",
		}}
	}

	// Bindings are judged first because they're direct evidence: a live binding
	// proves the config was sourced, whatever the file's mtime says.
	binds := checkTmuxBindings(p, keys)

	// "Any", not "all" — one live noz binding is proof the block was read, even
	// if another key is missing or was rebound. Requiring all of them would
	// misreport a partially-applied config as never sourced. status-right counts
	// too, so a user who drops every picker key still has evidence.
	nozSourced := strings.Contains(p.statusRight, "NOZ_REPO")
	for _, f := range binds {
		if f.status == checkOK {
			nozSourced = true
		}
	}

	out := []tmuxFinding{checkTmuxConfigFresh(p, nozSourced)}
	out = append(out, binds...)
	out = append(out, checkTmuxStatusRight(p), checkTmuxNozOnPath(p))
	return out
}

// checkTmuxConfigFresh catches the most common failure by far: the snippet was
// pasted into a config the running server has never re-read.
//
// mtime-after-start is only a heuristic — a manual `source-file` leaves no trace
// in the mtime — so it defers to nozLive, the direct evidence. Its real job is
// to *explain* a binding failure, not to override a working integration; saying
// "never sourced" while the keys demonstrably work would be exactly the kind of
// confident-and-wrong claim PRINCIPLES #6 rules out.
func checkTmuxConfigFresh(p tmuxProbe, nozLive bool) tmuxFinding {
	f := tmuxFinding{label: "config sourced"}
	if p.startTime.IsZero() || len(p.configMod) == 0 {
		f.status = checkSkip
		f.detail = "couldn't read the server start time or any config mtime"
		return f
	}

	var stale []string
	var newest time.Time
	for _, c := range p.configs {
		mod, ok := p.configMod[c]
		if !ok || !mod.After(p.startTime) {
			continue
		}
		stale = append(stale, c)
		if mod.After(newest) {
			newest = mod
		}
	}
	// Each combination of (config current?, noz live?) is a different diagnosis.
	switch {
	case len(stale) == 0 && nozLive:
		f.status = checkOK
		f.detail = "server read the current config (started " + p.startTime.Format("2006-01-02 15:04") + ")"
	case len(stale) == 0:
		f.status = checkOK
		f.detail = "config is current — so the noz block is likely not in it, rather than unsourced"
	case nozLive:
		f.status = checkOK
		f.detail = fmt.Sprintf("edited %s after the server started, but sourced since",
			newest.Sub(p.startTime).Round(time.Second))
	default:
		f.status = checkFail
		f.detail = fmt.Sprintf("%s edited %s after the server started — never sourced",
			strings.Join(stale, ", "), newest.Sub(p.startTime).Round(time.Second))
		f.hint = "tmux source-file " + stale[0]
	}
	return f
}

// checkTmuxBindings verifies each configured picker key is bound to the noz
// command it should be. A key bound to something else is a collision, not a
// missing paste — reporting them apart is the point, since the fixes differ.
func checkTmuxBindings(p tmuxProbe, keys tmuxKeys) []tmuxFinding {
	var out []tmuxFinding
	for _, b := range pickerBindings(keys) {
		f := tmuxFinding{label: "prefix+" + b.key}
		want := "noz pick " + b.view
		bound, ok := p.binds[b.key]
		switch {
		case !ok:
			f.status = checkFail
			f.detail = "not bound"
			f.hint = "add the snippet to your tmux config, then source it"
		case strings.Contains(bound, want):
			f.status = checkOK
			f.detail = want
		default:
			f.status = checkWarn
			f.detail = "bound to something else: " + clipStr(bound, 44)
			f.hint = fmt.Sprintf("keep yours and move noz: noz setup tmux --%s-key <key>", b.view)
		}
		out = append(out, f)
	}
	return out
}

// checkTmuxStatusRight catches a later non-append `set -g status-right` having
// eaten the appended noz segment. `-ga` protects the paste, but nothing
// protects it from being overwritten afterwards — and TPM's `run` line sits at
// the bottom of the config by convention, so plugins load last.
func checkTmuxStatusRight(p tmuxProbe) tmuxFinding {
	f := tmuxFinding{label: "status-right"}
	if strings.Contains(p.statusRight, "NOZ_REPO") {
		f.status = checkOK
		f.detail = "shows the session's repo/parent"
		return f
	}
	f.status = checkWarn
	f.detail = "noz segment missing"
	f.hint = "a later `set -g status-right` (often a plugin) replaced it — move the noz line after `run '~/.tmux/plugins/tpm/tpm'`"
	return f
}

// checkTmuxNozOnPath matters because the picker bindings shell out to `noz` via
// run-shell, and run-shell failures are silent: the key would simply do
// nothing, which looks identical to "not bound". tmux inherits the environment
// of whoever started the server, which often lacks the PATH entries an
// interactive shell adds (e.g. ~/go/bin).
func checkTmuxNozOnPath(p tmuxProbe) tmuxFinding {
	f := tmuxFinding{label: "noz on tmux PATH"}
	if p.envPath == "" {
		f.status = checkSkip
		f.detail = "couldn't read tmux's PATH"
		return f
	}
	for _, dir := range filepath.SplitList(p.envPath) {
		if dir == "" {
			continue
		}
		if st, err := os.Stat(filepath.Join(dir, "noz")); err == nil && !st.IsDir() && st.Mode()&0111 != 0 {
			f.status = checkOK
			f.detail = filepath.Join(dir, "noz")
			return f
		}
	}
	f.status = checkWarn
	f.detail = "not found in tmux's PATH"
	f.hint = "run-shell failures are silent, so the picker keys would do nothing — `tmux setenv -g PATH \"$PATH\"`, or restart the server from a shell that has noz"
	return f
}

// reportTmuxCheck renders findings and returns an error when any check failed,
// so `noz setup tmux --check` doubles as a scriptable gate (non-zero exit).
func reportTmuxCheck(w io.Writer, findings []tmuxFinding) error {
	var fails, warns, skips int
	for _, f := range findings {
		fmt.Fprintf(w, "  %-4s  %-18s %s\n", f.status, f.label, f.detail)
		if f.hint != "" {
			fmt.Fprintf(w, "        %-18s ↳ %s\n", "", f.hint)
		}
		switch f.status {
		case checkFail:
			fails++
		case checkWarn:
			warns++
		case checkSkip:
			skips++
		}
	}
	fmt.Fprintln(w)

	if fails > 0 {
		return fmt.Errorf("%s failed — the tmux integration is not active", plural(fails, "check"))
	}
	// A skip is an absence of evidence, so it can never roll up into "looks
	// good" — that would be the false all-clear this command exists to prevent.
	switch {
	case warns > 0 && skips > 0:
		fmt.Fprintf(w, "noz: integration active, with %s; %s could not be verified.\n",
			plural(warns, "warning"), plural(skips, "check"))
	case warns > 0:
		fmt.Fprintf(w, "noz: integration active, with %s.\n", plural(warns, "warning"))
	case skips > 0:
		fmt.Fprintf(w, "noz: %s could not be verified — nothing here confirms the integration is live.\n",
			plural(skips, "check"))
	default:
		fmt.Fprintln(w, "noz: tmux integration looks good.")
	}
	return nil
}

// plural renders "1 check" / "2 checks".
func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// clipStr shortens s to max runes, marking any elision.
func clipStr(s string, max int) string {
	r := []rune(strings.Join(strings.Fields(s), " "))
	if len(r) <= max {
		return string(r)
	}
	return string(r[:max]) + "…"
}
