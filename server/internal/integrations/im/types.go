package im

import (
	"bytes"
	"encoding/json"
)

// ChannelType identifies the kind of conversation a message belongs to.
type ChannelType uint8

const (
	ChannelDM    ChannelType = 1 // direct (1:1) message
	ChannelGroup ChannelType = 2 // group chat
	ChannelTopic ChannelType = 5 // community topic / thread
)

// MessageType is the content type of a message payload.
type MessageType int

const (
	MsgText            MessageType = 1
	MsgImage           MessageType = 2
	MsgGIF             MessageType = 3
	MsgVoice           MessageType = 4
	MsgVideo           MessageType = 5
	MsgLocation        MessageType = 6
	MsgCard            MessageType = 7
	MsgFile            MessageType = 8
	MsgMultipleForward MessageType = 11
	MsgRichText        MessageType = 14 // text + inline images
)

// BotRegisterResp is the result of POST /v1/bot/register.
type BotRegisterResp struct {
	RobotID        string `json:"robot_id"`
	IMToken        string `json:"im_token"`
	WSURL          string `json:"ws_url"`
	APIURL         string `json:"api_url"`
	OwnerUID       string `json:"owner_uid"`
	OwnerChannelID string `json:"owner_channel_id"`
}

// MentionEntity is the precise position of a single @mention within content.
// offset/length are in UTF-16 code units (matching the wire format from the
// JS clients).
type MentionEntity struct {
	UID    string `json:"uid"`
	Offset int    `json:"offset"`
	Length int    `json:"length"`
}

// MentionPayload describes who a message mentions. The three-state flags
// (humans/ais/all) are server-authoritative — the adapter only reads them and
// never decides semantics. all is a legacy field the server double-writes for
// older clients (semantically: humans-only).
type MentionPayload struct {
	UIDs     []string        `json:"uids,omitempty"`
	Entities []MentionEntity `json:"entities,omitempty"`
	All      any             `json:"all,omitempty"`    // bool|number (legacy)
	Humans   any             `json:"humans,omitempty"` // "@所有人"
	AIs      any             `json:"ais,omitempty"`    // "@所有AI"
}

// ReplyPayload is the message a reply points at.
type ReplyPayload struct {
	Payload  *MessagePayload `json:"payload,omitempty"`
	FromUID  string          `json:"from_uid,omitempty"`
	FromName string          `json:"from_name,omitempty"`
}

// MessagePayload is the decrypted JSON body of a message. Unknown fields are
// preserved in Extra so forward-compatible server additions are not lost
// (mirrors the TS `[key: string]: unknown` index signature).
type MessagePayload struct {
	Type    MessageType     `json:"type"`
	Content string          `json:"content,omitempty"`
	URL     string          `json:"url,omitempty"`
	Name    string          `json:"name,omitempty"`
	Mention *MentionPayload `json:"mention,omitempty"`
	Reply   *ReplyPayload   `json:"reply,omitempty"`

	// Extra holds any fields not modeled above.
	Extra map[string]json.RawMessage `json:"-"`
}

// knownPayloadFields are the JSON keys MessagePayload models explicitly; the
// rest land in Extra.
var knownPayloadFields = map[string]struct{}{
	"type": {}, "content": {}, "url": {}, "name": {}, "mention": {}, "reply": {},
}

// UnmarshalJSON decodes the modeled fields and collects everything else into
// Extra.
func (p *MessagePayload) UnmarshalJSON(data []byte) error {
	type alias MessagePayload
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*p = MessagePayload(a)

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	for k := range knownPayloadFields {
		delete(raw, k)
	}
	if len(raw) > 0 {
		p.Extra = raw
	}
	return nil
}

// BotMessage is a fully parsed inbound message handed to the application.
type BotMessage struct {
	MessageID   string
	MessageSeq  uint32
	FromUID     string
	ChannelID   string
	ChannelType ChannelType
	Timestamp   uint32
	Payload     MessagePayload
	// StreamOn is true when this message is part of a streaming sequence
	// (WuKongIM setting byte bit 1).
	StreamOn bool
}

// SendMessageResult is the response to POST /v1/bot/sendMessage. The Octo
// server emits message_id as a bare int64 JSON number (octo-lib MsgSendResp),
// which would overflow JavaScript's safe-integer range; we model it as a string
// and accept either a JSON number or a JSON string on the wire so 16+ digit IDs
// survive without precision loss.
type SendMessageResult struct {
	MessageID   string
	ClientMsgNo string
	MessageSeq  uint32
}

// UnmarshalJSON decodes message_id whether the server sends it as a number or a
// string. The bytes are taken verbatim (quotes stripped for the string form) so
// 16+ digit IDs survive without the float64 precision loss a plain interface{}
// decode would incur.
func (r *SendMessageResult) UnmarshalJSON(data []byte) error {
	var raw struct {
		MessageID   json.RawMessage `json:"message_id"`
		ClientMsgNo string          `json:"client_msg_no"`
		MessageSeq  uint32          `json:"message_seq"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	r.MessageID = string(bytes.Trim(raw.MessageID, `"`))
	r.ClientMsgNo = raw.ClientMsgNo
	r.MessageSeq = raw.MessageSeq
	return nil
}
