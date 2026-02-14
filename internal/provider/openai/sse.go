package openai

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"strings"
)

type SSEEvent struct {
	Type  string
	Delta string
	Text  string
	Error string
}

func ParseSSEStream(ctx context.Context, reader io.Reader, out chan<- SSEEvent) error {
	scanner := bufio.NewScanner(reader)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" {
			continue
		}
		if data == "[DONE]" {
			return nil
		}
		event, err := parseEventJSON(data)
		if err != nil {
			continue
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case out <- event:
		}
	}
	return scanner.Err()
}

func parseEventJSON(data string) (SSEEvent, error) {
	var payload struct {
		Type  string `json:"type"`
		Delta string `json:"delta"`
		Text  string `json:"text"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(data), &payload); err != nil {
		return SSEEvent{}, err
	}
	return SSEEvent{Type: payload.Type, Delta: payload.Delta, Text: payload.Text, Error: payload.Error.Message}, nil
}
