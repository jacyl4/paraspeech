package grpc

import (
	"bytes"
	"context"
	"sync"
	"time"

	pb "paraspeech/api/proto/paraspeech/v1"
	gogrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"paraspeech/internal/observe"
	"paraspeech/internal/stt"
)

type sttHandler struct {
	pb.UnimplementedSTTServiceServer
	svc *stt.Service
	wg  *sync.WaitGroup
}

func registerSTTHandler(s *gogrpc.Server, svc *stt.Service, wg *sync.WaitGroup) {
	pb.RegisterSTTServiceServer(s, &sttHandler{svc: svc, wg: wg})
}

func (h *sttHandler) Transcribe(ctx context.Context, req *pb.TranscribeRequest) (*pb.TranscribeResponse, error) {
	h.wg.Add(1)
	defer h.wg.Done()

	traceID := observe.NewTraceID()
	start := time.Now()

	if len(req.GetAudio()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "empty audio")
	}

	result, err := h.svc.Transcribe(ctx, bytes.NewReader(req.GetAudio()), req.GetFilename())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "transcribe: %v", err)
	}
	result.DurationMS = time.Since(start).Milliseconds()

	resp := &pb.TranscribeResponse{
		Text: result.Text,
		Meta: &pb.TranscribeMeta{
			TraceId:   traceID,
			ProcessMs: result.DurationMS,
		},
	}
	if result.VadMeta != nil {
		resp.Meta.Vad = &pb.VadMeta{
			Enabled:      result.VadMeta.Enabled,
			Reason:       result.VadMeta.Reason,
			Fallback:     result.VadMeta.Fallback,
			AudioMsBefore: result.VadMeta.AudioMsBefore,
			AudioMsAfter:  result.VadMeta.AudioMsAfter,
			TrimRatio:    result.VadMeta.TrimRatio,
			SegmentsCount: result.VadMeta.SegmentsCount,
			ElapsedMs:    result.VadMeta.ElapsedMS,
		}
	}
	return resp, nil
}

func (h *sttHandler) TranscribeStream(gogrpc.BidiStreamingServer[pb.AudioFrame, pb.TranscribeEvent]) error {
	return status.Error(codes.Unimplemented, "TranscribeStream is not implemented")
}
