package handler

// Phase 2 acceptance (spec 2.10, plan 9.14). Each scenario names the 2.10 bullet
// it checks and runs over the handlers the API serves. The clock-dependent parts
// (the 08:00 publish and the 10:00 catch-up) run in the service package, where
// the job tests' clock seams are; the Telegram round trip runs here against an
// httptest server, never the real Telegram API.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/telegram"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
)

// phase2TemplateCharter is the charter the quickstart saves for a template
// squad on creation, at revision 0 (packages/core/squads/quickstart.ts).
func phase2TemplateCharter() map[string]any {
	return map[string]any{
		"revision": 0,
		"mission":  "Scan markets, customers and competitors each week, and file only the proposals the evidence supports.",
		"responsibilities": []map[string]any{
			{"key": "market_scan", "text": "Watch markets and customers for changes in the last seven days."},
			{"key": "competitors", "text": "Track named competitors' products, pricing and moves."},
		},
		"outputs": []map[string]any{
			{"key": "weekly_report", "kind": "report", "kb_key": "", "cadence_hint": "weekly"},
		},
		"boundaries":     "Does not write product code.",
		"escalation":     "Questions for the boss go in a proposal.",
		"intake":         "The weekly market scan routine opens each week's issue.",
		"response_hours": 48,
	}
}

// phase2Routines lists a squad's routines as the API serves them.
func phase2Routines(t *testing.T, squadID string) []map[string]any {
	t.Helper()
	req := squadScopeReq(testUserID, "GET", "/api/squads/"+squadID+"/routines", nil, map[string]string{"id": squadID})
	var out []map[string]any
	testutil.Call(t, testHandler.ListSquadRoutines, req).Want(http.StatusOK).JSON(&out)
	return out
}

func phase2RoutineByPurpose(routines []map[string]any, purpose string) map[string]any {
	for _, r := range routines {
		if r["purpose"] == purpose {
			return r
		}
	}
	return nil
}

// TestPhase2Acceptance_Bullet1_TemplateSquadGetsACharterAndARoutine saves the
// template's charter at revision 0 and links the template's routine, as the
// quickstart does. The first charter save creates the operating review.
func TestPhase2Acceptance_Bullet1_TemplateSquadGetsACharterAndARoutine(t *testing.T) {
	fx := newProposalTestFixture(t)
	cleanupSquadCharter(t, fx.SquadID)

	var charter SquadCharterResponse
	putCharterAs(t, testUserID, fx.SquadID, phase2TemplateCharter()).Want(http.StatusOK).JSON(&charter)
	if charter.Revision != 1 || !charter.OperatingReviewEnabled || charter.Mission == "" {
		t.Fatalf("charter after the template save = %+v, want revision 1 with operating review on", charter)
	}
	if rev := charterRevisionAs(t, fx.SquadID); rev != 1 {
		t.Fatalf("GET charter revision = %d, want 1", rev)
	}

	review := phase2RoutineByPurpose(phase2Routines(t, fx.SquadID), "Operating review")
	if review == nil || review["required"] != true {
		t.Fatalf("the first charter save must create a required operating review routine, got %v", phase2Routines(t, fx.SquadID))
	}

	autopilot := dbfx.Insert(t, "autopilot", testutil.Cols{
		"workspace_id":    testWorkspaceID,
		"title":           "Market scan",
		"assignee_type":   "squad",
		"assignee_id":     fx.SquadID,
		"status":          "active",
		"execution_mode":  "create_issue",
		"created_by_type": "member",
		"created_by_id":   testUserID,
	})
	req := squadScopeReq(testUserID, "POST", "/api/squads/"+fx.SquadID+"/routines", map[string]any{
		"autopilot_id":    autopilot,
		"purpose":         "Market scan",
		"expected_output": "report",
		"required":        true,
	}, map[string]string{"id": fx.SquadID})
	testutil.Call(t, testHandler.CreateSquadRoutine, req).Want(http.StatusCreated)

	scan := phase2RoutineByPurpose(phase2Routines(t, fx.SquadID), "Market scan")
	if scan == nil || scan["expected_output"] != "report" || scan["required"] != true {
		t.Fatalf("the template routine is not a charter routine: %v", scan)
	}
}

// charterRevisionAs reads the squad's charter revision through the API.
func charterRevisionAs(t *testing.T, squadID string) int64 {
	t.Helper()
	var charter SquadCharterResponse
	getCharterAs(t, testUserID, squadID).Want(http.StatusOK).JSON(&charter)
	return charter.Revision
}

