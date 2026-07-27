package lark

import (
	"bytes"
	"encoding/hex"
	"testing"
)

// TestFrameRoundTripPreservesAllFields ensures every set field on the
// outbound Frame survives marshal+unmarshal. This catches symmetric
// bugs (where our marshal and unmarshal agree but neither matches the
// SDK); the golden-byte tests below are the actual byte-compat check.
func TestFrameRoundTripPreservesAllFields(t *testing.T) {
	t.Parallel()
	in := &Frame{
		SeqID:           42,
		LogID:           99,
		Service:         7,
		Method:          FrameMethodData,
		Headers:         []FrameHeader{{Key: "type", Value: "event"}, {Key: "message_id", Value: "om-1"}},
		PayloadEncoding: "json",
		PayloadType:     "im.message.receive_v1",
		Payload:         []byte(`{"schema":"2.0"}`),
		LogIDNew:        "log-new",
	}
	out, err := UnmarshalFrame(in.Marshal())
	if err != nil {
		t.Fatalf("UnmarshalFrame: %v", err)
	}
	if out.SeqID != in.SeqID || out.LogID != in.LogID || out.Service != in.Service || out.Method != in.Method {
		t.Errorf("scalar fields differ: in=%+v out=%+v", in, out)
	}
	if len(out.Headers) != len(in.Headers) {
		t.Fatalf("Headers len = %d; want %d", len(out.Headers), len(in.Headers))
	}
	for i, h := range out.Headers {
		if h != in.Headers[i] {
			t.Errorf("Headers[%d] = %+v; want %+v", i, h, in.Headers[i])
		}
	}
	if out.PayloadEncoding != in.PayloadEncoding {
		t.Errorf("PayloadEncoding = %q; want %q", out.PayloadEncoding, in.PayloadEncoding)
	}
	if out.PayloadType != in.PayloadType {
		t.Errorf("PayloadType = %q; want %q", out.PayloadType, in.PayloadType)
	}
	if !bytes.Equal(out.Payload, in.Payload) {
		t.Errorf("Payload = %q; want %q", string(out.Payload), string(in.Payload))
	}
	if out.LogIDNew != in.LogIDNew {
		t.Errorf("LogIDNew = %q; want %q", out.LogIDNew, in.LogIDNew)
	}
}

// TestFrameMarshalIsSDKByteCompatible pins the exact byte sequences the
// official SDK's pbbp2.Frame.MarshalToSizedBuffer would produce for the
// canonical frames we emit on the wire (ping, pong, ACK, full event).
//
// These goldens were computed by hand-replicating the SDK's MarshalTo
// SizedBuffer logic (which itself is gogo-generated code from
// pbbp2.proto, see the SDK at v3_main/ws/pbbp2.pb.go) — reversed byte
// build, proto2 req fields emitted unconditionally, gogo opt strings
// emitted unconditionally with zero-length when empty, opt Payload
// gated by nil. Any divergence between our Marshal() and these bytes
// means Lark's server will reject the frame as a RequiredNotSetError.
//
// DO NOT alter the goldens to match a refactor of Marshal(). Goldens
// pin SDK behaviour; if they drift, either the SDK changed or our
// implementation broke compatibility. Verify against the SDK source
// before touching them.
func TestFrameMarshalIsSDKByteCompatible(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		frame    *Frame
		expected string // hex
	}{
		{
			name:  "ping_frame",
			frame: NewPingFrame(7),
			// Field 1 (SeqID=0): 08 00
			// Field 2 (LogID=0): 10 00
			// Field 3 (Service=7): 18 07
			// Field 4 (Method=0/Control): 20 00
			// Field 5 (one header "type"="ping"): 2a 0c 0a 04 74 79 70 65 12 04 70 69 6e 67
			// Field 6 (PayloadEncoding=""): 32 00
			// Field 7 (PayloadType=""): 3a 00
			// Field 8 (Payload nil): omitted
			// Field 9 (LogIDNew=""): 4a 00
			expected: "08001000180720002a0c0a0474797065120470696e6732003a004a00",
		},
		{
			name:     "pong_frame_service_42",
			frame:    NewPongFrame(42),
			expected: "08001000182a20002a0c0a04747970651204706f6e6732003a004a00",
		},
		{
			name:  "ack_data_frame",
			frame: NewAckFrame(&Frame{
				Method:  FrameMethodData,
				Service: 7,
				Headers: []FrameHeader{
					{Key: FrameHeaderTypeKey, Value: FrameHeaderTypeEvent},
					{Key: FrameHeaderMessageIDKey, Value: "om-42"},
				},
			}, true),
			// Payload bytes are the JSON {"code":200,"headers":null,"data":null}
			// which the SDK's NewResponseByCode + json.Marshal produces.
			expected: "08001000180720012a0d0a047479706512056576656e742a130a0a6d6573736167655f696412056f6d2d343232003a0042277b22636f6465223a3230302c2268656164657273223a6e756c6c2c2264617461223a6e756c6c7d4a00",
		},
		{
			name: "full_data_frame_all_fields",
			frame: &Frame{
				SeqID:           42,
				LogID:           99,
				Service:         7,
				Method:          FrameMethodData,
				Headers:         []FrameHeader{{Key: "type", Value: "event"}, {Key: "message_id", Value: "om-1"}},
				PayloadEncoding: "json",
				PayloadType:     "im.message.receive_v1",
				Payload:         []byte(`{"schema":"2.0"}`),
				LogIDNew:        "log-new",
			},
			expected: "082a1063180720012a0d0a047479706512056576656e742a120a0a6d6573736167655f696412046f6d2d3132046a736f6e3a15696d2e6d6573736167652e726563656976655f763142107b22736368656d61223a22322e30227d4a076c6f672d6e6577",
		},
		{
			// Zero-valued frame still has all req + opt-string fields
			// emitted; only Payload (nil) is omitted. Matches the SDK
			// "no fields set" baseline.
			name:     "zero_frame_emits_required_and_opt_strings",
			frame:    &Frame{},
			expected: "080010001800200032003a004a00",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := c.frame.Marshal()
			want, err := hex.DecodeString(c.expected)
			if err != nil {
				t.Fatalf("decode golden hex: %v", err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("Marshal bytes mismatch\n  got:  %s\n  want: %s",
					hex.EncodeToString(got), c.expected)
			}
		})
	}
}

