package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/slack"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// phase2Slack is a recording stand-in for the Slack Web API's chat.postMessage.
type phase2Slack struct {
	srv   *httptest.Server
	mu    sync.Mutex
	posts []url.Values
}

func newPhase2Slack(t *testing.T) *phase2Slack {
	t.Helper()
	f := &phase2Slack{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		f.mu.Lock()
		if r.URL.Path == "/chat.postMessage" {
			f.posts = append(f.posts, r.PostForm)
		}
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = io.WriteString(w, `{"ok":true,"channel":"D0BRIEF","ts":"1767225600.000100"}`)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *phase2Slack) lastPost() url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.posts) == 0 {
		return nil
	}
	return f.posts[len(f.posts)-1]
}

// slackButtonValue finds the button whose label starts with prefix in the
// posted blocks and returns its value, the signed token.
func slackButtonValue(t *testing.T, form url.Values, prefix string) string {
	t.Helper()
	var blocks []struct {
		Type     string `json:"type"`
		Elements []struct {
			Text struct {
				Text string `json:"text"`
			} `json:"text"`
			Value string `json:"value"`
		} `json:"elements"`
	}
	if err := json.Unmarshal([]byte(form.Get("blocks")), &blocks); err != nil {
		t.Fatalf("blocks are not JSON: %v", err)
	}
	for _, b := range blocks {
		for _, e := range b.Elements {
			if strings.HasPrefix(e.Text.Text, prefix) {
				return e.Value
			}
		}
	}
	t.Fatalf("no button starting %q in %s", prefix, form.Get("blocks"))
	return ""
}

// TestPhase2Acceptance_SlackBriefPushDeliversSignedButtonsAndDecides is the Slack
// counterpart of bullet 4: a brief queued for a member with a Slack chat goes
// through the Slack notifier, its Approve button carries a signed token, and
// pressing that token as the member's Slack binding decides the proposal.
func TestPhase2Acceptance_SlackBriefPushDeliversSignedButtonsAndDecides(t *testing.T) {
	fx := newProposalTestFixture(t)
	p := fx.fileProposal(t, "slack push decision", nil)
	fake := newPhase2Slack(t)

	// The member is a new owner, so their preference is their own row and the
	// press is decided by the owner rule.
	member := dbfx.User(t, "Slack Owner", "phase2-slack-"+uuid.NewString()+"@example.com")
	dbfx.Member(t, testWorkspaceID, member, "owner")

	cfg, err := json.Marshal(map[string]string{
		"app_id":              "A1",
		"team_id":             "T1",
		"bot_user_id":         "UBOTACCEPT",
		"bot_token_encrypted": base64.StdEncoding.EncodeToString([]byte("xoxb-acceptance-token")),
	})
	if err != nil {
		t.Fatal(err)
	}
	installID := dbfx.Insert(t, "channel_installation", testutil.Cols{
		"workspace_id":      testWorkspaceID,
		"agent_id":          uuid.NewString(),
		"channel_type":      "slack",
		"config":            cfg,
		"installer_user_id": testUserID,
		"status":            "active",
	})
	const slackUser = "UACCEPT1"
	bindingID := briefPushBindingOn(t, testWorkspaceID, member, "slack", installID, slackUser, `{}`)
	dbfx.Insert(t, "brief_push_preference", testutil.Cols{
		"workspace_id": testWorkspaceID,
		"member_id":    member,
		"brief_push":   true,
		"binding_id":   bindingID,
	})

	briefID := dbfx.Insert(t, "brief", testutil.Cols{
		"workspace_id":         testWorkspaceID,
		"kind":                 "daily",
		"period_start":         time.Now().Add(-24 * time.Hour),
		"period_end":           time.Now(),
		"title":                "Daily brief",
		"body":                 "## Headline\nOne decision needs you.\n\n## Needs your decision\nSee the decisions.\n\n## Progress\nQuiet.\n\n## Blockers and risks\nNone.\n\n## Watch\nNone.\n\n## FYI\nNone.\n",
		"proposal_ids":         []pgtype.UUID{mustPhase2UUID(t, p.ID)},
		"created_by_member_id": testUserID,
	})
	dbfx.Cleanup(t, `DELETE FROM brief_delivery WHERE brief_id = $1`, briefID)

	signer := service.NewActionSigner([]byte(chatActionTestSecret))
	svc := service.NewBriefPushService(testHandler.Queries, "https://app.example")
	svc.RegisterNotifier(slack.TypeSlack, slack.NewNotifier(testHandler.Queries, nil, fake.srv.URL, fake.srv.Client()))
	svc.EnableActionButtons(signer, slack.TypeSlack)
	prev := testHandler.BriefPush
	testHandler.BriefPush = svc
	t.Cleanup(func() { testHandler.BriefPush = prev })

	ctx := context.Background()
	if n, err := svc.EnqueueForBrief(ctx, mustPhase2UUID(t, testWorkspaceID), mustPhase2UUID(t, briefID)); err != nil || n < 1 {
		t.Fatalf("EnqueueForBrief = %d, %v; want at least the Slack member", n, err)
	}
	if _, err := svc.DeliverDue(ctx); err != nil {
		t.Fatalf("DeliverDue: %v", err)
	}

	var state string
	dbfx.QueryRow(t, `SELECT state FROM brief_delivery WHERE brief_id = $1 AND member_id = $2`, briefID, member).Scan(&state)
	if state != "sent" {
		t.Fatalf("delivery state = %s, want sent", state)
	}

	post := fake.lastPost()
	if post == nil {
		t.Fatal("no chat.postMessage reached the Slack fake")
	}
	if post.Get("channel") != slackUser {
		t.Fatalf("posted to %q, want the member's Slack user %q", post.Get("channel"), slackUser)
	}
	if !strings.Contains(post.Get("text"), "push decision") {
		t.Fatalf("message text does not name the decision: %q", post.Get("text"))
	}
	approve := slackButtonValue(t, post, "Approve:")
	if approve == "" {
		t.Fatal("Approve carries no signed token")
	}

	actions := NewChannelActions(testHandler, signer)
	res, err := actions.Press(ctx, channel.Press{
		Sender: channel.Sender{ChannelType: "slack", InstallationID: mustPhase2UUID(t, installID), ChannelUserID: slackUser},
		Data:   approve,
	})
	if err != nil {
		t.Fatalf("Press: %v", err)
	}
	if !res.Settled || !strings.HasPrefix(res.Reply, "Approved:") {
		t.Fatalf("Approve press = %+v, want an approval", res)
	}
	if got := proposalState(t, p.ID); got != "approved" {
		t.Fatalf("proposal state = %s after the Approve button, want approved", got)
	}
}