// TestPhase2Acceptance_Bullet1_PausedRequiredRoutineIsInTheDigestWithItsState
// checks the digest the next brief is written from: the paused required routine
// is listed as not on time, with state paused, in the workspace section and in
// its squad's charter section.
func TestPhase2Acceptance_Bullet1_PausedRequiredRoutineIsInTheDigestWithItsState(t *testing.T) {
	e := newDigestEnv(t)
	sq := e.squad(t, "Acceptance paused")
	e.digestCharter(t, sq, "1 day")
	paused := e.digestRoutine(t, sq, "Weekly report", true, true, "")

	d := e.digestRaw(t)
	ws := digestObjectAt(t, "workspace", d["workspace"])
	off := digestObjectAt(t, "routines_not_on_time", ws["routines_not_on_time"])
	found := false
	for _, item := range digestArrayAt(t, "routines_not_on_time.items", off["items"]) {
		m := digestObjectAt(t, "items[]", item)
		if m["autopilot_id"] == paused {
			found = true
			if m["state"] != "paused" {
				t.Fatalf("workspace routine state = %v, want paused", m["state"])
			}
		}
	}
	if !found {
		t.Fatalf("the paused required routine is missing from routines_not_on_time: %v", off)
	}

	for _, s := range digestArrayAt(t, "squads", d["squads"]) {
		squad := digestObjectAt(t, "squads[]", s)
		if squad["id"] != sq {
			continue
		}
		charter := digestObjectAt(t, "charter", squad["charter"])
		for _, r := range digestArrayAt(t, "charter.routines", charter["routines"]) {
			m := digestObjectAt(t, "routines[]", r)
			if m["autopilot_id"] == paused {
				if m["state"] != "paused" || m["required"] != true {
					t.Fatalf("charter routine = %v, want required and paused", m)
				}
				return
			}
		}
	}
	t.Fatal("the squad's charter section does not list the paused routine")
}

// TestPhase2Acceptance_Bullet2_FiveRejectionsCapTheSquadWithAReason rejects five
// proposals in a row, runs the nightly attention recompute, and checks the cap
// and the reason the squad panel shows. Each rejection is aged out of the weekly
// window after it is made, so only the decisions drive the band.
func TestPhase2Acceptance_Bullet2_FiveRejectionsCapTheSquadWithAReason(t *testing.T) {
	fx := newProposalTestFixture(t)
	for i := 0; i < 5; i++ {
		p := fx.fileProposal(t, "rejected "+string(rune('a'+i)), nil)
		testutil.Call(t, testHandler.DecideProposal, decideRequest(p.ID, map[string]any{"decision": "reject"})).Want(http.StatusOK)
		dbfx.Exec(t, `UPDATE proposal SET created_at = now() - interval '8 days' WHERE id = $1`, p.ID)
	}

	recomputeAttention(t)

	got := getAttention(t, testWorkspaceID, fx.SquadID)
	if got.PendingCap != 1 || got.WeeklyCap != 1 || got.Reason != service.AttentionReasonBelowThreshold || got.SampleSize != 5 {
		t.Fatalf("after five rejections the squad is %d/%d reason %s sample %d, want 1/1 %s sample 5",
			got.PendingCap, got.WeeklyCap, got.Reason, got.SampleSize, service.AttentionReasonBelowThreshold)
	}

	// The capped squad files one proposal, and its second is refused.
	fx.fileProposal(t, "first after the cap", nil)
	res := testutil.Call(t, testHandler.CreateProposal, asAgent(newRequest("POST", "/api/proposals", proposalBody(fx.SquadID, fx.title("second after the cap"), nil)), fx.LeaderID, fx.LeaderTaskID)).Want(http.StatusConflict)
	if res.Map()["code"] != "proposal_limit" {
		t.Fatalf("second filing code = %v, want proposal_limit", res.Map()["code"])
	}
}

