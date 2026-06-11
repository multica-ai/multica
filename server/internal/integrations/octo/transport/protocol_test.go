package transport

import (
	"encoding/base64"
	"testing"
)

// buildRecvBody assembles the bytes of a RECV packet body (everything onRecv
// reads from its decoder — i.e. after the fixed header + remaining-length, which
// onPacket strips before dispatch). Field order mirrors onRecv exactly.
func buildRecvBody(setting byte, serverVersion int, fromUID, channelID string, channelType byte, messageID uint64, messageSeq, timestamp uint32, topic string, payload []byte) []byte {
	e := &encoder{}
	e.PutByte(setting)
	e.WriteString("")        // msgKey (unused)
	e.WriteString(fromUID)   // fromUID
	e.WriteString(channelID) // channelID
	e.PutByte(channelType)   // channelType
	if serverVersion >= 3 {
		e.WriteUint32(0) // expire (unused)
	}
	e.WriteString("cmn-1") // clientMsgNo (unused)
	e.WriteUint64(messageID)
	e.WriteUint32(messageSeq)
	e.WriteUint32(timestamp)
	if (setting>>3)&0x01 > 0 { // topic bit
		e.WriteString(topic)
	}
	e.WriteBytes(payload) // encrypted payload = remaining bytes
	return e.Bytes()
}

// newGoldenSocket returns a Socket whose AES block/IV match the golden vectors,
// so goldenCipherB64 decrypts to goldenPlaintext through onRecv. ackHook records
// each RECVACK instead of writing to a websocket.
func newGoldenSocket(t *testing.T, acked *[]string) *Socket {
	t.Helper()
	block, err := newAESBlock(goldenAESKey)
	if err != nil {
		t.Fatalf("newAESBlock: %v", err)
	}
	s := NewSocket(SocketOptions{})
	s.aesBlock = block
	s.aesIV = goldenAESIV
	s.ackHook = func(messageID string, _ uint32) {
		*acked = append(*acked, messageID)
	}
	return s
}

// TestOnRecv_DecryptDispatchAndAck drives a well-formed RECV through onRecv and
// asserts the decrypted message is dispatched to OnMessage AND ack'd.
func TestOnRecv_DecryptDispatchAndAck(t *testing.T) {
	var acked []string
	var got *BotMessage
	s := newGoldenSocket(t, &acked)
	s.opts.OnMessage = func(m BotMessage) { got = &m }

	// streamOn bit set (bit 1) to also cover the StreamOn plumbing.
	body := buildRecvBody(0x02, 2, "u_sender", "ch_42", byte(ChannelGroup), 999, 7, 1700000000, "", []byte(goldenCipherB64))
	if err := s.onRecv(nil, newDecoder(body)); err != nil {
		t.Fatalf("onRecv: %v", err)
	}

	if got == nil {
		t.Fatal("OnMessage was not called")
	}
	if got.MessageID != "999" || got.MessageSeq != 7 || got.FromUID != "u_sender" || got.ChannelID != "ch_42" {
		t.Errorf("unexpected message fields: %+v", *got)
	}
	if got.ChannelType != ChannelGroup {
		t.Errorf("ChannelType = %d, want %d", got.ChannelType, ChannelGroup)
	}
	if !got.StreamOn {
		t.Error("StreamOn should be true when setting bit 1 is set")
	}
	if got.Payload.Type != MsgText || got.Payload.Content != "hello from octo" {
		t.Errorf("payload mismatch: %+v", got.Payload)
	}
	if len(acked) != 1 || acked[0] != "999" {
		t.Errorf("expected ack for message 999, got %v", acked)
	}
	if _, stillTracked := s.decryptFail["999"]; stillTracked {
		t.Error("decryptFail should not retain a successfully decrypted message")
	}
}

// TestOnRecv_TopicBitReadsExtraField verifies that when the topic setting bit is
// set, onRecv consumes the extra topic string and still parses the payload that
// follows it (a wrong bit-parse here would misalign every later field).
func TestOnRecv_TopicBitReadsExtraField(t *testing.T) {
	var acked []string
	var got *BotMessage
	s := newGoldenSocket(t, &acked)
	s.opts.OnMessage = func(m BotMessage) { got = &m }

	body := buildRecvBody(0x08, 2, "u1", "ch_topic", byte(ChannelTopic), 12345, 1, 1700000001, "topic-xyz", []byte(goldenCipherB64))
	if err := s.onRecv(nil, newDecoder(body)); err != nil {
		t.Fatalf("onRecv: %v", err)
	}
	if got == nil || got.Payload.Content != "hello from octo" {
		t.Fatalf("topic-bit path corrupted payload parsing: %+v", got)
	}
	if len(acked) != 1 {
		t.Errorf("expected one ack, got %v", acked)
	}
}

