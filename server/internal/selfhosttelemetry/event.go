package selfhosttelemetry

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	eventName      = "instance_daily_snapshot"
	schemaVersion  = 1
	maxRequestSize = 16 * 1024
	maxTaskCount   = int64(1_000_000)
)

var releaseVersionPattern = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

// Event is the complete V1 wire contract. Keep this strongly typed: the Cloud
// receiver rejects unknown fields, and arbitrary maps could accidentally turn
// internal data into an outbound field.
type Event struct {
	EventName     string  `json:"event_name"`
	SchemaVersion int     `json:"schema_version"`
	InstanceID    string  `json:"instance_id"`
	OccurredAt    string  `json:"occurred_at"`
	Payload       Payload `json:"payload"`
}

type Payload struct {
	ServerVersion              string `json:"server_version"`
	WorkspaceCountBucket       string `json:"workspace_count_bucket"`
	HumanMemberCountBucket     string `json:"human_member_count_bucket"`
	AgentCountBucket           string `json:"agent_count_bucket"`
	ActiveDaemonCount24hBucket string `json:"active_daemon_count_24h_bucket"`
	TasksStarted24h            int64  `json:"tasks_started_24h"`
	TasksCompleted24h          int64  `json:"tasks_completed_24h"`
	TasksFailed24h             int64  `json:"tasks_failed_24h"`
	TasksCancelled24h          int64  `json:"tasks_cancelled_24h"`
}

// Counts contains deployment-wide aggregates at one snapshot instant.
type Counts struct {
	Workspaces     int64
	HumanMembers   int64
	Agents         int64
	ActiveDaemons  int64
	TasksStarted   int64
	TasksCompleted int64
	TasksFailed    int64
	TasksCancelled int64
}

func newEvent(instanceID uuid.UUID, occurredAt time.Time, serverVersion string, counts Counts) Event {
	return Event{
		EventName:     eventName,
		SchemaVersion: schemaVersion,
		InstanceID:    instanceID.String(),
		OccurredAt:    occurredAt.UTC().Format(time.RFC3339),
		Payload: Payload{
			ServerVersion:              normalizeVersion(serverVersion),
			WorkspaceCountBucket:       countBucket(counts.Workspaces),
			HumanMemberCountBucket:     countBucket(counts.HumanMembers),
			AgentCountBucket:           countBucket(counts.Agents),
			ActiveDaemonCount24hBucket: countBucket(counts.ActiveDaemons),
			TasksStarted24h:            clampTaskCount(counts.TasksStarted),
			TasksCompleted24h:          clampTaskCount(counts.TasksCompleted),
			TasksFailed24h:             clampTaskCount(counts.TasksFailed),
			TasksCancelled24h:          clampTaskCount(counts.TasksCancelled),
		},
	}
}

func marshalEvent(event Event) ([]byte, error) {
	body, err := json.Marshal(event)
	if err != nil {
		return nil, err
	}
	if len(body) > maxRequestSize {
		return nil, errors.New("telemetry event exceeds 16 KiB limit")
	}
	return body, nil
}

func countBucket(count int64) string {
	switch {
	case count <= 0:
		return "0"
	case count == 1:
		return "1"
	case count <= 5:
		return "2-5"
	case count <= 20:
		return "6-20"
	case count <= 100:
		return "21-100"
	default:
		return "101+"
	}
}

func clampTaskCount(count int64) int64 {
	if count < 0 {
		return 0
	}
	if count > maxTaskCount {
		return maxTaskCount
	}
	return count
}

func normalizeVersion(version string) string {
	version = strings.TrimSpace(version)
	if !releaseVersionPattern.MatchString(version) {
		return "unknown"
	}
	return version
}
