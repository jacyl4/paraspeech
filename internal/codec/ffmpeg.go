package codec

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os/exec"
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
	return r.cmd.Wait()
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
	return &byteReader{data: buf}
}

type byteReader struct {
	data []byte
	pos  int
}

func (r *byteReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}
