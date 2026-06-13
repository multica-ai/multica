package lark

import (
	"errors"
	"fmt"

	"google.golang.org/protobuf/encoding/protowire"
)

// Wire-compatible re-implementation of the Lark/Feishu long-connection
// binary Frame envelope. The encoded bytes are byte-identical to what
// the official SDK's github.com/larksuite/oapi-sdk-go/v3/ws Frame
// produces, which matters because Lark's server rejects frames whose
// required fields are missing — and the SDK is generated from
// pbbp2.proto using proto2 + gogo `req` semantics, so SeqID / LogID /
// Service / Method are emitted unconditionally (even when zero) and the
// opt string fields (PayloadEncoding / PayloadType / LogIDNew) are also
// emitted unconditionally because gogo's generated code skips the
// zero-guard for opt strings on this message. Only Payload uses the
// `nil` guard. See the SDK's pbbp2.pb.go MarshalToSizedBuffer for the
// reference; mismatches against that function silently corrupt the
// stream because round-tripping against our own UnmarshalFrame masks
// the bug.
//
// We re-encode by hand using google.golang.org/protobuf/encoding/protowire
// rather than importing the official SDK's pbbp2 package: the SDK has
// a large transitive dependency tree (full open-platform client,
// generated code for every API surface) and we only need this one
// message type. Hand-rolling the encode/decode for a 9-field message
// is bounded while the dependency surface stays small. The golden
// tests in ws_frame_test.go pin the exact byte sequence the SDK would
// produce for a handful of canonical frames; that is the load-bearing
// compatibility check.

const (
	// FrameMethodControl identifies a frame whose Method=Control(0).
	// Control frames carry ping/pong and server-pushed ClientConfig
	// updates; they never carry an inbound event payload.
	FrameMethodControl int32 = 0

	// FrameMethodData identifies a frame whose Method=Data(1). Data
	// frames carry the actual event payload (im.message.receive_v1,
	// card interaction, etc.) and require an ACK response.
	FrameMethodData int32 = 1
)

// FrameHeaderType enumerates the values Lark puts in the Frame
// header keyed `type`. These drive per-frame routing logic.
const (
	FrameHeaderTypeKey   = "type"
	FrameHeaderTypeEvent = "event"
	FrameHeaderTypeCard  = "card"
	FrameHeaderTypePing  = "ping"
	FrameHeaderTypePong  = "pong"

	// FrameHeaderMessageIDKey is the dedup / chunk key Lark sets on
	// each data frame; reused as-is in the ACK so server can correlate.
	FrameHeaderMessageIDKey = "message_id"

	// FrameHeaderSumKey / FrameHeaderSeqKey carry chunking metadata
	// for multi-frame payloads (sum>1 means N chunks indexed by seq).
	// The chunk assembler in ws_chunk_assembler.go uses these to
	// reassemble a single JSON payload from multiple Frames before
	// invoking the decoder.
	FrameHeaderSumKey = "sum"
	FrameHeaderSeqKey = "seq"
)

// FrameHeader is one (key, value) pair in Frame.Headers. Equivalent to
// the SDK's pbbp2.Header.
type FrameHeader struct {
	Key   string
	Value string
}

// Frame mirrors pbbp2.Frame. Field numbers match the SDK proto so the
// on-wire bytes are byte-identical to what oapi-sdk-go produces.
//
// The Marshal implementation matches the SDK's gogo-generated code:
// SeqID, LogID, Service, Method are emitted unconditionally as proto2
// required fields; PayloadEncoding, PayloadType, LogIDNew are emitted
// unconditionally with zero-length values when empty (matches the
// generated code's unconditional copy + length-encoding); Payload is
// emitted only when non-nil. The golden tests pin exact byte sequences.
type Frame struct {
	SeqID           uint64        // proto field 1 (req)
	LogID           uint64        // proto field 2 (req)
	Service         int32         // proto field 3 (req)
	Method          int32         // proto field 4 (req)
	Headers         []FrameHeader // proto field 5 (rep)
	PayloadEncoding string        // proto field 6 (opt)
	PayloadType     string        // proto field 7 (opt)
	Payload         []byte        // proto field 8 (opt)
	LogIDNew        string        // proto field 9 (opt)
}

