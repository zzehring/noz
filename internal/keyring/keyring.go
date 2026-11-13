// Package keyring reads secrets from the OS credential store.
//
// It exists so noz never has a long-lived credential on disk in plaintext. The
// store holds one standing secret per forge — a GitHub App private key, or a
// Forgejo token — and noz reads it only for as long as it takes to mint or send
// something, then zeroes the copy.
//
// Reads only. noz does not add or remove items: creating a credential is a
// deliberate act a human should perform with the OS tooling, and it keeps noz
// from being the thing that decides what a credential's ACL looks like
// (PRINCIPLES #4 — print instructions, don't edit the user's machine).
package keyring

import "fmt"

// ErrNotFound reports that the store has no such item, which is an ordinary
// "not configured yet" rather than a failure.
var ErrNotFound = fmt.Errorf("keyring: no such item")

// Secret is a credential read from the store. It is []byte rather than string
// so callers can zero it: Go strings are immutable and cannot be wiped, so
// every copy of a secret in a string lingers until the GC happens to collect it.
type Secret []byte

// Zero overwrites the secret in place. Call it as soon as the secret has been
// used, with defer at the point of acquisition.
func (s Secret) Zero() {
	for i := range s {
		s[i] = 0
	}
}
