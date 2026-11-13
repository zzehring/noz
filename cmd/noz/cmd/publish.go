package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/zzehring/noz/internal/forge"
	"github.com/zzehring/noz/internal/keyring"
)

// `noz publish` pushes the session's branch and opens a draft pull request,
// holding the credential so the session never does.
//
// This is the one outward action an unattended run is allowed, and it is narrow
// on purpose: there is no flag that turns it into a merge, a comment, or a
// release. That is the property that makes it safe to allowlist. A general
// `noz <verb>` would hand the verb back to the agent, which is exactly why `gh`
// cannot be allowlisted without parsing its subcommands and losing.
func newPublishCmd() *cobra.Command {
	var base, title, body string
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "publish",
		Short: "Push this session's branch and open a draft pull request",
		Long: `Pushes the current session's branch and opens a draft pull request.

noz holds the forge credential; the session never receives one. That is the
point — an agent cannot misuse a token it was never given, whatever command it
reaches for.

The credential comes from the OS credential store, never from a file or an
environment variable. Store one first:

  security add-generic-password -s "noz:forgejo:<host>" -a token -T "" -w

Deliberately narrow: this opens a draft and nothing else. Marking it ready,
merging, and commenting are human actions.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPublish(cmd, base, title, body, dryRun)
		},
	}
	cmd.Flags().StringVar(&base, "base", "", "branch to merge into (default: the repo's default branch)")
	cmd.Flags().StringVar(&title, "title", "", "pull request title (default: the branch's last commit subject)")
	cmd.Flags().StringVar(&body, "body", "", "pull request body")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would be pushed and opened, without doing it")
	return cmd
}

func runPublish(cmd *cobra.Command, base, title, body string, dryRun bool) error {
	w := cmd.OutOrStderr()

	branch := gitBranch()
	if branch == "" || branch == "HEAD" {
		return fmt.Errorf("not on a branch — publish needs a named branch to push")
	}

	remote, err := exec.Command("git", "config", "--get", "remote.origin.url").Output()
	if err != nil {
		return fmt.Errorf("no origin remote to publish to")
	}
	repo, err := forge.ParseRemote(strings.TrimSpace(string(remote)))
	if err != nil {
		return err
	}

	if base == "" {
		base = defaultBaseBranch()
	}
	if base == branch {
		return fmt.Errorf("branch %q is the base branch — nothing to publish", branch)
	}
	if title == "" {
		title = lastCommitSubject()
	}
	if title == "" {
		return fmt.Errorf("no commits on %q to describe — commit first, or pass --title", branch)
	}

	// Redact before anything leaves. A pull request body is attacker-controlled
	// by construction in an unattended run, and it lands somewhere world
	// readable, so it is egress like any other.
	title = redactString(title)
	body = redactString(body)

	// Reported before the forge is selected, so --dry-run shows the plan even
	// for a host noz cannot publish to yet.
	fmt.Fprintf(w, "noz: %s -> %s on %s\n", branch, base, repo)
	fmt.Fprintf(w, "noz: title: %s\n", title)

	if dryRun {
		fmt.Fprintln(w, "noz: dry-run — nothing pushed, nothing opened.")
		fmt.Fprintf(w, "noz: would push %s to origin, then open a draft pull request.\n", branch)
		if _, err := forgeFor(repo); err != nil {
			fmt.Fprintf(w, "noz: note — %v\n", err)
		}
		return nil
	}

	f, err := forgeFor(repo)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "noz: forge: %s\n", f.Name())

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	if err := pushWithBrokeredCredential(ctx, f, repo, branch); err != nil {
		return err
	}
	fmt.Fprintf(w, "noz: pushed %s\n", branch)

	url, err := f.OpenDraftPR(ctx, repo, forge.PullRequest{
		Head: branch, Base: base, Title: title, Body: body,
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "noz: draft pull request opened\n")
	fmt.Fprintln(cmd.OutOrStdout(), url)
	return nil
}

// pushWithBrokeredCredential pushes branch to origin, handing git the
// credential over an inherited pipe.
//
// The pipe is the reason this function exists. Git needs a credential; the
// session must not have one. Env is readable with `ps eww` by the same user,
// argv by everyone, and a file is plaintext at rest. A pipe is none of those,
// and it is consumed once.
func pushWithBrokeredCredential(ctx context.Context, f forge.Forge, repo forge.Repo, branch string) error {
	user, secret, err := f.GitCredential(ctx, repo)
	if err != nil {
		return err
	}
	defer keyring.Secret(secret).Zero()

	r, wr, err := os.Pipe()
	if err != nil {
		return fmt.Errorf("creating credential pipe: %w", err)
	}
	defer func() { _ = r.Close() }()

	// Write before starting git: the credential is small enough to fit the pipe
	// buffer, so this cannot deadlock, and git only reads it when it asks.
	if _, err := fmt.Fprintf(wr, "username=%s\npassword=%s\n", user, secret); err != nil {
		_ = wr.Close()
		return fmt.Errorf("writing credential: %w", err)
	}
	if err := wr.Close(); err != nil {
		return fmt.Errorf("closing credential pipe: %w", err)
	}

	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locating the noz binary: %w", err)
	}

	// ExtraFiles[0] becomes descriptor 3 in the child. git passes descriptors
	// it does not own through to the helper it execs.
	push := exec.CommandContext(ctx, "git",
		"-c", "credential.helper="+shellQuote(self)+" credential-helper --fd 3",
		"push", "--set-upstream", "origin", branch+":"+branch)
	push.ExtraFiles = []*os.File{r}
	push.Stdout = os.Stderr
	push.Stderr = os.Stderr

	if err := push.Run(); err != nil {
		return fmt.Errorf("pushing %s: %w", branch, err)
	}
	return nil
}

// forgeFor selects an implementation by host.
func forgeFor(repo forge.Repo) (forge.Forge, error) {
	switch repo.Host {
	case "github.com":
		return nil, fmt.Errorf("github is not wired up yet — %s", repo.Host)
	default:
		// Anything else is assumed Gitea-compatible, which covers Forgejo,
		// Gitea, and Codeberg. Being wrong here fails at the API call with the
		// forge's own message rather than guessing further.
		return forge.NewForgejo(repo.Host), nil
	}
}

// defaultBaseBranch asks git what origin's default branch is, falling back to
// main. Never guesses from the local branch list, which is often stale.
func defaultBaseBranch() string {
	out, err := exec.Command("git", "symbolic-ref", "--short", "refs/remotes/origin/HEAD").Output()
	if err == nil {
		if _, b, ok := strings.Cut(strings.TrimSpace(string(out)), "origin/"); ok && b != "" {
			return b
		}
	}
	return "main"
}

func lastCommitSubject() string {
	out, err := exec.Command("git", "log", "-1", "--format=%s").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
