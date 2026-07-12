// Package chatstream implements the FIR-2835 Phase 1 streaming run endpoint:
// a Server-Sent Events stream over a chat session's agent run, speaking the
// Vercel AI SDK UI-message-stream protocol (v1) so any AI-SDK client
// (useChat + DefaultChatTransport) can consume a Multica agent without a
// poll loop.
//
// Today Multica's internal chat events are message-granular (chat:done
// carries the whole assistant reply), so a run streams as one text-delta.
// The wire contract is already the token-granular protocol — when the
// runtime starts relaying token deltas the clients need no change.
//
// Delivery rides the in-process event bus (server/internal/events), same as
// the realtime WS hub: the daemon's complete/fail call publishes on the bus
// of the instance that handled it. Both Firtal deployments (staging mac
// mini, Sliplane prod) run a single backend instance, so no cross-instance
// buffering is needed yet; Phase 3 (resumability) moves this onto the Redis
// relay.
package chatstream

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// UIMessageStreamHeader / UIMessageStreamVersion mark the response as a
// Vercel AI SDK UI message stream; the AI SDK client checks this header.
const (
	UIMessageStreamHeader  = "x-vercel-ai-ui-message-stream"
	UIMessageStreamVersion = "v1"
)

// Chunk type discriminators for the UI message stream parts we emit.
const (
	ChunkTypeStart               = "start"
	ChunkTypeTextStart           = "text-start"
	ChunkTypeTextDelta           = "text-delta"
	ChunkTypeTextEnd             = "text-end"
	ChunkTypeFinish              = "finish"
	ChunkTypeError               = "error"
	ChunkTypeStartStep           = "start-step"
	ChunkTypeFinishStep          = "finish-step"
	ChunkTypeToolInputStart      = "tool-input-start"
	ChunkTypeToolInputDelta      = "tool-input-delta"
	ChunkTypeToolInputAvailable  = "tool-input-available"
	ChunkTypeToolOutputAvailable = "tool-output-available"
	ChunkTypeToolOutputDenied    = "tool-output-denied"
	ChunkTypeToolApprovalRequest = "tool-approval-request"
)

type StartChunk struct {
	Type      string `json:"type"`
	MessageID string `json:"messageId,omitempty"`
}

type TextStartChunk struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type TextDeltaChunk struct {
	Type  string `json:"type"`
	ID    string `json:"id"`
	Delta string `json:"delta"`
}

type TextEndChunk struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type FinishChunk struct {
	Type            string `json:"type"`
	MessageMetadata any    `json:"messageMetadata,omitempty"`
}

type ErrorChunk struct {
	Type      string `json:"type"`
	ErrorText string `json:"errorText"`
}

type StepStartChunk struct {
	Type string `json:"type"`
}
type StepFinishChunk struct {
	Type string `json:"type"`
}

type ToolInputStartChunk struct {
	Type       string `json:"type"`
	ToolCallID string `json:"toolCallId"`
	ToolName   string `json:"toolName"`
}

type ToolInputDeltaChunk struct {
	Type           string `json:"type"`
	ToolCallID     string `json:"toolCallId"`
	InputTextDelta string `json:"inputTextDelta"`
}

type ToolInputAvailableChunk struct {
	Type       string `json:"type"`
	ToolCallID string `json:"toolCallId"`
	ToolName   string `json:"toolName"`
	Input      any    `json:"input"`
}

type ToolOutputAvailableChunk struct {
	Type       string `json:"type"`
	ToolCallID string `json:"toolCallId"`
	Output     any    `json:"output"`
}

type ToolOutputDeniedChunk struct {
	Type       string `json:"type"`
	ToolCallID string `json:"toolCallId"`
}

type ToolApprovalRequestChunk struct {
	Type       string `json:"type"`
	ApprovalID string `json:"approvalId"`
	ToolCallID string `json:"toolCallId"`
}

// DataChunk is the protocol escape hatch for typed application data. Type
// must use the AI SDK data-* namespace (for example data-finance).
type DataChunk struct {
	Type string `json:"type"`
	ID   string `json:"id,omitempty"`
	Data any    `json:"data"`
}

