package grpc

import (
	"context"

	pb "paraspeech/api/proto/paraspeech/v1"
	gogrpc "google.golang.org/grpc"

	"paraspeech/internal/config"
	"paraspeech/internal/version"
)

type healthHandler struct {
	pb.UnimplementedHealthServiceServer
	cfg config.Config
}

func registerHealthHandler(s *gogrpc.Server, cfg config.Config) {
	pb.RegisterHealthServiceServer(s, &healthHandler{cfg: cfg})
}

func (h *healthHandler) Check(context.Context, *pb.HealthRequest) (*pb.HealthResponse, error) {
	return &pb.HealthResponse{
		Ok:      true,
		Service: "paraspeech",
		Version: version.Version,
		Stt: &pb.ChannelHealth{
			Enabled:    h.cfg.STT.Enabled,
			Model:      h.cfg.STT.DefaultModel,
			VadMode:    h.cfg.STT.VAD.Mode,
			VaultReady: true,
		},
		Tts: &pb.ChannelHealth{
			Enabled:    h.cfg.TTS.Enabled,
			Model:      h.cfg.TTS.DefaultModel,
			VaultReady: true,
		},
	}, nil
}
