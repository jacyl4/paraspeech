package tts

import (
	"regexp"
	"strings"
	"unicode"
)

type TextSegment struct {
	Text         string
	EstimatedSec float64
}

var reSentenceEnd = regexp.MustCompile(`([。！？.!?])\s*`)

// Split divides text at paragraph/line boundaries, preserving punctuation.
func Split(text string, maxSec float64) []TextSegment {
	safeSec := maxSec * 0.88

	paragraphs := strings.Split(text, "\n\n")
	var segments []TextSegment

	for _, para := range paragraphs {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}
		est := EstimateDuration(para)
		if est <= safeSec {
			segments = append(segments, TextSegment{Text: para, EstimatedSec: est})
			continue
		}
		// Split by newline
		lines := strings.Split(para, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			est = EstimateDuration(line)
			if est <= safeSec {
				segments = append(segments, TextSegment{Text: line, EstimatedSec: est})
				continue
			}
			// Split at sentence-ending punctuation only
			segments = append(segments, splitAtSentenceEnd(line, safeSec)...)
		}
	}
	return segments
}

func splitAtSentenceEnd(text string, safeSec float64) []TextSegment {
	indices := reSentenceEnd.FindAllStringIndex(text, -1)
	if len(indices) == 0 {
		return []TextSegment{{Text: text, EstimatedSec: EstimateDuration(text)}}
	}

	var segments []TextSegment
	start := 0
	var buf strings.Builder

	for _, idx := range indices {
		candidate := text[start : idx[1]]
		combined := buf.String() + candidate
		if EstimateDuration(combined) > safeSec && buf.Len() > 0 {
			segments = append(segments, TextSegment{
				Text:         strings.TrimSpace(buf.String()),
				EstimatedSec: EstimateDuration(buf.String()),
			})
			buf.Reset()
		}
		buf.WriteString(candidate)
		start = idx[1]
	}
	if start < len(text) {
		buf.WriteString(text[start:])
	}
	if buf.Len() > 0 {
		s := strings.TrimSpace(buf.String())
		if s != "" {
			segments = append(segments, TextSegment{Text: s, EstimatedSec: EstimateDuration(s)})
		}
	}
	return segments
}

// EstimateDuration estimates speech duration in seconds.
// CJK: 4.2 chars/s, Latin: 2.6 words/s, digits: 5/s, pauses: 0.18s each.
func EstimateDuration(text string) float64 {
	var cjk, digits, pauses int
	var latinWords int

	inWord := false
	for _, r := range text {
		if unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r) || unicode.Is(unicode.Hangul, r) {
			cjk++
			inWord = false
		} else if unicode.IsDigit(r) {
			digits++
			inWord = false
		} else if r == ',' || r == '.' || r == '!' || r == '?' || r == ';' || r == '，' || r == '。' || r == '！' || r == '？' || r == '；' {
			pauses++
			inWord = false
		} else if unicode.IsLetter(r) {
			if !inWord {
				latinWords++
				inWord = true
			}
		} else {
			inWord = false
		}
	}

	sec := float64(cjk)/4.2 + float64(latinWords)/2.6 + float64(digits)/5.0 + float64(pauses)*0.18
	if sec < 0.4 {
		sec = 0.4
	}
	return sec
}
