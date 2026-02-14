package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"paraspeech/internal/config"
	"paraspeech/internal/observe"
	"paraspeech/internal/provider/openai"
	"paraspeech/internal/stt"
	"paraspeech/internal/tts"
	grpctransport "paraspeech/internal/transport/grpc"
	"paraspeech/internal/vad"
	"paraspeech/internal/vault"
	"paraspeech/internal/voice"
)

func newServeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Start the gRPC server",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServe()
		},
	}
}

func runServe() error {
	cfg, err := config.Load(configFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if err := config.Validate(cfg); err != nil {
		return fmt.Errorf("validate config: %w", err)
	}

	observe.SetupLogger(cfg.Log.Level, cfg.Log.Format)
	slog.Info("starting paraspeech", "grpc_addr", cfg.Server.GRPCAddr)

	// Vault
	v, err := vault.New(vault.Config{
		SecretsFile:      cfg.Vault.SecretsFile,
		EnforceIsolation: cfg.Vault.EnforceIsolation,
	})
	if err != nil {
		return fmt.Errorf("vault init: %w", err)
	}
	go v.WatchReload(syscall.SIGHUP)

	// VAD
	var detector vad.Detector
	if cfg.STT.VAD.Mode != "off" {
		detector, err = vad.NewTenVad(cfg.STT.VAD.HopSize, cfg.STT.VAD.Threshold)
		if err != nil {
			slog.Warn("TEN VAD init failed, running with vad=off", "error", err)
		}
	}
	merger := vad.NewSegmentMerger(cfg.STT.VAD)

	// Providers
	adapter := &voice.OpenAIAdapter{}
	sttProvider := openai.NewSTT(v, cfg.STT.Upstream)
	ttsProvider := openai.NewTTS(v, adapter, cfg.TTS.Upstream)

	// Prewarm
	if cfg.STT.Upstream.PrewarmOnStart {
		ctx := context.Background()
		if err := openai.NewSharedClient(v, cfg.STT.Upstream).Prewarm(ctx, cfg.STT.Upstream.Endpoint, cfg.STT.Upstream.PrewarmTimeout); err != nil {
			if !cfg.STT.Upstream.PrewarmFailOpen {
				return fmt.Errorf("STT prewarm: %w", err)
			}
		}
	}

	// Services
	sttSvc := stt.NewService(detector, merger, sttProvider, cfg.STT)
	ttsSvc := tts.NewService(ttsProvider, adapter, cfg.TTS)

	// gRPC server
	srv := grpctransport.NewServer(cfg.Server, sttSvc, ttsSvc, adapter)

	// Graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		if err := srv.Serve(); err != nil {
			slog.Error("gRPC server failed", "error", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	slog.Info("shutdown signal received")
	srv.GracefulStop(cfg.Server.ShutdownTimeout)

	if detector != nil {
		detector.Close()
	}
	v.Close()
	slog.Info("paraspeech stopped")
	return nil
}
