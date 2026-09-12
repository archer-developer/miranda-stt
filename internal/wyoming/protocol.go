package wyoming

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strings"
)

// Reader wraps a net.Conn and reads Wyoming protocol messages.
//
// Both ReadHeader and ReadPayload go through the same bufio.Reader so that
// lookahead buffering for header lines doesn't silently consume binary PCM
// payload bytes that the next ReadPayload call expects to receive from the
// connection. Using bufio.Scanner + raw net.Conn reads was the original
// approach, but Scanner's internal read-ahead would buffer PCM bytes after
// the '\n', leaving ReadPayload to read stale data from the raw conn.
type Reader struct {
	r *bufio.Reader
}

// NewReader creates a Reader for conn.
func NewReader(conn net.Conn) *Reader {
	return &Reader{r: bufio.NewReader(conn)}
}

// ReadHeader reads the next JSON header line. Returns io.EOF when the
// connection is cleanly closed.
func (r *Reader) ReadHeader() (Header, error) {
	line, err := r.r.ReadString('\n')
	if err != nil {
		if err == io.EOF && len(line) == 0 {
			return Header{}, io.EOF
		}
		if err == io.EOF {
			// partial line without newline — treat as clean close
			return Header{}, io.EOF
		}
		return Header{}, fmt.Errorf("wyoming/protocol: read header: %w", err)
	}
	line = strings.TrimSuffix(line, "\n")

	var h Header
	if err := json.Unmarshal([]byte(line), &h); err != nil {
		return Header{}, fmt.Errorf("wyoming/protocol: parse header %q: %w", line, err)
	}
	return h, nil
}

// ReadPayload reads exactly n bytes of binary payload that immediately
// follows the header in the stream.
func (r *Reader) ReadPayload(n int) ([]byte, error) {
	buf := make([]byte, n)
	if _, err := io.ReadFull(r.r, buf); err != nil {
		return nil, fmt.Errorf("wyoming/protocol: read payload (%d bytes): %w", n, err)
	}
	return buf, nil
}

// DecodeData unmarshals h.Data (which json.Unmarshal leaves as a
// map[string]interface{}) into dst.
func DecodeData(h Header, dst interface{}) error {
	raw, err := json.Marshal(h.Data)
	if err != nil {
		return fmt.Errorf("wyoming/protocol: re-marshal data: %w", err)
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return fmt.Errorf("wyoming/protocol: decode data for type %q: %w", h.Type, err)
	}
	return nil
}

// Writer wraps a net.Conn and writes Wyoming protocol messages.
type Writer struct {
	conn net.Conn
}

// NewWriter creates a Writer for conn.
func NewWriter(conn net.Conn) *Writer {
	return &Writer{conn: conn}
}

// WriteMessage serialises hdr as JSON followed by '\n'. If payload is
// non-nil, it is written immediately after.
func (w *Writer) WriteMessage(msgType string, data interface{}, payload []byte) error {
	h := Header{Type: msgType, Data: data}
	if len(payload) > 0 {
		h.PayloadLength = len(payload)
	}
	b, err := json.Marshal(h)
	if err != nil {
		return fmt.Errorf("wyoming/protocol: marshal %s: %w", msgType, err)
	}
	b = append(b, '\n')
	if _, err := w.conn.Write(b); err != nil {
		return fmt.Errorf("wyoming/protocol: write %s header: %w", msgType, err)
	}
	if len(payload) > 0 {
		if _, err := w.conn.Write(payload); err != nil {
			return fmt.Errorf("wyoming/protocol: write %s payload: %w", msgType, err)
		}
	}
	return nil
}
