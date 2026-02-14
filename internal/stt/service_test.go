package stt

import (
	"bytes"
	"context"
	"io"
	"testing"
)

func TestPrepareDirectUpload_OggOpusPassThrough(t *testing.T) {
	svc := &Service{}
	in := []byte("OggS\x00\x02test-opus")
	r, name, err := svc.prepareDirectUpload(context.Background(), bytes.NewReader(in), "voice.ogg")
	if err != nil {
		t.Fatalf("prepareDirectUpload err: %v", err)
	}
	defer r.Close()

	if name != "audio.ogg" {
		t.Fatalf("upload name = %q, want audio.ogg", name)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read output err: %v", err)
	}
	if !bytes.Equal(out, in) {
		t.Fatalf("passthrough bytes changed")
	}
}

func TestIsUpstreamFormatRejected(t *testing.T) {
	if isUpstreamFormatRejected(nil) {
		t.Fatalf("nil err should be false")
	}
	if !isUpstreamFormatRejected(assertErr("upstream 400: unsupported file format")) {
		t.Fatalf("expected true for upstream 400 format error")
	}
	if isUpstreamFormatRejected(assertErr("upstream 500: internal")) {
		t.Fatalf("expected false for non-400 errors")
	}
}

type assertErr string

func (e assertErr) Error() string { return string(e) }
