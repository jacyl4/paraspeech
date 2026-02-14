.PHONY: build test lint proto clean

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")

build:
	go build -ldflags "-X paraspeech/internal/transport/cli.Version=$(VERSION)" \
		-o bin/paraspeech ./cmd/paraspeech

test:
	go test ./internal/... -race -cover -count=1

lint:
	golangci-lint run ./...

proto:
	buf generate api/proto

clean:
	rm -rf bin/
