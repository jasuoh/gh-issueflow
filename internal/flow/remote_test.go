package flow

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseRemoteURL(t *testing.T) {
	cases := []struct {
		url   string
		owner string
		name  string
		ok    bool
	}{
		{"https://github.com/jasuoh/lazygit.git", "jasuoh", "lazygit", true},
		{"https://github.com/jasuoh/lazygit", "jasuoh", "lazygit", true},
		{"git@github.com:jasuoh/lazygit.git", "jasuoh", "lazygit", true},
		{"ssh://git@github.com/jasuoh/lazygit.git", "jasuoh", "lazygit", true},
		{"https://ghe.example.com/org/repo.git", "org", "repo", true},
		{"../origin", "", "", false},
		{"https://github.com/onlyowner", "", "", false},
	}

	for _, c := range cases {
		t.Run(c.url, func(t *testing.T) {
			owner, name, ok := ParseRemoteURL(c.url)
			assert.Equal(t, c.ok, ok)
			assert.Equal(t, c.owner, owner)
			assert.Equal(t, c.name, name)
		})
	}
}

func TestFindRemote(t *testing.T) {
	remotes := map[string]string{
		"fork":     "git@github.com:me/repo.git",
		"origin":   "https://github.com/org/repo.git",
		"upstream": "https://github.com/org/repo.git",
	}

	assert.Equal(t, "upstream", FindRemote(remotes, "org", "repo"))
	assert.Equal(t, "fork", FindRemote(remotes, "ME", "Repo"))
	assert.Equal(t, "", FindRemote(remotes, "other", "repo"))
}

func TestParseIssueNumber(t *testing.T) {
	number, err := ParseIssueNumber("#12")
	assert.NoError(t, err)
	assert.Equal(t, 12, number)

	_, err = ParseIssueNumber("abc")
	assert.Error(t, err)
}
