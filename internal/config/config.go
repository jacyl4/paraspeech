package config

import "time"

type Config struct {
	Server Server `toml:"server"`
	Vault  Vault  `toml:"vault"`
	Log    Log    `toml:"log"`
	STT    STT    `toml:"stt"`
	TTS    TTS    `toml:"tts"`
}

type Server struct {
	GRPCAddr        string        `toml:"grpc_addr"`
	ShutdownTimeout time.Duration `toml:"shutdown_timeout"`
}

type Vault struct {
	SecretsFile string `toml:"secrets_file"`
}

type Log struct {
	Level  string `toml:"level"`
	Format string `toml:"format"`
}

type STT struct {
	Enabled        bool          `toml:"enabled"`
	MaxBytes       int64         `toml:"max_bytes"`
	DirectMaxBytes int64         `toml:"direct_max_bytes"`
	Timeout        time.Duration `toml:"timeout"`
	DefaultModel   string        `toml:"default_model"`
	VAD            VAD           `toml:"vad"`
	Upstream       Upstream      `toml:"upstream"`
}

type VAD struct {
	Mode         string  `toml:"mode"`
	HopSize      int     `toml:"hop_size"`
	Threshold    float32 `toml:"threshold"`
	MinSpeechMS  int     `toml:"min_speech_ms"`
	PadMS        int     `toml:"pad_ms"`
	MaxGapMS     int     `toml:"max_gap_ms"`
	MaxAudioSec  float64 `toml:"max_audio_sec"`
	MinTrimRatio float64 `toml:"min_trim_ratio"`
}

type Upstream struct {
	Endpoint       string        `toml:"endpoint"`
	ConnectTimeout time.Duration `toml:"connect_timeout"`
	ReadTimeout    time.Duration `toml:"read_timeout"`
	// MaxConnections/MaxKeepalive only affect HTTP/1.1 idle pool; HTTP/2 upstream generally ignores these limits.
	MaxConnections    int           `toml:"max_connections"`
	MaxKeepalive      int           `toml:"max_keepalive"`
	KeepaliveInterval time.Duration `toml:"keepalive_interval"`
	PrewarmOnStart    bool          `toml:"prewarm_on_start"`
	PrewarmFailOpen   bool          `toml:"prewarm_fail_open"`
	PrewarmTimeout    time.Duration `toml:"prewarm_timeout"`
}

type TTS struct {
	Enabled        bool          `toml:"enabled"`
	MaxBody        int64         `toml:"max_body"`
	Timeout        time.Duration `toml:"timeout"`
	DefaultModel   string        `toml:"default_model"`
	DefaultVoice   string        `toml:"default_voice"`
	DefaultSpeed   float64       `toml:"default_speed"`
	DefaultEmotion string        `toml:"default_emotion"`
	DefaultStyle   string        `toml:"default_style"`
	DefaultFormat  string        `toml:"default_format"`
	MaxSec         float64       `toml:"max_sec"`
	MaxParallel    int           `toml:"max_parallel"`
	Upstream       Upstream      `toml:"upstream"`
}

func Defaults() *Config {
	return &Config{
		Server: Server{GRPCAddr: "127.0.0.1:9800", ShutdownTimeout: 10 * time.Second},
		Vault:  Vault{SecretsFile: "/etc/paraspeech/secrets.env"},
		Log:    Log{Level: "info", Format: "json"},
		STT: STT{
			Enabled:        true,
			MaxBytes:       26214400,
			DirectMaxBytes: 1048576,
			Timeout:        90 * time.Second,
			DefaultModel:   "gpt-4o-mini-transcribe",
			VAD:            VAD{Mode: "on", HopSize: 256, Threshold: 0.5, MinSpeechMS: 200, PadMS: 150, MaxGapMS: 500, MaxAudioSec: 30, MinTrimRatio: 0.3},
			Upstream:       Upstream{Endpoint: "https://api.openai.com/v1/audio/transcriptions", ConnectTimeout: 5 * time.Second, ReadTimeout: 90 * time.Second, MaxConnections: 20, MaxKeepalive: 10, KeepaliveInterval: 60 * time.Second, PrewarmOnStart: true, PrewarmFailOpen: true, PrewarmTimeout: 800 * time.Millisecond},
		},
		TTS: TTS{
			Enabled:        true,
			MaxBody:        524288,
			Timeout:        45 * time.Second,
			DefaultModel:   "gpt-4o-mini-tts",
			DefaultVoice:   "nova",
			DefaultSpeed:   1.22,
			DefaultEmotion: "neutral",
			DefaultStyle:   "conversational",
			DefaultFormat:  "opus",
			MaxSec:         25.0,
			MaxParallel:    3,
			Upstream:       Upstream{Endpoint: "https://api.openai.com/v1/audio/speech", ConnectTimeout: 5 * time.Second, ReadTimeout: 45 * time.Second, MaxConnections: 20, MaxKeepalive: 10, KeepaliveInterval: 60 * time.Second, PrewarmOnStart: true, PrewarmFailOpen: true, PrewarmTimeout: 800 * time.Millisecond},
		},
	}
}
