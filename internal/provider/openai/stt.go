package openai

import (
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"

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

func (s *STT) transcribeSSE(ctx context.Context, req *stt.TranscribeRequest) (<-chan SSEEvent, <-chan error, error) {
	pr, pw := io.Pipe()
	mpWriter := multipart.NewWriter(pw)
	contentType := mpWriter.FormDataContentType()

	go func() {
		defer pw.Close()
		defer mpWriter.Close()

		if err := mpWriter.WriteField("model", req.Model); err != nil {
			pw.CloseWithError(fmt.Errorf("write model field: %w", err))
			return
		}
		if err := mpWriter.WriteField("response_format", "text"); err != nil {
			pw.CloseWithError(fmt.Errorf("write response_format field: %w", err))
			return
		}
		if err := mpWriter.WriteField("stream", "true"); err != nil {
			pw.CloseWithError(fmt.Errorf("write stream field: %w", err))
			return
		}
		if req.Language != "" {
			if err := mpWriter.WriteField("language", req.Language); err != nil {
				pw.CloseWithError(fmt.Errorf("write language field: %w", err))
				return
			}
		}
		if req.Prompt != "" {
			if err := mpWriter.WriteField("prompt", req.Prompt); err != nil {
				pw.CloseWithError(fmt.Errorf("write prompt field: %w", err))
				return
			}
		}

		filename := req.Filename
		if filename == "" {
			filename = "audio.webm"
		}
		part, err := mpWriter.CreateFormFile("file", filename)
		if err != nil {
			pw.CloseWithError(fmt.Errorf("create form file: %w", err))
			return
		}
		if _, err := io.Copy(part, req.Audio); err != nil {
			pw.CloseWithError(fmt.Errorf("copy audio: %w", err))
			return
		}
	}()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.Endpoint, pr)
	if err != nil {
		return nil, nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", contentType)

	resp, err := s.client.do(httpReq, vault.KeySTT)
	if err != nil {
		return nil, nil, fmt.Errorf("upstream request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, nil, fmt.Errorf("upstream %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	eventCh := make(chan SSEEvent, 64)
	errCh := make(chan error, 1)
	go func() {
		defer resp.Body.Close()
		defer close(eventCh)
		defer close(errCh)
		errCh <- ParseSSEStream(resp.Body, eventCh)
	}()

	return eventCh, errCh, nil
}

func (s *STT) Transcribe(ctx context.Context, req *stt.TranscribeRequest) (*stt.TranscribeResult, error) {
	eventCh, errCh, err := s.transcribeSSE(ctx, req)
	if err != nil {
		return nil, err
	}

	var text strings.Builder
	for event := range eventCh {
		switch event.Type {
		case "transcript.text.delta":
			text.WriteString(event.Delta)
		case "transcript.text.done":
			if event.Text != "" {
				return &stt.TranscribeResult{Text: event.Text}, nil
			}
		case "error":
			if event.Error != "" {
				return nil, fmt.Errorf("upstream stream error: %s", event.Error)
			}
		}
	}

	if parseErr := <-errCh; parseErr != nil {
		return nil, fmt.Errorf("parse sse: %w", parseErr)
	}
	return &stt.TranscribeResult{Text: text.String()}, nil
}

func (s *STT) TranscribeStreamSSE(ctx context.Context, req *stt.TranscribeRequest, out chan<- string) error {
	eventCh, errCh, err := s.transcribeSSE(ctx, req)
	if err != nil {
		return err
	}

	for event := range eventCh {
		switch event.Type {
		case "transcript.text.delta":
			out <- event.Delta
		case "transcript.text.done":
			if parseErr := <-errCh; parseErr != nil {
				return fmt.Errorf("parse sse: %w", parseErr)
			}
			return nil
		case "error":
			if event.Error != "" {
				return fmt.Errorf("upstream stream error: %s", event.Error)
			}
		}
	}

	if parseErr := <-errCh; parseErr != nil {
		return fmt.Errorf("parse sse: %w", parseErr)
	}
	return nil
}

func (s *STT) TranscribeStream(ctx context.Context, req *stt.TranscribeRequest, out chan<- *stt.Segment) error {
	result, err := s.Transcribe(ctx, req)
	if err != nil {
		return err
	}
	if result.Text != "" {
		out <- &stt.Segment{Index: 0, Text: result.Text}
	}
	return nil
}
