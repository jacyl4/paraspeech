package grpc

import (
	"context"

	gogrpc "google.golang.org/grpc"
	pb "paraspeech/api/proto/paraspeech/v1"

	"paraspeech/internal/config"
	"paraspeech/internal/vault"
	"paraspeech/internal/version"
)

type healthHandler struct {
	pb.UnimplementedHealthServiceServer
	cfg   config.Config
	vault vault.Vault
}

func registerHealthHandler(s *gogrpc.Server, cfg config.Config, v vault.Vault) {
	pb.RegisterHealthServiceServer(s, &healthHandler{cfg: cfg, vault: v})
}

func (h *healthHandler) Check(context.Context, *pb.HealthRequest) (*pb.HealthResponse, error) {
	vaultReady := h.vault != nil && h.vault.Healthy() == nil
	return &pb.HealthResponse{
		Ok:      true,
		Service: "paraspeech",
		Version: version.Version,
		Stt:     &pb.ChannelHealth{Enabled: h.cfg.STT.Enabled, Model: h.cfg.STT.DefaultModel, VadMode: h.cfg.STT.VAD.Mode, VaultReady: vaultReady},
		Tts:     &pb.ChannelHealth{Enabled: h.cfg.TTS.Enabled, Model: h.cfg.TTS.DefaultModel, VaultReady: vaultReady},
	}, nil
}
