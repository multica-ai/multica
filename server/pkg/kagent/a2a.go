package kagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// These wire types follow alpha7 and its a2a-go/v2 v2.6.0 dependency (A2A 1.0).
// The JSON-RPC method names, uppercase enums and SendMessage result envelope
// differ from A2A 0.3. Unknown fields are tolerated, unknown states fail closed.
type Part struct {
	Text string `json:"text,omitempty"`
}
type RemoteMessage struct {
	MessageID string `json:"messageId"`
	ContextID string `json:"contextId,omitempty"`
	Role      string `json:"role"`
	Parts     []Part `json:"parts"`
}
type RemoteTask struct {
	ID        string `json:"id"`
	ContextID string `json:"contextId"`
	Status    struct {
		State   string         `json:"state"`
		Message *RemoteMessage `json:"message,omitempty"`
	} `json:"status"`
	Artifacts []struct {
		Parts []Part `json:"parts"`
	} `json:"artifacts,omitempty"`
}

const (
	StateSubmitted     = "TASK_STATE_SUBMITTED"
	StateWorking       = "TASK_STATE_WORKING"
	StateCompleted     = "TASK_STATE_COMPLETED"
	StateFailed        = "TASK_STATE_FAILED"
	StateCanceled      = "TASK_STATE_CANCELED"
	StateRejected      = "TASK_STATE_REJECTED"
	StateInputRequired = "TASK_STATE_INPUT_REQUIRED"
	StateAuthRequired  = "TASK_STATE_AUTH_REQUIRED"
)

func (t *RemoteTask) Terminal() bool {
	switch t.Status.State {
	case StateCompleted, StateFailed, StateCanceled, StateRejected:
		return true
	}
	return false
}

// Text exposes intentional completed output only. Status diagnostics and partial
// artifacts can contain provider credentials, including on canceled tasks.
func (t *RemoteTask) Text() string {
	if t.Status.State != StateCompleted {
		return ""
	}
	var chunks []string
	for _, artifact := range t.Artifacts {
		for _, p := range artifact.Parts {
			chunks = append(chunks, p.Text)
		}
	}
	if len(chunks) == 0 && t.Status.Message != nil {
		for _, p := range t.Status.Message.Parts {
			chunks = append(chunks, p.Text)
		}
	}
	return strings.Join(chunks, "")
}
func validateTask(t *RemoteTask) error {
	if t == nil || t.ID == "" || !agentIDPattern.MatchString(t.ContextID) {
		return fmt.Errorf("kagent: missing task/session identity")
	}
	switch t.Status.State {
	case StateSubmitted, StateWorking, StateCompleted, StateFailed, StateCanceled, StateRejected, StateInputRequired, StateAuthRequired:
		return nil
	default:
		return fmt.Errorf("kagent: unsupported remote state")
	}
}

// A2AClient uses the public alpha7 HTTP JSON-RPC gateway, never actor addresses.
// Sends retry only alpha7's structured proof of non-dispatch. Persist the
// message ID before sending; reconcile an ambiguous send before any new turn.
type A2AClient struct{ wire *wireClient }

func NewA2AClient(c HTTPConfig) (*A2AClient, error) {
	w, err := newWire(c)
	if err != nil {
		return nil, err
	}
	return &A2AClient{w}, nil
}

type RPCError struct {
	Code            int
	sendNotAccepted bool
}

// alpha7 ErrSendNotAccepted and a2a-go/v2 v2.6.0 JSON-RPC ErrorInfo. Neither
// message strings nor a generic -32004 prove that runtime dispatch was skipped.
func provesSendNotAccepted(code int, data json.RawMessage) bool {
	if code != -32004 {
		return false
	}
	var details []struct {
		Type           string `json:"@type"`
		Reason, Domain string
		Metadata       struct{ Reason string }
	}
	if json.Unmarshal(data, &details) != nil || len(details) != 1 {
		return false
	}
	d := details[0]
	return d.Type == "type.googleapis.com/google.rpc.ErrorInfo" && d.Domain == "a2a-protocol.org" && d.Reason == "UNSUPPORTED_OPERATION" && d.Metadata.Reason == "KAGENT_SEND_NOT_ACCEPTED"
}

func (e *RPCError) Error() string { return fmt.Sprintf("kagent: JSON-RPC error %d", e.Code) }

