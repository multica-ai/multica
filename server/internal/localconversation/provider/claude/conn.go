// Package claude drives the per-conversation Claude adapter
// (apps/desktop/src/claude-host): one Node process that runs the Claude Agent
// SDK for one conversation and speaks versioned NDJSON over stdio.
package claude

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ProtocolVersion is the adapter protocol this daemon speaks.
const ProtocolVersion = 1

// Capabilities an adapter announces in its ready frame. An adapter that lists
// neither predates spares and only serves start.
const (
	CapabilityPrewarm = "prewarm"
	CapabilityClaim   = "claim"
)

// Error codes the daemon acts on.
const (
	CodePrewarmUnsupported = "prewarm_unsupported"
	CodeNotClaimed         = "not_claimed"
	CodeSpareExited        = "spare_exited"
)

// maxFrameBytes bounds one adapter output line. Tool output is forwarded
// unchanged, so the limit only guards against a runaway frame.
const maxFrameBytes = 64 << 20

var (
	// ErrClosed is returned for requests after the connection ended.
	ErrClosed = errors.New("claude adapter: connection closed")
	// ErrProtocol reports an adapter frame this daemon cannot accept.
	ErrProtocol = errors.New("claude adapter: protocol error")
)

// AdapterError is a structured error returned by the adapter.
type AdapterError struct {
	Code    string
	Message string
	// Dispatch tells whether a send reached the provider: rejected,
	// not_dispatched or outcome_unknown. Empty for other requests.
	Dispatch string
}

func (e *AdapterError) Error() string { return "claude adapter: " + e.Code + ": " + e.Message }

