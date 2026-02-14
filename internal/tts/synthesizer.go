package tts

import (
	"context"
	"io"

	"paraspeech/internal/voice"
)

type SynthesizeRequest struct {
	Text         string
	VoiceProfile *voice.VoiceProfile
	Model        string
	Format       string
}

type SynthesizeResult struct {
	Audio       io.Reader
	ContentType string
	SizeBytes   int64
	DurationMS  int64
}

type Synthesizer interface {
	Synthesize(ctx context.Context, req *SynthesizeRequest) (*SynthesizeResult, error)
	SynthesizeStream(ctx context.Context, req *SynthesizeRequest, out chan<- []byte) error
}
