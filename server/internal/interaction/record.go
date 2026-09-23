// Package interaction defines the durable, run-scoped control record.
// Keeping it on the task row gives input acceptance and finish a single lock.
package interaction

import (
	"errors"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/pkg/agent"
)

type Input struct {
	agent.InteractionCommand
	ActorID   string                    `json:"actor_id"`
	CreatedAt time.Time                 `json:"created_at"`
	Receipt   *agent.InteractionReceipt `json:"receipt,omitempty"`
	Error     string                    `json:"error,omitempty"`
}

type Record struct {
	OpeningInput string                    `json:"opening_input,omitempty"`
	Owner        string                    `json:"owner"`
	State        agent.InteractionSnapshot `json:"state"`
	Deadline     time.Time                 `json:"deadline"`
	UpdatedAt    time.Time                 `json:"updated_at"`
	Finishing    bool                      `json:"finishing"`
	Commands     []Input                   `json:"commands"`
	Followups    []Followup                `json:"followups,omitempty"`
}

type Followup struct {
	ID      string `json:"id"`
	ActorID string `json:"actor_id"`
	Text    string `json:"text"`
	RunID   string `json:"run_id"`
}

type Sync struct {
	Owner           string                    `json:"owner"`
	Register        bool                      `json:"register,omitempty"`
	Finish          bool                      `json:"finish,omitempty"`
	Deadline        time.Time                 `json:"deadline,omitempty"`
	State           agent.InteractionSnapshot `json:"state"`
	Acknowledgement *Input                    `json:"acknowledgement,omitempty"`
}

func (r *Record) Accept(command agent.InteractionCommand, actor string, now time.Time) error {
	for _, input := range r.Commands {
		if input.ID == command.ID {
			if input.InteractionCommand == command && input.ActorID == actor {
				return nil
			}
			return errors.New("command id already used")
		}
	}
	if r.Finishing || !now.Before(r.Deadline) {
		return errors.New("run is finishing or expired")
	}
	if now.Sub(r.UpdatedAt) > 15*time.Second {
		return errors.New("run controller is disconnected; wait for reconnection")
	}
	if r.State.State != agent.InteractionWorking && r.State.State != agent.InteractionAwaitingInput {
		return errors.New("run is not ready for input")
	}
	if command.Activity != r.State.Activity {
		return agent.ErrInteractionStale
	}
	if command.ID == "" || len(command.ID) > 128 || len(command.Text) > 65536 {
		return errors.New("invalid command size")
	}
	if command.Kind != "input" && command.Kind != "interrupt" && command.Kind != "finish" && command.Kind != "complete" {
		return errors.New("unsupported command")
	}
	if command.Kind == "finish" && r.State.State != agent.InteractionAwaitingInput {
		return errors.New("interrupt or wait for the reply before finishing the run")
	}
	if command.Kind == "input" && strings.TrimSpace(command.Text) == "" {
		return errors.New("empty input")
	}
	// Only one in-flight delivery per run. This also serializes Stop with sends
	// from other tabs and bounds the reconciliation work after reconnect.
	for _, input := range r.Commands {
		if input.Kind == "finish" && input.Error == "" {
			return errors.New("run finish already requested")
		}
		if input.Receipt == nil && input.Error == "" {
			return agent.ErrInteractionBusy
		}
	}
	if len(r.Commands) >= 128 {
		return errors.New("run input limit reached; finish this run before continuing")
	}
	r.Commands = append(r.Commands, Input{InteractionCommand: command, ActorID: actor, CreatedAt: now})
	return nil
}

func (r *Record) Pending() bool {
	for _, command := range r.Commands {
		if command.Receipt == nil && command.Error == "" {
			return true
		}
	}
	return false
}
