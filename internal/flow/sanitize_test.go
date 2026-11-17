package flow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSanitize(t *testing.T) {
	assert.Equal(t, "Fix the login form", Sanitize("Fix the login form"))
	assert.Equal(t, "Über Ümlaute 🚀", Sanitize("Über Ümlaute 🚀"))
	assert.Equal(t, " [2J evil", Sanitize("\x1b[2J evil"))
	assert.Equal(t, "one two three", Sanitize("one\ttwo\nthree"))
	assert.Equal(t, "abc fdp.exe", Sanitize("abc\u202Efdp.exe"))
	assert.Equal(t, "line one\nline  two", SanitizeMultiline("line one\nline\x07 two"))
}

func TestIssuesCantForgeMenuEntries(t *testing.T) {
	f := setup(t, &GitHubMock{Issues: []*GitHubMockIssue{
		{Number: 3, Title: "Harmless\n99\tFake entry \x1b[31mred\x1b[0m"},
	}}, false)

	require.NoError(t, f.app.Issues())

	assert.Equal(t, "3\tHarmless 99 Fake entry  [31mred [0m\n", f.out.String())
}
