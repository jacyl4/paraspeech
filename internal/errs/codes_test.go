package errs

import "testing"

func TestCode_String(t *testing.T) {
	tests := []struct {
		code Code
		want string
	}{
		{OK, "OK"},
		{ErrSTTDecodeFailed, "STT_DECODE_FAILED"},
		{ErrVaultMissing, "VAULT_MISSING"},
		{Code(9999), "UNKNOWN"},
	}
	for _, tt := range tests {
		if got := tt.code.String(); got != tt.want {
			t.Errorf("Code(%d).String() = %q, want %q", tt.code, got, tt.want)
		}
	}
}

func TestError_Format(t *testing.T) {
	err := New(ErrSTTUpstream, "provider returned 429")
	if err.Error() != "[210/STT_UPSTREAM] provider returned 429" {
		t.Errorf("unexpected error format: %s", err.Error())
	}
}

func TestWrap_Unwrap(t *testing.T) {
	cause := New(ErrInternal, "io error")
	wrapped := Wrap(ErrSTTDecodeFailed, cause)
	if wrapped.Unwrap() != cause {
		t.Error("Unwrap() should return cause")
	}
}
