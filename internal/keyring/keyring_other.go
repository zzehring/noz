//go:build !darwin && !linux

package keyring

import "fmt"

// Get fails honestly on platforms with no credential store noz knows about,
// rather than degrading to a file — a plaintext fallback would quietly defeat
// the only reason this package exists.
func Get(service, account string) (Secret, error) {
	return nil, fmt.Errorf("keyring: unsupported on this platform")
}

// Instructions reports that there is nothing to instruct.
func Instructions(service, account, trustedBin string) string {
	return "no supported credential store on this platform"
}
