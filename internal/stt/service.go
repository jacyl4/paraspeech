package stt

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"

	"paraspeech/internal/codec"
	"paraspeech/internal/config"
	"paraspeech/internal/errs"
	"paraspeech/internal/vad"
)

type Service struct {
	detector vad.Detector
	merger   vad.SegmentMerger
	provider Transcriber
	cfg      config.STT
}

func NewService(detector vad.Detector, merger vad.SegmentMerger, provider Transcriber, cfg config.STT) *Service {
	return &Service{
		detector: detector,
		merger:   merger,
		provider: provider,
		cfg:      cfg,
	}
}

func (s *Service) Transcribe(ctx context.Context, audio io.Reader, filename string) (*TranscribeResult, error) {
	pcm, err := codec.Decode(ctx, audio)
	if err != nil {
		return nil, errs.Wrap(errs.ErrSTTDecodeFailed, err)
	}
	defer pcm.Close()

	var trimmedAudio io.Reader
	var vadMeta *VadMeta

	if s.detector != nil && s.cfg.VAD.Mode != "off" {
		start := time.Now()
		trimmedAudio, vadMeta, err = s.vadProcess(pcm)
		if vadMeta != nil {
			vadMeta.ElapsedMS = time.Since(start).Milliseconds()
		}
		if err != nil {
			slog.Warn("VAD failed, falling back to raw audio", "error", err)
			vadMeta = &VadMeta{
				Enabled:  true,
				Fallback: true,
				Reason:   fmt.Sprintf("vad_error:%T", err),
			}
			// Re-decode for fallback
			trimmedAudio = audio
		}
	} else {
		trimmedAudio = pcm
		vadMeta = &VadMeta{Enabled: false, Reason: "vad_off"}
	}

	result, err := s.provider.Transcribe(ctx, &TranscribeRequest{
		Audio:    trimmedAudio,
		Filename: filename,
		Model:    s.cfg.DefaultModel,
	})
	if err != nil {
		return nil, errs.Wrap(errs.ErrSTTUpstream, err)
	}
	result.VadMeta = vadMeta
	return result, nil
}

func (s *Service) vadProcess(pcm io.ReadCloser) (io.Reader, *VadMeta, error) {
	frames := codec.ReadFrames(pcm, s.detector.HopSize())
	var results []vad.FrameResult
	var allSamples []int16

	for frame := range frames {
		allSamples = append(allSamples, frame...)
		fr, err := s.detector.Process(frame)
		if err != nil {
			return nil, nil, err
		}
		results = append(results, *fr)
	}

	if len(allSamples) == 0 {
		return nil, nil, fmt.Errorf("no audio samples decoded")
	}

	// Check audio duration against max
	audioSec := float64(len(allSamples)) / 16000.0
	if audioSec > s.cfg.VAD.MaxAudioSec {
		return codec.SamplesToReader(allSamples), &VadMeta{
			Enabled:       true,
			Reason:        "audio_too_long_skip_vad",
			AudioMsBefore: int64(audioSec * 1000),
			AudioMsAfter:  int64(audioSec * 1000),
		}, nil
	}

	segments := s.merger.Merge(results, s.detector.HopSize(), 16000)
	if len(segments) == 0 {
		return codec.SamplesToReader(allSamples), &VadMeta{
			Enabled:       true,
			Fallback:      true,
			Reason:        "no_voice_detected",
			AudioMsBefore: int64(audioSec * 1000),
			AudioMsAfter:  int64(audioSec * 1000),
		}, nil
	}

	trimmed := vad.ExtractSegments(allSamples, segments)
	if len(trimmed) == 0 {
		return codec.SamplesToReader(allSamples), &VadMeta{
			Enabled:       true,
			Fallback:      true,
			Reason:        "empty_trimmed_audio",
			AudioMsBefore: int64(audioSec * 1000),
			AudioMsAfter:  int64(audioSec * 1000),
		}, nil
	}

	audioMsBefore := int64(len(allSamples)) * 1000 / 16000
	audioMsAfter := int64(len(trimmed)) * 1000 / 16000
	trimRatio := float64(audioMsAfter) / float64(audioMsBefore)

	if trimRatio < s.cfg.VAD.MinTrimRatio {
		return codec.SamplesToReader(allSamples), &VadMeta{
			Enabled:       true,
			Fallback:      true,
			Reason:        "trim_ratio_too_small_fallback",
			AudioMsBefore: audioMsBefore,
			AudioMsAfter:  audioMsBefore,
			TrimRatio:     trimRatio,
		}, nil
	}

	return codec.SamplesToReader(trimmed), &VadMeta{
		Enabled:       true,
		Reason:        "ok",
		AudioMsBefore: audioMsBefore,
		AudioMsAfter:  audioMsAfter,
		TrimRatio:     trimRatio,
		SegmentsCount: int32(len(segments)),
	}, nil
}
