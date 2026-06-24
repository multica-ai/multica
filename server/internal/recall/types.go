package recall

import (
	"encoding/json"
	"time"
)

const CurrentIndexVersion = 1

type Status string

const (
	StatusHit             Status = "hit"
	StatusNoHit           Status = "no_hit"
	StatusControlledError Status = "controlled_error"
)

type Query struct {
	IssueTitle       string
	IssueDescription string
	TriggerComment   string
}

func (q Query) Text() string {
	parts := make([]string, 0, 3)
	for _, value := range []string{q.IssueTitle, q.IssueDescription, q.TriggerComment} {
		if value != "" {
			parts = append(parts, value)
		}
	}
	return joinWithNewlines(parts)
}

type Options struct {
	VaultRoot      string
	MaxHits        int
	MaxBundleBytes int
	MaxIndexAge    time.Duration
	Now            func() time.Time
}

type Hit struct {
	Path      string  `json:"path"`
	Recency   string  `json:"recency"`
	Relevance float64 `json:"relevance"`
	Excerpt   string  `json:"excerpt"`
}

type Result struct {
	Status       Status   `json:"recall_status"`
	HitCount     int      `json:"hit_count"`
	Query        string   `json:"query"`
	IndexVersion int      `json:"index_version"`
	ByteBudget   int      `json:"byte_budget"`
	BytesUsed    int      `json:"bytes_used"`
	Reason       string   `json:"reason,omitempty"`
	Hits         []Hit    `json:"hits"`
	SkippedFiles []string `json:"-"`
}

func (r *Result) Render() string {
	for range 8 {
		data, err := json.Marshal(r)
		if err != nil {
			return `{"recall_status":"controlled_error","reason":"render_failed"}`
		}
		bytesUsed := len(data)
		if bytesUsed == r.BytesUsed {
			return string(data)
		}
		r.BytesUsed = bytesUsed
	}
	data, _ := json.Marshal(r)
	return string(data)
}

type Entry struct {
	Path        string   `json:"path"`
	Title       string   `json:"title"`
	Tags        []string `json:"tags,omitempty"`
	MTime       string   `json:"mtime"`
	Summary     string   `json:"summary"`
	FolderClass string   `json:"folder_class"`
	SizeBytes   int64    `json:"size_bytes"`
}

type Index struct {
	IndexVersion int     `json:"index_version"`
	GeneratedAt  string  `json:"generated_at"`
	VaultCommit  string  `json:"vault_commit,omitempty"`
	EntryCount   int     `json:"entry_count"`
	Entries      []Entry `json:"entries"`
}

type IndexOptions struct {
	VaultRoot string
	Now       func() time.Time
}

func joinWithNewlines(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	result := parts[0]
	for _, part := range parts[1:] {
		result += "\n" + part
	}
	return result
}