// TestPhase2Acceptance_Bullet3_ReviseAndSnoozeFromTheAppAndAChatButton drives
// revise and snooze from the decision endpoint (the app) and snooze from a signed
// chat button.
func TestPhase2Acceptance_Bullet3_ReviseAndSnoozeFromTheAppAndAChatButton(t *testing.T) {
	fx := newProposalTestFixture(t)

	revised := fx.fileProposal(t, "revise from the app", nil)
	reviseProposal(t, revised.ID, "Narrow the pilot to one cohort.")
	if state := proposalState(t, revised.ID); state != "revising" {
		t.Fatalf("after revise the state is %s, want revising", state)
	}

	snoozed := fx.fileProposal(t, "snooze from the app", nil)
	until := time.Now().AddDate(0, 0, 5).Format(time.RFC3339)
	testutil.Call(t, testHandler.DecideProposal, decideRequest(snoozed.ID, map[string]any{"decision": "snooze", "until": until})).Want(http.StatusOK)
	if state := proposalState(t, snoozed.ID); state != "pending" || proposalSnoozedUntil(t, snoozed.ID) == nil {
		t.Fatalf("after snooze the state is %s with snoozed_until %v, want pending and a date", state, proposalSnoozedUntil(t, snoozed.ID))
	}

	env := newChatActionEnv(t)
	owner := newWorkspaceMemberUser(t, "owner")
	sender := bindChatSender(t, env.Installs, env.WorkspaceID, owner)
	res := env.press(t, env.Installs, sender, env.token(t, env.Proposal.ID, service.ProposalDecisionSnooze))
	if !res.Settled {
		t.Fatalf("chat snooze press = %+v, want it to settle", res)
	}
	until2 := proposalSnoozedUntil(t, env.Proposal.ID)
	if until2 == nil || until2.Before(time.Now().Add(6*24*time.Hour)) || until2.After(time.Now().Add(8*24*time.Hour)) {
		t.Fatalf("chat snooze date = %v, want about seven days ahead", until2)
	}
}

// TestPhase2Acceptance_Bullet3_ExpiredProposalFreesTheCapAndWakesTheLeaderOnce
// lets a pending proposal pass its window, runs the expiry job twice, and checks
// that the cap is free and the leader was woken once.
func TestPhase2Acceptance_Bullet3_ExpiredProposalFreesTheCapAndWakesTheLeaderOnce(t *testing.T) {
	fx := newProposalTestFixture(t)
	seedProposalRow(t, fx.SquadID, "pending", false, 8*24*time.Hour)
	seedProposalRow(t, fx.SquadID, "pending", false, 8*24*time.Hour)
	waiting := fx.fileProposal(t, "expires unanswered", nil)

	testutil.Call(t, testHandler.CreateProposal, asAgent(newRequest("POST", "/api/proposals", proposalBody(fx.SquadID, fx.title("blocked"), nil)), fx.LeaderID, fx.LeaderTaskID)).Want(http.StatusConflict)

	setExpiry(t, waiting.ID, "now() - interval '1 second'")
	runExpiry(t)
	runExpiry(t)

	if state := proposalState(t, waiting.ID); state != "expired" {
		t.Fatalf("state = %s, want expired", state)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2`, waiting.IssueID, fx.LeaderID); n != 1 {
		t.Fatalf("the leader was woken %d times across two runs, want once", n)
	}
	testutil.Call(t, testHandler.CreateProposal, asAgent(newRequest("POST", "/api/proposals", proposalBody(fx.SquadID, fx.title("cap freed"), nil)), fx.LeaderID, fx.LeaderTaskID)).Want(http.StatusCreated)
}

// phase2Telegram records every request the Telegram notifier makes and answers
// each sendMessage with a success envelope. It is the only Telegram the test sees.
type phase2Telegram struct {
	srv   *httptest.Server
	mu    sync.Mutex
	sends []map[string]any
}

func newPhase2Telegram(t *testing.T) *phase2Telegram {
	t.Helper()
	tg := &phase2Telegram{}
	tg.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if !strings.HasSuffix(r.URL.Path, "/sendMessage") {
			http.NotFound(w, r)
			return
		}
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("sendMessage body is not JSON: %v", err)
		}
		tg.mu.Lock()
		tg.sends = append(tg.sends, body)
		tg.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":501,"chat":{"id":4242,"type":"private"},"date":1767225600,"text":"ok"}}`))
	}))
	t.Cleanup(tg.srv.Close)
	return tg
}

func (tg *phase2Telegram) sent() []map[string]any {
	tg.mu.Lock()
	defer tg.mu.Unlock()
	return append([]map[string]any(nil), tg.sends...)
}

