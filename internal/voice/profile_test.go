package voice

import "testing"

func TestOpenAIAdapter_MapVoice(t *testing.T) {
	a := &OpenAIAdapter{}
	if got := a.MapVoice(&VoiceProfile{Voice: "alloy"}); got != "alloy" {
		t.Errorf("expected alloy, got %s", got)
	}
	if got := a.MapVoice(&VoiceProfile{}); got != "nova" {
		t.Errorf("expected default nova, got %s", got)
	}
}

func TestOpenAIAdapter_MapInstructions(t *testing.T) {
	a := &OpenAIAdapter{}
	if got := a.MapInstructions(&VoiceProfile{Emotion: "neutral"}); got != "" {
		t.Errorf("neutral should produce empty instructions, got %q", got)
	}
	if got := a.MapInstructions(&VoiceProfile{Emotion: "cheerful", Style: "narration"}); got == "" {
		t.Error("non-neutral emotion should produce instructions")
	}
}

func TestOpenAIAdapter_MapSpeed(t *testing.T) {
	a := &OpenAIAdapter{}
	if got := a.MapSpeed(&VoiceProfile{Speed: 1.5}); got != 1.5 {
		t.Errorf("expected 1.5, got %f", got)
	}
	if got := a.MapSpeed(&VoiceProfile{}); got != 1.0 {
		t.Errorf("expected default 1.0, got %f", got)
	}
}
