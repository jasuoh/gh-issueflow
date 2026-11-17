package naming

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
)

func TestSlugifyIssueTitle(t *testing.T) {
	cases := []struct {
		title    string
		expected string
	}{
		{"Fix the login form", "fix-the-login-form"},
		{"  Leading and trailing  ", "leading-and-trailing"},
		{"Crash on `git push --force`!!", "crash-on-git-push-force"},
		{"feat: Add PR #12 support (v2.0)", "feat-add-pr-12-support-v2-0"},
		{"Umlaute in Überschriften", "umlaute-in-überschriften"},
		{"!!!", ""},
		{"", ""},
	}

	for _, c := range cases {
		t.Run(c.title, func(t *testing.T) {
			assert.Equal(t, c.expected, SlugifyIssueTitle(c.title))
		})
	}
}

func TestSlugifyIssueTitleTruncatesAtWordBoundary(t *testing.T) {
	title := "Make the commit graph render correctly when there are many merges in a row"

	slug := SlugifyIssueTitle(title)

	assert.Equal(t, "make-the-commit-graph-render-correctly-when-there", slug)
	assert.LessOrEqual(t, len(slug), maxSlugLength)
}

func TestSlugifyIssueTitleTruncatesLongWordsOnRuneBoundary(t *testing.T) {
	title := strings.Repeat("ü", 40)

	slug := SlugifyIssueTitle(title)

	assert.True(t, utf8.ValidString(slug))
	assert.Equal(t, strings.Repeat("ü", 25), slug)
}

func TestIssueBranchName(t *testing.T) {
	assert.Equal(t, "42-fix-the-login-form", IssueBranchName(42, "Fix the login form"))
	assert.Equal(t, "7", IssueBranchName(7, "???"))
}

func TestIssueNumberFromBranchName(t *testing.T) {
	cases := []struct {
		branchName     string
		expectedNumber int
		expectedOk     bool
	}{
		{"42-fix-the-login-form", 42, true},
		{"42", 42, true},
		{"1234_underscore", 1234, true},
		{"0-not-an-issue", 0, false},
		{"feature/42-prefixed", 0, false},
		{"main", 0, false},
		{"", 0, false},
	}

	for _, c := range cases {
		t.Run(c.branchName, func(t *testing.T) {
			number, ok := IssueNumberFromBranchName(c.branchName)
			assert.Equal(t, c.expectedOk, ok)
			assert.Equal(t, c.expectedNumber, number)
		})
	}
}
