//go:build darwin

package keyring

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// Get reads a generic password from the macOS Keychain.
//
// The secret arrives on stdout, never in argv — an argument would be visible to
// `ps` for every process the user owns, which is the same mistake the audit log
// made before it learned to redact.
//
// Keychain ACLs matter here and noz deliberately does not manage them. An item
// added with `-A` is readable by any process the user runs, which would hand the
// agent exactly the standing secret this package exists to keep away from it.
func Get(service, account string) (Secret, error) {
	cmd := exec.Command("security", "find-generic-password",
		"-s", service, "-a", account, "-w")
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb

	if err := cmd.Run(); err != nil {
		if strings.Contains(errb.String(), "could not be found") {
			return nil, ErrNotFound
		}
		// Deliberately not including stderr: it can echo back item attributes,
		// and an error string is the easiest way for a secret to reach a log.
		return nil, fmt.Errorf("keyring: reading %q: %w", service, err)
	}
	return Secret(bytes.TrimRight(out.Bytes(), "\n")), nil
}

// Instructions returns the command a human should run to store the secret.
// Printed rather than executed (PRINCIPLES #4).
//
// -T scopes the item to a single trusted binary — the noz path passed in — so
// reads do not prompt and no other process the user runs can read it. An empty
// -T "" was wrong here: it means "trust nothing", which makes every read pop a
// GUI prompt, and a prompt nobody answers hangs an unattended run. -A is never
// used: that trusts every process, handing the agent the standing secret this
// store exists to withhold.
func Instructions(service, account, trustedBin string) string {
	if trustedBin == "" {
		trustedBin = "/path/to/noz"
	}
	return fmt.Sprintf("security add-generic-password -s %q -a %q -T %q -U -w",
		service, account, trustedBin)
}
