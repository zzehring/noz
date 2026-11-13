//go:build linux

package keyring

import (
	"bytes"
	"fmt"
	"os/exec"
)

// Get reads a secret from the freedesktop secret-service via secret-tool.
//
// Note the honest limit: secret-service needs a session keyring daemon, so a
// headless host — a CI runner, a cluster node — usually has none. That is
// exactly where an unattended agent runs, so "no plaintext at rest" has no
// answer there yet and Get returns ErrNotFound rather than inventing one.
func Get(service, account string) (Secret, error) {
	cmd := exec.Command("secret-tool", "lookup", "service", service, "account", account)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, ErrNotFound
	}
	b := bytes.TrimRight(out.Bytes(), "\n")
	if len(b) == 0 {
		return nil, ErrNotFound
	}
	return Secret(b), nil
}

// Instructions returns the command a human should run to store the secret.
// trustedBin is unused on Linux: secret-service has no per-application ACL, so
// the item is readable by any process in the user's session — a real limit of
// this backend, not something to paper over.
func Instructions(service, account, trustedBin string) string {
	_ = trustedBin
	return fmt.Sprintf("secret-tool store --label=noz service %q account %q", service, account)
}
