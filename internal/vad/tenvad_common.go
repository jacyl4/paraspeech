package vad

import "fmt"

var ErrVadNotAvailable = fmt.Errorf("TEN VAD not available (CGo disabled or libten_vad not found)")
