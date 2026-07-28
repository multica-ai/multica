package collab

import "encoding/json"

// Wire protocol between the note editor and the live engine. Every frame is a
// JSON object with a "type". Steps and documents are passed through as opaque
// JSON: the server orders and relays them, it never parses editor content.
const (
	// client -> server
	MsgAuth     = "auth"     // PAT/JWT clients that cannot set a cookie
	MsgSteps    = "steps"    // submit local steps at a known version
	MsgCaret    = "caret"    // caret / selection moved
	MsgSnapshot = "snapshot" // answer to snapshot_request
	MsgPing     = "ping"

	// server -> client
	MsgWelcome         = "welcome"
	MsgPeers           = "peers"
	MsgStepsApplied    = "steps_applied"
	MsgRejected        = "rejected"
	MsgSnapshotRequest = "snapshot_request"
	MsgReload          = "reload"
	MsgAuthAck         = "auth_ack"
	MsgAuthError       = "auth_error"
	MsgError           = "error"
	MsgPong            = "pong"
)

// ClientMessage is any frame sent by the editor.
type ClientMessage struct {
	Type string `json:"type"`

	// MsgAuth
	Token string `json:"token,omitempty"`

	// MsgSteps
	Version int               `json:"version,omitempty"`
	Steps   []json.RawMessage `json:"steps,omitempty"`

	// MsgCaret — positions are ProseMirror document offsets.
	Anchor int `json:"anchor,omitempty"`
	Head   int `json:"head,omitempty"`

	// MsgSnapshot
	Doc json.RawMessage `json:"doc,omitempty"`
}

// WelcomeMessage is the first frame a peer receives. Version + Snapshot tell
// the editor where to start: with a snapshot it adopts that document, without
// one it keeps the note body it already loaded from the database.
// Steps carries everything submitted after Snapshot.Version. A snapshot is
// taken once and then left behind by further typing, so a joiner that adopted
// it alone would hold an out-of-date document while believing it is current —
// and its next autosave would write that stale text over everyone's work.
// Snapshot plus these steps lands the joiner exactly on Version.
type WelcomeMessage struct {
	Type     string       `json:"type"`
	PeerID   string       `json:"peer_id"`
	Color    string       `json:"color"`
	CanEdit  bool         `json:"can_edit"`
	Version  int          `json:"version"`
	Snapshot *Snapshot    `json:"snapshot,omitempty"`
	Steps    []StepRecord `json:"steps,omitempty"`
	Peers    []*Peer      `json:"peers"`
}

// PeersMessage is broadcast whenever somebody joins or leaves.
type PeersMessage struct {
	Type  string  `json:"type"`
	Peers []*Peer `json:"peers"`
}

// StepsAppliedMessage carries accepted steps to every peer, sender included.
type StepsAppliedMessage struct {
	Type    string       `json:"type"`
	Version int          `json:"version"`
	Steps   []StepRecord `json:"steps"`
}

// RejectedMessage tells a client its submission was based on a stale version
// and hands it the steps it is missing so it can rebase and resubmit.
type RejectedMessage struct {
	Type    string       `json:"type"`
	Version int          `json:"version"`
	Missing []StepRecord `json:"missing"`
}

// CaretMessage relays one peer's caret to the others.
type CaretMessage struct {
	Type   string `json:"type"`
	PeerID string `json:"peer_id"`
	Name   string `json:"name"`
	Color  string `json:"color"`
	Anchor int    `json:"anchor"`
	Head   int    `json:"head"`
}

// SimpleMessage covers the frames that carry no payload beyond their type.
type SimpleMessage struct {
	Type   string `json:"type"`
	Reason string `json:"reason,omitempty"`
}

func encode(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		// Every message in this package is a plain struct of JSON-safe
		// fields; a marshal error would be a programming mistake, and an
		// empty frame is safer than panicking inside a room broadcast.
		return []byte(`{"type":"error","reason":"encode failed"}`)
	}
	return b
}
