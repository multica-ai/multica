package mattermost

import (
	"encoding/json"
	"strings"
)

// This file holds the Mattermost WebSocket wire shapes. Two frame families
// arrive on one socket: EVENTS (an "event" field plus data/broadcast/seq) and
// REPLIES to actions this client sent (a "seq_reply" field plus status/error).
// The authentication challenge is the only action the adapter sends, so the
// reply family exists to tell "the token was rejected" apart from "the socket
// closed".

// Event names the adapter reacts to. Mattermost emits dozens; everything not
// listed here is ignored by the read loop.
const (
	eventPosted = "posted"
	eventHello  = "hello"
)

// wsFrame is one decoded inbound frame. Exactly one of Event / SeqReply is
// meaningful: Mattermost never sets both.
type wsFrame struct {
	Event     string          `json:"event"`
	Data      json.RawMessage `json:"data"`
	Broadcast json.RawMessage `json:"broadcast"`
	Seq       int64           `json:"seq"`

	// SeqReply echoes the Seq of the action being answered. Zero means this
	// frame is an event, not a reply.
	SeqReply int64  `json:"seq_reply"`
	Status   string `json:"status"`
	Error    *struct {
		ID         string `json:"id"`
		Message    string `json:"message"`
		StatusCode int    `json:"status_code"`
	} `json:"error"`
}

// isReply reports whether the frame answers an action rather than announcing
// an event.
func (f wsFrame) isReply() bool { return f.SeqReply != 0 }

// wsAction is one outbound action frame. The adapter sends exactly one:
// authentication_challenge.
type wsAction struct {
	Seq    int64          `json:"seq"`
	Action string         `json:"action"`
	Data   map[string]any `json:"data,omitempty"`
}

// statusOK is the value Mattermost sets on a successful action reply.
const statusOK = "OK"

// postedData is the payload of a "posted" event. Mattermost serializes the
// post itself as a JSON *string* inside the data object, so it needs a second
// decode pass — that is the platform's wire format, not a mistake here.
type postedData struct {
	Post               string `json:"post"`
	ChannelType        string `json:"channel_type"`
	ChannelName        string `json:"channel_name"`
	ChannelDisplayName string `json:"channel_display_name"`
	SenderName         string `json:"sender_name"`
	TeamID             string `json:"team_id"`
}

// decodePosted extracts the post from a "posted" event payload. ok=false means
// the frame carried nothing usable and must be skipped.
func decodePosted(raw json.RawMessage) (Post, postedData, bool) {
	var data postedData
	if err := json.Unmarshal(raw, &data); err != nil {
		return Post{}, postedData{}, false
	}
	if strings.TrimSpace(data.Post) == "" {
		return Post{}, postedData{}, false
	}
	var post Post
	if err := json.Unmarshal([]byte(data.Post), &post); err != nil {
		return Post{}, postedData{}, false
	}
	if post.ID == "" || post.ChannelID == "" {
		return Post{}, postedData{}, false
	}
	return post, data, true
}

// senderDisplayName renders the human-readable name of the poster. Mattermost
// puts "@username" in sender_name; the leading @ is dropped so the value reads
// as a name in quoted-message context rather than as a live mention.
func senderDisplayName(data postedData) string {
	return strings.TrimPrefix(strings.TrimSpace(data.SenderName), "@")
}
