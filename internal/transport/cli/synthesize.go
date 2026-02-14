package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"paraspeech/internal/config"
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

	conn, err := grpc.NewClient(cfg.Server.GRPCAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return fmt.Errorf("connect to serve: %w (is 'paraspeech serve' running?)", err)
	}
	defer conn.Close()

	if dryRun {
		var resp previewResp
		err = conn.Invoke(ctx, "/paraspeech.v1.TTSService/Preview", &previewReq{
			Text:   text,
			MaxSec: cfg.TTS.MaxSec,
		}, &resp)
		if err != nil {
			return fmt.Errorf("preview failed: %w", err)
		}
		return printPreviewResult(os.Stdout, &resp, format)
	}

	start := time.Now()
	var resp synthesizeResp
	err = conn.Invoke(ctx, "/paraspeech.v1.TTSService/Synthesize", &synthesizeReq{
		Text:    text,
		Voice:   voiceN,
		Speed:   speed,
		Emotion: emotion,
		Style:   style,
		Format:  audioFmt,
	}, &resp)
	if err != nil {
		return fmt.Errorf("synthesize failed: %w", err)
	}
	_ = time.Since(start)

	return printSynthesizeResult(os.Stdout, &resp, format)
}

// Temporary wire types
type synthesizeReq struct {
	Text    string  `protobuf:"bytes,1,opt,name=text"`
	Voice   string  `protobuf:"bytes,2,opt,name=voice"`
	Speed   float64 `protobuf:"fixed64,3,opt,name=speed"`
	Emotion string  `protobuf:"bytes,4,opt,name=emotion"`
	Style   string  `protobuf:"bytes,5,opt,name=style"`
	Format  string  `protobuf:"bytes,6,opt,name=format"`
}

type synthesizeResp struct {
	Count int32 `protobuf:"varint,1,opt,name=count"`
}

type previewReq struct {
	Text   string  `protobuf:"bytes,1,opt,name=text"`
	MaxSec float64 `protobuf:"fixed64,2,opt,name=max_sec"`
}

type previewResp struct {
	Count int32 `protobuf:"varint,1,opt,name=count"`
}

func (r *synthesizeReq) ProtoReflect()  {}
func (r *synthesizeReq) Reset()         {}
func (r *synthesizeReq) String() string { return "" }
func (r *synthesizeResp) ProtoReflect()  {}
func (r *synthesizeResp) Reset()         {}
func (r *synthesizeResp) String() string { return "" }
func (r *previewReq) ProtoReflect()      {}
func (r *previewReq) Reset()             {}
func (r *previewReq) String() string     { return "" }
func (r *previewResp) ProtoReflect()     {}
func (r *previewResp) Reset()            {}
func (r *previewResp) String() string    { return "" }

func printSynthesizeResult(w io.Writer, resp *synthesizeResp, format string) error {
	if format == "json" {
		_, err := fmt.Fprintf(w, "{\"count\":%d}\n", resp.Count)
		return err
	}
	_, err := fmt.Fprintf(w, "count: %d\n", resp.Count)
	return err
}

func printPreviewResult(w io.Writer, resp *previewResp, format string) error {
	if format == "json" {
		_, err := fmt.Fprintf(w, "{\"count\":%d}\n", resp.Count)
		return err
	}
	_, err := fmt.Fprintf(w, "count: %d\n", resp.Count)
	return err
}
