package tts

import (
	"bytes"
	"context"
	"io"
	"log/slog"

	"paraspeech/internal/config"
	"paraspeech/internal/errs"
	"paraspeech/internal/voice"
)

type Service struct {
	provider Synthesizer
	adapter  voice.ProviderAdapter
	cfg      config.TTS
}

func NewService(provider Synthesizer, adapter voice.ProviderAdapter, cfg config.TTS) *Service {
	return &Service{
		provider: provider,
		adapter:  adapter,
		cfg:      cfg,
	}
}

func (s *Service) Synthesize(ctx context.Context, text string, profile *voice.VoiceProfile) (*SynthesizeResult, error) {
	cleaned := Sanitize(text)
	if cleaned == "" {
		return nil, errs.New(errs.ErrEmptyInput, "empty text after sanitization")
	}

	if profile == nil {
		profile = &voice.VoiceProfile{
			Voice:   s.cfg.DefaultVoice,
			Speed:   s.cfg.DefaultSpeed,
			Emotion: s.cfg.DefaultEmotion,
			Style:   s.cfg.DefaultStyle,
		}
	}

	segments := Split(cleaned, s.cfg.MaxSec)
	slog.Info("tts segments", "count", len(segments), "text_len", len(cleaned))

	var allAudio bytes.Buffer
	for i, seg := range segments {
		result, err := s.provider.Synthesize(ctx, &SynthesizeRequest{
			Text:         seg.Text,
			VoiceProfile: profile,
			Model:        s.cfg.DefaultModel,
			Format:       s.cfg.DefaultFormat,
		})
		if err != nil {
			return nil, errs.WithDetails(errs.ErrTTSUpstream, "upstream synthesis failed", map[string]any{
				"segment_index": i,
				"segment_text":  seg.Text,
			})
		}
		if _, err := io.Copy(&allAudio, result.Audio); err != nil {
			return nil, errs.Wrap(errs.ErrInternal, err)
		}
	}

	return &SynthesizeResult{
		Audio:       &allAudio,
		ContentType: "audio/ogg",
		SizeBytes:   int64(allAudio.Len()),
	}, nil
}
