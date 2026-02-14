package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"paraspeech/internal/config"
	"paraspeech/internal/tts"
	"paraspeech/internal/vault"
	"paraspeech/internal/voice"
)

type TTS struct {
	client  *SharedClient
	adapter voice.ProviderAdapter
	cfg     config.Upstream
}

func NewTTS(v vault.Vault, adapter voice.ProviderAdapter, cfg config.Upstream) *TTS {
	return &TTS{
		client:  NewSharedClient(v, cfg),
		adapter: adapter,
		cfg:     cfg,
	}
}

func (t *TTS) Synthesize(ctx context.Context, req *tts.SynthesizeRequest) (*tts.SynthesizeResult, error) {
	profile := req.VoiceProfile
	if profile == nil {
		profile = &voice.VoiceProfile{}
	}

	payload := map[string]any{
		"model": req.Model,
		"input": req.Text,
		"voice": t.adapter.MapVoice(profile),
		"speed": t.adapter.MapSpeed(profile),
	}
	if instructions := t.adapter.MapInstructions(profile); instructions != "" {
		payload["instructions"] = instructions
	}
	format := req.Format
	if format == "" {
		format = "opus"
	}
	payload["response_format"] = format

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, t.cfg.Endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := t.client.do(httpReq, vault.KeyTTS)
	if err != nil {
		return nil, fmt.Errorf("upstream request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("upstream %d: %s", resp.StatusCode, string(respBody))
	}

	var buf bytes.Buffer
	n, err := io.Copy(&buf, resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	return &tts.SynthesizeResult{
		Audio:       &buf,
		ContentType: resp.Header.Get("Content-Type"),
		SizeBytes:   n,
	}, nil
}

func (t *TTS) SynthesizeStream(ctx context.Context, req *tts.SynthesizeRequest, out chan<- []byte) error {
	result, err := t.Synthesize(ctx, req)
	if err != nil {
		return err
	}
	data, err := io.ReadAll(result.Audio)
	if err != nil {
		return err
	}
	out <- data
	return nil
}