// phase2Buttons flattens a sendMessage's inline keyboard.
func phase2Buttons(t *testing.T, msg map[string]any) []map[string]any {
	t.Helper()
	markup, ok := msg["reply_markup"].(map[string]any)
	if !ok {
		t.Fatalf("message has no reply_markup: %v", msg)
	}
	var out []map[string]any
	rows, _ := markup["inline_keyboard"].([]any)
	for _, row := range rows {
		for _, b := range row.([]any) {
			out = append(out, b.(map[string]any))
		}
	}
	return out
}

// TestPhase2Acceptance_Bullet4_BriefPushReachesTelegramWithWorkingDecideButtons
// queues a brief for a member with push on and a Telegram binding, runs the push
// job through the Telegram notifier against httptest, and presses the Approve
// button's signed token to decide the proposal.
func TestPhase2Acceptance_Bullet4_BriefPushReachesTelegramWithWorkingDecideButtons(t *testing.T) {
	fx := newProposalTestFixture(t)
	p := fx.fileProposal(t, "push decision", nil)
	tg := newPhase2Telegram(t)

	// A Telegram installation with a bot token the notifier can read (the test
	// stores the token base64 and decrypts with nil, as the notifier tests do).
	cfg, err := json.Marshal(map[string]string{
		"app_id":              "111",
		"bot_token_encrypted": base64.StdEncoding.EncodeToString([]byte("111:acceptance-token")),
	})
	if err != nil {
		t.Fatal(err)
	}
	installID := dbfx.Insert(t, "channel_installation", testutil.Cols{
		"workspace_id":      testWorkspaceID,
		"agent_id":          uuid.NewString(),
		"channel_type":      "telegram",
		"config":            cfg,
		"installer_user_id": testUserID,
		"status":            "active",
	})
	const chatID = "4242"
	bindingID := briefPushBindingOn(t, testWorkspaceID, testUserID, "telegram", installID, chatID, `{}`)
	dbfx.Insert(t, "brief_push_preference", testutil.Cols{
		"workspace_id": testWorkspaceID,
		"member_id":    testUserID,
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
	svc.RegisterNotifier(telegram.TypeTelegram, telegram.NewNotifier(testHandler.Queries, nil, tg.srv.URL, tg.srv.Client()))
	svc.EnableActionButtons(signer, telegram.TypeTelegram)
	prev := testHandler.BriefPush
	testHandler.BriefPush = svc
	t.Cleanup(func() { testHandler.BriefPush = prev })

	ctx := context.Background()
	if n, err := svc.EnqueueForBrief(ctx, mustPhase2UUID(t, testWorkspaceID), mustPhase2UUID(t, briefID)); err != nil || n < 1 {
		t.Fatalf("EnqueueForBrief = %d, %v; want at least the test member", n, err)
	}
	if _, err := svc.DeliverDue(ctx); err != nil {
		t.Fatalf("DeliverDue: %v", err)
	}

	var state string
	dbfx.QueryRow(t, `SELECT state FROM brief_delivery WHERE brief_id = $1 AND member_id = $2`, briefID, testUserID).Scan(&state)
	if state != "sent" {
		t.Fatalf("delivery state = %s, want sent", state)
	}

	var msg map[string]any
	for _, m := range tg.sent() {
		if m["chat_id"] == float64(4242) {
			msg = m
		}
	}
	if msg == nil {
		t.Fatalf("no sendMessage to chat 4242; got %d sends", len(tg.sent()))
	}
	if text, _ := msg["text"].(string); !strings.Contains(text, "push decision") {
		t.Fatalf("message text does not name the decision: %q", text)
	}

	var openURL, reviseURL, approve string
	for _, b := range phase2Buttons(t, msg) {
		label, _ := b["text"].(string)
		switch {
		case label == "Open the brief":
			openURL, _ = b["url"].(string)
		case strings.HasPrefix(label, "Revise:"):
			reviseURL, _ = b["url"].(string)
		case strings.HasPrefix(label, "Approve:"):
			approve, _ = b["callback_data"].(string)
		}
	}
	if !strings.HasPrefix(openURL, "https://app.example/") || reviseURL == "" || approve == "" {
		t.Fatalf("buttons: open=%q revise=%q approve token=%q", openURL, reviseURL, approve)
	}

	actions := NewChannelActions(testHandler, signer)
	res, err := actions.Press(ctx, channel.Press{
		Sender: channel.Sender{ChannelType: "telegram", InstallationID: mustPhase2UUID(t, installID), ChannelUserID: chatID},
		Data:   approve,
	})
	if err != nil {
		t.Fatalf("Press: %v", err)
	}
	if !res.Settled || !strings.HasPrefix(res.Reply, "Approved:") {
		t.Fatalf("Approve press = %+v, want an approval", res)
	}
	if got := proposalState(t, p.ID); got != "approved" {
		t.Fatalf("proposal state = %s after the Decide button, want approved", got)
	}
}

func mustPhase2UUID(t *testing.T, s string) pgtype.UUID {
	t.Helper()
	id, err := util.ParseUUID(s)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// TestPhase2Acceptance_Bullet5_InquiriesStopAtThreePerDay sends three inquiries
// from the Secretary and refuses the fourth with 429.
func TestPhase2Acceptance_Bullet5_InquiriesStopAtThreePerDay(t *testing.T) {
	e := newInquiryEnv(t)
	for i := 1; i <= 3; i++ {
		e.ask(t, e.Secretary, e.askBody("Acceptance question "+string(rune('0'+i)))).Want(http.StatusCreated)
	}
	res := e.ask(t, e.Secretary, e.askBody("Acceptance question 4")).Want(http.StatusTooManyRequests)
	if res.Map()["code"] != "inquiry_limit" {
		t.Fatalf("fourth inquiry code = %v, want inquiry_limit", res.Map()["code"])
	}
	if service.BriefInquiryDailyLimit != 3 {
		t.Fatalf("daily limit = %d, want 3", service.BriefInquiryDailyLimit)
	}
}

// TestPhase2Acceptance_Bullet5_AnswersAppearInTheNextDigest asks a question, the
// squad answers on the issue, and the next digest carries the answer.
func TestPhase2Acceptance_Bullet5_AnswersAppearInTheNextDigest(t *testing.T) {
	e := newInquiryEnv(t)
	const question = "Acceptance: what changed on the pilot?"
	e.ask(t, e.Secretary, e.askBody(question)).Want(http.StatusCreated)
	e.reply(t, e.Leader, e.IssueID, "The pilot cohort grew by 40 percent this week.")

	d := e.digestRaw(t)
	items := digestArrayAt(t, "inquiries", d["inquiries"])
	for _, item := range items {
		m := digestObjectAt(t, "inquiries[]", item)
		if m["question"] == question {
			if m["answered"] != true || m["answer_excerpt"] != "The pilot cohort grew by 40 percent this week." {
				t.Fatalf("digest inquiry = %v, want the answer", m)
			}
			return
		}
	}
	t.Fatalf("the next digest does not carry the inquiry: %v", items)
}

// TestPhase2Acceptance_Bullet5_FeedbackIsRecordedAndTheSecretaryReadsIt rates a
// brief, reads the entry back as the Secretary's run does, and checks the
// Secretary's template tells it to read the feedback first and record standing
// preferences. The template is TypeScript, so the check reads its source.
func TestPhase2Acceptance_Bullet5_FeedbackIsRecordedAndTheSecretaryReadsIt(t *testing.T) {
	e := newDigestEnv(t)
	briefID := e.publishFeedbackBrief(t)
	member := newWorkspaceMemberUserIn(t, e.WorkspaceID, "member")
	e.submitFeedback(t, member, briefID, map[string]any{
		"rating": "not_useful", "tags": []string{"too_long"}, "note": "Lead with the decisions.",
	}).Want(http.StatusOK)

	var entries []BriefFeedbackEntry
	e.workspaceRead(t, testHandler.ListBriefFeedbackRecent, "/api/briefs/feedback/recent?days=14", "", "").Want(http.StatusOK).JSON(&entries)
	if len(entries) != 1 || entries[0].BriefID != briefID || entries[0].Note != "Lead with the decisions." {
		t.Fatalf("the Secretary's read does not carry the entry: %+v", entries)
	}

	src, err := os.ReadFile(filepath.Join("..", "..", "..", "packages", "core", "agents", "instruction-templates.ts"))
	if err != nil {
		t.Fatalf("read the instruction templates: %v", err)
	}
	text := string(src)
	const command = "multica brief feedback --recent 14d --json"
	if !strings.Contains(text, command) {
		t.Fatalf("the Secretary's template does not run %q", command)
	}
	if !strings.Contains(text, "Record a standing preference in your memory") {
		t.Fatal("the Secretary's template does not say to record standing preferences from feedback")
	}
	if strings.Index(text, command) > strings.Index(text, "Pull, not push") {
		t.Fatal("the feedback read must come before the digest read in the Secretary's template")
	}
}
