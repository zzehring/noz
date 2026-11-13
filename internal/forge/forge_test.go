package forge

import "testing"

// ParseRemote keeps the host, which repo identity deliberately drops: a short
// tag wants org/repo, while choosing a forge needs to know which host.
func TestParseRemote(t *testing.T) {
	cases := map[string]Repo{
		"https://github.com/zzehring/noz.git":           {Host: "github.com", Owner: "zzehring", Name: "noz"},
		"git@github.com:zzehring/noz.git":               {Host: "github.com", Owner: "zzehring", Name: "noz"},
		"ssh://git@forgejo.example.com:2222/zz/noz.git": {Host: "forgejo.example.com", Owner: "zz", Name: "noz"},
		"https://forgejo.example.com:3000/zz/noz.git":   {Host: "forgejo.example.com", Owner: "zz", Name: "noz"},
		"https://user:tok@github.com/zz/noz.git":        {Host: "github.com", Owner: "zz", Name: "noz"},
		"https://codeberg.org/zz/noz":                   {Host: "codeberg.org", Owner: "zz", Name: "noz"},
		// Subgroup nesting stays in Owner, because that is what the API route needs.
		"git@gitlab.com:acme/group/sub/repo.git": {Host: "gitlab.com", Owner: "acme/group/sub", Name: "repo"},
	}
	for in, want := range cases {
		got, err := ParseRemote(in)
		if err != nil {
			t.Errorf("ParseRemote(%q): %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("ParseRemote(%q) = %+v, want %+v", in, got, want)
		}
	}
}

// A remote noz cannot read must be an error, not a half-built Repo that gets
// published to the wrong place.
func TestParseRemoteRejectsNonForges(t *testing.T) {
	for _, in := range []string{
		"", "   ",
		"file:///Users/zz/noz.git",
		"/Users/zz/projects/noz",
		"../sibling",
		"https://github.com/single",
		"noz",
	} {
		if got, err := ParseRemote(in); err == nil {
			t.Errorf("ParseRemote(%q) = %+v, want an error", in, got)
		}
	}
}

// An auth failure must not echo the forge's raw body, which can contain the
// credential that was just rejected.
func TestForgejoMessageIsBounded(t *testing.T) {
	long := `{"message":"` + string(make([]byte, 0)) + ""
	_ = long
	big := make([]byte, 0, 600)
	for len(big) < 500 {
		big = append(big, 'x')
	}
	got := forgejoMessage([]byte(`{"message":"` + string(big) + `"}`))
	if len(got) > 210 {
		t.Errorf("forgejoMessage returned %d bytes; want it bounded", len(got))
	}
	if got := forgejoMessage([]byte("not json at all")); got != "no message" {
		t.Errorf("forgejoMessage(non-json) = %q, want %q", got, "no message")
	}
}
