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
	grpctransport "paraspeech/internal/transport/grpc"
	"paraspeech/internal/tts"
	"paraspeech/internal/vad"
	"paraspeech/internal/vault"
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

	v, err := vault.New(vault.Config{SecretsFile: cfg.Vault.SecretsFile})
	if err != nil {
		return fmt.Errorf("vault init: %w", err)
	}
	go v.WatchReload(syscall.SIGHUP)

	var detector vad.Detector
	var sttSvc *stt.Service
	var sttProvider *openai.STT
	if cfg.STT.Enabled {
		if cfg.STT.VAD.Mode != "off" {
			detector, err = vad.NewTenVad(cfg.STT.VAD.HopSize, cfg.STT.VAD.Threshold)
			if err != nil {
				slog.Warn("TEN VAD init failed, running with vad=off", "error", err)
			}
		}
		merger := vad.NewSegmentMerger(cfg.STT.VAD)
		sttProvider = openai.NewSTT(v, cfg.STT.Upstream)
		if cfg.STT.Upstream.PrewarmOnStart {
			ctx := context.Background()
			if err := sttProvider.Prewarm(ctx, cfg.STT.Upstream.PrewarmTimeout); err != nil && !cfg.STT.Upstream.PrewarmFailOpen {
				return fmt.Errorf("STT prewarm: %w", err)
			}
		}
		sttSvc = stt.NewService(detector, merger, sttProvider, cfg.STT)
	} else {
		slog.Info("STT channel disabled by config")
	}

	var ttsSvc *tts.Service
	var ttsProvider *openai.TTS
	if cfg.TTS.Enabled {
		ttsProvider = openai.NewTTS(v, cfg.TTS.Upstream)
		if cfg.TTS.Upstream.PrewarmOnStart {
			ctx := context.Background()
			if err := ttsProvider.Prewarm(ctx, cfg.TTS.Upstream.PrewarmTimeout); err != nil && !cfg.TTS.Upstream.PrewarmFailOpen {
				return fmt.Errorf("TTS prewarm: %w", err)
			}
		}
		ttsSvc = tts.NewService(ttsProvider, cfg.TTS)
	} else {
		slog.Info("TTS channel disabled by config")
	}

	srv := grpctransport.NewServer(*cfg, sttSvc, ttsSvc, v)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if sttProvider != nil && cfg.STT.Upstream.KeepaliveInterval > 0 {
		go sttProvider.Keepalive(ctx, cfg.STT.Upstream.KeepaliveInterval)
	}
	if ttsProvider != nil && cfg.TTS.Upstream.KeepaliveInterval > 0 {
		go ttsProvider.Keepalive(ctx, cfg.TTS.Upstream.KeepaliveInterval)
	}

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
		_ = detector.Close()
	}
	v.Close()
	slog.Info("paraspeech stopped")
	return nil
}
