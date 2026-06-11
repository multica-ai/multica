package transport

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("bad hex %q: %v", s, err)
	}
	return b
}

// Golden frames generated from the TypeScript Encoder (cc-channel-octo).
func TestEncodeConnectPacket_Golden(t *testing.T) {
	got := encodeConnectPacket(connectOptions{
		version:         protoVersion,
		deviceFlag:      0,
		deviceID:        "dev123W",
		uid:             "uid_bot",
		token:           "tok_abc",
		clientTimestamp: 1700000000000,
		clientKey:       "PUBKEYB64",
	})
	want := mustHex(t, "1030040000076465763132335700077569645f626f740007746f6b5f6162630000018bcfe5680000095055424b4559423634")
	if !bytes.Equal(got, want) {
		t.Errorf("CONNECT frame mismatch:\n got  %x\n want %x", got, want)
	}
}

func TestEncodePingPacket_Golden(t *testing.T) {
	if got := encodePingPacket(); !bytes.Equal(got, mustHex(t, "70")) {
		t.Errorf("PING mismatch: got %x", got)
	}
}

func TestEncodeRecvackPacket_Golden(t *testing.T) {
	got, err := encodeRecvackPacket("123456789012345", 42)
	if err != nil {
		t.Fatalf("encodeRecvack: %v", err)
	}
	want := mustHex(t, "600c00007048860ddf790000002a")
	if !bytes.Equal(got, want) {
		t.Errorf("RECVACK mismatch:\n got  %x\n want %x", got, want)
	}
}

func TestEncodeVarLen_Golden(t *testing.T) {
	cases := []struct {
		n   int
		hex string
	}{
		{0, "00"},
		{127, "7f"},
		{128, "8001"},
		{300, "ac02"},
		{16384, "808001"},
	}
	for _, c := range cases {
		if got := encodeVarLen(c.n); !bytes.Equal(got, mustHex(t, c.hex)) {
			t.Errorf("varlen(%d): got %x want %s", c.n, got, c.hex)
		}
	}
}

func TestEncoderDecoder_RoundTrip(t *testing.T) {
	e := &encoder{}
	e.PutByte(0xAB)
	e.WriteUint16(0x1234)
	e.WriteUint32(0xDEADBEEF)
	e.WriteUint64(0x0102030405060708)
	e.WriteString("héllo") // multibyte: byte length != char length

	d := newDecoder(e.Bytes())
	if b, _ := d.ReadByte(); b != 0xAB {
		t.Errorf("byte: got %x", b)
	}
	if v, _ := d.ReadUint16(); v != 0x1234 {
		t.Errorf("u16: got %x", v)
	}
	if v, _ := d.ReadUint32(); v != 0xDEADBEEF {
		t.Errorf("u32: got %x", v)
	}
	if v, _ := d.ReadUint64(); v != 0x0102030405060708 {
		t.Errorf("u64: got %x", v)
	}
	if s, _ := d.ReadString(); s != "héllo" {
		t.Errorf("string: got %q", s)
	}
}

func TestDecoder_OutOfBounds(t *testing.T) {
	d := newDecoder([]byte{0x01})
	if _, err := d.ReadUint32(); err == nil {
		t.Error("expected out-of-bounds error reading u32 from 1-byte buffer")
	}
}

func TestDecoder_ReadString_OversizedLength(t *testing.T) {
	// Declares length 0xFFFF but has no body — must error, not over-read.
	d := newDecoder([]byte{0xFF, 0xFF})
	if _, err := d.ReadString(); err == nil {
		t.Error("expected error for oversized declared string length")
	}
}

// collectFrames runs unpackOne across a buffer the way handleRawData does and
// returns each complete packet's bytes.
func collectFrames(t *testing.T, buf []byte) ([][]byte, int, error) {
	t.Helper()
	var frames [][]byte
	consumed := 0
	for {
		used, err := unpackOne(buf, consumed, func(p []byte) {
			cp := make([]byte, len(p))
			copy(cp, p)
			frames = append(frames, cp)
		})
		if err != nil {
			return frames, consumed, err
		}
		if used == 0 {
			break
		}
		consumed += used
	}
	return frames, consumed, nil
}

func TestUnpackOne_StickyPackets(t *testing.T) {
	// Two PINGs and one RECVACK concatenated into one buffer.
	recvack, _ := encodeRecvackPacket("123456789012345", 42)
	buf := append([]byte{}, encodePingPacket()...)
	buf = append(buf, recvack...)
	buf = append(buf, encodePingPacket()...)

	frames, consumed, err := collectFrames(t, buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(frames) != 3 {
		t.Fatalf("expected 3 frames, got %d", len(frames))
	}
	if consumed != len(buf) {
		t.Errorf("consumed %d, want %d", consumed, len(buf))
	}
}

func TestUnpackOne_PartialPacket(t *testing.T) {
	// A RECVACK split across two reads: first half yields nothing, full yields one.
	recvack, _ := encodeRecvackPacket("123456789012345", 42)
	half := recvack[:len(recvack)-3]

	frames, consumed, err := collectFrames(t, half)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(frames) != 0 || consumed != 0 {
		t.Fatalf("partial packet should yield nothing, got %d frames / consumed %d", len(frames), consumed)
	}

	frames, consumed, err = collectFrames(t, recvack)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(frames) != 1 || consumed != len(recvack) {
		t.Errorf("full packet: got %d frames / consumed %d", len(frames), consumed)
	}
}

func TestUnpackOne_VarLenOverrun(t *testing.T) {
	// Header + 4 continuation bytes (all 0x80) → malformed, must error.
	buf := []byte{byte(pktConnack) << 4, 0x80, 0x80, 0x80, 0x80, 0x80}
	_, _, err := collectFrames(t, buf)
	if err == nil {
		t.Error("expected malformed-varlen error")
	}
}
