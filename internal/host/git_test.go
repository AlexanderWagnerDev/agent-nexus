package host

import "testing"

func TestNormalizeRepoURL(t *testing.T) {
	cases := map[string]string{
		"git@github.com:owner/repo.git":         "https://github.com/owner/repo",
		"ssh://git@github.com/owner/repo.git":   "https://github.com/owner/repo",
		"https://github.com/owner/repo.git":     "https://github.com/owner/repo",
		"https://gitlab.com/group/sub/repo/":    "https://gitlab.com/group/sub/repo",
		"  https://example.com/team/repo.git  ": "https://example.com/team/repo",
		"":                                      "",
	}
	for input, want := range cases {
		if got := normalizeRepoURL(input); got != want {
			t.Errorf("normalizeRepoURL(%q) = %q, want %q", input, got, want)
		}
	}
}
