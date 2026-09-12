package selfhosttelemetry

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestConfigFromDoNotTrack(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		raw     string
		enabled bool
	}{
		{"", true},
		{"0", true},
		{"false", true},
		{"yes", true},
		{"1", false},
		{" true ", false},
		{"TRUE", false},
		{" TrUe\t", false},
	} {
		t.Run(tt.raw, func(t *testing.T) {
			t.Parallel()
			if got := ConfigFromDoNotTrack(tt.raw).Enabled; got != tt.enabled {
				t.Fatalf("Enabled = %v, want %v", got, tt.enabled)
			}
		})
	}
}

func TestLogStartupStatus(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		message string
	}{
		{name: "enabled", config: Config{Enabled: true}, message: "self-host telemetry enabled"},
		{name: "disabled", config: Config{Enabled: false}, message: "self-host telemetry disabled via DO_NOT_TRACK"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&output, nil))

			LogStartupStatus(logger, tt.config)

			if got := strings.Count(output.String(), "\n"); got != 1 {
				t.Fatalf("startup log lines = %d, want 1: %q", got, output.String())
			}
			if !strings.Contains(output.String(), "msg=\""+tt.message+"\"") {
				t.Fatalf("startup log = %q, want message %q", output.String(), tt.message)
			}
		})
	}
}

func TestDisabledConfigConstructsNoTelemetryDependencies(t *testing.T) {
	t.Parallel()
	constructed := 0
	factories := workerFactories{
		store: func() leaderStore {
			constructed++
			return nil
		},
		collector: func() eventCollector {
			constructed++
			return nil
		},
		sender: func() eventSender {
			constructed++
			return nil
		},
	}
	worker := newWithFactories(ConfigFromDoNotTrack(" TRUE "), "v1.0.0", nil, factories)
	if worker != nil {
		t.Fatal("disabled telemetry constructed a worker")
	}
	if constructed != 0 {
		t.Fatalf("disabled telemetry constructed %d dependencies, want 0 DB collectors and 0 HTTP clients", constructed)
	}
}

func TestEventGoldenJSON(t *testing.T) {
	t.Parallel()
	event := newEvent(
		uuid.MustParse("11111111-2222-3333-4444-555555555555"),
		time.Date(2026, 9, 10, 0, 0, 0, 987, time.FixedZone("offset", 8*60*60)),
		"v1.0.0",
		Counts{
			Workspaces: 2, HumanMembers: 6, Agents: 20, ActiveDaemons: 1,
			TasksStarted: 12, TasksCompleted: 10, TasksFailed: 1, TasksCancelled: 1,
		},
	)
	body, err := marshalEvent(event)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"event_name":"instance_daily_snapshot","schema_version":1,"instance_id":"11111111-2222-3333-4444-555555555555","occurred_at":"2026-09-09T16:00:00Z","payload":{"server_version":"v1.0.0","workspace_count_bucket":"2-5","human_member_count_bucket":"6-20","agent_count_bucket":"6-20","active_daemon_count_24h_bucket":"1","tasks_started_24h":12,"tasks_completed_24h":10,"tasks_failed_24h":1,"tasks_cancelled_24h":1}}`
	if string(body) != want {
		t.Fatalf("body mismatch\n got: %s\nwant: %s", body, want)
	}

	var top map[string]json.RawMessage
	if err := json.Unmarshal(body, &top); err != nil {
		t.Fatal(err)
	}
	if len(top) != 5 {
		t.Fatalf("top-level key count = %d, want exactly 5", len(top))
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(top["payload"], &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload) != 9 {
		t.Fatalf("payload key count = %d, want exactly 9", len(payload))
	}
}

func TestEventCannotPassFreeTextOrIdentifiers(t *testing.T) {
	t.Parallel()
	forbidden := []string{
		"person@example.test", "private.example.test", "192.0.2.10",
		"workspace-secret", "agent-secret", "daemon-secret", "task-secret",
		"hostname-secret", "repo-secret", "plugin-secret", "model-secret",
		"prompt-secret", "output-secret", "/private/file", "token-secret",
		"cost-secret", "credential-secret", "error-secret", "stack-secret",
		"log-secret",
	}
	event := newEvent(uuid.MustParse("11111111-2222-3333-4444-555555555555"), time.Now(), strings.Join(forbidden, ":"), Counts{})
	body, err := marshalEvent(event)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range forbidden {
		if strings.Contains(string(body), value) {
			t.Errorf("body contains prohibited value %q: %s", value, body)
		}
	}
	if !strings.Contains(string(body), `"server_version":"unknown"`) {
		t.Fatalf("untrusted build string was not normalized: %s", body)
	}
}

func TestCountBucketBoundaries(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		count int64
		want  string
	}{
		{-1, "0"}, {0, "0"}, {1, "1"}, {2, "2-5"}, {5, "2-5"},
		{6, "6-20"}, {20, "6-20"}, {21, "21-100"}, {100, "21-100"}, {101, "101+"},
	} {
		if got := countBucket(tt.count); got != tt.want {
			t.Errorf("countBucket(%d) = %q, want %q", tt.count, got, tt.want)
		}
	}
}

func TestNormalizeVersion(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		input string
		want  string
	}{
		{"v1.2.3", "v1.2.3"},
		{"1.2.3", "1.2.3"},
		{" v0.4.30 ", "v0.4.30"},
		{"dev", "unknown"},
		{"", "unknown"},
		{"v1.2.3-dirty", "unknown"},
		{"v1.2.3-4-gabcdef", "unknown"},
		{"abcdef012345", "unknown"},
		{"v01.2.3", "unknown"},
	} {
		if got := normalizeVersion(tt.input); got != tt.want {
			t.Errorf("normalizeVersion(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestClampTaskCount(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		input int64
		want  int64
	}{
		{-1, 0}, {0, 0}, {999_999, 999_999}, {1_000_000, 1_000_000}, {1_000_001, 1_000_000},
	} {
		if got := clampTaskCount(tt.input); got != tt.want {
			t.Errorf("clampTaskCount(%d) = %d, want %d", tt.input, got, tt.want)
		}
	}
}
