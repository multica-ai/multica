package wecom

// quoted_media_bind_db_test.go — the quoted-picture marker, end to end.
//
// quoted_marker_position_test.go proves the numbers the adapter stamps are the
// right ones for the text it renders. This file asks the question the user
// would: after the real Router, the real wecom resolver (HTTP download of
// really-encrypted bytes), the real ChatSession binder and a real Postgres, what
// does the stored message say, and does the id it names belong to the picture
// that was quoted.
//
// Each scenario runs over both download paths. The buffered and the streaming
// ingest build their MediaRef separately, and a stamp lost in either one leaves
// the marker "unavailable" while the attachment exists.

import (
	"context"
	"fmt"
	"io"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	dbfx "github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// quotedBindWorld is what the wecom ResolverSet resolves against: an
// installation keyed by bot id and a user binding keyed by the anonymized
// aibot userid, in one workspace.
type quotedBindWorld struct {
	workspaceID string
	botID       string
	senderID    string
}

// seedQuotedBindWorld builds the same rows seedMediaBindFixture does, through
// the shared testutil fixture so its teardown runs in reverse creation order.
func seedQuotedBindWorld(t *testing.T, pool *pgxpool.Pool) quotedBindWorld {
	t.Helper()
	tag := fmt.Sprintf("%d", time.Now().UnixNano())
	w := quotedBindWorld{botID: "wb-qbind-" + tag, senderID: "T-qbind-" + tag}

	f := dbfx.New(pool, "", "")
	f.UserID = f.User(t, "wecom quoted bind", "wecom-quoted-bind-"+tag+"@multica.test")
	f.WorkspaceID = f.Workspace(t, "wecom quoted bind", "wecom-quoted-bind-"+tag)
	w.workspaceID = f.WorkspaceID
	f.Member(t, f.WorkspaceID, f.UserID, "owner")
	runtimeID := f.Runtime(t, "wecom-qbind-runtime-"+tag, dbfx.Cols{"runtime_mode": "local", "provider": "multica_daemon"})
	agentID := f.Agent(t, "wecom-qbind-agent-"+tag, runtimeID, dbfx.Cols{"runtime_mode": "local"})
	installationID := f.Insert(t, "channel_installation", dbfx.Cols{
		"workspace_id":      f.WorkspaceID,
		"agent_id":          agentID,
		"channel_type":      "wecom",
		"config":            fmt.Sprintf(`{"app_id":%q,"bot_id":%q,"app_secret_encrypted":""}`, w.botID, w.botID),
		"installer_user_id": f.UserID,
		"status":            "active",
	})
	f.Insert(t, "channel_user_binding", dbfx.Cols{
		"workspace_id":    f.WorkspaceID,
		"multica_user_id": f.UserID,
		"installation_id": installationID,
		"channel_type":    "wecom",
		"channel_user_id": w.senderID,
	})
	// Rows the router and resolver write on their own: they reference the
	// workspace and the installation, so they go before either.
	f.Cleanup(t, `DELETE FROM channel_media_pending_object WHERE workspace_id = $1`, f.WorkspaceID)
	f.Cleanup(t, `DELETE FROM channel_inbound_message_dedup WHERE installation_id = $1`, installationID)
	return w
}

// countingStreamStorage is a backend that can stream, and says how often it was
// asked to. The buffered half is the embedded fake's Upload, which must stay
// untouched when streaming is available.
type countingStreamStorage struct {
	fakeStreamStorage
	mu2      sync.Mutex
	streamed int
}

func (s *countingStreamStorage) UploadStream(ctx context.Context, key string, data io.Reader, size int64, contentType, filename string) (string, error) {
	s.mu2.Lock()
	s.streamed++
	s.mu2.Unlock()
	return s.fakeStreamStorage.UploadStream(ctx, key, data, size, contentType, filename)
}

func (s *countingStreamStorage) streams() int {
	s.mu2.Lock()
	defer s.mu2.Unlock()
	return s.streamed
}

// newPictureServer serves one encrypted picture whose download names itself, so
// the attachment row it becomes can be told apart from the others.
func newPictureServer(t *testing.T, filename string) *httptest.Server {
	t.Helper()
	srv := cosServer(t, []byte("\xff\xd8\xff\xe0 "+filename), fmt.Sprintf(`attachment; filename=%q`, filename))
	t.Cleanup(srv.Close)
	return srv
}

type quotedBindScenario struct {
	name string
	// quoted builds the quoted message's runs; urls are the three pictures'
	// download addresses, "q1" and "q2" the quoted ones, "own" the sender's.
	quoted func(q1, q2 string) []wire
	// own builds the sender's own 图文混排; nil sends plain words.
	own func(own string) []wire
	// wantAttachments are the filenames that must exist on the message.
	wantAttachments []string
	// wantBody is the stored body once everything has landed. {q1} and {q2}
	// stand for the ids of the attachments the quote's pictures became.
	wantBody string
}

func quotedBindScenarios() []quotedBindScenario {
	return []quotedBindScenario{
		{
			// Review finding 1: the words quoted ahead of the picture spell the
			// marker out. The sentence must come out as it went in, and the
			// picture's own marker must carry its id.
			name: "a quoted sentence that spells the marker",
			quoted: func(q1, _ string) []wire {
				return []wire{qWords("The UI displays " + imgMarker), qImage(q1, testAESKey)}
			},
			own: func(own string) []wire {
				return []wire{qImage(own, testAESKey), qWords("看这个")}
			},
			wantAttachments: []string{"q1.jpg", "own.jpg"},
			wantBody:        "> [Quote] The UI displays [Image: unavailable]\n> [Image: {q1}]\n\n[Image]\n看这个",
		},
		{
			// Review finding 2: the words run past the budget, and a new picture
			// is sent with the reply. The quote has to name its picture, and the
			// id has to be the quoted one's, not the one just sent.
			name: "a quote cut at 500 runes beside a new picture",
			quoted: func(q1, _ string) []wire {
				return []wire{qWords(strings.Repeat("字", 501)), qImage(q1, testAESKey)}
			},
			own: func(own string) []wire {
				return []wire{qImage(own, testAESKey), qWords("还有这个")}
			},
			wantAttachments: []string{"q1.jpg", "own.jpg"},
			wantBody:        "> [Quote] " + strings.Repeat("字", 500) + "…\n> [Image: {q1}]\n\n[Image]\n还有这个",
		},
		{
			// Literals after the real marker, several of them, and one in the
			// sender's own words — none of them is rewritten, both pictures are.
			name: "literals after, between and in the sender's words",
			quoted: func(q1, q2 string) []wire {
				return []wire{
					qImage(q1, testAESKey),
					qWords("shown as " + imgMarker + " and " + imgMarker),
					qImage(q2, testAESKey),
				}
			},
			own: nil,
			// Plain words reply: the text message is "这个怎么处理".
			wantAttachments: []string{"q1.jpg", "q2.jpg"},
			wantBody: "> [Quote] [Image: {q1}]\n> shown as [Image: unavailable] and [Image: unavailable]\n" +
				"> [Image: {q2}]\n\n这个怎么处理",
		},
		{
			// Two pictures with long passages between them: the cut falls
			// between the markers and removes words, never a marker.
			name: "two pictures with the cut between them",
			quoted: func(q1, q2 string) []wire {
				return []wire{
					qWords(strings.Repeat("甲", 300)), qImage(q1, testAESKey),
					qWords(strings.Repeat("乙", 300)), qImage(q2, testAESKey),
				}
			},
			own:             nil,
			wantAttachments: []string{"q1.jpg", "q2.jpg"},
			// 300 + (newline, marker) = 321 used; the second passage gets
			// 500-321 runes less its newline.
			wantBody: "> [Quote] " + strings.Repeat("甲", 300) + "\n> [Image: {q1}]\n> " +
				strings.Repeat("乙", 178) + "…\n> [Image: {q2}]\n\n这个怎么处理",
		},
	}
}

func TestAQuotedPictureIsNamedInTheStoredBody(t *testing.T) {
	storages := []struct {
		name string
		make func() (mediaStorage, func() (objects int, streamed int))
	}{
		{"buffered download", func() (mediaStorage, func() (int, int)) {
			s := &fakeMediaStorage{}
			return s, func() (int, int) { return len(s.stored()), 0 }
		}},
		{"streaming download", func() (mediaStorage, func() (int, int)) {
			s := &countingStreamStorage{}
			return s, func() (int, int) { return len(s.stored()), s.streams() }
		}},
	}
	for _, sc := range quotedBindScenarios() {
		for _, st := range storages {
			t.Run(sc.name+"/"+st.name, func(t *testing.T) {
				runQuotedBindScenario(t, sc, st.name == "streaming download", st.make)
			})
		}
	}
}

func runQuotedBindScenario(t *testing.T, sc quotedBindScenario, streaming bool, makeStorage func() (mediaStorage, func() (int, int))) {
	pool := mediaBindTestDB(t)
	fixture := seedQuotedBindWorld(t, pool)
	ctx := context.Background()
	queries := db.New(pool)

	q1, q2, own := newPictureServer(t, "q1.jpg"), newPictureServer(t, "q2.jpg"), newPictureServer(t, "own.jpg")

	storage, counts := makeStorage()
	resolver := newTestResolver(storage, engine.NewDBMediaIntentLedger(queries), nil)
	session := engine.NewChatSession(queries, pool, TypeWecom, engine.SessionTitles{
		Group: "群聊", Direct: "单聊", Fallback: "会话",
	})
	tasks := &bindTestTasks{}
	router := engine.NewRouter(bindTestIssues{}, tasks, queries, engine.RouterConfig{
		MediaTimeout: 20 * time.Second,
		Logger:       testLogger(),
	})
	router.Register(TypeWecom, NewResolverSet(NewStore(queries), session, nil, resolver))

	var ownItems []wire
	if sc.own != nil {
		ownItems = sc.own(own.URL)
	}
	msgID := fmt.Sprintf("MSGID-QBIND-%d", time.Now().UnixNano())
	msg := replyToAs(t, fixture.botID, fixture.senderID, msgID, sc.quoted(q1.URL, q2.URL), ownItems)
	if err := router.Handle(ctx, msg); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	// Binding is detached from the ACK path, so poll for the end of it: every
	// attachment row exists AND the run was released, which is the call after
	// the bind transaction.
	var chatMessageID pgtype.UUID
	var body string
	var attachments int
	deadline := time.Now().Add(20 * time.Second)
	for {
		err := pool.QueryRow(ctx, `
			SELECT cm.id, cm.content
			FROM chat_message cm
			JOIN chat_session cs ON cs.id = cm.chat_session_id
			WHERE cs.workspace_id = $1 AND cm.role = 'user'
			ORDER BY cm.created_at DESC LIMIT 1`, fixture.workspaceID).Scan(&chatMessageID, &body)
		if err == nil {
			if err := pool.QueryRow(ctx,
				`SELECT count(*) FROM attachment WHERE chat_message_id = $1`, chatMessageID).Scan(&attachments); err != nil {
				t.Fatalf("count attachments: %v", err)
			}
			if (attachments >= len(sc.wantAttachments) && tasks.promotions() > 0) || time.Now().After(deadline) {
				break
			}
		} else if time.Now().After(deadline) {
			t.Fatalf("no durable chat_message was ever written: %v", err)
		}
		time.Sleep(100 * time.Millisecond)
	}

	// The precondition: the download took the path this run is named for.
	objects, streamed := counts()
	if objects != len(sc.wantAttachments) {
		t.Fatalf("objects in storage = %d, want %d — the download did not complete, so the binder was not exercised",
			objects, len(sc.wantAttachments))
	}
	if streaming && streamed != objects {
		t.Fatalf("UploadStream ran %d times for %d objects — this run was meant to take the streaming path", streamed, objects)
	}
	if !streaming && streamed != 0 {
		t.Fatalf("UploadStream ran on a backend that cannot stream")
	}

	// Which id is which picture.
	ids := map[string]string{}
	rows, err := pool.Query(ctx, `SELECT filename, id::text FROM attachment WHERE chat_message_id = $1`, chatMessageID)
	if err != nil {
		t.Fatalf("load attachments: %v", err)
	}
	for rows.Next() {
		var name, id string
		if err := rows.Scan(&name, &id); err != nil {
			t.Fatalf("scan attachment: %v", err)
		}
		ids[name] = id
	}
	rows.Close()
	for _, name := range sc.wantAttachments {
		if ids[name] == "" {
			t.Fatalf("no attachment named %s on the message (have %v)", name, ids)
		}
	}

	want := strings.NewReplacer("{q1}", ids["q1.jpg"], "{q2}", ids["q2.jpg"]).Replace(sc.wantBody)
	if body != want {
		t.Errorf("stored body after binding:\n got: %q\nwant: %q", body, want)
	}
	// The sender's own picture is never named by a quote marker, whatever the
	// body looks like: a quote that carried its id would be saying the quoted
	// picture is the one just sent.
	if id := ids["own.jpg"]; id != "" && strings.Contains(body, id) {
		t.Errorf("the sender's own attachment id %s appears in the body %q", id, body)
	}
}
