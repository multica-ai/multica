// Package transport implements the WuKongIM binary protocol used by Octo IM, plus a
// REST client for the Octo bot API. It is the transport layer for the Octo
// integration: a User Bot (bf_* token) registers via REST to obtain an im_token
// and ws_url, opens a WuKongIM WebSocket long connection to receive messages,
// and sends replies back over REST.
//
// Ported from cc-channel-octo/src/octo/{socket,types,api}.ts (the TypeScript
// reference implementation). See PORTING.md for the function-by-function map.
package transport

import (
	"encoding/binary"
	"fmt"
)

// Framing limits. A malicious or buggy server can dribble partial packets
// forever (e.g. an unterminated variable-length encoding) and exhaust memory;
// these caps bound the damage and force a reconnect instead.
const (
	// maxTempBufferBytes caps the inbound reassembly buffer. 1 MiB is far above
	// any legitimate single packet — crossing it means the stream is malformed.
	maxTempBufferBytes = 1 << 20

	// maxVarLenBytes caps a single variable-length integer (MQTT remaining
	// length). More than 4 continuation bytes is malformed.
	maxVarLenBytes = 4
)

// encoder writes WuKongIM frame fields in big-endian order. Strings are
// length-prefixed with a uint16 byte count (UTF-8 bytes, not characters).
type encoder struct {
	buf []byte
}

func (e *encoder) PutByte(b byte) {
	e.buf = append(e.buf, b)
}

func (e *encoder) WriteBytes(b []byte) {
	e.buf = append(e.buf, b...)
}

func (e *encoder) WriteUint16(v uint16) {
	e.buf = binary.BigEndian.AppendUint16(e.buf, v)
}

func (e *encoder) WriteUint32(v uint32) {
	e.buf = binary.BigEndian.AppendUint32(e.buf, v)
}

// WriteUint64 writes an 8-byte big-endian value. WuKongIM message IDs are
// unsigned 64-bit, so callers pass them as uint64.
func (e *encoder) WriteUint64(v uint64) {
	e.buf = binary.BigEndian.AppendUint64(e.buf, v)
}

// WriteString writes a uint16 length prefix (UTF-8 byte count) followed by the
// bytes. An empty string writes a zero length.
func (e *encoder) WriteString(s string) {
	b := []byte(s)
	e.WriteUint16(uint16(len(b)))
	e.buf = append(e.buf, b...)
}

// Bytes returns the accumulated frame.
func (e *encoder) Bytes() []byte {
	return e.buf
}

// decoder reads WuKongIM frame fields. Every read is bounds-checked via require
// so a truncated or malformed packet returns an error instead of silently
// reading zero (which would corrupt message IDs / seqs and break ack matching).
type decoder struct {
	data   []byte
	offset int
}

// newDecoder wraps a complete packet's bytes for field-by-field reading.
func newDecoder(data []byte) *decoder {
	return &decoder{data: data}
}

func (d *decoder) require(n int) error {
	if d.offset+n > len(d.data) {
		return fmt.Errorf("im decoder: out-of-bounds read (need %d byte(s) at offset %d, have %d)", n, d.offset, len(d.data))
	}
	return nil
}

func (d *decoder) ReadByte() (byte, error) {
	if err := d.require(1); err != nil {
		return 0, err
	}
	b := d.data[d.offset]
	d.offset++
	return b, nil
}

func (d *decoder) ReadUint16() (uint16, error) {
	if err := d.require(2); err != nil {
		return 0, err
	}
	v := binary.BigEndian.Uint16(d.data[d.offset:])
	d.offset += 2
	return v, nil
}

func (d *decoder) ReadUint32() (uint32, error) {
	if err := d.require(4); err != nil {
		return 0, err
	}
	v := binary.BigEndian.Uint32(d.data[d.offset:])
	d.offset += 4
	return v, nil
}

