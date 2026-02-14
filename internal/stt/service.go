package stt

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"paraspeech/internal/codec"
	"paraspeech/internal/config"
	"paraspeech/internal/errs"
	"paraspeech/internal/vad"
)

type TranscribeEvent struct {
	Type            string
	Text            string
	AccumulatedText string
	VadMeta         *VadMeta
}

const (
	EventPartial = "partial"
	EventFinal   = "final"
)

type route int

const (
	routeDirect route = iota
	routeVAD
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

func (s *Service) chooseRoute(durationHint float64, audioSize int64) route {
	if durationHint > 0 {
		if durationHint >= s.cfg.VAD.MaxAudioSec {
			return routeVAD
		}
		return routeDirect
	}
	if audioSize >= s.cfg.DirectMaxBytes {
		return routeVAD
	}
	return routeDirect
}

func (s *Service) resolveModel(model string) string {
	if strings.TrimSpace(model) != "" {
		return model
	}
	return s.cfg.DefaultModel
}

func (s *Service) prepareDirectUpload(ctx context.Context, audio io.Reader, filename string) (io.ReadCloser, string, error) {
	reader := bufio.NewReader(audio)
	head, _ := reader.Peek(64)
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(filename)))
	if isOggOpus(ext, head) {
		r, err := codec.RemuxToWebm(ctx, reader)
		if err != nil {
			return nil, "", err
		}
		return r, "audio.webm", nil
	}
	if isWebm(ext, head) {
		return io.NopCloser(reader), "audio.webm", nil
	}
	r, err := codec.TranscodeToWebmOpus(ctx, reader)
	if err != nil {
		return nil, "", err
	}
	return r, "audio.webm", nil
}

func isOggOpus(ext string, head []byte) bool {
	if ext == ".ogg" || ext == ".opus" {
		return true
	}
	return len(head) >= 4 && string(head[:4]) == "OggS"
}

func isWebm(ext string, head []byte) bool {
	if ext == ".webm" {
		return true
	}
	return len(head) >= 4 && bytes.Equal(head[:4], []byte{0x1A, 0x45, 0xDF, 0xA3})
}

func (s *Service) Transcribe(ctx context.Context, audio io.Reader, filename string, durationHint float64, audioSize int64, language, model, prompt string) (*TranscribeResult, error) {
	if audioSize <= 0 {
		if b, err := io.ReadAll(audio); err == nil {
			audioSize = int64(len(b))
			audio = bytes.NewReader(b)
		} else {
			return nil, errs.Wrap(errs.ErrSTTDecodeFailed, err)
		}
	}

	r := s.chooseRoute(durationHint, audioSize)
	if r == routeDirect {
		directReader, uploadName, err := s.prepareDirectUpload(ctx, audio, filename)
		if err != nil {
			return nil, errs.Wrap(errs.ErrSTTDecodeFailed, err)
		}
		defer directReader.Close()

		result, err := s.provider.Transcribe(ctx, &TranscribeRequest{
			Audio:    directReader,
			Filename: uploadName,
			Language: language,
			Prompt:   prompt,
			Model:    s.resolveModel(model),
		})
		if err != nil {
			return nil, errs.Wrap(errs.ErrSTTUpstream, err)
		}
		result.VadMeta = &VadMeta{Enabled: false, Reason: "direct_route"}
		_ = filename
		return result, nil
	}

	audioBytes, err := io.ReadAll(audio)
	if err != nil {
		return nil, errs.Wrap(errs.ErrSTTDecodeFailed, err)
	}

	pcm, err := codec.Decode(ctx, bytes.NewReader(audioBytes))
	if err != nil {
		return nil, errs.Wrap(errs.ErrSTTDecodeFailed, err)
	}
	defer pcm.Close()

	trimmedAudio := io.Reader(pcm)
	vadMeta := &VadMeta{Enabled: false, Reason: "vad_off"}
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
			fallbackPCM, decodeErr := codec.Decode(ctx, bytes.NewReader(audioBytes))
			if decodeErr != nil {
				return nil, errs.Wrap(errs.ErrSTTDecodeFailed, decodeErr)
			}
			defer fallbackPCM.Close()
			trimmedAudio = fallbackPCM
		}
	}

	wavAudio, err := toWAVAudio(trimmedAudio)
	if err != nil {
		return nil, errs.Wrap(errs.ErrSTTDecodeFailed, err)
	}

	result, err := s.provider.Transcribe(ctx, &TranscribeRequest{
		Audio:    bytes.NewReader(wavAudio),
		Filename: "audio.wav",
		Language: language,
		Prompt:   prompt,
		Model:    s.resolveModel(model),
	})
	if err != nil {
		return nil, errs.Wrap(errs.ErrSTTUpstream, err)
	}
	result.VadMeta = vadMeta
	_ = filename
	return result, nil
}

