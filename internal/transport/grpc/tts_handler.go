package grpc

import (
	"context"
	"io"
	"sync"
	"time"

	pb "paraspeech/api/proto/paraspeech/v1"
	gogrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"paraspeech/internal/observe"
	"paraspeech/internal/tts"
	"paraspeech/internal/voice"
)

type ttsHandler struct {
	pb.UnimplementedTTSServiceServer
	svc     *tts.Service
	adapter voice.ProviderAdapter
	wg      *sync.WaitGroup
}

func registerTTSHandler(s *gogrpc.Server, svc *tts.Service, adapter voice.ProviderAdapter, wg *sync.WaitGroup) {
	pb.RegisterTTSServiceServer(s, &ttsHandler{svc: svc, adapter: adapter, wg: wg})
}

func (h *ttsHandler) Synthesize(ctx context.Context, req *pb.SynthesizeRequest) (*pb.SynthesizeResponse, error) {
	h.wg.Add(1)
	defer h.wg.Done()

	traceID := observe.NewTraceID()
	start := time.Now()

	if req.GetText() == "" {
		return nil, status.Error(codes.InvalidArgument, "empty text")
	}

	profile := &voice.VoiceProfile{
		Voice:   req.GetVoiceProfile().GetVoice(),
		Speed:   req.GetVoiceProfile().GetSpeed(),
		Emotion: req.GetVoiceProfile().GetEmotion(),
		Pitch:   req.GetVoiceProfile().GetPitch(),
		Style:   req.GetVoiceProfile().GetStyle(),
		Custom:  req.GetVoiceProfile().GetCustom(),
	}
	result, err := h.svc.Synthesize(ctx, req.GetText(), profile)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "synthesize: %v", err)
	}

	audioBytes, err := io.ReadAll(result.Audio)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "read synthesized audio: %v", err)
	}

	resp := &pb.SynthesizeResponse{
		Count: 1,
		Segments: []*pb.SynthesizeSegment{
			{
				Index:       0,
				Text:        req.GetText(),
				Audio:       audioBytes,
				SizeBytes:   int64(len(audioBytes)),
				ContentType: result.ContentType,
			},
		},
		Meta: &pb.SynthesizeMeta{
			TraceId:   traceID,
			Model:     req.GetModel(),
			MaxSec:    req.GetMaxSec(),
			ProcessMs: time.Since(start).Milliseconds(),
		},
	}
	return resp, nil
}

func (h *ttsHandler) Preview(_ context.Context, req *pb.PreviewRequest) (*pb.PreviewResponse, error) {
	if req.GetText() == "" {
		return nil, status.Error(codes.InvalidArgument, "empty text")
	}

	cleaned := tts.Sanitize(req.GetText())
	if cleaned == "" {
		return &pb.PreviewResponse{Count: 0}, nil
	}

	maxSec := req.GetMaxSec()
	if maxSec <= 0 {
		maxSec = 25.0
	}
	segments := tts.Split(cleaned, maxSec)

	resp := &pb.PreviewResponse{
		Count: int32(len(segments)),
	}
	for i, seg := range segments {
		resp.Segments = append(resp.Segments, &pb.PreviewSegment{
			Index:        int32(i),
			Text:         seg.Text,
			EstimatedSec: seg.EstimatedSec,
		})
	}
	return resp, nil
}

func (h *ttsHandler) SynthesizeStream(*pb.SynthesizeRequest, gogrpc.ServerStreamingServer[pb.SynthesizeEvent]) error {
	return status.Error(codes.Unimplemented, "SynthesizeStream is not implemented")
}
