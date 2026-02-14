package grpc

import (
	"bytes"
	"context"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"paraspeech/internal/observe"
	"paraspeech/internal/stt"
)

type sttHandler struct {
	svc *stt.Service
	wg  *sync.WaitGroup
}

func registerSTTHandler(s *grpc.Server, svc *stt.Service, wg *sync.WaitGroup) {
	// Proto-generated registration will go here once buf generate runs.
	// For now we store the handler for manual wiring.
	_ = &sttHandler{svc: svc, wg: wg}
}

// Transcribe handles unary STT requests.
func (h *sttHandler) Transcribe(ctx context.Context, audio []byte, filename, language, model string) (*stt.TranscribeResult, string, error) {
	h.wg.Add(1)
	defer h.wg.Done()

	traceID := observe.NewTraceID()
	start := time.Now()

	if len(audio) == 0 {
		return nil, traceID, status.Error(codes.InvalidArgument, "empty audio")
	}

	result, err := h.svc.Transcribe(ctx, bytes.NewReader(audio), filename)
	if err != nil {
		return nil, traceID, status.Errorf(codes.Internal, "transcribe: %v", err)
	}
	result.DurationMS = time.Since(start).Milliseconds()
	return result, traceID, nil
}
