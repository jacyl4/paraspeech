package vad

// FrameResult is the per-frame output from TEN VAD.
type FrameResult struct {
	Probability float32
	IsVoice     bool
}

// Detector processes 16kHz mono PCM int16 frames.
type Detector interface {
	Process(frame []int16) (*FrameResult, error)
	HopSize() int
	Close() error
}

// AudioSegment represents a contiguous speech region in samples.
type AudioSegment struct {
	StartSample int
	EndSample   int
	StartMS     int64
	EndMS       int64
}

// SegmentMerger merges per-frame results into speech segments.
type SegmentMerger interface {
	Merge(results []FrameResult, hopSize int, sampleRate int) []AudioSegment
}
