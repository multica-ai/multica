package commands

import (
	"time"

	"github.com/google/uuid"
)

type Command struct {
	ID            uuid.UUID `json:"id"`
	WorkspaceID   uuid.UUID `json:"workspace_id"`
	Key           string    `json:"key"`
	Title         string    `json:"title"`
	Description   string    `json:"description"`
	Argv          []string  `json:"argv"`
	CreatedByID   uuid.UUID `json:"created_by_id"`
	CreatedByType string    `json:"created_by_type"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type CommandInput struct {
	Key         string   `json:"key"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Argv        []string `json:"argv"`
}
