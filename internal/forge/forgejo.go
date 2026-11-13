package forge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/zzehring/noz/internal/keyring"
)

// Forgejo talks to a Forgejo or Gitea instance. The API is Gitea-compatible,
// so one implementation serves both.
//
// Honest limit on scoping, worth knowing before trusting the credential as a
// boundary: Forgejo's token scopes are coarser than GitHub's. `write:repository`
// covers every POST/PUT/PATCH/DELETE on repository routes, and that includes
// the merge route — so unlike a GitHub App token, a Forgejo token that can open
// a pull request can also merge one. On Forgejo the merge boundary has to come
// from branch protection on the repository, not from the token. Forgejo tokens
// also do not expire yet, so the standing secret is the token itself rather
// than a key that mints short-lived ones.
type Forgejo struct {
	Host   string
	client *http.Client
}

// NewForgejo returns a Forgejo client for host.
func NewForgejo(host string) *Forgejo {
	return &Forgejo{
		Host: host,
		// Explicit timeout: http.DefaultClient has none, and an unattended run
		// that blocks on a hung forge never finishes and never reports why.
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (f *Forgejo) Name() string { return "forgejo" }

// keyringService namespaces the credential per host, so one machine can hold
// tokens for several instances without them colliding.
func (f *Forgejo) keyringService() string { return "noz:forgejo:" + f.Host }

// token reads the instance token from the OS credential store. Callers zero it.
func (f *Forgejo) token() ([]byte, error) {
	s, err := keyring.Get(f.keyringService(), "token")
	if err != nil {
		if err == keyring.ErrNotFound {
			self, _ := os.Executable()
			return nil, fmt.Errorf("no Forgejo token for %s — store one with:\n  %s",
				f.Host, keyring.Instructions(f.keyringService(), "token", self))
		}
		return nil, err
	}
	return s, nil
}

// GitCredential returns the username and secret for an HTTPS push. Forgejo
// accepts the token as the password with any non-empty username.
func (f *Forgejo) GitCredential(ctx context.Context, repo Repo) (string, []byte, error) {
	tok, err := f.token()
	if err != nil {
		return "", nil, err
	}
	return "noz", tok, nil
}

// OpenDraftPR opens a draft pull request via the Gitea-compatible API.
func (f *Forgejo) OpenDraftPR(ctx context.Context, repo Repo, pr PullRequest) (string, error) {
	tok, err := f.token()
	if err != nil {
		return "", err
	}
	defer keyring.Secret(tok).Zero()

	// Forgejo has no draft flag; the convention it understands is a WIP title
	// prefix, which its UI treats as blocking merge.
	body, err := json.Marshal(map[string]any{
		"head":  pr.Head,
		"base":  pr.Base,
		"title": "WIP: " + pr.Title,
		"body":  pr.Body,
	})
	if err != nil {
		return "", fmt.Errorf("encoding pull request: %w", err)
	}

	url := fmt.Sprintf("https://%s/api/v1/repos/%s/%s/pulls", f.Host, repo.Owner, repo.Name)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	// The token rides in a header, never in the URL: a URL reaches logs,
	// proxies, and error strings.
	req.Header.Set("Authorization", "token "+string(tok))

	resp, err := f.client.Do(req)
	if err != nil {
		// Deliberately not wrapping err's full text with the request, since a
		// transport error can echo the URL and headers.
		return "", fmt.Errorf("posting pull request to %s: request failed", f.Host)
	}
	defer func() { _ = resp.Body.Close() }()

	payload, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Only the status and the forge's own message, never the raw body:
		// an auth error can echo the credential back.
		return "", fmt.Errorf("%s refused the pull request: %s (%s)",
			f.Host, resp.Status, forgejoMessage(payload))
	}

	var created struct {
		HTMLURL string `json:"html_url"`
		Number  int    `json:"number"`
	}
	if err := json.Unmarshal(payload, &created); err != nil {
		return "", fmt.Errorf("reading the forge's response: %w", err)
	}
	if created.HTMLURL != "" {
		return created.HTMLURL, nil
	}
	return fmt.Sprintf("https://%s/%s/%s/pulls/%d", f.Host, repo.Owner, repo.Name, created.Number), nil
}

// forgejoMessage extracts the forge's error message, so a failure says what
// went wrong without echoing a whole response body into an error string.
func forgejoMessage(payload []byte) string {
	var e struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(payload, &e); err == nil && e.Message != "" {
		if len(e.Message) > 200 {
			return e.Message[:200] + "…"
		}
		return e.Message
	}
	return "no message"
}
