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

	// Until proto-generated client is available, use raw gRPC invocation.
	// For now, directly call the unary RPC method.
	var resp transcribeResp
	start := time.Now()
	err = conn.Invoke(ctx, "/paraspeech.v1.STTService/Transcribe", &transcribeReq{
		Audio:    audio,
		Filename: file,
		Language: language,
		Model:    model,
		VadDebug: vadDebug,
	}, &resp)
	if err != nil {
		return fmt.Errorf("transcribe failed: %w", err)
	}
	elapsed := time.Since(start)

	return printTranscribeResult(os.Stdout, &resp, vadDebug, elapsed, format)
}

// Temporary wire types until proto generation is set up.
type transcribeReq struct {
	Audio    []byte `protobuf:"bytes,1,opt,name=audio"`
	Filename string `protobuf:"bytes,2,opt,name=filename"`
	Language string `protobuf:"bytes,3,opt,name=language"`
	Model    string `protobuf:"bytes,4,opt,name=model"`
	VadDebug bool   `protobuf:"varint,5,opt,name=vad_debug"`
}

type transcribeResp struct {
	Text string `protobuf:"bytes,1,opt,name=text"`
}

// Minimal proto.Message stubs for grpc.Invoke
func (r *transcribeReq) ProtoReflect() {}
func (r *transcribeReq) Reset()        {}
func (r *transcribeReq) String() string { return "" }
func (r *transcribeResp) ProtoReflect() {}
func (r *transcribeResp) Reset()        {}
func (r *transcribeResp) String() string { return "" }

func printTranscribeResult(w io.Writer, resp *transcribeResp, vadDebug bool, elapsed time.Duration, format string) error {
	if format == "json" {
		_, err := fmt.Fprintf(w, "{\"text\":%q}\n", resp.Text)
		return err
	}
	// Default prototext
	_, err := fmt.Fprintf(w, "text: %q\n", resp.Text)
	return err
}
