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

func newHealthCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "health",
		Short: "Check service health (delegates to serve via gRPC)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runHealth()
		},
	}
}

func runHealth() error {
	cfg, err := config.Load(configFile)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := dialServe(cfg)
	if err != nil {
		return err
	}
	defer conn.Close()

	resp, err := pb.NewHealthServiceClient(conn).Check(ctx, &pb.HealthRequest{})
	if err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}
	return printHealthResult(os.Stdout, resp, format)
}

func printHealthResult(w io.Writer, resp *pb.HealthResponse, format string) error {
	if format == "json" {
		_, err := fmt.Fprintf(w, "{\"ok\":%t,\"service\":%q,\"version\":%q}\n", resp.GetOk(), resp.GetService(), resp.GetVersion())
		return err
	}
	_, err := fmt.Fprintf(w, "ok: %t\nservice: %q\nversion: %q\n", resp.GetOk(), resp.GetService(), resp.GetVersion())
	return err
}
