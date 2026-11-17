package controlpipe

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"strings"
	"testing"
)

func TestFrameRejectsUnboundedAndIdentityClaims(t *testing.T) {
	for _, size := range []uint32{0, MaxFrame + 1, 0xffffffff} {
		var b [4]byte
		binary.BigEndian.PutUint32(b[:], size)
		if readFrame(bytes.NewReader(b[:]), new(Request)) == nil {
			t.Fatal("accepted size", size)
		}
	}
	for _, body := range []string{`{"version":1,"id":1,"operation":"status","uid":0}`, `{"version":1,"id":1,"operation":"status","pid":1}`, `{} {}`, `{"payload":`, strings.Repeat("x", 10)} {
		var b bytes.Buffer
		var h [4]byte
		binary.BigEndian.PutUint32(h[:], uint32(len(body)))
		b.Write(h[:])
		b.WriteString(body)
		if readFrame(&b, new(Request)) == nil {
			t.Fatal("accepted malformed or claimed identity")
		}
	}
	var b bytes.Buffer
	request := Request{Version: Version, ID: 42, Operation: "status", Payload: json.RawMessage(`{"desired":"direct"}`)}
	if err := writeFrame(&b, request); err != nil {
		t.Fatal(err)
	}
	var actual Request
	if err := readFrame(&b, &actual); err != nil || actual.ID != request.ID || !bytes.Equal(actual.Payload, request.Payload) {
		t.Fatal(actual, err)
	}
	if err := writeFrame(&b, Response{Payload: json.RawMessage(`"` + strings.Repeat("a", MaxFrame) + `"`)}); err == nil {
		t.Fatal("unbounded response")
	}
}
