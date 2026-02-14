package codec

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os/exec"
	"time"
)

type ffmpegReader struct {
	cmd    *exec.Cmd
	reader io.ReadCloser
}

func (r *ffmpegReader) Read(p []byte) (int, error) {
	return r.reader.Read(p)
}

func (r *ffmpegReader) Close() error {
	_ = r.reader.Close()
	done := make(chan error, 1)
	go func() { done <- r.cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		if r.cmd.Process != nil {
			_ = r.cmd.Process.Kill()
		}
		return fmt.Errorf("ffmpeg wait timeout, process killed")
	}
}

// Decode converts any audio format to 16kHz mono PCM int16 via ffmpeg pipe.
// Zero disk IO — pure memory pipe.
func Decode(ctx context.Context, input io.Reader) (io.ReadCloser, error) {
	cmd := exec.CommandContext(ctx,
		"ffmpeg", "-hide_banner", "-loglevel", "error",
		"-i", "pipe:0",
		"-ac", "1",
		"-ar", "16000",
		"-f", "s16le",
		"pipe:1",
	)
	cmd.Stdin = input

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("ffmpeg stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("ffmpeg start: %w", err)
	}
	return &ffmpegReader{cmd: cmd, reader: stdout}, nil
}

// RemuxToWebm remuxes input audio into a webm container without audio re-encode.
func RemuxToWebm(ctx context.Context, input io.Reader) (io.ReadCloser, error) {
	cmd := exec.CommandContext(ctx,
		"ffmpeg", "-hide_banner", "-loglevel", "error",
		"-i", "pipe:0",
		"-c:a", "copy",
		"-f", "webm",
		"pipe:1",
	)
	cmd.Stdin = input
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("ffmpeg stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("ffmpeg start: %w", err)
	}
	return &ffmpegReader{cmd: cmd, reader: stdout}, nil
}

// EncodeToWebmOpus encodes 16kHz mono s16le PCM to webm/opus.
func EncodeToWebmOpus(ctx context.Context, pcmInput io.Reader) (io.ReadCloser, error) {
	cmd := exec.CommandContext(ctx,
		"ffmpeg", "-hide_banner", "-loglevel", "error",
		"-f", "s16le", "-ar", "16000", "-ac", "1",
		"-i", "pipe:0",
		"-c:a", "libopus", "-b:a", "32k",
		"-f", "webm",
		"pipe:1",
	)
	cmd.Stdin = pcmInput
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("ffmpeg stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("ffmpeg start: %w", err)
	}
	return &ffmpegReader{cmd: cmd, reader: stdout}, nil
}

// TranscodeToWebmOpus transcodes input audio stream to webm/opus.
func TranscodeToWebmOpus(ctx context.Context, input io.Reader) (io.ReadCloser, error) {
	cmd := exec.CommandContext(ctx,
		"ffmpeg", "-hide_banner", "-loglevel", "error",
		"-i", "pipe:0",
		"-c:a", "libopus", "-b:a", "32k",
		"-f", "webm",
		"pipe:1",
	)
	cmd.Stdin = input
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("ffmpeg stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("ffmpeg start: %w", err)
	}
	return &ffmpegReader{cmd: cmd, reader: stdout}, nil
}

// ReadFrames reads fixed-size int16 frames from a raw PCM stream.
func ReadFrames(pcm io.Reader, hopSize int) <-chan []int16 {
	ch := make(chan []int16, 64)
	go func() {
		defer close(ch)
		buf := make([]byte, hopSize*2)
		for {
			_, err := io.ReadFull(pcm, buf)
			if err != nil {
				return
			}
			frame := make([]int16, hopSize)
			for i := range frame {
				frame[i] = int16(binary.LittleEndian.Uint16(buf[i*2:]))
			}
			ch <- frame
		}
	}()
	return ch
}

// SamplesToReader converts int16 samples to a raw PCM byte reader.
func SamplesToReader(samples []int16) io.Reader {
	buf := make([]byte, len(samples)*2)
	for i, s := range samples {
		binary.LittleEndian.PutUint16(buf[i*2:], uint16(s))
	}
	return bytes.NewReader(buf)
}
