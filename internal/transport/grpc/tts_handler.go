package grpc

import (
	"context"
	"math"
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
	svc           *tts.Service
	maxBody       int64
	defaultMaxSec float64
}

func registerTTSHandler(s *gogrpc.Server, svc *tts.Service, maxBody int64, defaultMaxSec float64) {
	pb.RegisterTTSServiceServer(s, &ttsHandler{svc: svc, maxBody: maxBody, defaultMaxSec: defaultMaxSec})
}

func profileFromPB(p *pb.VoiceProfile) *voice.VoiceProfile {
	if p == nil {
		return &voice.VoiceProfile{}
	}
	return &voice.VoiceProfile{Voice: p.GetVoice(), Speed: p.GetSpeed(), Emotion: p.GetEmotion(), Style: p.GetStyle()}
}

func (h *ttsHandler) Synthesize(ctx context.Context, req *pb.SynthesizeRequest) (*pb.SynthesizeResponse, error) {
	traceID := observe.NewTraceID()
	start := time.Now()
	if req.GetText() == "" {
		return nil, status.Error(codes.InvalidArgument, "empty text")
	}
	if h.maxBody > 0 && int64(len(req.GetText())) > h.maxBody {
		return nil, status.Errorf(codes.InvalidArgument, "text exceeds tts.max_body (%d)", h.maxBody)
	}

	profile := profileFromPB(req.GetVoiceProfile())
	maxSec := req.GetMaxSec()
	if maxSec <= 0 {
		// Unary API should favor a single directly playable audio output.
		maxSec = math.MaxFloat64
	}
	segments, err := h.svc.SynthesizeSegments(ctx, req.GetText(), profile, &tts.SynthesizeOptions{Model: req.GetModel(), Format: req.GetFormat(), MaxSec: maxSec})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "synthesize: %v", err)
	}

	pbSegments := make([]*pb.SynthesizeSegment, 0, len(segments))
	for _, seg := range segments {
		pbSegments = append(pbSegments, &pb.SynthesizeSegment{Index: int32(seg.Index), Text: seg.Text, EstimatedSec: seg.EstimatedSec, Audio: seg.Audio, SizeBytes: seg.SizeBytes, ContentType: seg.ContentType})
	}

	resp := &pb.SynthesizeResponse{
		Count:    int32(len(pbSegments)),
		Segments: pbSegments,
		Meta:     &pb.SynthesizeMeta{TraceId: traceID, VoiceProfile: &pb.VoiceProfile{Voice: profile.Voice, Speed: profile.Speed, Emotion: profile.Emotion, Style: profile.Style}, Model: req.GetModel(), MaxSec: req.GetMaxSec(), ProcessMs: time.Since(start).Milliseconds()},
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
		maxSec = h.defaultMaxSec
	}
	segments := tts.Split(cleaned, maxSec)
	resp := &pb.PreviewResponse{Count: int32(len(segments))}
	for i, seg := range segments {
		resp.Segments = append(resp.Segments, &pb.PreviewSegment{Index: int32(i), Text: seg.Text, EstimatedSec: seg.EstimatedSec})
	}
	return resp, nil
}

func (h *ttsHandler) SynthesizeStream(req *pb.SynthesizeRequest, stream gogrpc.ServerStreamingServer[pb.SynthesizeEvent]) error {
	if req.GetText() == "" {
		return status.Error(codes.InvalidArgument, "empty text")
	}
	if h.maxBody > 0 && int64(len(req.GetText())) > h.maxBody {
		return status.Errorf(codes.InvalidArgument, "text exceeds tts.max_body (%d)", h.maxBody)
	}

	traceID := observe.NewTraceID()
	start := time.Now()
	profile := profileFromPB(req.GetVoiceProfile())
	const chunkSize = 32 * 1024
	_, err := h.svc.SynthesizeStreamSegments(stream.Context(), req.GetText(), profile, &tts.SynthesizeOptions{Model: req.GetModel(), Format: req.GetFormat(), MaxSec: req.GetMaxSec()}, func(seg tts.SegmentResult) error {
		for off := 0; off < len(seg.Audio); off += chunkSize {
			end := off + chunkSize
			if end > len(seg.Audio) {
				end = len(seg.Audio)
			}
			if err := stream.Send(&pb.SynthesizeEvent{Event: &pb.SynthesizeEvent_Chunk{Chunk: &pb.AudioChunk{SegmentIndex: int32(seg.Index), Data: seg.Audio[off:end], ContentType: seg.ContentType}}}); err != nil {
				return err
			}
		}
		if err := stream.Send(&pb.SynthesizeEvent{Event: &pb.SynthesizeEvent_SegmentDone{SegmentDone: &pb.SegmentDone{Index: int32(seg.Index), Text: seg.Text, EstimatedSec: seg.EstimatedSec, SizeBytes: seg.SizeBytes}}}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return status.Errorf(codes.Internal, "synthesize stream: %v", err)
	}

	if err := stream.Send(&pb.SynthesizeEvent{Event: &pb.SynthesizeEvent_FinalMeta{FinalMeta: &pb.SynthesizeMeta{TraceId: traceID, VoiceProfile: &pb.VoiceProfile{Voice: profile.Voice, Speed: profile.Speed, Emotion: profile.Emotion, Style: profile.Style}, Model: req.GetModel(), MaxSec: req.GetMaxSec(), ProcessMs: time.Since(start).Milliseconds()}}}); err != nil {
		return status.Errorf(codes.Internal, "send final meta: %v", err)
	}
	return nil
}
