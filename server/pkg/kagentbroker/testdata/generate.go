//go:build ignore

// Run in a temporary module with a2a-go/v2 v2.6.0, as described in README.md.
package main

import (
	"encoding/json"
	"os"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

func main() {
	timestamp := time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC)
	const taskID = a2a.TaskID("44444444-4444-4444-8444-444444444444")
	const contextID = "55555555-5555-4555-8555-555555555555"
	user := &a2a.Message{ID: "33333333-3333-4333-8333-333333333333", Role: a2a.MessageRoleUser, TaskID: taskID, ContextID: contextID, Parts: a2a.ContentParts{a2a.NewTextPart("hello")}}
	part := a2a.NewTextPart("alpha7 answer")
	part.Metadata = map[string]any{"kagent.dev/a2a/part-type": "text", "private": "gateway-credential"}
	answer := &a2a.Message{ID: "agent-message-1", Role: a2a.MessageRoleAgent, TaskID: taskID, ContextID: contextID, Parts: a2a.ContentParts{part}, Extensions: []string{"https://kagent.dev/a2a/extension"}, Metadata: map[string]any{"kagent.dev/a2a/timeline-position": timestamp.Format(time.RFC3339Nano)}}
	task := &a2a.Task{ID: taskID, ContextID: contextID, Status: a2a.TaskStatus{State: a2a.TaskStateCompleted, Timestamp: &timestamp, Message: answer}, History: []*a2a.Message{user, answer}, Metadata: map[string]any{"kagent.dev/a2a/task-created-at": timestamp.Format(time.RFC3339Nano), "private": "caller-credential"}, Artifacts: []*a2a.Artifact{{ID: "result", Name: "answer", Parts: a2a.ContentParts{part}, Metadata: map[string]any{"private": "gateway-credential"}, Extensions: []string{"https://kagent.dev/a2a/extension"}}}}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(task); err != nil {
		panic(err)
	}
}
