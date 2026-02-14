package config

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

func Load(paths ...string) (*Config, error) {
	cfg := Defaults()
	path := resolveConfigPath(paths...)
	if path != "" {
		if _, err := toml.DecodeFile(path, cfg); err != nil {
			return nil, fmt.Errorf("load config %s: %w", path, err)
		}
	}
	applyEnvOverrides(cfg)
	return cfg, nil
}

func resolveConfigPath(paths ...string) string {
	if len(paths) > 0 && paths[0] != "" {
		return paths[0]
	}
	if p := os.Getenv("PARASPEECH_CONFIG"); p != "" {
		return p
	}
	if _, err := os.Stat("/etc/paraspeech/paraspeech.toml"); err == nil {
		return "/etc/paraspeech/paraspeech.toml"
	}
	return ""
}

func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("PARASPEECH_SERVER_GRPC_ADDR"); v != "" {
		cfg.Server.GRPCAddr = v
	}
	if v := os.Getenv("PARASPEECH_STT_VAD_MODE"); v != "" {
		cfg.STT.VAD.Mode = v
	}
	if v := os.Getenv("PARASPEECH_TTS_DEFAULT_VOICE"); v != "" {
		cfg.TTS.DefaultVoice = v
	}
}

func Validate(cfg *Config) error {
	if cfg.Server.GRPCAddr == "" {
		return fmt.Errorf("server.grpc_addr is required")
	}
	if cfg.STT.Enabled && cfg.STT.VAD.HopSize != 160 && cfg.STT.VAD.HopSize != 256 {
		return fmt.Errorf("stt.vad.hop_size must be 160 or 256, got %d", cfg.STT.VAD.HopSize)
	}
	if cfg.STT.VAD.Threshold < 0 || cfg.STT.VAD.Threshold > 1 {
		return fmt.Errorf("stt.vad.threshold must be in [0.0, 1.0]")
	}
	return nil
}