// TestFrameMarshalEmitsRequiredZeroFields verifies the proto2 `req`
// semantics: SeqID/LogID/Service/Method must appear in the byte stream
// even when their values are zero. A previous implementation skipped
// these on zero (proto3 semantics), which would cause Lark's server to
// reject every ping frame.
func TestFrameMarshalEmitsRequiredZeroFields(t *testing.T) {
	t.Parallel()
	raw := (&Frame{}).Marshal()
	// Required fields are written as: tag(1byte) + value-varint.
	// All zero values varint-encode to a single 0x00 byte.
	// So we expect the prefix to contain field 1..4 tags + 0x00 each.
	wantPrefix := []byte{
		0x08, 0x00, // field 1 (SeqID=0)
		0x10, 0x00, // field 2 (LogID=0)
		0x18, 0x00, // field 3 (Service=0)
		0x20, 0x00, // field 4 (Method=0)
	}
	if !bytes.HasPrefix(raw, wantPrefix) {
		t.Errorf("required zero fields missing\n  got prefix: %x\n  want: %x",
			raw[:min(len(raw), len(wantPrefix))], wantPrefix)
	}
}

func TestFrameMarshalPayloadNilVsEmpty(t *testing.T) {
	t.Parallel()
	// Nil payload: field 8 omitted entirely.
	noPayload := (&Frame{}).Marshal()
	if bytes.Contains(noPayload, []byte{0x42}) {
		t.Errorf("nil Payload should not emit field 8 tag; got %x", noPayload)
	}
	// Empty (non-nil) payload: field 8 emitted with zero length.
	emptyPayload := (&Frame{Payload: []byte{}}).Marshal()
	// Look for "42 00" (tag, length=0) appearing between PayloadType
	// (field 7, tag=0x3a) and LogIDNew (field 9, tag=0x4a).
	if !bytes.Contains(emptyPayload, []byte{0x3a, 0x00, 0x42, 0x00, 0x4a, 0x00}) {
		t.Errorf("empty non-nil Payload should emit tag+0-length; got %x", emptyPayload)
	}
}

func TestNewAckFrameReusesInboundHeaders(t *testing.T) {
	t.Parallel()
	inbound := &Frame{
		Method:  FrameMethodData,
		Service: 7,
		Headers: []FrameHeader{
			{Key: FrameHeaderTypeKey, Value: FrameHeaderTypeEvent},
			{Key: FrameHeaderMessageIDKey, Value: "om-42"},
		},
	}
	ack := NewAckFrame(inbound, true)
	if ack.Method != inbound.Method || ack.Service != inbound.Service {
		t.Errorf("ack method/service mismatch")
	}
	if len(ack.Headers) != len(inbound.Headers) {
		t.Errorf("ack headers length mismatch")
	}
	if ack.HeaderValue(FrameHeaderMessageIDKey) != "om-42" {
		t.Errorf("ack should echo message_id; got %q", ack.HeaderValue(FrameHeaderMessageIDKey))
	}
	if !bytes.Contains(ack.Payload, []byte(`"code":200`)) {
		t.Errorf("ack payload missing code=200: %s", string(ack.Payload))
	}
	if !bytes.Contains(ack.Payload, []byte(`"headers":null`)) {
		t.Errorf("ack payload should have null headers (SDK shape): %s", string(ack.Payload))
	}
	if !bytes.Contains(ack.Payload, []byte(`"data":null`)) {
		t.Errorf("ack payload should have null data (SDK shape): %s", string(ack.Payload))
	}

	nack := NewAckFrame(inbound, false)
	if !bytes.Contains(nack.Payload, []byte(`"code":500`)) {
		t.Errorf("nack payload missing code=500: %s", string(nack.Payload))
	}
}

func TestUnmarshalFrameRejectsTruncatedBuffer(t *testing.T) {
	t.Parallel()
	if _, err := UnmarshalFrame(nil); err == nil {
		t.Error("expected error on empty buffer")
	}
	if _, err := UnmarshalFrame([]byte{0x08}); err == nil {
		t.Error("expected error on truncated varint")
	}
}

func TestUnmarshalFrameSkipsUnknownFields(t *testing.T) {
	t.Parallel()
	buf := []byte{}
	// field 3 (varint): tag = 3<<3|0 = 0x18, value = 5
	buf = append(buf, 0x18, 0x05)
	// field 31 (varint): tag = 31<<3|0 = 0xF8 0x01, value = 99 (0x63)
	buf = append(buf, 0xF8, 0x01, 0x63)
	f, err := UnmarshalFrame(buf)
	if err != nil {
		t.Fatalf("expected unknown field to be skipped, got error: %v", err)
	}
	if f.Service != 5 {
		t.Errorf("Service = %d; want 5", f.Service)
	}
}
