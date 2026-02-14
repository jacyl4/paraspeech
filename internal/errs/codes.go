package errs

type Code int

const (
	OK Code = 0

	// General 1xx
	ErrInvalidRequest     Code = 100
	ErrEmptyInput         Code = 101
	ErrPayloadTooLarge    Code = 102
	ErrTimeout            Code = 103
	ErrRateLimited        Code = 104
	ErrInternal           Code = 199

	// STT 2xx
	ErrSTTDecodeFailed    Code = 200
	ErrSTTVadFailed       Code = 201
	ErrSTTUpstream        Code = 210
	ErrSTTUpstreamTimeout Code = 211

	// TTS 3xx
	ErrTTSSanitizeFailed  Code = 300
	ErrTTSSplitFailed     Code = 301
	ErrTTSUpstream        Code = 310
	ErrTTSUpstreamTimeout Code = 311

	// Vault 4xx
	ErrVaultMissing       Code = 400
	ErrVaultIsolation     Code = 401
)

var codeNames = map[Code]string{
	OK:                    "OK",
	ErrInvalidRequest:     "INVALID_REQUEST",
	ErrEmptyInput:         "EMPTY_INPUT",
	ErrPayloadTooLarge:    "PAYLOAD_TOO_LARGE",
	ErrTimeout:            "TIMEOUT",
	ErrRateLimited:        "RATE_LIMITED",
	ErrInternal:           "INTERNAL",
	ErrSTTDecodeFailed:    "STT_DECODE_FAILED",
	ErrSTTVadFailed:       "STT_VAD_FAILED",
	ErrSTTUpstream:        "STT_UPSTREAM",
	ErrSTTUpstreamTimeout: "STT_UPSTREAM_TIMEOUT",
	ErrTTSSanitizeFailed:  "TTS_SANITIZE_FAILED",
	ErrTTSSplitFailed:     "TTS_SPLIT_FAILED",
	ErrTTSUpstream:        "TTS_UPSTREAM",
	ErrTTSUpstreamTimeout: "TTS_UPSTREAM_TIMEOUT",
	ErrVaultMissing:       "VAULT_MISSING",
	ErrVaultIsolation:     "VAULT_ISOLATION",
}

func (c Code) String() string {
	if name, ok := codeNames[c]; ok {
		return name
	}
	return "UNKNOWN"
}
