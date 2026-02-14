package tts

import (
	"context"
	"log/slog"
	"strings"

	"golang.org/x/sync/errgroup"

	"paraspeech/internal/config"
	"paraspeech/internal/errs"
	"paraspeech/internal/voice"
)

type Service struct {
	provider Synthesizer
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

type synthesisParams struct {
	profile    *voice.VoiceProfile
	model      string
	format     string
	segments   []TextSegment
	cleanedLen int
}

func NewService(provider Synthesizer, cfg config.TTS) *Service {
	return &Service{provider: provider, cfg: cfg}
}

func (s *Service) SynthesizeSegments(ctx context.Context, text string, profile *voice.VoiceProfile, opt *SynthesizeOptions) ([]SegmentResult, error) {
	params, err := s.prepareSynthesisRequest(text, profile, opt)
	if err != nil {
		return nil, err
	}
	slog.Info("tts segments", "count", len(params.segments), "text_len", params.cleanedLen)

	n := len(params.segments)
	if n == 1 {
		seg := params.segments[0]
		upstream, err := s.provider.Synthesize(ctx, &SynthesizeRequest{
			Text:         seg.Text,
			VoiceProfile: params.profile,
			Model:        params.model,
			Format:       params.format,
		})
		if err != nil {
			return nil, errs.Wrap(errs.ErrTTSUpstream, err)
		}
		return []SegmentResult{{
			Index:        0,
			Text:         seg.Text,
			EstimatedSec: seg.EstimatedSec,
			Audio:        upstream.Audio,
			SizeBytes:    int64(len(upstream.Audio)),
			ContentType:  upstream.ContentType,
		}}, nil
	}

	limit := s.cfg.MaxParallel
	if limit <= 0 {
		limit = 1
	}
	result := make([]SegmentResult, n)
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(limit)
	for i, seg := range params.segments {
		i, seg := i, seg
		g.Go(func() error {
			upstream, err := s.provider.Synthesize(gctx, &SynthesizeRequest{
				Text:         seg.Text,
				VoiceProfile: params.profile,
				Model:        params.model,
				Format:       params.format,
			})
			if err != nil {
				return errs.Wrap(errs.ErrTTSUpstream, err)
			}
			result[i] = SegmentResult{
				Index:        i,
				Text:         seg.Text,
				EstimatedSec: seg.EstimatedSec,
				Audio:        upstream.Audio,
				SizeBytes:    int64(len(upstream.Audio)),
				ContentType:  upstream.ContentType,
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Service) SynthesizeStreamSegments(ctx context.Context, text string, profile *voice.VoiceProfile, opt *SynthesizeOptions, onSegment func(SegmentResult) error) (int, error) {
	params, err := s.prepareSynthesisRequest(text, profile, opt)
	if err != nil {
		return 0, err
	}
	slog.Info("tts stream segments", "count", len(params.segments), "text_len", params.cleanedLen)

	n := len(params.segments)
	if n == 1 {
		seg := params.segments[0]
		upstream, err := s.provider.Synthesize(ctx, &SynthesizeRequest{
			Text:         seg.Text,
			VoiceProfile: params.profile,
			Model:        params.model,
			Format:       params.format,
		})
		if err != nil {
			return 0, errs.Wrap(errs.ErrTTSUpstream, err)
		}
		if err := onSegment(SegmentResult{
			Index:        0,
			Text:         seg.Text,
			EstimatedSec: seg.EstimatedSec,
			Audio:        upstream.Audio,
			SizeBytes:    int64(len(upstream.Audio)),
			ContentType:  upstream.ContentType,
		}); err != nil {
			return 0, err
		}
		return 1, nil
	}

	type slotResult struct {
		seg SegmentResult
		err error
	}
	slots := make([]chan slotResult, n)
	for i := range slots {
		slots[i] = make(chan slotResult, 1)
	}

	limit := s.cfg.MaxParallel
	if limit <= 0 {
		limit = 1
	}
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(limit)
	for i, seg := range params.segments {
		i, seg := i, seg
		slot := slots[i]
		g.Go(func() error {
			upstream, err := s.provider.Synthesize(gctx, &SynthesizeRequest{
				Text:         seg.Text,
				VoiceProfile: params.profile,
				Model:        params.model,
				Format:       params.format,
			})
			if err != nil {
				err = errs.Wrap(errs.ErrTTSUpstream, err)
				slot <- slotResult{err: err}
				return err
			}
			slot <- slotResult{seg: SegmentResult{
				Index:        i,
				Text:         seg.Text,
				EstimatedSec: seg.EstimatedSec,
				Audio:        upstream.Audio,
				SizeBytes:    int64(len(upstream.Audio)),
				ContentType:  upstream.ContentType,
			}}
			return nil
		})
	}

	for _, slot := range slots {
		r := <-slot
		if r.err != nil {
			return 0, r.err
		}
		if err := onSegment(r.seg); err != nil {
			return 0, err
		}
	}
	if err := g.Wait(); err != nil {
		return 0, err
	}
	return n, nil
}

func (s *Service) prepareSynthesisRequest(text string, profile *voice.VoiceProfile, opt *SynthesizeOptions) (*synthesisParams, error) {
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
	format = normalizeAudioFormat(format)
	return &synthesisParams{
		profile:    profile,
		model:      model,
		format:     format,
		segments:   Split(cleaned, maxSec),
		cleanedLen: len(cleaned),
	}, nil
}

func normalizeAudioFormat(format string) string {
	f := strings.ToLower(strings.TrimSpace(format))
	switch f {
	case "ogg", "opus", "audio/ogg", "audio/opus", "ogg/opus", "audio/ogg;codecs=opus", "audio/ogg; codecs=opus":
		return "opus"
	default:
		return f
	}
}
