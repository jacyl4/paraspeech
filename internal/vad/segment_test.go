package vad

import (
	"paraspeech/internal/config"
	"testing"
)

func TestMerge_BasicVoiceRegion(t *testing.T) {
	cfg := config.VAD{
		MinSpeechMS: 200,
		MaxGapMS:    500,
		PadMS:       150,
	}
	m := NewSegmentMerger(cfg)

	// 100 frames, all voice, hopSize=256 @ 16kHz → 16ms/frame
	results := make([]FrameResult, 100)
	for i := range results {
		results[i] = FrameResult{Probability: 0.9, IsVoice: true}
	}

	segments := m.Merge(results, 256, 16000)
	if len(segments) == 0 {
		t.Fatal("expected at least 1 segment")
	}
	if segments[0].StartMS > 0 {
		t.Logf("padded start: %dms", segments[0].StartMS)
	}
}

func TestMerge_FilterShortSpeech(t *testing.T) {
	cfg := config.VAD{
		MinSpeechMS: 200,
		MaxGapMS:    500,
		PadMS:       0,
	}
	m := NewSegmentMerger(cfg)

	// 5 frames voice (80ms @ 16ms/frame) → below MinSpeechMS → filtered
	results := make([]FrameResult, 20)
	for i := 0; i < 5; i++ {
		results[i] = FrameResult{Probability: 0.9, IsVoice: true}
	}

	segments := m.Merge(results, 256, 16000)
	if len(segments) != 0 {
		t.Errorf("expected 0 segments for short speech, got %d", len(segments))
	}
}

func TestMerge_MergeGap(t *testing.T) {
	cfg := config.VAD{
		MinSpeechMS: 100,
		MaxGapMS:    500,
		PadMS:       0,
	}
	m := NewSegmentMerger(cfg)

	// Two voice regions with a small gap (< MaxGapMS) → merged into one
	results := make([]FrameResult, 60)
	for i := 0; i < 20; i++ {
		results[i] = FrameResult{IsVoice: true}
	}
	// gap: frames 20-29 (10 frames = 160ms < 500ms)
	for i := 30; i < 50; i++ {
		results[i] = FrameResult{IsVoice: true}
	}

	segments := m.Merge(results, 256, 16000)
	if len(segments) != 1 {
		t.Errorf("expected 1 merged segment, got %d", len(segments))
	}
}

func TestMerge_NoVoice(t *testing.T) {
	cfg := config.VAD{MinSpeechMS: 200, MaxGapMS: 500, PadMS: 0}
	m := NewSegmentMerger(cfg)

	results := make([]FrameResult, 50) // all silence
	segments := m.Merge(results, 256, 16000)
	if len(segments) != 0 {
		t.Errorf("expected 0 segments, got %d", len(segments))
	}
}

func TestExtractSegments(t *testing.T) {
	samples := make([]int16, 1000)
	for i := range samples {
		samples[i] = int16(i)
	}

	segments := []AudioSegment{
		{StartSample: 100, EndSample: 200},
		{StartSample: 500, EndSample: 600},
	}

	result := ExtractSegments(samples, segments)
	if len(result) != 200 {
		t.Errorf("expected 200 samples, got %d", len(result))
	}
	if result[0] != 100 || result[100] != 500 {
		t.Error("extracted samples have wrong values")
	}
}
