package tts

import (
	"context"

	"paraspeech/internal/voice"
)

type SynthesizeRequest struct {
	Text         string
	VoiceProfile *voice.VoiceProfile
	Model        string
	Format       string
}

type SynthesizeResult struct {
	Audio       []byte
	ContentType string
}

type Synthesizer interface {
	Synthesize(ctx context.Context, req *SynthesizeRequest) (*SynthesizeResult, error)
}
