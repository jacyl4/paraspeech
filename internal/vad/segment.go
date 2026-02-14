package vad

import "paraspeech/internal/config"

type segmentMerger struct {
	cfg config.VAD
}

func NewSegmentMerger(cfg config.VAD) SegmentMerger {
	return &segmentMerger{cfg: cfg}
}

func (m *segmentMerger) Merge(results []FrameResult, hopSize int, sampleRate int) []AudioSegment {
	msPerFrame := float64(hopSize) * 1000.0 / float64(sampleRate)

	// 1. Find contiguous voice regions
	type rawSeg struct{ start, end int }
	var raw []rawSeg
	inSpeech := false
	start := 0
	for i, r := range results {
		if r.IsVoice && !inSpeech {
			start = i
			inSpeech = true
		} else if !r.IsVoice && inSpeech {
			raw = append(raw, rawSeg{start, i})
			inSpeech = false
		}
	}
	if inSpeech {
		raw = append(raw, rawSeg{start, len(results)})
	}

	// 2. Filter segments shorter than MinSpeechMS
	var filtered []rawSeg
	for _, s := range raw {
		durMS := float64(s.end-s.start) * msPerFrame
		if durMS >= float64(m.cfg.MinSpeechMS) {
			filtered = append(filtered, s)
		}
	}

	// 3. Merge segments with gap <= MaxGapMS
	var merged []rawSeg
	for _, s := range filtered {
		if len(merged) > 0 {
			last := &merged[len(merged)-1]
			gapMS := float64(s.start-last.end) * msPerFrame
			if gapMS <= float64(m.cfg.MaxGapMS) {
				last.end = s.end
				continue
			}
		}
		merged = append(merged, s)
	}

	// 4. Pad and convert to AudioSegment
	padFrames := int(float64(m.cfg.PadMS) / msPerFrame)
	totalFrames := len(results)
	var segments []AudioSegment
	for _, s := range merged {
		startFrame := s.start - padFrames
		if startFrame < 0 {
			startFrame = 0
		}
		endFrame := s.end + padFrames
		if endFrame > totalFrames {
			endFrame = totalFrames
		}
		seg := AudioSegment{
			StartSample: startFrame * hopSize,
			EndSample:   endFrame * hopSize,
			StartMS:     int64(float64(startFrame) * msPerFrame),
			EndMS:       int64(float64(endFrame) * msPerFrame),
		}
		// Overlap elimination with previous segment
		if len(segments) > 0 {
			prev := &segments[len(segments)-1]
			if seg.StartSample < prev.EndSample {
				prev.EndSample = seg.EndSample
				prev.EndMS = seg.EndMS
				continue
			}
		}
		segments = append(segments, seg)
	}

	return segments
}
