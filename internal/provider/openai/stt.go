package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"

	"paraspeech/internal/config"
	"paraspeech/internal/stt"
	"paraspeech/internal/vault"
)

type STT struct {
	client *SharedClient
	cfg    config.Upstream
}

func NewSTT(v vault.Vault, cfg config.Upstream) *STT {
	return &STT{
		client: NewSharedClient(v, cfg),
		cfg:    cfg,
	}
}

func (s *STT) Transcribe(ctx context.Context, req *stt.TranscribeRequest) (*stt.TranscribeResult, error) {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)

	filename := req.Filename
	if filename == "" {
		filename = "audio.wav"
	}

	part, err := w.CreateFormFile("file", filename)
	if err != nil {
		return nil, fmt.Errorf("create form file: %w", err)
	}
	if _, err := io.Copy(part, req.Audio); err != nil {
		return nil, fmt.Errorf("copy audio: %w", err)
	}

	_ = w.WriteField("model", req.Model)
	if req.Language != "" {
		_ = w.WriteField("language", req.Language)
	}
	if req.Prompt != "" {
		_ = w.WriteField("prompt", req.Prompt)
	}
	_ = w.WriteField("response_format", "verbose_json")
	w.Close()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.Endpoint, &body)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := s.client.do(httpReq, vault.KeySTT)
	if err != nil {
		return nil, fmt.Errorf("upstream request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("upstream %d: %s", resp.StatusCode, string(respBody))
	}

	var apiResp struct {
		Text     string  `json:"text"`
		Duration float64 `json:"duration"`
		Segments []struct {
			ID    int     `json:"id"`
			Start float64 `json:"start"`
			End   float64 `json:"end"`
			Text  string  `json:"text"`
		} `json:"segments"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	result := &stt.TranscribeResult{
		Text:       apiResp.Text,
		DurationMS: int64(apiResp.Duration * 1000),
	}
	for _, s := range apiResp.Segments {
		result.Segments = append(result.Segments, stt.Segment{
			Index:   s.ID,
			StartMS: int64(s.Start * 1000),
			EndMS:   int64(s.End * 1000),
			Text:    s.Text,
		})
	}
	return result, nil
}

func (s *STT) TranscribeStream(ctx context.Context, req *stt.TranscribeRequest, out chan<- *stt.Segment) error {
	// OpenAI currently only supports batch upload — accumulate then submit.
	result, err := s.Transcribe(ctx, req)
	if err != nil {
		return err
	}
	for i := range result.Segments {
		out <- &result.Segments[i]
	}
	return nil
}