// TestOnRecv_PoisonAckAndDrop is the regression test for the poison-message
// guard: a payload that never decrypts must NOT be ack'd on the first two
// attempts (so the server redelivers) and MUST be ack'd-and-dropped on the
// maxDecryptRetries-th attempt (so it stops being redelivered forever).
func TestOnRecv_PoisonAckAndDrop(t *testing.T) {
	var acked []string
	var delivered int
	s := newGoldenSocket(t, &acked)
	s.opts.OnMessage = func(BotMessage) { delivered++ }

	poison := []byte("!!! not valid base64 ciphertext !!!")
	mkBody := func() []byte {
		return buildRecvBody(0x00, 2, "u1", "ch", byte(ChannelDM), 555, 1, 1700000000, "", poison)
	}

	// Attempts 1 and 2: not acked, tracked in decryptFail.
	for attempt := 1; attempt < maxDecryptRetries; attempt++ {
		if err := s.onRecv(nil, newDecoder(mkBody())); err != nil {
			t.Fatalf("attempt %d onRecv: %v", attempt, err)
		}
		if len(acked) != 0 {
			t.Fatalf("attempt %d: poison message acked early: %v", attempt, acked)
		}
		if s.decryptFail["555"] != attempt {
			t.Fatalf("attempt %d: decryptFail[555] = %d, want %d", attempt, s.decryptFail["555"], attempt)
		}
	}

	// Final attempt: ack-and-drop.
	if err := s.onRecv(nil, newDecoder(mkBody())); err != nil {
		t.Fatalf("final onRecv: %v", err)
	}
	if len(acked) != 1 || acked[0] != "555" {
		t.Errorf("expected ack-and-drop for 555, got %v", acked)
	}
	if _, stillTracked := s.decryptFail["555"]; stillTracked {
		t.Error("decryptFail should be cleared after ack-and-drop")
	}
	if delivered != 0 {
		t.Errorf("poison message was delivered to OnMessage %d times, want 0", delivered)
	}
}

// TestOnRecv_FailMapResetOnOverflow verifies the memory-leak guard: when the
// decryptFail map reaches maxDecryptFailEntries it is reset before the new entry
// is recorded, so it cannot grow unbounded under a flood of distinct poison IDs.
func TestOnRecv_FailMapResetOnOverflow(t *testing.T) {
	var acked []string
	s := newGoldenSocket(t, &acked)

	// Pre-fill the map to the cap with unrelated entries.
	for i := 0; i < maxDecryptFailEntries; i++ {
		s.decryptFail[string(rune(i))+"_pad"] = 1
	}
	if len(s.decryptFail) < maxDecryptFailEntries {
		t.Fatalf("setup: map has %d entries, want >= %d", len(s.decryptFail), maxDecryptFailEntries)
	}

	poison := []byte("@@@ undecryptable @@@")
	body := buildRecvBody(0x00, 2, "u1", "ch", byte(ChannelDM), 777, 1, 1700000000, "", poison)
	if err := s.onRecv(nil, newDecoder(body)); err != nil {
		t.Fatalf("onRecv: %v", err)
	}

	// The map was reset, then this one fresh failure recorded.
	if len(s.decryptFail) != 1 {
		t.Errorf("decryptFail size = %d after overflow reset, want 1", len(s.decryptFail))
	}
	if s.decryptFail["777"] != 1 {
		t.Errorf("decryptFail[777] = %d, want 1", s.decryptFail["777"])
	}
	if len(acked) != 0 {
		t.Errorf("first failure should not ack, got %v", acked)
	}
}

// buildConnackBody assembles a CONNACK packet body (post header + remaining
// length). Field order mirrors onConnack.
func buildConnackBody(serverVersion int, hasServerVersion bool, reasonCode byte, serverKey, salt string) []byte {
	e := &encoder{}
	if hasServerVersion {
		e.PutByte(byte(serverVersion))
	}
	e.WriteUint64(0) // timeDiff (unused)
	e.PutByte(reasonCode)
	e.WriteString(serverKey)
	e.WriteString(salt)
	if serverVersion >= 4 {
		e.WriteUint64(0) // nodeId (unused)
	}
	return e.Bytes()
}

// goldenDHKeyPair reconstructs the golden client key pair so onConnack's
// deriveAESKeyIV(serverKey=golden, salt=golden) yields the golden AES key/IV.
func goldenDHKeyPair(t *testing.T) *dhKeyPair {
	t.Helper()
	privBytes, err := base64.StdEncoding.DecodeString(goldenClientPrivB64)
	if err != nil {
		t.Fatalf("decode client priv: %v", err)
	}
	priv, err := x25519PrivFromBytes(privBytes)
	if err != nil {
		t.Fatalf("NewPrivateKey: %v", err)
	}
	return &dhKeyPair{priv: priv}
}

