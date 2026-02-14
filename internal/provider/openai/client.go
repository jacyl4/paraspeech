package openai

import (
	"context"
	"log/slog"
	"net"
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
	headerTimeout := cfg.ReadTimeout
	if headerTimeout <= 0 {
		headerTimeout = 30 * time.Second
	}
	transport := &http.Transport{
		MaxIdleConns:          cfg.MaxConnections,
		MaxIdleConnsPerHost:   cfg.MaxKeepalive,
		IdleConnTimeout:       90 * time.Second,
		ForceAttemptHTTP2:     true,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: headerTimeout,
		ExpectContinueTimeout: 0,
		DisableCompression:    true,
		WriteBufferSize:       64 * 1024,
		ReadBufferSize:        16 * 1024,
		DialContext:           (&net.Dialer{Timeout: cfg.ConnectTimeout, KeepAlive: 30 * time.Second}).DialContext,
	}
	return &SharedClient{client: &http.Client{Transport: transport}, vault: v}
}

func (c *SharedClient) Prewarm(ctx context.Context, endpoint string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodOptions, endpoint, nil)
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

func (c *SharedClient) Keepalive(ctx context.Context, endpoint string, interval time.Duration) {
	if interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = c.Prewarm(ctx, endpoint, 3*time.Second)
		}
	}
}

func (c *SharedClient) do(req *http.Request, keyPurpose vault.KeyPurpose) (*http.Response, error) {
	apiKey, err := c.vault.GetKey(keyPurpose)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	return c.client.Do(req)
}
