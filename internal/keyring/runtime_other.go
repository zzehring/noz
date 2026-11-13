//go:build !darwin

package keyring

func runtimeIsDarwin() bool { return false }
