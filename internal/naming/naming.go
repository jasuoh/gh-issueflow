package naming

import (
	"strconv"
	"strings"
	"unicode"
)

// Keeps branch names readable in the branches panel; the issue number at the
// front is what identifies the issue anyway.
const maxSlugLength = 50

// SlugifyIssueTitle turns an issue title into something usable in a branch
// name: lowercase letters and digits, with every run of other characters
// collapsed into a single dash.
func SlugifyIssueTitle(title string) string {
	var builder strings.Builder
	pendingDash := false
	for _, r := range strings.ToLower(title) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if pendingDash && builder.Len() > 0 {
				builder.WriteByte('-')
			}
			pendingDash = false
			builder.WriteRune(r)
		} else {
			pendingDash = true
		}
	}

	slug := builder.String()
	if len(slug) <= maxSlugLength {
		return slug
	}

	// Cut at a word boundary where possible, and never in the middle of a
	// multi-byte character.
	cut := strings.LastIndexByte(slug[:maxSlugLength+1], '-')
	if cut <= 0 {
		cut = maxSlugLength
		for cut > 0 && !isRuneStart(slug[cut]) {
			cut--
		}
	}
	return strings.TrimRight(slug[:cut], "-")
}

func isRuneStart(b byte) bool {
	return b&0xC0 != 0x80
}

// IssueBranchName returns the name of the branch to create for an issue,
// e.g. "42-fix-the-login-form".
func IssueBranchName(number int, title string) string {
	name := strconv.Itoa(number)
	if slug := SlugifyIssueTitle(title); slug != "" {
		name += "-" + slug
	}
	return name
}

// IssueNumberFromBranchName returns the number of the issue that a branch
// belongs to, taken from the digits the branch name starts with.
func IssueNumberFromBranchName(branchName string) (int, bool) {
	end := strings.IndexFunc(branchName, func(r rune) bool { return r < '0' || r > '9' })
	if end == -1 {
		end = len(branchName)
	}
	number, err := strconv.Atoi(branchName[:end])
	if err != nil || number <= 0 {
		return 0, false
	}
	return number, true
}
