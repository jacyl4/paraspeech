//go:build !cgo

package vad

// NewTenVad returns ErrVadNotAvailable when CGO is disabled.
func NewTenVad(hopSize int, threshold float32) (Detector, error) {
	return nil, ErrVadNotAvailable
}
