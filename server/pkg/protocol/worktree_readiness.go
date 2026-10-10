package protocol

import "encoding/json"

const (
	WorktreeReplayMaxFiles = 2000
	WorktreeReplayMaxBytes = 200 << 20
)

// WorktreeReadiness is an additive, structured resource diagnostic. Timestamps
// are server-authored and bound to the exact resource_ref inspected by a daemon.
type WorktreeReadiness struct {
	UsageUnavailable bool                    `json:"-"`
	ReasonCode       string                  `json:"reason_code,omitempty"`
	Status           string                  `json:"status"`
	CheckedAt        string                  `json:"checked_at,omitempty"`
	ExpiresAt        string                  `json:"expires_at,omitempty"`
	FileCount        int                     `json:"file_count"`
	TotalBytes       int64                   `json:"total_bytes"`
	MaxFiles         int                     `json:"max_files"`
	MaxBytes         int64                   `json:"max_bytes"`
	SymlinkCount     int                     `json:"symlink_count"`
	LargestPaths     []WorktreeReadinessPath `json:"largest_paths"`
	Message          string                  `json:"message,omitempty"`
}

type WorktreeReadinessPath struct {
	Path       string `json:"path"`
	FileCount  int    `json:"file_count"`
	TotalBytes int64  `json:"total_bytes"`
}

type WorktreeReadinessResource struct {
	ID          string          `json:"id"`
	ResourceRef json.RawMessage `json:"resource_ref"`
}

type WorktreeReadinessReport struct {
	WorktreeReadinessResource
	Measurement WorktreeReadiness `json:"measurement"`
}

// MarshalJSON distinguishes missing measurements from a measured empty tree.
// Daemon reports have no reason_code; the server adds it when projecting the
// stored observation and its freshness onto the resource API.
func (r WorktreeReadiness) MarshalJSON() ([]byte, error) {
	type wire WorktreeReadiness
	if !r.UsageUnavailable && r.Status != "checking" && r.ReasonCode != "inspection_failed" && !(r.ReasonCode == "daemon_unavailable" && r.CheckedAt == "") {
		return json.Marshal(wire(r))
	}
	return json.Marshal(struct {
		Status     string `json:"status"`
		ReasonCode string `json:"reason_code,omitempty"`
		CheckedAt  string `json:"checked_at,omitempty"`
		ExpiresAt  string `json:"expires_at,omitempty"`
		MaxFiles   int    `json:"max_files"`
		MaxBytes   int64  `json:"max_bytes"`
		Message    string `json:"message,omitempty"`
	}{r.Status, r.ReasonCode, r.CheckedAt, r.ExpiresAt, r.MaxFiles, r.MaxBytes, r.Message})
}
