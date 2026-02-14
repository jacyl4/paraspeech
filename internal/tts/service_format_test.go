package tts

import "testing"

func TestNormalizeAudioFormat(t *testing.T) {
	tests := map[string]string{
		"opus":                   "opus",
		"OpUs":                   "opus",
		"ogg":                    "opus",
		"audio/ogg":              "opus",
		"audio/opus":             "opus",
		"ogg/opus":               "opus",
		"audio/ogg; codecs=opus": "opus",
		"audio/ogg;codecs=opus":  "opus",
		"mp3":                    "mp3",
		"wav":                    "wav",
		"  ":                     "",
	}

	for in, want := range tests {
		if got := normalizeAudioFormat(in); got != want {
			t.Fatalf("normalizeAudioFormat(%q) = %q, want %q", in, got, want)
		}
	}
}
