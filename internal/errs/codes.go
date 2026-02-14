package errs

type Code int

const (
	OK Code = 0

	ErrEmptyInput      Code = 101
	ErrSTTDecodeFailed Code = 200
	ErrSTTUpstream     Code = 210
	ErrTTSUpstream     Code = 310
)

var codeNames = map[Code]string{
	OK:                 "OK",
	ErrEmptyInput:      "EMPTY_INPUT",
	ErrSTTDecodeFailed: "STT_DECODE_FAILED",
	ErrSTTUpstream:     "STT_UPSTREAM",
	ErrTTSUpstream:     "TTS_UPSTREAM",
}

func (c Code) String() string {
	if name, ok := codeNames[c]; ok {
		return name
	}
	return "UNKNOWN"
}
