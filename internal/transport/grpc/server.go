package grpc

import (
	"log/slog"
	"net"
	"sync"
	"time"

	"google.golang.org/grpc"

	"paraspeech/internal/config"
	"paraspeech/internal/stt"
	"paraspeech/internal/tts"
	"paraspeech/internal/voice"
)

type Server struct {
	cfg     config.Server
	appCfg  config.Config
	grpc    *grpc.Server
	sttSvc  *stt.Service
	ttsSvc  *tts.Service
	adapter voice.ProviderAdapter
	wg      sync.WaitGroup
}

func NewServer(cfg config.Config, sttSvc *stt.Service, ttsSvc *tts.Service, adapter voice.ProviderAdapter) *Server {
	s := &Server{
		cfg:     cfg.Server,
		appCfg:  cfg,
		sttSvc:  sttSvc,
		ttsSvc:  ttsSvc,
		adapter: adapter,
	}
	s.grpc = grpc.NewServer()
	registerSTTHandler(s.grpc, sttSvc, &s.wg)
	registerTTSHandler(s.grpc, ttsSvc, adapter, &s.wg)
	registerHealthHandler(s.grpc, cfg)
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
