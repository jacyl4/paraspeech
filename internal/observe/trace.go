package observe

import (
	"crypto/rand"
	"encoding/hex"
)

// NewTraceID generates a random trace ID (16 hex chars).
func NewTraceID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
