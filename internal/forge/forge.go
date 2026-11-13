// Package forge performs outward actions against a code host, holding the
// credential so a session never does.
//
// The boundary this package draws is the whole point. An agent running
// unattended can be denied `gh pr merge` by policy, but policy inspects a
// command string to guess intent, and bash is a general interpreter — the same
// request leaves through curl, or python, or a git alias. What cannot be
// reinterpreted is a credential the agent never holds. So noz performs the
// outward act itself and the session gets no token at all.
//
// Two rules keep that true, and both are easy to break by accident:
//
//  1. A command that holds a credential must have no parameter that changes
//     which capability it exercises. `noz publish` pushes a branch and opens a
//     draft pull request; there is no flag that turns it into a merge. A general
//     dispatcher would put the verb back in the agent's hands, which is the
//     problem `gh` has.
//
//  2. Nothing outbound goes unredacted. An outward action is the one sanctioned
//     egress from a locked-down session, so its content — titles, bodies — is
//     attacker-controlled by construction. A secret can leave in a pull request
//     description just as easily as over a socket.
package forge

import (
	"context"
	"fmt"
	"strings"
)

// Repo identifies a repository on a host.
type Repo struct {
	Host  string // "github.com", "forgejo.example.com"
	Owner string
	Name  string
}

func (r Repo) String() string { return r.Host + "/" + r.Owner + "/" + r.Name }

// PullRequest is the change being offered. Draft is not optional in the type
// because an unattended run should never open something that reads as ready;
// marking it ready is a human's call.
type PullRequest struct {
	Head  string // branch to merge from
	Base  string // branch to merge into
	Title string
	Body  string
}

// Forge is the minimum an outward action needs. Implementations hold the
// credential and must never return it to a caller outside this package.
type Forge interface {
	// Name identifies the implementation, for messages and the audit record.
	Name() string

	// GitCredential returns a username and secret for pushing over HTTPS. The
	// caller must Zero the secret when done.
	GitCredential(ctx context.Context, repo Repo) (user string, secret []byte, err error)

	// OpenDraftPR opens a draft pull request and returns its URL.
	OpenDraftPR(ctx context.Context, repo Repo, pr PullRequest) (string, error)
}

// ParseRemote turns a git remote URL into a Repo. It keeps the host, which the
// identity in `noz status` deliberately drops — identity wants a short tag,
// while choosing a forge needs to know which host is being talked to.
func ParseRemote(raw string) (Repo, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return Repo{}, fmt.Errorf("empty remote URL")
	}

	var hostAndPath string
	if scheme, rest, found := strings.Cut(s, "://"); found {
		if strings.EqualFold(scheme, "file") {
			return Repo{}, fmt.Errorf("local remote %q has no forge", raw)
		}
		hostAndPath = rest
	} else if _, rest, found := strings.Cut(s, ":"); found {
		// scp-like: [user@]host:owner/repo.git
		host, _, _ := strings.Cut(s, ":")
		if at := strings.LastIndex(host, "@"); at >= 0 {
			host = host[at+1:]
		}
		return repoFromPath(host, rest)
	} else {
		return Repo{}, fmt.Errorf("cannot read a forge from %q", raw)
	}

	host, path, found := strings.Cut(hostAndPath, "/")
	if !found {
		return Repo{}, fmt.Errorf("no repository path in %q", raw)
	}
	if at := strings.LastIndex(host, "@"); at >= 0 {
		host = host[at+1:]
	}
	if h, _, ok := strings.Cut(host, ":"); ok { // strip :port
		host = h
	}
	return repoFromPath(host, path)
}

// repoFromPath takes the last two path segments as owner and repo. Deeper
// nesting (a GitLab subgroup, a forge served under a subpath) keeps its tail,
// because that is what the API route needs.
func repoFromPath(host, path string) (Repo, error) {
	path = strings.TrimSuffix(strings.Trim(path, "/"), ".git")
	path = strings.Trim(path, "/")
	segs := strings.Split(path, "/")
	if host == "" || len(segs) < 2 {
		return Repo{}, fmt.Errorf("cannot read owner/repo from %q", path)
	}
	for _, s := range segs {
		if s == "" {
			return Repo{}, fmt.Errorf("malformed repository path %q", path)
		}
	}
	return Repo{
		Host:  host,
		Owner: strings.Join(segs[:len(segs)-1], "/"),
		Name:  segs[len(segs)-1],
	}, nil
}
