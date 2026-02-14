package observe

import (
	"log/slog"
	"os"
	"strings"
)

func SetupLogger(level, format string) {
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{
		Level: lvl,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			// Redact sensitive fields
			key := strings.ToLower(a.Key)
			for _, word := range []string{"key", "secret", "token", "sk-"} {
				if strings.Contains(key, word) {
					a.Value = slog.StringValue("[REDACTED]")
				}
			}
			return a
		},
	}

	var handler slog.Handler
	if strings.ToLower(format) == "text" {
		handler = slog.NewTextHandler(os.Stderr, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stderr, opts)
	}
	slog.SetDefault(slog.New(handler))
}
