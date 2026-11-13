package cmd

import (
	"bufio"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
)

// `noz credential-helper` is a git credential helper that relays a credential
// handed to it on an inherited file descriptor.
//
// The descriptor is the whole design. The obvious alternatives all leak:
//
//   - the environment is readable with `ps eww` by any process the same user
//     owns, and the agent is that same user
//   - argv is worse, being visible to every process on the box
//   - a temp file is plaintext at rest, however briefly
//
// A pipe is none of those, and it is single-use for free: `noz publish` writes
// the credential once, and whoever reads it first consumes it. If something
// else got there first, the push fails rather than succeeding with a credential
// that leaked — a denial, not a compromise.
//
// It is not meant to be run by hand, and running it by hand gets nothing: with
// no descriptor to read, there is nothing to hand out.
func newCredentialHelperCmd() *cobra.Command {
	var fd int

	cmd := &cobra.Command{
		Use:    "credential-helper <get|store|erase>",
		Short:  "Internal: relay a credential from an inherited descriptor",
		Hidden: true,
		Args:   cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			action := "get"
			if len(args) > 0 {
				action = args[0]
			}
			// store and erase are where git would persist a credential. noz has
			// nowhere to persist one on purpose, so they are accepted and do
			// nothing rather than failing the push.
			if action != "get" {
				return nil
			}
			if fd <= 2 {
				return fmt.Errorf("credential-helper: no credential descriptor")
			}
			return relayCredential(uintptr(fd), cmd.OutOrStdout())
		},
	}
	cmd.Flags().IntVar(&fd, "fd", 0, "descriptor carrying the credential")
	return cmd
}

// relayCredential copies the credential from fd to w in git's key=value form.
// git reads the helper's stdout and keeps nothing on disk.
func relayCredential(fd uintptr, w io.Writer) error {
	f := os.NewFile(fd, "noz-credential")
	if f == nil {
		return fmt.Errorf("credential-helper: descriptor %d is not open", fd)
	}
	defer func() { _ = f.Close() }()

	sc := bufio.NewScanner(f)
	wrote := false
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
		wrote = true
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("credential-helper: reading descriptor: %w", err)
	}
	if !wrote {
		// Say nothing about why — the caller is git, and a detailed reason
		// here would end up in push output.
		return fmt.Errorf("credential-helper: no credential available")
	}
	return nil
}
