package ctrl

import (
	"encoding/binary"
	"errors"
	"io"
)

const frameHeaderLen = 4

// ErrFrameTooLarge is returned when a frame header declares a payload
// exceeding MaxMessageBytes.
var ErrFrameTooLarge = errors.New("ctrl: frame exceeds max size")

// writeFrame writes a single length-prefixed message to w. The header
// and payload are combined into a single Write call so that, on the
// EWP SecureStream, the entire frame is sent as one atomic TCP data
// frame (SecureStream.SendTCPData holds an internal write mutex).
func writeFrame(w io.Writer, data []byte) error {
	if len(data) > MaxMessageBytes {
		return ErrFrameTooLarge
	}
	buf := make([]byte, frameHeaderLen+len(data))
	binary.BigEndian.PutUint32(buf[:frameHeaderLen], uint32(len(data)))
	copy(buf[frameHeaderLen:], data)
	_, err := w.Write(buf)
	return err
}

// readFrame reads a single length-prefixed message from r. It uses
// io.ReadFull so partial reads from the underlying stream are handled
// correctly. Returns io.EOF (or io.ErrUnexpectedEOF) on connection
// close.
func readFrame(r io.Reader) ([]byte, error) {
	var hdr [frameHeaderLen]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(hdr[:])
	if n > MaxMessageBytes {
		return nil, ErrFrameTooLarge
	}
	if n == 0 {
		return []byte{}, nil
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}
