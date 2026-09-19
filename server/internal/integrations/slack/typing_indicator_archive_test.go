package slack

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/slack-go/slack"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The task-state and transaction matrix lives in service/task_archive_cancel_test.go.
// This regression follows one archive through the real bus subscriber and Slack
// SDK, replacing only Slack's HTTP endpoint with a local server.
func TestTypingIndicator_ArchiveRemovesSlackReaction(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	f := testutil.New(pool, "", "")
	suffix := fmt.Sprint(time.Now().UnixNano())
	f.UserID = f.User(t, "Archive reaction test", "archive-reaction-"+suffix+"@example.test")
	f.WorkspaceID = f.Workspace(t, "Archive reaction test", "archive-reaction-"+suffix)
	f.Member(t, f.WorkspaceID, f.UserID, "owner")
	runtimeID := f.Runtime(t, "Archive reaction test")
	agentID := f.Agent(t, "Archive reaction test", runtimeID)
	sessionID := f.ChatSession(t, agentID)
	taskID := f.Task(t, agentID, testutil.Cols{
		"chat_session_id": sessionID, "status": "running", "runtime_id": runtimeID,
	})
	installationID := f.Insert(t, "channel_installation", testutil.Cols{
		"workspace_id": f.WorkspaceID, "agent_id": agentID, "channel_type": "slack",
		"installer_user_id": f.UserID, "config": slackInstallConfigJSON(), "status": "active",
	})
	const channelID = "C_ARCHIVE_TEST"
	ts := freshTS()
	f.Insert(t, "channel_chat_session_binding", testutil.Cols{
		"chat_session_id": sessionID, "installation_id": installationID, "channel_type": "slack",
		"channel_chat_id": channelID, "chat_type": "group", "last_message_id": ts,
	})
	q := db.New(pool)
	inst, err := q.GetChannelInstallation(ctx, db.GetChannelInstallationParams{
		ID: util.MustParseUUID(installationID), ChannelType: "slack",
	})
	if err != nil {
		t.Fatal(err)
	}

	type reactionCall struct{ path, channel, timestamp, name string }
	var mu sync.Mutex
	var calls []reactionCall
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		mu.Lock()
		calls = append(calls, reactionCall{r.URL.Path, r.FormValue("channel"), r.FormValue("timestamp"), r.FormValue("name")})
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(apiServer.Close)
	manager := NewTypingIndicatorManager(q, nil, nil)
	manager.newAPI = func(c credentials) reactionAPI {
		return slack.New(c.BotToken, slack.OptionAPIURL(apiServer.URL+"/"), slack.OptionHTTPClient(apiServer.Client()))
	}
	bus := events.New()
	manager.Register(bus)
	manager.Add(ctx, inst, util.MustParseUUID(sessionID), channelID, ts)
	mu.Lock()
	added := append([]reactionCall(nil), calls...)
	mu.Unlock()
	if len(added) != 1 || added[0] != (reactionCall{"/reactions.add", channelID, ts, typingEmoji}) {
		t.Fatalf("fixture must add the processing reaction through Slack HTTP: %+v", added)
	}

	// ArchiveAgent commits before the handler calls CancelTasksForArchivedAgent.
	if _, err := q.ArchiveAgent(ctx, db.ArchiveAgentParams{
		ID: util.MustParseUUID(agentID), ArchivedBy: util.MustParseUUID(f.UserID),
	}); err != nil {
		t.Fatal(err)
	}
	svc := &service.TaskService{Queries: q, TxStarter: pool, Bus: bus}
	if _, err := svc.CancelTasksForArchivedAgent(ctx, util.MustParseUUID(agentID)); err != nil {
		t.Fatal(err)
	}
	var status string
	f.QueryRow(t, "SELECT status FROM agent_task_queue WHERE id=$1", taskID).Scan(&status)
	if status != "cancelled" {
		t.Fatalf("task status = %q, want cancelled", status)
	}
	mu.Lock()
	got := append([]reactionCall(nil), calls...)
	mu.Unlock()
	if len(got) != 2 || got[1] != (reactionCall{"/reactions.remove", channelID, ts, typingEmoji}) {
		t.Fatalf("archived task is cancelled, but Slack must remove its processing reaction: %+v", got)
	}
}