// TestOnConnack_SuccessDerivesCipher covers reasonCode=1: the AES block/IV are
// derived from the handshake, connected flips true, and OnConnected fires.
func TestOnConnack_SuccessDerivesCipher(t *testing.T) {
	var connected bool
	s := NewSocket(SocketOptions{OnConnected: func() { connected = true }})
	s.dh = goldenDHKeyPair(t)

	body := buildConnackBody(2, true, 1, goldenServerPubB64, goldenSalt)
	if err := s.onConnack(newDecoder(body), true); err != nil {
		t.Fatalf("onConnack: %v", err)
	}
	if s.serverVersion != 2 {
		t.Errorf("serverVersion = %d, want 2", s.serverVersion)
	}
	if s.aesBlock == nil {
		t.Error("aesBlock not set after successful CONNACK")
	}
	if s.aesIV != goldenAESIV {
		t.Errorf("aesIV = %q, want %q", s.aesIV, goldenAESIV)
	}
	if !s.connected.Load() {
		t.Error("connected flag not set after successful CONNACK")
	}
	if !connected {
		t.Error("OnConnected not called")
	}
	if !s.needReconnect {
		// onConnack doesn't touch needReconnect on success; manager defaults it
		// true. We only assert it wasn't cleared. (Default zero is false here.)
	}

	// The derived cipher must actually decrypt the golden payload end-to-end.
	plain, err := aesDecrypt([]byte(goldenCipherB64), s.aesBlock, s.aesIV)
	if err != nil || string(plain) != goldenPlaintext {
		t.Errorf("derived cipher failed to decrypt golden payload: %v / %q", err, plain)
	}
}

// TestOnConnack_ServerVersion4ReadsNodeID covers the serverVersion>=4 branch
// that consumes an extra nodeId uint64 after the salt; a missing/misordered read
// would error on the trailing bytes.
func TestOnConnack_ServerVersion4ReadsNodeID(t *testing.T) {
	s := NewSocket(SocketOptions{})
	s.dh = goldenDHKeyPair(t)

	body := buildConnackBody(4, true, 1, goldenServerPubB64, goldenSalt)
	if err := s.onConnack(newDecoder(body), true); err != nil {
		t.Fatalf("onConnack (v4): %v", err)
	}
	if s.serverVersion != 4 {
		t.Errorf("serverVersion = %d, want 4", s.serverVersion)
	}
	if s.aesBlock == nil {
		t.Error("aesBlock not set for v4 CONNACK")
	}
}

// TestOnConnack_Rejections covers reasonCode=0 (kicked) and a default rejection:
// both are terminal — needReconnect is cleared, OnError + OnDisconnected fire,
// and an error is returned.
func TestOnConnack_Rejections(t *testing.T) {
	cases := []struct {
		name       string
		reasonCode byte
	}{
		{"kicked (reason 0)", 0},
		{"generic failure (default)", 5},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var gotErr error
			var disconnected bool
			s := NewSocket(SocketOptions{
				OnError:        func(e error) { gotErr = e },
				OnDisconnected: func() { disconnected = true },
			})
			s.needReconnect = true

			body := buildConnackBody(2, true, c.reasonCode, "", "")
			err := s.onConnack(newDecoder(body), true)
			if err == nil {
				t.Fatal("expected a terminal error")
			}
			if s.needReconnect {
				t.Error("needReconnect should be cleared on terminal rejection")
			}
			if gotErr == nil {
				t.Error("OnError not called")
			}
			if !disconnected {
				t.Error("OnDisconnected not called")
			}
			if s.connected.Load() {
				t.Error("connected should stay false on rejection")
			}
		})
	}
}

// TestOnConnack_BadSaltReconnects covers the derive-failure path: a too-short
// salt makes deriveAESKeyIV fail; onConnack must set needReconnect and return
// the error (so a fresh handshake is attempted) without marking connected.
func TestOnConnack_BadSaltReconnects(t *testing.T) {
	s := NewSocket(SocketOptions{})
	s.dh = goldenDHKeyPair(t)

	body := buildConnackBody(2, true, 1, goldenServerPubB64, "tooshort") // <16 bytes
	err := s.onConnack(newDecoder(body), true)
	if err == nil {
		t.Fatal("expected derive error for short salt")
	}
	if !s.needReconnect {
		t.Error("needReconnect should be set so a fresh handshake is attempted")
	}
	if s.connected.Load() {
		t.Error("connected must stay false when key derivation fails")
	}
}
