package grpc

import (
	"context"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"paraspeech/internal/observe"
	"paraspeech/internal/tts"
	"paraspeech/internal/voice"
)

type ttsHandler struct {
	svc     *tts.Service
	adapter voice.ProviderAdapter
	wg      *sync.WaitGroup
}

func registerTTSHandler(s *grpc.Server, svc *tts.Service, adapter voice.ProviderAdapter, wg *sync.WaitGroup) {
	_ = &ttsHandler{svc: svc, adapter: adapter, wg: wg}
}

func (h *ttsHandler) Synthesize(ctx context.Context, text string, profile *voice.VoiceProfile, model, format string) (*tts.SynthesizeResult, string, error) {
	h.wg.Add(1)
	defer h.wg.Done()

	traceID := observe.NewTraceID()
	_ = time.Now()

	if text == "" {
		return nil, traceID, status.Error(codes.InvalidArgument, "empty text")
	}

	result, err := h.svc.Synthesize(ctx, text, profile)
	if err != nil {
		return nil, traceID, status.Errorf(codes.Internal, "synthesize: %v", err)
	}
	return result, traceID, nil
}