// HeaderValue returns the value for the first header with the supplied
// key, or "" if absent. Lark uses headers as a flat map, but the SDK's
// proto schema is a repeated field — we treat duplicates as "first
// wins" because that's what the SDK does in practice.
func (f *Frame) HeaderValue(key string) string {
	for _, h := range f.Headers {
		if h.Key == key {
			return h.Value
		}
	}
	return ""
}

// Marshal encodes the frame to the wire format Lark expects. The
// returned bytes are sent verbatim as the WebSocket binary payload.
//
// Field emission order matches the SDK's MarshalToSizedBuffer reverse
// build (fields 1→9 in the final byte stream). DO NOT change which
// fields are emitted unconditionally without re-checking the SDK's
// generated MarshalToSizedBuffer — it is the authority on wire shape
// and divergence is invisible until Lark's server starts dropping
// frames in production.
func (f *Frame) Marshal() []byte {
	buf := make([]byte, 0, 64+len(f.Payload))

	// Required fields (proto2 req): always emit tag + varint, even when
	// the value is zero. The SDK's generated code unconditionally writes
	// these; a Lark server reading a Frame missing one of them returns
	// a RequiredNotSetError and discards the frame.
	buf = protowire.AppendTag(buf, 1, protowire.VarintType)
	buf = protowire.AppendVarint(buf, f.SeqID)
	buf = protowire.AppendTag(buf, 2, protowire.VarintType)
	buf = protowire.AppendVarint(buf, f.LogID)
	buf = protowire.AppendTag(buf, 3, protowire.VarintType)
	buf = protowire.AppendVarint(buf, uint64(uint32(f.Service)))
	buf = protowire.AppendTag(buf, 4, protowire.VarintType)
	buf = protowire.AppendVarint(buf, uint64(uint32(f.Method)))

	// Repeated headers — emit one length-prefixed entry per FrameHeader.
	// Empty Headers list emits nothing, matching the SDK guard.
	for _, h := range f.Headers {
		buf = protowire.AppendTag(buf, 5, protowire.BytesType)
		buf = protowire.AppendVarint(buf, uint64(headerSize(h)))
		// Header.Key (field 1) and Header.Value (field 2) are both
		// proto2 req — emit unconditionally.
		buf = protowire.AppendTag(buf, 1, protowire.BytesType)
		buf = protowire.AppendString(buf, h.Key)
		buf = protowire.AppendTag(buf, 2, protowire.BytesType)
		buf = protowire.AppendString(buf, h.Value)
	}

	// PayloadEncoding (field 6) — gogo's generated code copies the
	// string unconditionally and emits the tag + length prefix even
	// when len==0. Empty string still produces tag + zero-length.
	buf = protowire.AppendTag(buf, 6, protowire.BytesType)
	buf = protowire.AppendString(buf, f.PayloadEncoding)

	// PayloadType (field 7) — same unconditional emission as PayloadEncoding.
	buf = protowire.AppendTag(buf, 7, protowire.BytesType)
	buf = protowire.AppendString(buf, f.PayloadType)

	// Payload (field 8) — the SDK uses `if m.Payload != nil`. We mirror
	// that: nil omits the tag entirely; non-nil emits, even if len==0.
	if f.Payload != nil {
		buf = protowire.AppendTag(buf, 8, protowire.BytesType)
		buf = protowire.AppendBytes(buf, f.Payload)
	}

	// LogIDNew (field 9) — unconditional like PayloadEncoding/Type.
	buf = protowire.AppendTag(buf, 9, protowire.BytesType)
	buf = protowire.AppendString(buf, f.LogIDNew)

	return buf
}

func headerSize(h FrameHeader) int {
	// Both fields are req and always emitted; their byte cost is
	// tag + length-prefix(len) + len.
	return protowire.SizeTag(1) + protowire.SizeBytes(len(h.Key)) +
		protowire.SizeTag(2) + protowire.SizeBytes(len(h.Value))
}

