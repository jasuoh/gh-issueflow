package flow

import (
	"strings"
	"unicode"
)

// Sanitize makes text that anyone can put on GitHub, like issue titles, safe
// to print. Control characters become spaces: escape sequences could
// otherwise rewrite the terminal, and tabs or newlines could forge extra
// fields or menu entries in the output that lazygit parses. So do the
// Unicode bidirectional controls, which can make text read differently from
// what it is.
func Sanitize(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || isBidiControl(r) {
			return ' '
		}
		return r
	}, text)
}

// SanitizeMultiline is like Sanitize but keeps line breaks, for messages
// meant to be shown as several lines.
func SanitizeMultiline(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = Sanitize(line)
	}
	return strings.Join(lines, "\n")
}

func isBidiControl(r rune) bool {
	return (r >= '\u202A' && r <= '\u202E') || (r >= '\u2066' && r <= '\u2069') || r == '\u200E' || r == '\u200F' || r == '\u061C'
}
