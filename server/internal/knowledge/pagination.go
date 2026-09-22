package knowledge

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

type knowledgeCursor struct {
	CreatedAt string `json:"created_at,omitempty"`
	Name      string `json:"name,omitempty"`
	ID        string `json:"id"`
}

func encodeKnowledgeCreatedCursor(createdAt time.Time, id string) string {
	return encodeKnowledgeCursor(knowledgeCursor{CreatedAt: createdAt.UTC().Format(time.RFC3339Nano), ID: id})
}

func encodeKnowledgeEntityCursor(name, id string) string {
	return encodeKnowledgeCursor(knowledgeCursor{Name: name, ID: id})
}

func encodeKnowledgeCursor(cursor knowledgeCursor) string {
	data, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(data)
}

func decodeKnowledgeCreatedCursor(raw string) (time.Time, pgtype.UUID, error) {
	cursor, err := decodeKnowledgeCursor(raw)
	if err != nil || cursor.CreatedAt == "" {
		return time.Time{}, pgtype.UUID{}, badRequest("invalid_cursor", "cursor is invalid")
	}
	createdAt, err := time.Parse(time.RFC3339Nano, cursor.CreatedAt)
	if err != nil {
		return time.Time{}, pgtype.UUID{}, badRequest("invalid_cursor", "cursor is invalid")
	}
	id, err := parseID(cursor.ID, "cursor")
	if err != nil {
		return time.Time{}, pgtype.UUID{}, badRequest("invalid_cursor", "cursor is invalid")
	}
	return createdAt, id, nil
}

func decodeKnowledgeEntityCursor(raw string) (string, pgtype.UUID, error) {
	cursor, err := decodeKnowledgeCursor(raw)
	if err != nil || cursor.Name == "" {
		return "", pgtype.UUID{}, badRequest("invalid_cursor", "cursor is invalid")
	}
	id, err := parseID(cursor.ID, "cursor")
	if err != nil {
		return "", pgtype.UUID{}, badRequest("invalid_cursor", "cursor is invalid")
	}
	return cursor.Name, id, nil
}

func decodeKnowledgeCursor(raw string) (knowledgeCursor, error) {
	data, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(raw))
	if err != nil {
		return knowledgeCursor{}, badRequest("invalid_cursor", "cursor is invalid")
	}
	var cursor knowledgeCursor
	if err := json.Unmarshal(data, &cursor); err != nil || strings.TrimSpace(cursor.ID) == "" {
		return knowledgeCursor{}, badRequest("invalid_cursor", "cursor is invalid")
	}
	return cursor, nil
}

func knowledgePageLimit(limit int) int {
	if limit <= 0 {
		return 50
	}
	if limit > 100 {
		return 100
	}
	return limit
}
