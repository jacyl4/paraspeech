package tts

import (
	"regexp"
	"strings"
)

var (
	reFencedCode = regexp.MustCompile("(?s)```.*?```")
	reInlineCode = regexp.MustCompile("`[^`]+`")
	reMdLink     = regexp.MustCompile(`\[([^\]]+)\]\([^)]+\)`)
	reURL        = regexp.MustCompile(`https?://\S+`)
	reControlTag = regexp.MustCompile(`\[\[[^\]]*\]\]`)
	reMdPunct    = regexp.MustCompile(`[*_~>#]+`)
	reMultiSpace = regexp.MustCompile(`\s+`)
)

// Sanitize removes markdown formatting, code blocks, URLs, and control tags.
func Sanitize(text string) string {
	text = reFencedCode.ReplaceAllString(text, "")
	text = reInlineCode.ReplaceAllString(text, "")
	text = reMdLink.ReplaceAllString(text, "$1")
	text = reURL.ReplaceAllString(text, "")
	text = reControlTag.ReplaceAllString(text, "")
	text = reMdPunct.ReplaceAllString(text, "")
	text = reMultiSpace.ReplaceAllString(text, " ")
	return strings.TrimSpace(text)
}
