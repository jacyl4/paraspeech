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
	Model    string
}

type TranscribeResult struct {
	Text       string
	DurationMS int64
	VadMeta    *VadMeta
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
	TranscribeStreamSSE(ctx context.Context, req *TranscribeRequest, out chan<- string) error
}
