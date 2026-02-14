package stt

import (
	"context"
	"io"
)

type TranscribeRequest struct {
	Audio    io.Reader
	Filename string
	Language string
	Prompt   string
	Format   string
	Model    string
}

type TranscribeResult struct {
	Text       string
	DurationMS int64
	Segments   []Segment
	VadMeta    *VadMeta
}

type Segment struct {
	Index   int
	StartMS int64
	EndMS   int64
	Text    string
}

type VadMeta struct {
	Enabled       bool
	Reason        string
	Fallback      bool
	AudioMsBefore int64
	AudioMsAfter  int64
	TrimRatio     float64
	SegmentsCount int32
	ElapsedMS     int64
}

type Transcriber interface {
	Transcribe(ctx context.Context, req *TranscribeRequest) (*TranscribeResult, error)
	TranscribeStream(ctx context.Context, req *TranscribeRequest, out chan<- *Segment) error
}
