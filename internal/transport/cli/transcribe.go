package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	pb "paraspeech/api/proto/paraspeech/v1"
	"paraspeech/internal/config"

	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func newTranscribeCmd() *cobra.Command {
	var (
		language string
		model    string
		vadDebug bool
	)

	cmd := &cobra.Command{
		Use:   "transcribe <audio-file>",
		Short: "Transcribe audio to text (delegates to serve via gRPC)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTranscribe(args[0], language, model, vadDebug)
		},
	}

	cmd.Flags().StringVar(&language, "language", "", "language hint (e.g., zh, en)")
	cmd.Flags().StringVar(&model, "model", "", "STT model override")
	cmd.Flags().BoolVar(&vadDebug, "vad-debug", false, "include VAD metadata in output")
	return cmd
}

func runTranscribe(file, language, model string, vadDebug bool) error {
	cfg, err := config.Load(configFile)
	if err != nil {
		return err
	}

	audio, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.STT.Timeout)
	defer cancel()

	conn, err := grpc.NewClient(cfg.Server.GRPCAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return fmt.Errorf("connect to serve: %w (is 'paraspeech serve' running?)", err)
	}
	defer conn.Close()

	start := time.Now()
	resp, err := pb.NewSTTServiceClient(conn).Transcribe(ctx, &pb.TranscribeRequest{
		Audio:    audio,
		Filename: file,
		Language: language,
		Model:    model,
		VadDebug: vadDebug,
	})
	if err != nil {
		return fmt.Errorf("transcribe failed: %w", err)
	}
	elapsed := time.Since(start)

	return printTranscribeResult(os.Stdout, resp, vadDebug, elapsed, format)
}

func printTranscribeResult(w io.Writer, resp *pb.TranscribeResponse, vadDebug bool, elapsed time.Duration, format string) error {
	if format == "json" {
		_, err := fmt.Fprintf(w, "{\"text\":%q}\n", resp.GetText())
		return err
	}
	_, err := fmt.Fprintf(w, "text: %q\n", resp.GetText())
	return err
}
