package codec

import (
	"encoding/binary"
	"fmt"
	"io"
)

// PCMToWAV wraps raw little-endian PCM bytes into a standard WAV container.
func PCMToWAV(pcm []byte, sampleRate int, channels int, bitsPerSample int) ([]byte, error) {
	if channels <= 0 || sampleRate <= 0 || bitsPerSample <= 0 {
		return nil, fmt.Errorf("invalid wav params")
	}
	if bitsPerSample%8 != 0 {
		return nil, fmt.Errorf("bitsPerSample must be multiple of 8")
	}

	byteRate := sampleRate * channels * (bitsPerSample / 8)
	blockAlign := channels * (bitsPerSample / 8)
	dataSize := len(pcm)
	riffSize := 36 + dataSize

	out := make([]byte, 44+dataSize)
	copy(out[0:4], []byte("RIFF"))
	binary.LittleEndian.PutUint32(out[4:8], uint32(riffSize))
	copy(out[8:12], []byte("WAVE"))
	copy(out[12:16], []byte("fmt "))
	binary.LittleEndian.PutUint32(out[16:20], 16) // PCM fmt chunk size
	binary.LittleEndian.PutUint16(out[20:22], 1)  // PCM format
	binary.LittleEndian.PutUint16(out[22:24], uint16(channels))
	binary.LittleEndian.PutUint32(out[24:28], uint32(sampleRate))
	binary.LittleEndian.PutUint32(out[28:32], uint32(byteRate))
	binary.LittleEndian.PutUint16(out[32:34], uint16(blockAlign))
	binary.LittleEndian.PutUint16(out[34:36], uint16(bitsPerSample))
	copy(out[36:40], []byte("data"))
	binary.LittleEndian.PutUint32(out[40:44], uint32(dataSize))
	copy(out[44:], pcm)
	return out, nil
}

func PCMReaderToWAV(r io.Reader, sampleRate int, channels int, bitsPerSample int) ([]byte, error) {
	pcm, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	return PCMToWAV(pcm, sampleRate, channels, bitsPerSample)
}
