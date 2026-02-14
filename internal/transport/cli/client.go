package cli

import (
	"fmt"

	"paraspeech/internal/config"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func dialServe(cfg *config.Config) (*grpc.ClientConn, error) {
	conn, err := grpc.NewClient(
		cfg.Server.GRPCAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(4*1024*1024),
			grpc.MaxCallSendMsgSize(26*1024*1024),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("connect to serve: %w (is 'paraspeech serve' running?)", err)
	}
	return conn, nil
}
