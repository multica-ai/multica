package store

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type Note struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Content   string    `json:"content"`
	VaultPath string    `json:"vault_path"`
	Source    string    `json:"source"`
	ThreadID  string    `json:"thread_id"`
	Synced    bool      `json:"synced"`
	SyncedAt  *string   `json:"synced_at,omitempty"`
	CreatedAt string    `json:"created_at"`
}

type NoteStore struct {
	db *sql.DB
}

func NewNoteStore(dbPath string) (*NoteStore, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS notes (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL,
			content TEXT NOT NULL,
			vault_path TEXT NOT NULL,
			source TEXT NOT NULL DEFAULT 'telegram',
			thread_id TEXT NOT NULL DEFAULT '',
			synced INTEGER NOT NULL DEFAULT 0,
			synced_at TEXT,
			created_at TEXT NOT NULL
		)
	`)
	if err != nil {
		return nil, fmt.Errorf("create notes table: %w", err)
	}

	return &NoteStore{db: db}, nil
}

func (s *NoteStore) Add(note Note) error {
	if note.CreatedAt == "" {
		note.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	_, err := s.db.Exec(
		`INSERT INTO notes (id, title, content, vault_path, source, thread_id, synced, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, 0, ?)`,
		note.ID, note.Title, note.Content, note.VaultPath, note.Source, note.ThreadID, note.CreatedAt,
	)
	return err
}

func (s *NoteStore) ListUnsynced() ([]Note, error) {
	rows, err := s.db.Query(
		`SELECT id, title, content, vault_path, source, thread_id, synced, synced_at, created_at
		 FROM notes WHERE synced = 0 ORDER BY created_at ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var notes []Note
	for rows.Next() {
		var n Note
		if err := rows.Scan(&n.ID, &n.Title, &n.Content, &n.VaultPath, &n.Source, &n.ThreadID, &n.Synced, &n.SyncedAt, &n.CreatedAt); err != nil {
			return nil, err
		}
		notes = append(notes, n)
	}
	return notes, nil
}

func (s *NoteStore) MarkSynced(id string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.Exec(`UPDATE notes SET synced = 1, synced_at = ? WHERE id = ?`, now, id)
	return err
}

func (s *NoteStore) Close() error {
	return s.db.Close()
}