// Ready is the adapter's first frame.
type Ready struct {
	SDKVersion   string   `json:"sdk_version"`
	CLIVersion   string   `json:"cli_version,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
}

// Output is an adapter frame that is not a reply to a request: a raw SDK
// message, an interaction request, or an error not tied to a request.
type Output struct {
	Type string
	// QueryGeneration identifies the SDK Query that produced an sdk_message.
	QueryGeneration int64
	// Message is the raw SDK message for sdk_message frames.
	Message json.RawMessage
	// Error is set for unsolicited error frames, e.g. query_ended.
	Error *AdapterError
	// Interaction is the raw interaction_request frame.
	Interaction json.RawMessage
}

// Handler receives adapter output on the connection's single reader
// goroutine. It must not block: the reader never pauses, so control replies
// are never stuck behind slow event handling.
type Handler func(Output)

type frame struct {
	Version         int             `json:"version"`
	Type            string          `json:"type"`
	ID              string          `json:"id,omitempty"`
	Data            json.RawMessage `json:"data,omitempty"`
	Code            string          `json:"code,omitempty"`
	Message         json.RawMessage `json:"message,omitempty"`
	Dispatch        string          `json:"dispatch,omitempty"`
	QueryGeneration int64           `json:"query_generation,omitempty"`
	SDKVersion      string          `json:"sdk_version,omitempty"`
	CLIVersion      string          `json:"cli_version,omitempty"`
	Capabilities    []string        `json:"capabilities,omitempty"`
}

type reply struct {
	data json.RawMessage
	err  error
}

// Conn is the protocol client over an adapter's stdin and stdout.
type Conn struct {
	handler Handler

	writeMu sync.Mutex
	w       io.Writer

	mu      sync.Mutex
	nextID  uint64
	pending map[string]chan reply
	err     error
	done    chan struct{}

	ready    chan Ready
	readDone chan struct{}
}

// NewConn starts reading r. The caller closes the underlying streams.
func NewConn(r io.Reader, w io.Writer, handler Handler) *Conn {
	c := &Conn{handler: handler, w: w, pending: make(map[string]chan reply), done: make(chan struct{}), ready: make(chan Ready, 1), readDone: make(chan struct{})}
	go c.read(r)
	return c
}

// WaitReady returns the adapter's ready frame.
func (c *Conn) WaitReady(ctx context.Context) (Ready, error) {
	select {
	case r := <-c.ready:
		c.ready <- r
		return r, nil
	case <-c.done:
		return Ready{}, c.Err()
	case <-ctx.Done():
		return Ready{}, ctx.Err()
	}
}

// Request sends one request and waits for its result or error. req must be a
// JSON object with a "type"; version and id are added here.
func (c *Conn) Request(ctx context.Context, typ string, fields map[string]any) (json.RawMessage, error) {
	c.mu.Lock()
	if c.err != nil {
		err := c.err
		c.mu.Unlock()
		return nil, err
	}
	c.nextID++
	id := strconv.FormatUint(c.nextID, 10)
	ch := make(chan reply, 1)
	c.pending[id] = ch
	c.mu.Unlock()

	msg := make(map[string]any, len(fields)+3)
	for k, v := range fields {
		msg[k] = v
	}
	msg["version"], msg["type"], msg["id"] = ProtocolVersion, typ, id
	data, err := json.Marshal(msg)
	if err != nil {
		c.forget(id)
		return nil, err
	}
	c.writeMu.Lock()
	_, err = c.w.Write(append(data, '\n'))
	c.writeMu.Unlock()
	if err != nil {
		c.forget(id)
		return nil, fmt.Errorf("write %s: %w", typ, err)
	}

	select {
	case r := <-ch:
		return r.data, r.err
	case <-c.done:
		select {
		case r := <-ch:
			return r.data, r.err
		default:
		}
		return nil, c.Err()
	case <-ctx.Done():
		c.forget(id)
		return nil, ctx.Err()
	}
}

// Done is closed when the connection ends.
func (c *Conn) Done() <-chan struct{} { return c.done }

// Err returns why the connection ended, or nil while it is open.
func (c *Conn) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// Fail ends the connection with err, failing every pending request.
func (c *Conn) Fail(err error) {
	if err == nil {
		err = ErrClosed
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return
	}
	c.err = err
	close(c.done)
	for id, ch := range c.pending {
		ch <- reply{err: err}
		delete(c.pending, id)
	}
}

func (c *Conn) forget(id string) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

func (c *Conn) read(r io.Reader) {
	defer close(c.readDone)
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64<<10), maxFrameBytes)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		if err := c.dispatch(line); err != nil {
			c.Fail(err)
			return
		}
	}
	if err := scanner.Err(); err != nil {
		c.Fail(fmt.Errorf("%w: read adapter output: %w", ErrProtocol, err))
		return
	}
	c.Fail(ErrClosed)
}

func (c *Conn) dispatch(line []byte) error {
	var f frame
	if err := json.Unmarshal(line, &f); err != nil {
		return fmt.Errorf("%w: frame is not JSON", ErrProtocol)
	}
	if f.Version != ProtocolVersion {
		return fmt.Errorf("%w: adapter speaks version %d, want %d", ErrProtocol, f.Version, ProtocolVersion)
	}
	switch f.Type {
	case "ready":
		select {
		case c.ready <- Ready{SDKVersion: f.SDKVersion, CLIVersion: f.CLIVersion, Capabilities: f.Capabilities}:
		default:
			return fmt.Errorf("%w: duplicate ready", ErrProtocol)
		}
	case "result":
		c.resolve(f.ID, reply{data: f.Data})
	case "error":
		adapterErr := &AdapterError{Code: f.Code, Message: messageText(f.Message), Dispatch: f.Dispatch}
		if f.ID == "" {
			c.handler(Output{Type: "error", Error: adapterErr})
		} else {
			c.resolve(f.ID, reply{err: adapterErr})
		}
	case "sdk_message":
		if len(f.Message) == 0 || f.Message[0] != '{' {
			return fmt.Errorf("%w: sdk_message without an object message", ErrProtocol)
		}
		c.handler(Output{Type: "sdk_message", QueryGeneration: f.QueryGeneration, Message: f.Message})
	case "interaction_request":
		c.handler(Output{Type: "interaction_request", Interaction: append(json.RawMessage(nil), line...)})
	default:
		return fmt.Errorf("%w: unknown frame type %q", ErrProtocol, f.Type)
	}
	return nil
}

func (c *Conn) resolve(id string, r reply) {
	c.mu.Lock()
	ch, ok := c.pending[id]
	delete(c.pending, id)
	c.mu.Unlock()
	if ok {
		ch <- r
	}
}

func messageText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return string(raw)
}

// ClaimReason is the SDK's reason for a refused claim: the leading token of a
// not_claimed message, such as option_not_applied.
func ClaimReason(err error) string {
	var adapterErr *AdapterError
	if !errors.As(err, &adapterErr) || adapterErr.Code != CodeNotClaimed {
		return ""
	}
	reason, _, _ := strings.Cut(adapterErr.Message, ":")
	return reason
}

// MCPSetFailed reports whether a set_mcp_servers result lists a server the
// session could not bind.
func MCPSetFailed(raw json.RawMessage) bool {
	var result struct {
		Errors map[string]any `json:"errors"`
	}
	return json.Unmarshal(raw, &result) == nil && len(result.Errors) > 0
}

// StartTiming is what the adapter measured inside a start request. Fields are
// nil when an older adapter's result does not carry them.
type StartTiming struct {
	SDKLoad   *time.Duration
	QueryInit *time.Duration
}

// ParseStartTiming reads the optional timing of a start result; anything
// malformed reads as absent, because timing must never fail a start.
func ParseStartTiming(raw json.RawMessage) StartTiming {
	var result struct {
		Timing struct {
			SDKLoadMs   *float64 `json:"sdk_load_ms"`
			QueryInitMs *float64 `json:"query_init_ms"`
		} `json:"timing"`
	}
	if json.Unmarshal(raw, &result) != nil {
		return StartTiming{}
	}
	return StartTiming{SDKLoad: millis(result.Timing.SDKLoadMs), QueryInit: millis(result.Timing.QueryInitMs)}
}

func millis(ms *float64) *time.Duration {
	if ms == nil || *ms < 0 {
		return nil
	}
	d := time.Duration(*ms * float64(time.Millisecond))
	return &d
}
