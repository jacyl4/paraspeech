.PHONY: build test lint proto clean version

APP := paraspeech
VERSION := v1.0.1
PKG := paraspeech/internal/version
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE := $(shell TZ=Asia/Shanghai date '+%Y-%m-%dT%H:%M:%S%:z')

LDFLAGS := -X '$(PKG).Version=$(VERSION)' \
	-X '$(PKG).Commit=$(COMMIT)' \
	-X '$(PKG).BuildTime=$(DATE)'

build:
	go build -ldflags "$(LDFLAGS)" -o $(APP) ./cmd/paraspeech

test:
	go test ./internal/... -race -cover -count=1

lint:
	golangci-lint run ./...

proto:
	buf generate api/proto

clean:
	rm -f $(APP)

version:
	@echo $(VERSION)
