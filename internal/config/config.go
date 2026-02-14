package config

import "time"

type Config struct {
	Server Server `toml:"server"`
	Vault  Vault  `toml:"vault"`
	Log    Log    `toml:"log"`
	STT    STT    `toml:"stt"`
	TTS    TTS    `toml:"tts"`
	Queue  Queue  `toml:"queue"`
}

type Server struct {
	GRPCAddr        string        `toml:"grpc_addr"`
	MetricsAddr     string        `toml:"metrics_addr"`
	ShutdownTimeout time.Duration `toml:"shutdown_timeout"`
}

type Vault struct {
	SecretsFile string `toml:"secrets_file"`
}

type Log struct {
	Level  string `toml:"level"`
	Format string `toml:"format"`
	Output string `toml:"output"`
	File   string `toml:"file"`
}

type STT struct {
	Enabled      bool     `toml:"enabled"`
	MaxBytes     int64    `toml:"max_bytes"`
	Timeout      time.Duration `toml:"timeout"`
	DefaultModel string   `toml:"default_model"`
	MaxConcurrent int     `toml:"max_concurrent"`
	VAD          VAD      `toml:"vad"`
	Stream       Stream   `toml:"stream"`
	Upstream     Upstream `toml:"upstream"`
}

type VAD struct {
	Mode         string  `toml:"mode"`
	HopSize      int     `toml:"hop_size"`
	Threshold    float32 `toml:"threshold"`
	MinSpeechMS  int     `toml:"min_speech_ms"`
	MinSilenceMS int     `toml:"min_silence_ms"`
	PadMS        int     `toml:"pad_ms"`
	MaxGapMS     int     `toml:"max_gap_ms"`
	MaxAudioSec  float64 `toml:"max_audio_sec"`
	MinTrimRatio float64 `toml:"min_trim_ratio"`
}

type Stream struct {
	Enabled    bool    `toml:"enabled"`
	ChunkSec   float64 `toml:"chunk_sec"`
	OverlapSec float64 `toml:"overlap_sec"`
	MaxChunks  int     `toml:"max_chunks"`
}

type Upstream struct {
	Provider       string        `toml:"provider"`
	Endpoint       string        `toml:"endpoint"`
	ConnectTimeout time.Duration `toml:"connect_timeout"`
	ReadTimeout    time.Duration `toml:"read_timeout"`
	MaxConnections int           `toml:"max_connections"`
	MaxKeepalive   int           `toml:"max_keepalive"`
	PrewarmOnStart bool          `toml:"prewarm_on_start"`
	PrewarmOnTask  bool          `toml:"prewarm_on_task"`
	PrewarmFailOpen bool         `toml:"prewarm_fail_open"`
	PrewarmTimeout time.Duration `toml:"prewarm_timeout"`
}

type TTS struct {
	Enabled        bool     `toml:"enabled"`
	MaxBody        int64    `toml:"max_body"`
	Timeout        time.Duration `toml:"timeout"`
	DefaultModel   string   `toml:"default_model"`
	DefaultVoice   string   `toml:"default_voice"`
	DefaultSpeed   float64  `toml:"default_speed"`
	DefaultEmotion string   `toml:"default_emotion"`
	DefaultStyle   string   `toml:"default_style"`
	DefaultFormat  string   `toml:"default_format"`
	MaxSec         float64  `toml:"max_sec"`
	MaxConcurrent  int      `toml:"max_concurrent"`
	OutDir         string   `toml:"out_dir"`
	Upstream       Upstream `toml:"upstream"`
}

type Queue struct {
	STTBurst int     `toml:"stt_burst"`
	STTRate  float64 `toml:"stt_rate"`
	TTSBurst int     `toml:"tts_burst"`
	TTSRate  float64 `toml:"tts_rate"`
}

func Defaults() *Config {
	return &Config{
		Server: Server{
			GRPCAddr:        "127.0.0.1:9800",
			MetricsAddr:     "127.0.0.1:9801",
			ShutdownTimeout: 10 * time.Second,
		},
		Vault: Vault{
			SecretsFile: "/etc/paraspeech/secrets.env",
		},
		Log: Log{
			Level:  "info",
			Format: "json",
			Output: "stderr",
		},
		STT: STT{
			Enabled:       true,
			MaxBytes:      26214400,
			Timeout:       90 * time.Second,
			DefaultModel:  "gpt-4o-mini-transcribe",
			MaxConcurrent: 10,
			VAD: VAD{
				Mode:         "on",
				HopSize:      256,
				Threshold:    0.5,
				MinSpeechMS:  200,
				MinSilenceMS: 300,
				PadMS:        150,
				MaxGapMS:     500,
				MaxAudioSec:  45,
				MinTrimRatio: 0.3,
			},
			Stream: Stream{
				Enabled:    true,
				ChunkSec:   8.0,
				OverlapSec: 1.0,
				MaxChunks:  12,
			},
			Upstream: Upstream{
				Provider:       "openai",
				Endpoint:       "https://api.openai.com/v1/audio/transcriptions",
				ConnectTimeout: 5 * time.Second,
				ReadTimeout:    90 * time.Second,
				MaxConnections: 20,
				MaxKeepalive:   10,
				PrewarmOnStart: true,
				PrewarmOnTask:  true,
				PrewarmFailOpen: true,
				PrewarmTimeout: 800 * time.Millisecond,
			},
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
			MaxConcurrent:  10,
			Upstream: Upstream{
				Provider:       "openai",
				Endpoint:       "https://api.openai.com/v1/audio/speech",
				ConnectTimeout: 5 * time.Second,
				ReadTimeout:    45 * time.Second,
				MaxConnections: 20,
				MaxKeepalive:   10,
				PrewarmOnStart: true,
				PrewarmOnTask:  true,
				PrewarmFailOpen: true,
				PrewarmTimeout: 800 * time.Millisecond,
			},
		},
		Queue: Queue{
			STTBurst: 20,
			STTRate:  10.0,
			TTSBurst: 20,
			TTSRate:  10.0,
		},
	}
}
