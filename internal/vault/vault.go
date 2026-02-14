package vault

import (
	"bufio"
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"sync"

	"golang.org/x/sys/unix"
)

type KeyPurpose int

const (
	KeySTT KeyPurpose = iota
	KeyTTS
)

type Vault interface {
	GetKey(purpose KeyPurpose) (string, error)
	Healthy() error
}

type Config struct {
	SecretsFile      string
	EnforceIsolation bool
}

type fileVault struct {
	mu     sync.RWMutex
	keys   map[KeyPurpose][]byte
	cfg    Config
	loaded bool
}

func New(cfg Config) (*fileVault, error) {
	v := &fileVault{cfg: cfg}
	if err := v.reload(); err != nil {
		return nil, err
	}
	return v, nil
}

func (v *fileVault) GetKey(purpose KeyPurpose) (string, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	k, ok := v.keys[purpose]
	if !ok || len(k) == 0 {
		return "", fmt.Errorf("key not found for purpose %d", purpose)
	}
	return string(k), nil
}

func (v *fileVault) Healthy() error {
	v.mu.RLock()
	defer v.mu.RUnlock()
	if !v.loaded {
		return fmt.Errorf("vault not loaded")
	}
	if _, ok := v.keys[KeySTT]; !ok {
		return fmt.Errorf("STT key missing")
	}
	if _, ok := v.keys[KeyTTS]; !ok {
		return fmt.Errorf("TTS key missing")
	}
	return nil
}

// WatchReload listens for the given signal and reloads secrets.
func (v *fileVault) WatchReload(sig os.Signal) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, sig)
	for range ch {
		if err := v.reload(); err != nil {
			slog.Error("vault reload failed, keeping old keys", "error", err)
		} else {
			slog.Info("vault reloaded successfully")
		}
	}
}

func (v *fileVault) reload() error {
	newKeys, err := loadAndValidate(v.cfg)
	if err != nil {
		return err
	}
	v.mu.Lock()
	oldKeys := v.keys
	v.keys = newKeys
	v.loaded = true
	v.mu.Unlock()
	memzero(oldKeys)
	return nil
}

func loadAndValidate(cfg Config) (map[KeyPurpose][]byte, error) {
	info, err := os.Stat(cfg.SecretsFile)
	if err != nil {
		return nil, fmt.Errorf("secrets file: %w", err)
	}
	if info.Mode().Perm()&0o004 != 0 {
		return nil, fmt.Errorf("secrets file %s is world-readable (mode %o), refusing",
			cfg.SecretsFile, info.Mode().Perm())
	}

	data, err := os.ReadFile(cfg.SecretsFile)
	if err != nil {
		return nil, fmt.Errorf("read secrets: %w", err)
	}

	envMap := parseEnvFile(data)
	keys := make(map[KeyPurpose][]byte)

	if v, ok := envMap["PARASPEECH_STT_KEY"]; ok {
		keys[KeySTT] = []byte(v)
	}
	if v, ok := envMap["PARASPEECH_TTS_KEY"]; ok {
		keys[KeyTTS] = []byte(v)
	}

	if len(keys[KeySTT]) == 0 {
		return nil, fmt.Errorf("PARASPEECH_STT_KEY not found in %s", cfg.SecretsFile)
	}
	if len(keys[KeyTTS]) == 0 {
		return nil, fmt.Errorf("PARASPEECH_TTS_KEY not found in %s", cfg.SecretsFile)
	}

	if cfg.EnforceIsolation && bytes.Equal(keys[KeySTT], keys[KeyTTS]) {
		memzero(keys)
		return nil, fmt.Errorf("STT and TTS keys must be different (isolation enforced)")
	}

	for _, k := range keys {
		_ = unix.Mlock(k)
	}

	return keys, nil
}

func parseEnvFile(data []byte) map[string]string {
	result := make(map[string]string)
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			result[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}
	return result
}

func memzero(keys map[KeyPurpose][]byte) {
	for _, k := range keys {
		for i := range k {
			k[i] = 0
		}
	}
}

// Close zeros all keys in memory.
func (v *fileVault) Close() {
	v.mu.Lock()
	defer v.mu.Unlock()
	memzero(v.keys)
	v.loaded = false
}

func (v *fileVault) String() string {
	return "[vault:REDACTED]"
}