func (s *Service) TranscribeStream(ctx context.Context, audio io.Reader, filename string, durationHint float64, audioSize int64, language, model, prompt string, out chan<- *TranscribeEvent) error {
	_ = filename
	r := s.chooseRoute(durationHint, audioSize)
	finalVadMeta := &VadMeta{Enabled: false, Reason: "direct_route"}

	var uploadReader io.Reader
	var closers []io.Closer
	uploadFilename := "audio.webm"

	switch r {
	case routeDirect:
		directReader, uploadName, err := s.prepareDirectUpload(ctx, audio, filename)
		if err != nil {
			return errs.Wrap(errs.ErrSTTDecodeFailed, err)
		}
		uploadReader = directReader
		uploadFilename = uploadName
		closers = append(closers, directReader)
	case routeVAD:
		finalVadMeta = &VadMeta{Enabled: true, Reason: "stream_vad_route"}
		pcm, err := codec.Decode(ctx, audio)
		if err != nil {
			return errs.Wrap(errs.ErrSTTDecodeFailed, err)
		}
		var vadOutput io.Reader = pcm
		if s.detector != nil && s.cfg.VAD.Mode != "off" {
			vadOutput = s.vadStreamProcess(pcm)
		} else {
			finalVadMeta = &VadMeta{Enabled: false, Reason: "stream_vad_route_without_detector"}
		}
		encoded, err := codec.EncodeToWebmOpus(ctx, vadOutput)
		if err != nil {
			_ = pcm.Close()
			return errs.Wrap(errs.ErrSTTDecodeFailed, err)
		}
		uploadReader = encoded
		closers = append(closers, encoded, pcm)
	}

	defer func() {
		for _, c := range closers {
			_ = c.Close()
		}
	}()

	deltaCh := make(chan string, 64)
	errCh := make(chan error, 1)
	go func() {
		errCh <- s.provider.TranscribeStreamSSE(ctx, &TranscribeRequest{
			Audio:    uploadReader,
			Filename: uploadFilename,
			Language: language,
			Prompt:   prompt,
			Model:    s.resolveModel(model),
		}, deltaCh)
		close(deltaCh)
	}()

	var accumulated strings.Builder
	for delta := range deltaCh {
		accumulated.WriteString(delta)
		out <- &TranscribeEvent{
			Type:            EventPartial,
			Text:            delta,
			AccumulatedText: accumulated.String(),
		}
	}

	if err := <-errCh; err != nil {
		return errs.Wrap(errs.ErrSTTUpstream, err)
	}

	out <- &TranscribeEvent{
		Type:    EventFinal,
		Text:    accumulated.String(),
		VadMeta: finalVadMeta,
	}
	return nil
}

func toWAVAudio(r io.Reader) ([]byte, error) {
	// ffmpeg Decode already normalized to 16kHz mono s16le.
	return codec.PCMReaderToWAV(r, 16000, 1, 16)
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

func (s *Service) vadStreamProcess(pcm io.ReadCloser) io.Reader {
	pr, pw := io.Pipe()

	go func() {
		defer pw.Close()
		frames := codec.ReadFrames(pcm, s.detector.HopSize())
		hopSize := s.detector.HopSize()
		msPerFrame := float64(hopSize) * 1000.0 / 16000.0
		maxGapFrames := int(float64(s.cfg.VAD.MaxGapMS) / msPerFrame)
		minSpeechFrames := int(float64(s.cfg.VAD.MinSpeechMS) / msPerFrame)
		padFrames := int(float64(s.cfg.VAD.PadMS) / msPerFrame)

		padBuf := make([][]int16, 0, padFrames)
		inSpeech := false
		speechFrameCount := 0
		silenceFrameCount := 0

		for frame := range frames {
			fr, err := s.detector.Process(frame)
			if err != nil {
				pw.CloseWithError(err)
				return
			}

			if fr.IsVoice {
				if !inSpeech {
					for _, pf := range padBuf {
						if err := writeFrameBytes(pw, pf); err != nil {
							pw.CloseWithError(err)
							return
						}
					}
					padBuf = padBuf[:0]
					inSpeech = true
					speechFrameCount = 0
				}
				speechFrameCount++
				silenceFrameCount = 0
				if err := writeFrameBytes(pw, frame); err != nil {
					pw.CloseWithError(err)
					return
				}
				continue
			}

			if inSpeech {
				silenceFrameCount++
				if silenceFrameCount <= maxGapFrames {
					if err := writeFrameBytes(pw, frame); err != nil {
						pw.CloseWithError(err)
						return
					}
					continue
				}
				if speechFrameCount < minSpeechFrames {
					// Stream mode cannot roll back short segments already written.
				}
				inSpeech = false
				speechFrameCount = 0
				silenceFrameCount = 0
				continue
			}

			if len(padBuf) >= padFrames && padFrames > 0 {
				padBuf = padBuf[1:]
			}
			if padFrames > 0 {
				padBuf = append(padBuf, frame)
			}
		}
	}()

	return pr
}

func writeFrameBytes(w io.Writer, frame []int16) error {
	buf := make([]byte, len(frame)*2)
	for i, s := range frame {
		binary.LittleEndian.PutUint16(buf[i*2:], uint16(s))
	}
	_, err := w.Write(buf)
	return err
}
