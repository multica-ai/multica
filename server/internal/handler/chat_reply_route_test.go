package handler

import (
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// A Secretary reply from a later run takes the channel route of the session's
// channel-bound run, so Telegram or WeCom hears it. A session with no channel
// binding copies nothing.
func TestReplyChatSession_CopiesChannelRouteFromSessionRun(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	env := newChatReplyEnv(t)
	channelRun := env.runFor(t, env.secretary, env.session, testUserID)
	dbfx.Exec(t, `INSERT INTO channel_task_delivery
		(task_id, binding_id, installation_id, channel_type, channel_chat_id, chat_type, route_revision)
		VALUES ($1, gen_random_uuid(), gen_random_uuid(), 'telegram', '555', 'private', 1)`, channelRun)

	laterRun := env.runFor(t, env.secretary, env.session, testUserID)
	env.reply(t, env.session, env.secretary, laterRun, "Here is the assessment.").Want(http.StatusCreated)

	var routed int
	dbfx.QueryRow(t, `SELECT count(*) FROM channel_task_delivery WHERE task_id = $1 AND channel_chat_id = '555'`, laterRun).Scan(&routed)
	if routed != 1 {
		t.Fatalf("later run has %d route rows, want 1 copied from the channel run", routed)
	}

	// A session with no channel binding copies no route.
	webSession := dbfx.ChatSession(t, env.secretary, testutil.Cols{"creator_id": testUserID})
	webRun := env.runFor(t, env.secretary, webSession, testUserID)
	env.reply(t, webSession, env.secretary, webRun, "Web only.").Want(http.StatusCreated)
	dbfx.QueryRow(t, `SELECT count(*) FROM channel_task_delivery WHERE task_id = $1`, webRun).Scan(&routed)
	if routed != 0 {
		t.Fatalf("web-only run has %d route rows, want none", routed)
	}
}
