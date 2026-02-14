package openai

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"paraspeech/internal/config"
	"paraspeech/internal/vault"
)

type SharedClient struct {
	client *http.Client
	vault  vault.Vault
}

func NewSharedClient(v vault.Vault, cfg config.Upstream) *SharedClient {
	transport := &http.Transport{
		MaxIdleConns:        cfg.MaxConnections,
		MaxIdleConnsPerHost: cfg.MaxKeepalive,
		IdleConnTimeout:     90 * time.Second,
		ForceAttemptHTTP2:   true,
	}
	return &SharedClient{
		client: &http.Client{
			Transport: transport,
			Timeout:   cfg.ReadTimeout,
		},
		vault: v,
	}
}

func (c *SharedClient) Prewarm(ctx context.Context, endpoint string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, endpoint, nil)
	if err != nil {
		return err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		slog.Warn("prewarm failed", "endpoint", endpoint, "error", err)
		return err
	}
	resp.Body.Close()
	slog.Debug("prewarm ok", "endpoint", endpoint)
	return nil
}

func (c *SharedClient) do(req *http.Request, keyPurpose vault.KeyPurpose) (*http.Response, error) {
	apiKey, err := c.vault.GetKey(keyPurpose)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	return c.client.Do(req)
}