// ReadUint64 reads an 8-byte big-endian unsigned integer.
func (d *decoder) ReadUint64() (uint64, error) {
	if err := d.require(8); err != nil {
		return 0, err
	}
	v := binary.BigEndian.Uint64(d.data[d.offset:])
	d.offset += 8
	return v, nil
}

// ReadString reads a uint16-length-prefixed UTF-8 string. The declared length
// is validated against the remaining buffer so an oversized length field cannot
// over-read and corrupt every subsequent field.
func (d *decoder) ReadString() (string, error) {
	n, err := d.ReadUint16()
	if err != nil {
		return "", err
	}
	if n == 0 {
		return "", nil
	}
	if err := d.require(int(n)); err != nil {
		return "", err
	}
	s := string(d.data[d.offset : d.offset+int(n)])
	d.offset += int(n)
	return s, nil
}

// ReadRemaining returns all unread bytes from the current offset to the end
// (the encrypted payload of a RECV packet).
func (d *decoder) ReadRemaining() []byte {
	r := d.data[d.offset:]
	d.offset = len(d.data)
	return r
}

// readVarLen reads an MQTT-style variable-length integer, used to skip the
// remaining-length field of a packet that has already been framed. It enforces
// the maxVarLenBytes cap.
func (d *decoder) readVarLen() (int, error) {
	value := 0
	multiplier := 1
	for i := 0; i < maxVarLenBytes; i++ {
		b, err := d.ReadByte()
		if err != nil {
			return 0, err
		}
		value += int(b&127) * multiplier
		if b&0x80 == 0 {
			return value, nil
		}
		multiplier *= 128
	}
	return 0, fmt.Errorf("im: variable-length encoding exceeded %d bytes", maxVarLenBytes)
}

// encodeVarLen encodes an integer as an MQTT-style variable-length quantity
// (7 bits per byte, high bit signals continuation).
func encodeVarLen(n int) []byte {
	if n == 0 {
		return []byte{0}
	}
	var ret []byte
	for n > 0 {
		digit := n % 0x80
		n /= 0x80
		if n > 0 {
			digit |= 0x80
		}
		ret = append(ret, byte(digit))
	}
	return ret
}

// unpackOne parses a single packet from data starting at start. It returns the
// number of bytes consumed, or 0 if the buffer does not yet hold a complete
// packet (the caller should wait for more bytes). It reads only this packet's
// bytes — never the whole buffer — so repeated calls over a large buffer stay
// O(n) total. The handler callback receives each complete packet's bytes.
//
// A malformed frame (variable-length overrun) returns an error so the caller
// can drop the buffer and reconnect.
func unpackOne(data []byte, start int, handle func(packet []byte)) (int, error) {
	available := len(data) - start
	if available <= 0 {
		return 0, nil
	}

	header := data[start]
	packetType := PacketType(header >> 4)

	// PONG and PING are single-byte frames with no body.
	if packetType == pktPong || packetType == pktPing {
		handle(data[start : start+1])
		return 1, nil
	}

	const fixedHeaderLength = 1
	pos := start + fixedHeaderLength
	remLength := 0
	multiplier := 1
	hasMore := false

	for {
		if pos > len(data)-1 {
			// Variable-length field not fully received yet — wait for more bytes.
			return 0, nil
		}
		if pos-(start+fixedHeaderLength) >= maxVarLenBytes {
			return 0, fmt.Errorf("im: variable-length encoding exceeded %d bytes — malformed packet", maxVarLenBytes)
		}
		digit := data[pos]
		pos++
		remLength += int(digit&127) * multiplier
		multiplier *= 128
		hasMore = digit&0x80 != 0
		if !hasMore {
			break
		}
	}

	remLengthLength := pos - (start + fixedHeaderLength)
	totalLength := fixedHeaderLength + remLengthLength + remLength

	if totalLength > available {
		return 0, nil // Incomplete packet — wait for more bytes.
	}

	handle(data[start : start+totalLength])
	return totalLength, nil
}
