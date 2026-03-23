package conversation

import (
	"regexp"
	"strings"
)

var markdownRe = regexp.MustCompile(`[*_#\[\]()~` + "`" + `>|]`)

func isSentenceEnd(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	last := s[len(s)-1]
	return last == '.' || last == '!' || last == '?' || last == ':' || last == ';' || last == '\n'
}

func stripMarkdown(s string) string {
	s = markdownRe.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "  ", " ")
	return strings.TrimSpace(s)
}
