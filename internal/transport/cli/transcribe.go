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
)

func newTranscribeCmd() *cobra.Command {
	var (
		language  string
		model     string
		vadDebug  bool
		useStream bool
	)

	cmd := &cobra.Command{
		Use:   "transcribe <audio-file>",
		Short: "Transcribe audio to text (delegates to serve via gRPC)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if useStream {
				return runTranscribeStream(args[0], language, model, vadDebug)
			}
			return runTranscribe(args[0], language, model, vadDebug)
		},
	}
	cmd.Flags().StringVar(&language, "language", "", "language hint (e.g., zh, en)")
	cmd.Flags().StringVar(&model, "model", "", "STT model override")
	cmd.Flags().BoolVar(&vadDebug, "vad-debug", false, "include VAD metadata in output")
	cmd.Flags().BoolVar(&useStream, "stream", false, "use streaming transcription")
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

	conn, err := dialServe(cfg)
	if err != nil {
		return err
	}
	defer conn.Close()

	start := time.Now()
	resp, err := pb.NewSTTServiceClient(conn).Transcribe(ctx, &pb.TranscribeRequest{Audio: audio, Filename: file, Language: language, Model: model, VadDebug: vadDebug})
	if err != nil {
		return fmt.Errorf("transcribe failed: %w", err)
	}
	elapsed := time.Since(start)
	return printTranscribeResult(os.Stdout, resp, vadDebug, elapsed, format)
}

func runTranscribeStream(file, language, model string, vadDebug bool) error {
	cfg, err := config.Load(configFile)
	if err != nil {
		return err
	}
	audio, err := os.Open(file)
	if err != nil {
		return fmt.Errorf("open file: %w", err)
	}
	defer audio.Close()

	stat, _ := audio.Stat()
	durationHint := 0.0
	if stat != nil && stat.Size() >= cfg.STT.DirectMaxBytes {
		durationHint = cfg.STT.VAD.MaxAudioSec
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.STT.Timeout)
	defer cancel()
	conn, err := dialServe(cfg)
	if err != nil {
		return err
	}
	defer conn.Close()

	stream, err := pb.NewSTTServiceClient(conn).TranscribeStream(ctx)
	if err != nil {
		return fmt.Errorf("open stream: %w", err)
	}

	sendErrCh := make(chan error, 1)
	go func() {
		buf := make([]byte, 32*1024)
		first := true
		for {
			n, readErr := audio.Read(buf)
			if n > 0 {
				frame := &pb.AudioFrame{Data: append([]byte(nil), buf[:n]...)}
				if first {
					frame.DurationHint = durationHint
					frame.Language = language
					frame.Model = model
					frame.VadDebug = vadDebug
					first = false
				}
				if err := stream.Send(frame); err != nil {
					sendErrCh <- fmt.Errorf("send frame: %w", err)
					return
				}
			}
			if readErr == io.EOF {
				if err := stream.Send(&pb.AudioFrame{EndOfAudio: true}); err != nil {
					sendErrCh <- fmt.Errorf("send end frame: %w", err)
					return
				}
				if err := stream.CloseSend(); err != nil {
					sendErrCh <- fmt.Errorf("close send: %w", err)
					return
				}
				sendErrCh <- nil
				return
			}
			if readErr != nil {
				sendErrCh <- fmt.Errorf("read file: %w", readErr)
				return
			}
		}
	}()

	for {
		event, err := stream.Recv()
		if err == io.EOF {
			return <-sendErrCh
		}
		if err != nil {
			return fmt.Errorf("recv stream event: %w", err)
		}
		switch e := event.GetEvent().(type) {
		case *pb.TranscribeEvent_Partial:
			fmt.Print(e.Partial.GetText())
		case *pb.TranscribeEvent_Final:
			fmt.Println()
		}
	}
}

func printTranscribeResult(w io.Writer, resp *pb.TranscribeResponse, vadDebug bool, elapsed time.Duration, format string) error {
	if format == "json" {
		_, err := fmt.Fprintf(w, "{\"text\":%q}\n", resp.GetText())
		return err
	}
	_, err := fmt.Fprintf(w, "text: %q\n", resp.GetText())
	return err
}