// UnmarshalFrame parses one binary protobuf message into a Frame.
// Unknown fields are skipped (proto3 behaviour) so server-side schema
// additions do not break us. Truncated / malformed bytes return an
// error and the caller (the WS connector) treats the frame as bad
// and drops it without tearing down the connection.
func UnmarshalFrame(b []byte) (*Frame, error) {
	if len(b) == 0 {
		return nil, errors.New("ws frame: empty buffer")
	}
	f := &Frame{}
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if err := protowire.ParseError(n); err != nil {
			return nil, fmt.Errorf("ws frame: consume tag: %w", err)
		}
		b = b[n:]
		switch num {
		case 1: // SeqID uint64
			if typ != protowire.VarintType {
				return nil, fmt.Errorf("ws frame: field 1 expects varint, got %v", typ)
			}
			v, m := protowire.ConsumeVarint(b)
			if err := protowire.ParseError(m); err != nil {
				return nil, fmt.Errorf("ws frame: consume seq_id: %w", err)
			}
			f.SeqID = v
			b = b[m:]
		case 2: // LogID uint64
			if typ != protowire.VarintType {
				return nil, fmt.Errorf("ws frame: field 2 expects varint, got %v", typ)
			}
			v, m := protowire.ConsumeVarint(b)
			if err := protowire.ParseError(m); err != nil {
				return nil, fmt.Errorf("ws frame: consume log_id: %w", err)
			}
			f.LogID = v
			b = b[m:]
		case 3: // Service int32
			if typ != protowire.VarintType {
				return nil, fmt.Errorf("ws frame: field 3 expects varint, got %v", typ)
			}
			v, m := protowire.ConsumeVarint(b)
			if err := protowire.ParseError(m); err != nil {
				return nil, fmt.Errorf("ws frame: consume service: %w", err)
			}
			f.Service = int32(v)
			b = b[m:]
		case 4: // Method int32
			if typ != protowire.VarintType {
				return nil, fmt.Errorf("ws frame: field 4 expects varint, got %v", typ)
			}
			v, m := protowire.ConsumeVarint(b)
			if err := protowire.ParseError(m); err != nil {
				return nil, fmt.Errorf("ws frame: consume method: %w", err)
			}
			f.Method = int32(v)
			b = b[m:]
		case 5: // Headers (repeated)
			if typ != protowire.BytesType {
				return nil, fmt.Errorf("ws frame: field 5 expects bytes, got %v", typ)
			}
			hb, m := protowire.ConsumeBytes(b)
			if err := protowire.ParseError(m); err != nil {
				return nil, fmt.Errorf("ws frame: consume header: %w", err)
			}
			h, herr := unmarshalHeader(hb)
			if herr != nil {
				return nil, herr
			}
			f.Headers = append(f.Headers, h)
			b = b[m:]
		case 6: // PayloadEncoding string
			if typ != protowire.BytesType {
				return nil, fmt.Errorf("ws frame: field 6 expects bytes, got %v", typ)
			}
			s, m := protowire.ConsumeString(b)
			if err := protowire.ParseError(m); err != nil {
				return nil, fmt.Errorf("ws frame: consume payload_encoding: %w", err)
			}
			f.PayloadEncoding = s
			b = b[m:]
		case 7: // PayloadType string
			if typ != protowire.BytesType {
				return nil, fmt.Errorf("ws frame: field 7 expects bytes, got %v", typ)
			}
			s, m := protowire.ConsumeString(b)
			if err := protowire.ParseError(m); err != nil {
				return nil, fmt.Errorf("ws frame: consume payload_type: %w", err)
			}
			f.PayloadType = s
			b = b[m:]
		case 8: // Payload bytes
			if typ != protowire.BytesType {
				return nil, fmt.Errorf("ws frame: field 8 expects bytes, got %v", typ)
			}
			raw, m := protowire.ConsumeBytes(b)
			if err := protowire.ParseError(m); err != nil {
				return nil, fmt.Errorf("ws frame: consume payload: %w", err)
			}
			// Copy out so the Frame outlives the input buffer
			// (ConsumeBytes returns a sub-slice).
			f.Payload = append([]byte(nil), raw...)
			b = b[m:]
		case 9: // LogIDNew string
			if typ != protowire.BytesType {
				return nil, fmt.Errorf("ws frame: field 9 expects bytes, got %v", typ)
			}
			s, m := protowire.ConsumeString(b)
			if err := protowire.ParseError(m); err != nil {
				return nil, fmt.Errorf("ws frame: consume log_id_new: %w", err)
			}
			f.LogIDNew = s
			b = b[m:]
		default:
			m := protowire.ConsumeFieldValue(num, typ, b)
			if err := protowire.ParseError(m); err != nil {
				return nil, fmt.Errorf("ws frame: skip unknown field %d: %w", num, err)
			}
			b = b[m:]
		}
	}
	return f, nil
}

