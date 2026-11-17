// Package controlpipe provides the authenticated local control transport for
// desktop services. It does not install a service or make network decisions.
package controlpipe

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
)

const Version = 1
const MaxFrame = 64 * 1024

// Identity is obtained from OS credentials, never decoded from a request.
type Identity struct{ UserSID, LogonSID string }
type Request struct {
	Version   int             `json:"version"`
	ID        uint64          `json:"id"`
	Operation string          `json:"operation"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}
type Response struct {
	Version int             `json:"version"`
	ID      uint64          `json:"id"`
	Code    string          `json:"code"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

func readFrame(r io.Reader, value any) error {
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return err
	}
	n := binary.BigEndian.Uint32(header[:])
	if n == 0 || n > MaxFrame {
		return fmt.Errorf("invalid control frame size")
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(r, b); err != nil {
		return err
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(value); err != nil {
		return fmt.Errorf("invalid control message")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("trailing control message")
	}
	return nil
}
func writeFrame(w io.Writer, value any) error {
	b, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("invalid control response")
	}
	if len(b) == 0 || len(b) > MaxFrame {
		return fmt.Errorf("control message too large")
	}
	var h [4]byte
	binary.BigEndian.PutUint32(h[:], uint32(len(b)))
	for _, p := range [][]byte{h[:], b} {
		for len(p) > 0 {
			n, e := w.Write(p)
			if e != nil {
				return e
			}
			if n == 0 {
				return io.ErrShortWrite
			}
			p = p[n:]
		}
	}
	return nil
}
