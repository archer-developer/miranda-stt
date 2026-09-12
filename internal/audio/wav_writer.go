// Package audio provides utilities for writing PCM audio data to WAV files.
package audio

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
)

const (
	sampleRate  = 16000
	numChannels = 1
	bitDepth    = 16
)

// Buffer accumulates raw PCM chunks in memory and can flush them as a
// valid RIFF WAV file.
type Buffer struct {
	pcm bytes.Buffer
}

// Write appends raw PCM bytes to the buffer.
func (b *Buffer) Write(p []byte) {
	b.pcm.Write(p)
}

// Len returns the number of PCM bytes accumulated.
func (b *Buffer) Len() int {
	return b.pcm.Len()
}

// SaveWAV writes the accumulated PCM data as a 16 kHz / 16-bit / mono WAV
// file at path, creating any parent directories as needed.
func (b *Buffer) SaveWAV(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("audio/wav_writer: mkdir %s: %w", filepath.Dir(path), err)
	}

	pcm := b.pcm.Bytes()
	header := wavHeader(len(pcm))

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("audio/wav_writer: create %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	if _, err := f.Write(header); err != nil {
		return fmt.Errorf("audio/wav_writer: write header %s: %w", path, err)
	}
	if _, err := f.Write(pcm); err != nil {
		return fmt.Errorf("audio/wav_writer: write pcm %s: %w", path, err)
	}
	return nil
}

// wavHeader builds a minimal RIFF WAV header for the given PCM data length.
func wavHeader(pcmLen int) []byte {
	byteRate := sampleRate * numChannels * (bitDepth / 8)
	blockAlign := numChannels * (bitDepth / 8)

	var h bytes.Buffer
	le := binary.LittleEndian

	// RIFF chunk
	h.WriteString("RIFF")
	_ = binary.Write(&h, le, uint32(36+pcmLen)) // file size minus 8
	h.WriteString("WAVE")

	// fmt sub-chunk
	h.WriteString("fmt ")
	_ = binary.Write(&h, le, uint32(16))             // sub-chunk size
	_ = binary.Write(&h, le, uint16(1))              // PCM format
	_ = binary.Write(&h, le, uint16(numChannels))
	_ = binary.Write(&h, le, uint32(sampleRate))
	_ = binary.Write(&h, le, uint32(byteRate))
	_ = binary.Write(&h, le, uint16(blockAlign))
	_ = binary.Write(&h, le, uint16(bitDepth))

	// data sub-chunk
	h.WriteString("data")
	_ = binary.Write(&h, le, uint32(pcmLen))

	return h.Bytes()
}
