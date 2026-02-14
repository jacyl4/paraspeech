package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"paraspeech/internal/config"
	"paraspeech/internal/tts"
	"paraspeech/internal/vault"
)

type TTS struct {
	client *SharedClient
	cfg    config.Upstream
}

func NewTTS(v vault.Vault, cfg config.Upstream) *TTS {
	return &TTS{client: NewSharedClient(v, cfg), cfg: cfg}
}

func (t *TTS) Prewarm(ctx context.Context, timeout time.Duration) error {
	return t.client.Prewarm(ctx, t.cfg.Endpoint, timeout)
}

func (t *TTS) Keepalive(ctx context.Context, interval time.Duration) {
	t.client.Keepalive(ctx, t.cfg.Endpoint, interval)
}

func (t *TTS) Synthesize(ctx context.Context, req *tts.SynthesizeRequest) (*tts.SynthesizeResult, error) {
	profile := req.VoiceProfile
	if profile == nil {
		return nil, fmt.Errorf("missing voice profile")
	}
	instructions := ""
	if profile.Emotion != "" && profile.Emotion != "neutral" {
		instructions = fmt.Sprintf("Speak in a %s tone with %s style.", profile.Emotion, profile.Style)
	}

	payload := map[string]any{"model": req.Model, "input": req.Text, "voice": profile.Voice, "speed": profile.Speed, "response_format": req.Format}
	if instructions != "" {
		payload["instructions"] = instructions
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, t.cfg.Endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")

	resp, err := t.client.do(request, vault.KeyTTS)
	if err != nil {
		return nil, fmt.Errorf("upstream request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("upstream %d: %s", resp.StatusCode, string(respBody))
	}

	audio, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}
	return &tts.SynthesizeResult{Audio: audio, ContentType: resp.Header.Get("Content-Type")}, nil
}
