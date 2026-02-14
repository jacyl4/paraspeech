package vad

// TEN VAD CGo implementation stub.
//
// The actual CGo bindings require libten_vad.so in the link path.
// Build with CGO_ENABLED=1 and proper #cgo directives pointing to
// third_party/ten-vad/include and third_party/ten-vad/lib/Linux/x64.
//
// When CGO_ENABLED=0 or the library is unavailable, NewTenVad returns
// ErrVadNotAvailable and the service falls back to vad.mode=off.

import (
	"fmt"
)

var ErrVadNotAvailable = fmt.Errorf("TEN VAD not available (CGo disabled or libten_vad not found)")

// NewTenVad creates a TEN VAD detector.
// This is a stub — replace with CGo implementation when libten_vad is present.
func NewTenVad(hopSize int, threshold float32) (Detector, error) {
	// TODO: Replace with CGo implementation from ARCHITECTURE.md Section 6.5
	return nil, ErrVadNotAvailable
}
