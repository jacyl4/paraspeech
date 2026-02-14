package cli

import (
	"context"
	"fmt"
	"io"
	"os"

	pb "paraspeech/api/proto/paraspeech/v1"
	"paraspeech/internal/config"

	"github.com/spf13/cobra"
)

func newSynthesizeCmd() *cobra.Command {
	var (
		text    string
		voiceN  string
		speed   float64
		emotion string
		style   string
		fmtStr  string
		dryRun  bool
	)

	cmd := &cobra.Command{
		Use:   "synthesize",
		Short: "Synthesize text to audio (delegates to serve via gRPC)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSynthesize(text, voiceN, emotion, style, fmtStr, speed, dryRun)
		},
	}
	cmd.Flags().StringVar(&text, "text", "", "text to synthesize")
	cmd.Flags().StringVar(&voiceN, "voice", "", "voice name")
	cmd.Flags().Float64Var(&speed, "speed", 0, "speed multiplier")
	cmd.Flags().StringVar(&emotion, "emotion", "", "emotion tag")
	cmd.Flags().StringVar(&style, "style", "", "style tag")
	cmd.Flags().StringVar(&fmtStr, "audio-format", "opus", "audio format: opus, mp3, pcm")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview segmentation only")
	_ = cmd.MarkFlagRequired("text")
	return cmd
}

func runSynthesize(text, voiceN, emotion, style, audioFmt string, speed float64, dryRun bool) error {
	cfg, err := config.Load(configFile)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), cfg.TTS.Timeout)
	defer cancel()

	conn, err := dialServe(cfg)
	if err != nil {
		return err
	}
	defer conn.Close()

	client := pb.NewTTSServiceClient(conn)
	if dryRun {
		resp, err := client.Preview(ctx, &pb.PreviewRequest{Text: text, MaxSec: cfg.TTS.MaxSec})
		if err != nil {
			return fmt.Errorf("preview failed: %w", err)
		}
		return printCountResult(os.Stdout, resp.GetCount(), format)
	}

	resp, err := client.Synthesize(ctx, &pb.SynthesizeRequest{
		Text: text,
		VoiceProfile: &pb.VoiceProfile{
			Voice:   voiceN,
			Speed:   speed,
			Emotion: emotion,
			Style:   style,
		},
		Format: audioFmt,
	})
	if err != nil {
		return fmt.Errorf("synthesize failed: %w", err)
	}
	return printCountResult(os.Stdout, resp.GetCount(), format)
}

func printCountResult(w io.Writer, count int32, format string) error {
	if format == "json" {
		_, err := fmt.Fprintf(w, "{\"count\":%d}\n", count)
		return err
	}
	_, err := fmt.Fprintf(w, "count: %d\n", count)
	return err
}