// FinishUsage carries the run's token + cost spend inside the finish chunk's
// messageMetadata, so the consuming app never needs the separate
// message-costs endpoint for the live turn.
type FinishUsage struct {
	InputTokens      int64 `json:"inputTokens"`
	OutputTokens     int64 `json:"outputTokens"`
	CacheReadTokens  int64 `json:"cacheReadTokens"`
	CacheWriteTokens int64 `json:"cacheWriteTokens"`
	CostCents        int64 `json:"costCents"`
}

// FinishMetadata is the messageMetadata payload on the finish chunk.
type FinishMetadata struct {
	TaskID    string       `json:"taskId"`
	MessageID string       `json:"messageId,omitempty"`
	ElapsedMs int64        `json:"elapsedMs,omitempty"`
	Model     string       `json:"model,omitempty"`
	Usage     *FinishUsage `json:"usage,omitempty"`
}

// Writer frames UI-message-stream chunks as SSE onto an http.ResponseWriter,
// flushing after every frame so deltas reach the client immediately.
type Writer struct {
	w http.ResponseWriter
	f http.Flusher
}

// NewWriter sets the SSE + protocol headers and returns a flushing writer.
// Fails when the ResponseWriter cannot stream (no http.Flusher).
func NewWriter(w http.ResponseWriter) (*Writer, error) {
	f, ok := w.(http.Flusher)
	if !ok {
		return nil, errors.New("response writer does not support streaming")
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	// Disable proxy buffering (nginx et al.) so frames are not held back.
	h.Set("X-Accel-Buffering", "no")
	h.Set(UIMessageStreamHeader, UIMessageStreamVersion)
	return &Writer{w: w, f: f}, nil
}

// WriteChunk emits one `data: {json}` SSE frame and flushes.
func (s *Writer) WriteChunk(chunk any) error {
	payload, err := json.Marshal(chunk)
	if err != nil {
		return fmt.Errorf("marshal stream chunk: %w", err)
	}
	if _, err := fmt.Fprintf(s.w, "data: %s\n\n", payload); err != nil {
		return err
	}
	s.f.Flush()
	return nil
}

// WriteDone emits the AI SDK stream terminator.
func (s *Writer) WriteDone() error {
	if _, err := fmt.Fprint(s.w, "data: [DONE]\n\n"); err != nil {
		return err
	}
	s.f.Flush()
	return nil
}

// WriteParts emits already-typed UI message parts without terminating the
// stream. This lets runtime adapters relay tools, approvals, steps and custom
// data while the transport remains independent of the runtime/provider.
func (s *Writer) WriteParts(parts []any) error {
	for _, part := range parts {
		if err := s.WriteChunk(part); err != nil {
			return err
		}
	}
	return nil
}

// WritePing emits an SSE comment as a keep-alive; AI SDK clients ignore it.
func (s *Writer) WritePing() error {
	if _, err := fmt.Fprint(s.w, ": ping\n\n"); err != nil {
		return err
	}
	s.f.Flush()
	return nil
}

// WriteAssistantMessage emits the complete part sequence for one finished
// assistant turn — start → text-start → text-delta → text-end → finish →
// [DONE] — with the whole reply as a single delta (message-granular source).
func (s *Writer) WriteAssistantMessage(messageID, content string, meta FinishMetadata) error {
	const textPartID = "text-0"
	chunks := []any{
		StartChunk{Type: ChunkTypeStart, MessageID: messageID},
		TextStartChunk{Type: ChunkTypeTextStart, ID: textPartID},
		TextDeltaChunk{Type: ChunkTypeTextDelta, ID: textPartID, Delta: content},
		TextEndChunk{Type: ChunkTypeTextEnd, ID: textPartID},
		FinishChunk{Type: ChunkTypeFinish, MessageMetadata: meta},
	}
	for _, c := range chunks {
		if err := s.WriteChunk(c); err != nil {
			return err
		}
	}
	return s.WriteDone()
}

// WriteError emits a typed error chunk followed by the terminator, so the
// client sees the real failure text in-stream instead of a dropped socket.
func (s *Writer) WriteError(errorText string) error {
	if err := s.WriteChunk(ErrorChunk{Type: ChunkTypeError, ErrorText: errorText}); err != nil {
		return err
	}
	return s.WriteDone()
}
