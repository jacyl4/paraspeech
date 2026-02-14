package vault

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func writeSecretsFile(t *testing.T, content string, mode os.FileMode) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "secrets.env")
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestNew_Success(t *testing.T) {
	path := writeSecretsFile(t, "PARASPEECH_STT_KEY=sk-stt-test\nPARASPEECH_TTS_KEY=sk-tts-test\n", 0o640)
	v, err := New(Config{SecretsFile: path, EnforceIsolation: true})
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()

	sttKey, err := v.GetKey(KeySTT)
	if err != nil || sttKey != "sk-stt-test" {
		t.Errorf("STT key: %q, err: %v", sttKey, err)
	}
	ttsKey, err := v.GetKey(KeyTTS)
	if err != nil || ttsKey != "sk-tts-test" {
		t.Errorf("TTS key: %q, err: %v", ttsKey, err)
	}
}

func TestNew_MissingFile(t *testing.T) {
	_, err := New(Config{SecretsFile: "/nonexistent/secrets.env"})
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestNew_WorldReadable(t *testing.T) {
	path := writeSecretsFile(t, "PARASPEECH_STT_KEY=sk-stt\nPARASPEECH_TTS_KEY=sk-tts\n", 0o644)
	_, err := New(Config{SecretsFile: path})
	if err == nil {
		t.Fatal("expected error for world-readable file")
	}
}

func TestNew_MissingSTTKey(t *testing.T) {
	path := writeSecretsFile(t, "PARASPEECH_TTS_KEY=sk-tts\n", 0o640)
	_, err := New(Config{SecretsFile: path})
	if err == nil {
		t.Fatal("expected error for missing STT key")
	}
}

func TestNew_IsolationViolation(t *testing.T) {
	path := writeSecretsFile(t, "PARASPEECH_STT_KEY=same-key\nPARASPEECH_TTS_KEY=same-key\n", 0o640)
	_, err := New(Config{SecretsFile: path, EnforceIsolation: true})
	if err == nil {
		t.Fatal("expected error for same STT/TTS key")
	}
}

func TestNew_IsolationNotEnforced(t *testing.T) {
	path := writeSecretsFile(t, "PARASPEECH_STT_KEY=same-key\nPARASPEECH_TTS_KEY=same-key\n", 0o640)
	v, err := New(Config{SecretsFile: path, EnforceIsolation: false})
	if err != nil {
		t.Fatal(err)
	}
	v.Close()
}

func TestHealthy(t *testing.T) {
	path := writeSecretsFile(t, "PARASPEECH_STT_KEY=sk-stt\nPARASPEECH_TTS_KEY=sk-tts\n", 0o640)
	v, err := New(Config{SecretsFile: path})
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Healthy(); err != nil {
		t.Errorf("expected healthy, got: %v", err)
	}
	v.Close()
	if err := v.Healthy(); err == nil {
		t.Error("expected unhealthy after close")
	}
}

func TestString_Redacted(t *testing.T) {
	path := writeSecretsFile(t, "PARASPEECH_STT_KEY=sk-stt\nPARASPEECH_TTS_KEY=sk-tts\n", 0o640)
	v, err := New(Config{SecretsFile: path})
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()

	s := fmt.Sprintf("%v", v)
	if s != "[vault:REDACTED]" {
		t.Errorf("String() should be redacted, got: %q", s)
	}
}
