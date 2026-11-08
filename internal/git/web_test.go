package git

import "testing"

func TestWebURL(t *testing.T) {
	for in, want := range map[string]string{
		"git@github.com:byjuliocesa/orla.git":       "https://github.com/byjuliocesa/orla",
		"https://github.com/byjuliocesa/orla.git\n": "https://github.com/byjuliocesa/orla",
		"https://user:tok@gitlab.com/a/b":           "https://gitlab.com/a/b",
		"ssh://git@example.org/a/b.git":             "https://example.org/a/b",
		"/srv/git/repo":                             "",
	} {
		if got := webURL(in); got != want {
			t.Errorf("webURL(%q) = %q, want %q", in, got, want)
		}
	}
}
