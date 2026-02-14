package grpc

import (
	"context"
	"sync"
	"time"

	gogrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	pb "paraspeech/api/proto/paraspeech/v1"

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
	segments, err := h.svc.SynthesizeSegments(ctx, req.GetText(), profile, &tts.SynthesizeOptions{
		Model:  req.GetModel(),
		Format: req.GetFormat(),
		MaxSec: req.GetMaxSec(),
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "synthesize: %v", err)
	}

	pbSegments := make([]*pb.SynthesizeSegment, 0, len(segments))
	for _, seg := range segments {
		pbSegments = append(pbSegments, &pb.SynthesizeSegment{
			Index:        int32(seg.Index),
			Text:         seg.Text,
			EstimatedSec: seg.EstimatedSec,
			Audio:        seg.Audio,
			SizeBytes:    seg.SizeBytes,
			ContentType:  seg.ContentType,
		})
	}

	resp := &pb.SynthesizeResponse{
		Count:    int32(len(pbSegments)),
		Segments: pbSegments,
		Meta: &pb.SynthesizeMeta{
			TraceId: traceID,
			VoiceProfile: &pb.VoiceProfile{
				Voice:   profile.Voice,
				Speed:   profile.Speed,
				Emotion: profile.Emotion,
				Pitch:   profile.Pitch,
				Style:   profile.Style,
				Custom:  profile.Custom,
			},
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

func (h *ttsHandler) SynthesizeStream(req *pb.SynthesizeRequest, stream gogrpc.ServerStreamingServer[pb.SynthesizeEvent]) error {
	h.wg.Add(1)
	defer h.wg.Done()

	if req.GetText() == "" {
		return status.Error(codes.InvalidArgument, "empty text")
	}

	traceID := observe.NewTraceID()
	start := time.Now()
	profile := &voice.VoiceProfile{
		Voice:   req.GetVoiceProfile().GetVoice(),
		Speed:   req.GetVoiceProfile().GetSpeed(),
		Emotion: req.GetVoiceProfile().GetEmotion(),
		Pitch:   req.GetVoiceProfile().GetPitch(),
		Style:   req.GetVoiceProfile().GetStyle(),
		Custom:  req.GetVoiceProfile().GetCustom(),
	}

	segments, err := h.svc.SynthesizeSegments(stream.Context(), req.GetText(), profile, &tts.SynthesizeOptions{
		Model:  req.GetModel(),
		Format: req.GetFormat(),
		MaxSec: req.GetMaxSec(),
	})
	if err != nil {
		return status.Errorf(codes.Internal, "synthesize stream: %v", err)
	}

	const chunkSize = 32 * 1024
	for _, seg := range segments {
		for off := 0; off < len(seg.Audio); off += chunkSize {
			end := off + chunkSize
			if end > len(seg.Audio) {
				end = len(seg.Audio)
			}
			if err := stream.Send(&pb.SynthesizeEvent{
				Event: &pb.SynthesizeEvent_Chunk{
					Chunk: &pb.AudioChunk{
						SegmentIndex: int32(seg.Index),
						Data:         seg.Audio[off:end],
						ContentType:  seg.ContentType,
					},
				},
			}); err != nil {
				return status.Errorf(codes.Internal, "send audio chunk: %v", err)
			}
		}

		if err := stream.Send(&pb.SynthesizeEvent{
			Event: &pb.SynthesizeEvent_SegmentDone{
				SegmentDone: &pb.SegmentDone{
					Index:        int32(seg.Index),
					Text:         seg.Text,
					EstimatedSec: seg.EstimatedSec,
					SizeBytes:    seg.SizeBytes,
					Path:         "",
				},
			},
		}); err != nil {
			return status.Errorf(codes.Internal, "send segment done: %v", err)
		}
	}

	if err := stream.Send(&pb.SynthesizeEvent{
		Event: &pb.SynthesizeEvent_FinalMeta{
			FinalMeta: &pb.SynthesizeMeta{
				TraceId: traceID,
				VoiceProfile: &pb.VoiceProfile{
					Voice:   profile.Voice,
					Speed:   profile.Speed,
					Emotion: profile.Emotion,
					Pitch:   profile.Pitch,
					Style:   profile.Style,
					Custom:  profile.Custom,
				},
				Model:     req.GetModel(),
				MaxSec:    req.GetMaxSec(),
				ProcessMs: time.Since(start).Milliseconds(),
			},
		},
	}); err != nil {
		return status.Errorf(codes.Internal, "send final meta: %v", err)
	}

	return nil
}
