package grpc

import (
	"log/slog"
	"net"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"

	"paraspeech/internal/config"
	"paraspeech/internal/stt"
	"paraspeech/internal/tts"
	"paraspeech/internal/vault"
)

type Server struct {
	cfg  config.Server
	grpc *grpc.Server
}

func NewServer(cfg config.Config, sttSvc *stt.Service, ttsSvc *tts.Service, v vault.Vault) *Server {
	s := &Server{cfg: cfg.Server}
	s.grpc = grpc.NewServer(
		grpc.MaxRecvMsgSize(26*1024*1024),
		grpc.MaxSendMsgSize(4*1024*1024),
		grpc.KeepaliveParams(keepalive.ServerParameters{MaxConnectionIdle: 5 * time.Minute, Time: 30 * time.Second, Timeout: 10 * time.Second}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{MinTime: 10 * time.Second, PermitWithoutStream: true}),
	)
	if cfg.STT.Enabled && sttSvc != nil {
		registerSTTHandler(s.grpc, sttSvc, cfg.STT.MaxBytes)
	} else {
		slog.Info("STT gRPC service not registered", "enabled", cfg.STT.Enabled)
	}
	if cfg.TTS.Enabled && ttsSvc != nil {
		registerTTSHandler(s.grpc, ttsSvc, cfg.TTS.MaxBody, cfg.TTS.MaxSec)
	} else {
		slog.Info("TTS gRPC service not registered", "enabled", cfg.TTS.Enabled)
	}
	registerHealthHandler(s.grpc, cfg, v)
	return s
}

func (s *Server) Serve() error {
	lis, err := net.Listen("tcp", s.cfg.GRPCAddr)
	if err != nil {
		return err
	}
	slog.Info("gRPC server listening", "addr", s.cfg.GRPCAddr)
	return s.grpc.Serve(lis)
}

func (s *Server) GracefulStop(timeout time.Duration) {
	slog.Info("shutting down, draining requests...", "timeout", timeout)
	done := make(chan struct{})
	go func() {
		s.grpc.GracefulStop()
		close(done)
	}()
	select {
	case <-done:
		slog.Info("all requests drained")
	case <-time.After(timeout):
		slog.Warn("shutdown timeout, forcing stop")
		s.grpc.Stop()
	}
}