func (c *A2AClient) call(ctx context.Context, ns, name, method string, params, out any) error {
	if !namespacePattern.MatchString(ns) || !resourcePattern.MatchString(name) {
		return ErrInvalidDesired
	}
	id := NewID()
	req := struct {
		JSONRPC string `json:"jsonrpc"`
		ID      string `json:"id"`
		Method  string `json:"method"`
		Params  any    `json:"params"`
	}{"2.0", id, method, params}
	var resp struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      string          `json:"id"`
		Result  json.RawMessage `json:"result"`
		Error   *struct {
			Code int
			Data json.RawMessage
		} `json:"error"`
	}
	if err := c.wire.do(ctx, "POST", "/agents/"+ns+"/"+name, "application/json", req, &resp); err != nil {
		return err
	}
	if resp.JSONRPC != "2.0" || resp.ID != id {
		return fmt.Errorf("kagent: mismatched JSON-RPC response")
	}
	if resp.Error != nil {
		return &RPCError{Code: resp.Error.Code, sendNotAccepted: (len(resp.Result) == 0 || string(resp.Result) == "null") && provesSendNotAccepted(resp.Error.Code, resp.Error.Data)}
	}
	if len(resp.Result) == 0 || string(resp.Result) == "null" || json.Unmarshal(resp.Result, out) != nil {
		return fmt.Errorf("kagent: malformed JSON-RPC result")
	}
	return nil
}
func (c *A2AClient) Send(ctx context.Context, ns, name, messageID, sessionID, prompt string) (*RemoteTask, error) {
	if !agentIDPattern.MatchString(messageID) || (sessionID != "" && !agentIDPattern.MatchString(sessionID)) {
		return nil, ErrInvalidDesired
	}
	params := struct {
		Message       RemoteMessage `json:"message"`
		Configuration struct {
			ReturnImmediately   bool     `json:"returnImmediately"`
			AcceptedOutputModes []string `json:"acceptedOutputModes"`
		} `json:"configuration"`
	}{Message: RemoteMessage{messageID, sessionID, "ROLE_USER", []Part{{Text: prompt}}}}
	params.Configuration.ReturnImmediately = true
	params.Configuration.AcceptedOutputModes = []string{"text/plain"}
	var result struct {
		Task    *RemoteTask     `json:"task"`
		Message json.RawMessage `json:"message"`
	}
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		err := c.call(ctx, ns, name, "SendMessage", params, &result)
		if err == nil {
			break
		}
		var rpc *RPCError
		if attempt >= 2 || !errors.As(err, &rpc) || !rpc.sendNotAccepted {
			return nil, err
		}
		// Keep the exact message/session identity and durable agent fence throughout
		// the bounded retry. Ignore remote retry hints and never retry uncertainty.
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	if len(result.Message) != 0 && string(result.Message) != "null" {
		return nil, fmt.Errorf("kagent: task response required")
	}
	if err := validateTask(result.Task); err != nil {
		return nil, err
	}
	if sessionID != "" && result.Task.ContextID != sessionID {
		return nil, fmt.Errorf("kagent: session identity changed")
	}
	return result.Task, nil
}
func (c *A2AClient) Get(ctx context.Context, ns, name, taskID, sessionID string) (*RemoteTask, error) {
	return c.taskCall(ctx, ns, name, "GetTask", taskID, sessionID)
}
func (c *A2AClient) Cancel(ctx context.Context, ns, name, taskID, sessionID string) (*RemoteTask, error) {
	return c.taskCall(ctx, ns, name, "CancelTask", taskID, sessionID)
}
func (c *A2AClient) taskCall(ctx context.Context, ns, name, method, taskID, sessionID string) (*RemoteTask, error) {
	if taskID == "" || !agentIDPattern.MatchString(sessionID) {
		return nil, ErrInvalidDesired
	}
	var task RemoteTask
	if err := c.call(ctx, ns, name, method, map[string]string{"id": taskID}, &task); err != nil {
		return nil, err
	}
	if err := validateTask(&task); err != nil {
		return nil, err
	}
	if task.ID != taskID || task.ContextID != sessionID {
		return nil, fmt.Errorf("kagent: task identity changed")
	}
	return &task, nil
}
