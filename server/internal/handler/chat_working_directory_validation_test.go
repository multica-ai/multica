package handler

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestNormalizeChatWorkingDirectory(t *testing.T) {
	for _, path := range []string{"/tmp/项目 with spaces", "/tmp/项目 trailing space ", `C:\Users\Jane Doe\项目`, `\\host\share\项目`} {
		got, err := normalizeChatWorkingDirectory(&protocol.ChatWorkingDirectory{DaemonID: " host ", Path: path})
		if err != nil || got.DaemonID != "host" || got.Path != path {
			t.Fatalf("normalize(%q) = %+v, %v", path, got, err)
		}
	}
	for _, value := range []protocol.ChatWorkingDirectory{{}, {DaemonID: "host", Path: "relative"}, {DaemonID: "host", Path: " "}, {Path: "/tmp"}, {DaemonID: "host", Path: "/tmp/\x00bad"}} {
		if _, err := normalizeChatWorkingDirectory(&value); err == nil {
			t.Errorf("accepted invalid directory %+v", value)
		}
	}
	if got, err := normalizeChatWorkingDirectory(nil); got != nil || err != nil {
		t.Fatalf("nil = %+v, %v", got, err)
	}
}

func TestValidateChatDirectoryRuntime(t *testing.T) {
	owner := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	base := db.AgentRuntime{RuntimeMode: "local", Status: "online", DaemonID: pgtype.Text{String: "host", Valid: true}, OwnerID: owner, Metadata: []byte(`{"capabilities":["chat-working-directory-v1"]}`)}
	if err := validateChatDirectoryRuntime(base, "host", "01000000-0000-0000-0000-000000000000"); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name          string
		change        func(*db.AgentRuntime)
		daemon, owner string
	}{
		{"cloud", func(r *db.AgentRuntime) { r.RuntimeMode = "cloud" }, "host", ""},
		{"offline", func(r *db.AgentRuntime) { r.Status = "offline" }, "host", ""},
		{"missing machine", func(r *db.AgentRuntime) { r.DaemonID = pgtype.Text{} }, "host", ""},
		{"wrong machine", func(r *db.AgentRuntime) {}, "other", ""},
		{"unsupported", func(r *db.AgentRuntime) { r.Metadata = []byte(`{}`) }, "host", ""},
		{"non owner", func(r *db.AgentRuntime) {}, "host", "01900000-0000-7000-8000-000000000002"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rt := base
			tc.change(&rt)
			if err := validateChatDirectoryRuntime(rt, tc.daemon, tc.owner); err == nil {
				t.Fatal("accepted ineligible runtime")
			}
		})
	}
}

func TestDecodeStoredChatWorkingDirectory(t *testing.T) {
	for _, raw := range []string{"", "null"} {
		if got, err := decodeStoredChatWorkingDirectory([]byte(raw)); got != nil || err != nil {
			t.Fatalf("default %q = %+v, %v", raw, got, err)
		}
	}
	for _, raw := range []string{`{`, `{}`, `{"daemon_id":"host","path":"relative"}`, `{"daemon_id":4,"path":"/tmp"}`} {
		if _, err := decodeStoredChatWorkingDirectory([]byte(raw)); err == nil {
			t.Errorf("accepted corrupt stored selection %q", raw)
		}
	}
}

func TestChatWorkingDirectoryClaim(t *testing.T) {
	runtimeID := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	workspaceID := pgtype.UUID{Bytes: [16]byte{2}, Valid: true}
	runtime := db.AgentRuntime{Provider: "openclaw", ID: runtimeID, WorkspaceID: workspaceID, RuntimeMode: "local", Status: "online", DaemonID: pgtype.Text{String: "host", Valid: true}, Metadata: []byte(`{"capabilities":["chat-working-directory-v1"]}`)}
	agent := db.Agent{RuntimeID: runtimeID, WorkspaceID: workspaceID, RuntimeConfig: []byte(`{}`)}
	session := db.ChatSession{WorkspaceID: workspaceID, WorkingDirectory: []byte(`{"daemon_id":"host","path":"/tmp/项目 with spaces"}`)}
	got, err := chatWorkingDirectoryForClaim(session, agent, runtime, true)
	if err != nil || got == nil || got.Path != "/tmp/项目 with spaces" {
		t.Fatalf("claim = %+v, %v", got, err)
	}
	t.Run("default does not require a local runtime", func(t *testing.T) {
		got, err := chatWorkingDirectoryForClaim(db.ChatSession{}, db.Agent{}, db.AgentRuntime{}, true)
		if err != nil || got != nil {
			t.Fatalf("default=%+v,%v", got, err)
		}
	})
	t.Run("corrupt saved path fails", func(t *testing.T) {
		bad := session
		bad.WorkingDirectory = []byte(`{}`)
		if _, err := chatWorkingDirectoryForClaim(bad, agent, runtime, true); err == nil {
			t.Fatal("corrupt saved path silently fell back")
		}
	})
	t.Run("rebound agent fails", func(t *testing.T) {
		bad := agent
		bad.RuntimeID = workspaceID
		if _, err := chatWorkingDirectoryForClaim(session, bad, runtime, true); err == nil {
			t.Fatal("rebound agent accepted")
		}
	})
	t.Run("gateway fails", func(t *testing.T) {
		bad := agent
		bad.RuntimeConfig = []byte(`{"mode":"gateway"}`)
		if _, err := chatWorkingDirectoryForClaim(session, bad, runtime, true); err == nil {
			t.Fatal("gateway directory accepted")
		}
	})
	t.Run("withdrawn capability fails", func(t *testing.T) {
		bad := runtime
		bad.Metadata = []byte(`{}`)
		if _, err := chatWorkingDirectoryForClaim(session, agent, bad, true); err == nil {
			t.Fatal("unsupported claim accepted")
		}
	})
}

func TestChatWorkingDirectoryClaimRequiresCurrentCapability(t *testing.T) {
	session := db.ChatSession{WorkingDirectory: []byte(`{"daemon_id":"host","path":"/tmp"}`)}
	if _, err := chatWorkingDirectoryForClaim(session, db.Agent{}, db.AgentRuntime{Metadata: []byte(`{"capabilities":["chat-working-directory-v1"]}`)}, false); err == nil {
		t.Fatal("current daemon omitted capability but received an explicit directory")
	}
}

func TestChatWorkingDirectoryIgnoresUnrelatedGatewayConfig(t *testing.T) {
	runtimeID := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	workspaceID := pgtype.UUID{Bytes: [16]byte{2}, Valid: true}
	runtime := db.AgentRuntime{Provider: "codex", ID: runtimeID, WorkspaceID: workspaceID, RuntimeMode: "local", Status: "online", DaemonID: pgtype.Text{String: "host", Valid: true}, Metadata: []byte(`{"capabilities":["chat-working-directory-v1"]}`)}
	agent := db.Agent{RuntimeID: runtimeID, WorkspaceID: workspaceID, RuntimeConfig: []byte(`{"mode":"gateway"}`)}
	session := db.ChatSession{WorkspaceID: workspaceID, WorkingDirectory: []byte(`{"daemon_id":"host","path":"/tmp"}`)}
	if _, err := chatWorkingDirectoryForClaim(session, agent, runtime, true); err != nil {
		t.Fatalf("unrelated provider config rejected: %v", err)
	}
}
