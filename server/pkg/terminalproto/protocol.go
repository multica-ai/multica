// Package terminalproto defines the bounded, versioned messages shared by the
// browser relay and daemon PTY data plane. Raw terminal bytes use binary frames;
// lifecycle and lease operations use JSON Message values.
package terminalproto

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

const (
	Version         byte = 1
	HeaderSize           = 28
	MaxPayloadBytes      = 32 * 1024

	KindOutput byte = 1
	KindInput  byte = 2
)

var (
	ErrInvalidFrame = errors.New("invalid terminal binary frame")
	magic           = [2]byte{'M', 'T'}
)

// BinaryFrame carries opaque PTY bytes. Sequence is output_seq for output and
// the client-generated input ID for input.
type BinaryFrame struct {
	Kind      byte
	SessionID uuid.UUID
	Sequence  uint64
	Payload   []byte
}

func EncodeBinary(kind byte, sessionID uuid.UUID, sequence uint64, payload []byte) ([]byte, error) {
	if kind != KindOutput && kind != KindInput {
		return nil, fmt.Errorf("%w: unknown kind %d", ErrInvalidFrame, kind)
	}
	if len(payload) == 0 || len(payload) > MaxPayloadBytes {
		return nil, fmt.Errorf("%w: payload length %d", ErrInvalidFrame, len(payload))
	}
	out := make([]byte, HeaderSize+len(payload))
	copy(out[:2], magic[:])
	out[2] = Version
	out[3] = kind
	copy(out[4:20], sessionID[:])
	binary.BigEndian.PutUint64(out[20:28], sequence)
	copy(out[HeaderSize:], payload)
	return out, nil
}

func DecodeBinary(raw []byte) (BinaryFrame, error) {
	if len(raw) <= HeaderSize || len(raw)-HeaderSize > MaxPayloadBytes || raw[0] != magic[0] || raw[1] != magic[1] || raw[2] != Version {
		return BinaryFrame{}, ErrInvalidFrame
	}
	kind := raw[3]
	if kind != KindOutput && kind != KindInput {
		return BinaryFrame{}, ErrInvalidFrame
	}
	var sessionID uuid.UUID
	copy(sessionID[:], raw[4:20])
	payload := make([]byte, len(raw)-HeaderSize)
	copy(payload, raw[HeaderSize:])
	return BinaryFrame{Kind: kind, SessionID: sessionID, Sequence: binary.BigEndian.Uint64(raw[20:28]), Payload: payload}, nil
}

// Message is the JSON control envelope. Fields are intentionally optional so
// old peers can ignore additions while type and protocol_version remain stable.
type Message struct {
	Type                  string          `json:"type"`
	ProtocolVersion       int             `json:"protocol_version,omitempty"`
	SessionID             string          `json:"session_id,omitempty"`
	TaskID                string          `json:"task_id,omitempty"`
	IssueID               string          `json:"issue_id,omitempty"`
	AgentID               string          `json:"agent_id,omitempty"`
	WorkspaceID           string          `json:"workspace_id,omitempty"`
	RuntimeID             string          `json:"runtime_id,omitempty"`
	DaemonID              string          `json:"daemon_id,omitempty"`
	Provider              string          `json:"provider,omitempty"`
	Mode                  string          `json:"mode,omitempty"`
	Status                string          `json:"status,omitempty"`
	StructuredObservation string          `json:"structured_observation,omitempty"`
	Generation            int             `json:"generation,omitempty"`
	Cols                  uint16          `json:"cols,omitempty"`
	Rows                  uint16          `json:"rows,omitempty"`
	LastSeq               uint64          `json:"last_seq,omitempty"`
	OldestSeq             uint64          `json:"oldest_seq,omitempty"`
	OutputSeq             uint64          `json:"output_seq,omitempty"`
	Controller            bool            `json:"controller,omitempty"`
	LeaseToken            string          `json:"lease_token,omitempty"`
	LeaseExpiresAt        string          `json:"lease_expires_at,omitempty"`
	ProviderSessionID     string          `json:"provider_session_id,omitempty"`
	ExitCode              *int            `json:"exit_code,omitempty"`
	ExitReason            string          `json:"exit_reason,omitempty"`
	Error                 string          `json:"error,omitempty"`
	Payload               json.RawMessage `json:"payload,omitempty"`
}