func unmarshalHeader(b []byte) (FrameHeader, error) {
	var h FrameHeader
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if err := protowire.ParseError(n); err != nil {
			return FrameHeader{}, fmt.Errorf("ws frame: header tag: %w", err)
		}
		b = b[n:]
		switch num {
		case 1:
			if typ != protowire.BytesType {
				return FrameHeader{}, fmt.Errorf("ws frame: header.key expects bytes, got %v", typ)
			}
			s, m := protowire.ConsumeString(b)
			if err := protowire.ParseError(m); err != nil {
				return FrameHeader{}, fmt.Errorf("ws frame: header.key: %w", err)
			}
			h.Key = s
			b = b[m:]
		case 2:
			if typ != protowire.BytesType {
				return FrameHeader{}, fmt.Errorf("ws frame: header.value expects bytes, got %v", typ)
			}
			s, m := protowire.ConsumeString(b)
			if err := protowire.ParseError(m); err != nil {
				return FrameHeader{}, fmt.Errorf("ws frame: header.value: %w", err)
			}
			h.Value = s
			b = b[m:]
		default:
			m := protowire.ConsumeFieldValue(num, typ, b)
			if err := protowire.ParseError(m); err != nil {
				return FrameHeader{}, fmt.Errorf("ws frame: skip header field %d: %w", num, err)
			}
			b = b[m:]
		}
	}
	return h, nil
}

// NewPingFrame builds the client-side keepalive frame. Lark's long
// connection uses an app-layer ping (binary Frame with type=ping),
// NOT a WebSocket protocol-level PING — gorilla's WriteControl pings
// would be ignored.
func NewPingFrame(serviceID int32) *Frame {
	return &Frame{
		Method:  FrameMethodControl,
		Service: serviceID,
		Headers: []FrameHeader{{Key: FrameHeaderTypeKey, Value: FrameHeaderTypePing}},
	}
}

// NewPongFrame builds the client-side response to a server-initiated
// ping. Lark may push ping frames at any cadence; we reply in kind.
func NewPongFrame(serviceID int32) *Frame {
	return &Frame{
		Method:  FrameMethodControl,
		Service: serviceID,
		Headers: []FrameHeader{{Key: FrameHeaderTypeKey, Value: FrameHeaderTypePong}},
	}
}

// NewAckFrame builds the ACK response for an inbound data frame.
// Per the SDK, the ACK reuses the inbound frame's Headers verbatim
// (so the server can correlate by message_id) and writes a JSON-
// encoded Response struct as the Payload.
//
// codeOK is true on successful dispatch (Response.code=200); false
// surfaces 500 to the server (it will retry the event). The payload
// shape mirrors the SDK's NewResponseByCode JSON: a nil Headers map
// and nil Data slice both marshal to JSON null in stdlib encoding/json,
// which is what the server expects to receive.
func NewAckFrame(inbound *Frame, codeOK bool) *Frame {
	code := 200
	if !codeOK {
		code = 500
	}
	payload := fmt.Sprintf(`{"code":%d,"headers":null,"data":null}`, code)
	return &Frame{
		Method:  inbound.Method,
		Service: inbound.Service,
		Headers: inbound.Headers,
		Payload: []byte(payload),
	}
}
