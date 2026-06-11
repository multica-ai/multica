package transport

import (
	"crypto/rand"
	"encoding/hex"
	"strconv"
)

// PacketType is the WuKongIM frame type (high nibble of the header byte).
type PacketType byte

const (
	pktConnect    PacketType = 1
	pktConnack    PacketType = 2
	pktSend       PacketType = 3 // client→server send; bots don't use it
	pktSendack    PacketType = 4 // ack for SEND; ignored
	pktRecv       PacketType = 5 // inbound message
	pktRecvack    PacketType = 6 // our ack for a received message
	pktPing       PacketType = 7
	pktPong       PacketType = 8
	pktDisconnect PacketType = 9
)

// protoVersion is the WuKongIM protocol version this client speaks.
const protoVersion = 4

// connectOptions are the fields of a CONNECT packet body.
type connectOptions struct {
	version         byte
	deviceFlag      byte
	deviceID        string
	uid             string
	token           string
	clientTimestamp int64
	clientKey       string // base64-encoded DH public key
}

// encodeConnectPacket builds a CONNECT frame. Body field order is strict:
// version, deviceFlag, deviceID, uid, token, clientTimestamp(int64), clientKey.
func encodeConnectPacket(opts connectOptions) []byte {
	body := &encoder{}
	body.PutByte(opts.version)
	body.PutByte(opts.deviceFlag)
	body.WriteString(opts.deviceID)
	body.WriteString(opts.uid)
	body.WriteString(opts.token)
	body.WriteUint64(uint64(opts.clientTimestamp))
	body.WriteString(opts.clientKey)
	return frameWithBody(pktConnect, body.Bytes())
}

// encodePingPacket builds a single-byte PING frame.
func encodePingPacket() []byte {
	return []byte{byte(pktPing) << 4}
}

// encodeRecvackPacket builds a RECVACK frame acknowledging a received message.
// Body: messageID(int64) + messageSeq(int32). messageID arrives as a decimal
// string (int64 precision protection) and is converted back to uint64.
func encodeRecvackPacket(messageID string, messageSeq uint32) ([]byte, error) {
	id, err := strconv.ParseUint(messageID, 10, 64)
	if err != nil {
		return nil, err
	}
	body := &encoder{}
	body.WriteUint64(id)
	body.WriteUint32(messageSeq)
	return frameWithBody(pktRecvack, body.Bytes()), nil
}

// frameWithBody prepends the fixed header byte (packetType<<4 | flags=0) and the
// variable-length body size to a packet body.
func frameWithBody(t PacketType, body []byte) []byte {
	frame := &encoder{}
	frame.PutByte(byte(t) << 4)
	frame.WriteBytes(encodeVarLen(len(body)))
	frame.WriteBytes(body)
	return frame.Bytes()
}

// settingFlags decode the RECV setting byte. Only the flags the adapter acts on
// are modeled (topic gates an extra wire field; streamOn marks streaming
// messages); other bits (e.g. receipt-enabled) are ignored.
type settingFlags struct {
	topic    bool
	streamOn bool
}

func parseSettingByte(v byte) settingFlags {
	return settingFlags{
		topic:    (v>>3)&0x01 > 0,
		streamOn: (v>>1)&0x01 > 0,
	}
}

// generateDeviceID returns a random 32-char hex device id. The bot appends a
// "W" suffix when building the CONNECT packet (web/bot device marker).
func generateDeviceID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
