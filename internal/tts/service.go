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

type SynthesizeOptions struct {
	Model  string
	Format string
	MaxSec float64
}

type SegmentResult struct {
	Index        int
	Text         string
	EstimatedSec float64
	Audio        []byte
	SizeBytes    int64
	ContentType  string
}

func NewService(provider Synthesizer, adapter voice.ProviderAdapter, cfg config.TTS) *Service {
	return &Service{
		provider: provider,
		adapter:  adapter,
		cfg:      cfg,
	}
}

func (s *Service) Synthesize(ctx context.Context, text string, profile *voice.VoiceProfile) (*SynthesizeResult, error) {
	segments, err := s.SynthesizeSegments(ctx, text, profile, nil)
	if err != nil {
		return nil, err
	}
	var allAudio bytes.Buffer
	contentType := "audio/ogg"
	for _, seg := range segments {
		if _, err := allAudio.Write(seg.Audio); err != nil {
			return nil, errs.Wrap(errs.ErrInternal, err)
		}
		if seg.ContentType != "" {
			contentType = seg.ContentType
		}
	}
	return &SynthesizeResult{
		Audio:       bytes.NewReader(allAudio.Bytes()),
		ContentType: contentType,
		SizeBytes:   int64(allAudio.Len()),
	}, nil
}

func (s *Service) SynthesizeSegments(ctx context.Context, text string, profile *voice.VoiceProfile, opt *SynthesizeOptions) ([]SegmentResult, error) {
	cleaned := Sanitize(text)
	if cleaned == "" {
		return nil, errs.New(errs.ErrEmptyInput, "empty text after sanitization")
	}

	if profile == nil {
		profile = &voice.VoiceProfile{}
	}
	if profile.Voice == "" {
		profile.Voice = s.cfg.DefaultVoice
	}
	if profile.Speed == 0 {
		profile.Speed = s.cfg.DefaultSpeed
	}
	if profile.Emotion == "" {
		profile.Emotion = s.cfg.DefaultEmotion
	}
	if profile.Style == "" {
		profile.Style = s.cfg.DefaultStyle
	}

	model := s.cfg.DefaultModel
	format := s.cfg.DefaultFormat
	maxSec := s.cfg.MaxSec
	if opt != nil {
		if opt.Model != "" {
			model = opt.Model
		}
		if opt.Format != "" {
			format = opt.Format
		}
		if opt.MaxSec > 0 {
			maxSec = opt.MaxSec
		}
	}

	segments := Split(cleaned, maxSec)
	slog.Info("tts segments", "count", len(segments), "text_len", len(cleaned))

	result := make([]SegmentResult, 0, len(segments))
	for i, seg := range segments {
		upstream, err := s.provider.Synthesize(ctx, &SynthesizeRequest{
			Text:         seg.Text,
			VoiceProfile: profile,
			Model:        model,
			Format:       format,
		})
		if err != nil {
			return nil, errs.WithDetails(errs.ErrTTSUpstream, "upstream synthesis failed", map[string]any{
				"segment_index": i,
				"segment_text":  seg.Text,
			})
		}
		audioBytes, err := io.ReadAll(upstream.Audio)
		if err != nil {
			return nil, errs.Wrap(errs.ErrInternal, err)
		}
		result = append(result, SegmentResult{
			Index:        i,
			Text:         seg.Text,
			EstimatedSec: seg.EstimatedSec,
			Audio:        audioBytes,
			SizeBytes:    int64(len(audioBytes)),
			ContentType:  upstream.ContentType,
		})
	}
	return result, nil
}
