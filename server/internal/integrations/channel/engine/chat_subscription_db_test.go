package engine

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/service"
	dbfx "github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type subscriptionDB struct {
	f            *dbfx.Fixture
	q            *db.Queries
	workspaceID  pgtype.UUID
	userID       pgtype.UUID
	installation pgtype.UUID
	projectID    string
	webProject   pgtype.UUID
	webIssue     db.Issue
	public       db.Issue
	private      db.Issue
}

func newSubscriptionDB(t *testing.T) *subscriptionDB {
	t.Helper()
	pool := invokeGateTestDB(t)
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	base := dbfx.New(pool, "", "")
	userID := base.User(t, "Subscriber", "sub-"+suffix+"@example.com")
	wsID := base.Workspace(t, "Subscriptions", "sub-"+suffix, dbfx.Cols{"issue_prefix": "SUB"})
	f := dbfx.New(pool, wsID, userID)
	f.Member(t, wsID, userID, "owner")
	projectID := f.Project(t, "Secret", dbfx.Cols{"visibility": "private"})
	webProjectID := f.Project(t, "Web App")
	webIssueID := f.Issue(t, "Polish header", dbfx.Cols{"project_id": webProjectID})
	publicID := f.Issue(t, "Fix login")
	privateID := f.Issue(t, "Secret work", dbfx.Cols{"project_id": projectID})
	instID := f.Insert(t, "channel_installation", dbfx.Cols{
		"workspace_id": wsID, "agent_id": uuid.NewString(), "channel_type": string(channel.TypeFeishu),
		"installer_user_id": userID,
	})
	f.Cleanup(t, `DELETE FROM channel_issue_subscription WHERE workspace_id = $1`, wsID)
	f.Cleanup(t, `DELETE FROM channel_project_subscription WHERE workspace_id = $1`, wsID)

	s := &subscriptionDB{
		f: f, q: db.New(pool),
		workspaceID: util.MustParseUUID(wsID), userID: util.MustParseUUID(userID),
		installation: util.MustParseUUID(instID), projectID: projectID,
		webProject: util.MustParseUUID(webProjectID),
	}
	var err error
	if s.webIssue, err = s.q.GetIssueInWorkspace(context.Background(), db.GetIssueInWorkspaceParams{ID: util.MustParseUUID(webIssueID), WorkspaceID: s.workspaceID}); err != nil {
		t.Fatalf("load project issue: %v", err)
	}
	if s.public, err = s.q.GetIssueInWorkspace(context.Background(), db.GetIssueInWorkspaceParams{ID: util.MustParseUUID(publicID), WorkspaceID: s.workspaceID}); err != nil {
		t.Fatalf("load public issue: %v", err)
	}
	if s.private, err = s.q.GetIssueInWorkspace(context.Background(), db.GetIssueInWorkspaceParams{ID: util.MustParseUUID(privateID), WorkspaceID: s.workspaceID}); err != nil {
		t.Fatalf("load private issue: %v", err)
	}
	return s
}

// joinPrivateProject gives the user access to the private issue.
func (s *subscriptionDB) joinPrivateProject(t *testing.T, userID pgtype.UUID) {
	t.Helper()
	uid := util.UUIDToString(userID)
	s.f.InsertNoID(t, "project_member", dbfx.Cols{
		"workspace_id": util.UUIDToString(s.workspaceID), "project_id": s.projectID, "user_id": uid,
	}, "project_id = $1 AND user_id = $2", s.projectID, uid)
}

// otherMember adds a second workspace member and returns their id.
func (s *subscriptionDB) otherMember(t *testing.T) pgtype.UUID {
	t.Helper()
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	id := s.f.User(t, "Teammate", "mate-"+suffix+"@example.com")
	s.f.Member(t, util.UUIDToString(s.workspaceID), id, "member")
	return util.MustParseUUID(id)
}

// harness wires the real subscription queries into a router whose other
// ports stay fake.
func (s *subscriptionDB) harness(t *testing.T) (*harness, *fakePoster) {
	h := newHarness(t)
	h.inst.inst.ID = s.installation
	h.inst.inst.WorkspaceID = s.workspaceID
	h.ident.id = ResolvedIdentity{UserID: s.userID}
	poster := &fakePoster{}
	h.router = NewRouter(h.issues, h.tasks, s.q, RouterConfig{Logger: discardLogger(), Subscriptions: s.q})
	h.router.Register(channel.TypeFeishu, ResolverSet{
		Installation: h.inst, Identity: h.ident, Dedup: h.dedup, Session: h.binder,
		Audit: h.audit, Replier: h.replier, Poster: poster, OriginType: "lark_chat",
	})
	return h, poster
}

func (s *subscriptionDB) rows(t *testing.T, issueID pgtype.UUID) []db.ChannelIssueSubscription {
	t.Helper()
	rows, err := s.q.ListChannelIssueSubscriptionsForIssue(context.Background(), db.ListChannelIssueSubscriptionsForIssueParams{
		IssueID: issueID, WorkspaceID: s.workspaceID,
	})
	if err != nil {
		t.Fatalf("list subscriptions: %v", err)
	}
	return rows
}

// send handles one group message and returns the subscription verdict it got.
func sendSubscription(t *testing.T, h *harness, msg channel.InboundMessage) Result {
	t.Helper()
	before := len(h.replier.calls())
	if err := h.router.Handle(context.Background(), msg); err != nil {
		t.Fatalf("handle %q: %v", msg.Text, err)
	}
	var got Result
	if !waitFor(time.Second, func() bool {
		calls := h.replier.calls()
		if len(calls) <= before {
			return false
		}
		got = calls[len(calls)-1]
		return true
	}) {
		t.Fatalf("no reply to %q", msg.Text)
	}
	return got
}

func TestRouter_SubscribeAndUnsubscribe_DB(t *testing.T) {
	s := newSubscriptionDB(t)
	h, _ := s.harness(t)
	ref := fmt.Sprintf("sub-%d", s.public.Number)

	got := sendSubscription(t, h, groupSubscribeMessage(t, "/subscribe "+ref))
	if got.Subscription.Action != SubscriptionSubscribed || got.IssueIdentifier != fmt.Sprintf("SUB-%d", s.public.Number) {
		t.Fatalf("first subscribe = %+v", got)
	}
	rows := s.rows(t, s.public.ID)
	if len(rows) != 1 || rows[0].ChatID != "oc_chat" || rows[0].ThreadID != "" || rows[0].InstallationID != s.installation || rows[0].CreatedBy != s.userID {
		t.Fatalf("stored subscription = %+v", rows)
	}

	if got := sendSubscription(t, h, groupSubscribeMessage(t, "/subscribe #"+fmt.Sprint(s.public.Number))); got.Subscription.Action != SubscriptionAlreadySubscribed {
		t.Fatalf("repeat subscribe = %q", got.Subscription.Action)
	}
	link := "https://app.example/sub/issues/" + util.UUIDToString(s.public.ID)
	if got := sendSubscription(t, h, groupSubscribeMessage(t, "/unsubscribe "+link)); got.Subscription.Action != SubscriptionUnsubscribed {
		t.Fatalf("unsubscribe by link = %q", got.Subscription.Action)
	}
	if rows := s.rows(t, s.public.ID); len(rows) != 0 {
		t.Fatalf("unsubscribe left rows: %+v", rows)
	}
	if got := sendSubscription(t, h, groupSubscribeMessage(t, "/unsubscribe "+ref)); got.Subscription.Action != SubscriptionNotSubscribed {
		t.Fatalf("second unsubscribe = %q", got.Subscription.Action)
	}
}

func TestRouter_SubscribeInThreadTargetsThread_DB(t *testing.T) {
	s := newSubscriptionDB(t)
	h, _ := s.harness(t)
	msg := groupSubscribeMessage(t, fmt.Sprintf("/subscribe SUB-%d", s.public.Number))
	msg.MessageID = "om-thread-cmd"
	msg.Source.ThreadID = "omt_topic"
	if got := sendSubscription(t, h, msg); got.Subscription.Action != SubscriptionSubscribed {
		t.Fatalf("subscribe in thread = %q", got.Subscription.Action)
	}
	rows := s.rows(t, s.public.ID)
	if len(rows) != 1 || rows[0].ThreadID != "omt_topic" || rows[0].AnchorMessageID != "om-thread-cmd" {
		t.Fatalf("thread subscription = %+v", rows)
	}
}

// A chat-level subscription posts into the topic its reply opened under the
// command; commands sent inside that topic address the same subscription.
func TestRouter_SubscriptionTopicUnderCommand_DB(t *testing.T) {
	s := newSubscriptionDB(t)
	h, _ := s.harness(t)
	ref := fmt.Sprintf("SUB-%d", s.public.Number)
	cmd := groupSubscribeMessage(t, "/subscribe issue "+ref)
	cmd.MessageID = "om-cmd"
	if got := sendSubscription(t, h, cmd); got.Subscription.Action != SubscriptionSubscribed {
		t.Fatalf("chat-level subscribe = %+v", got)
	}
	rows := s.rows(t, s.public.ID)
	if len(rows) != 1 || rows[0].ThreadID != "" || rows[0].AnchorMessageID != "om-cmd" {
		t.Fatalf("chat-level subscription = %+v", rows)
	}

	inTopic := func(text string) channel.InboundMessage {
		msg := groupSubscribeMessage(t, text)
		msg.MessageID = "om-in-topic-" + strings.ReplaceAll(text, " ", "-")
		msg.Source.ThreadID = "omt_created"
		msg.ReplyTo = &channel.ReplyCtx{MessageID: "om-cmd", RootID: "om-cmd"}
		return msg
	}
	if got := sendSubscription(t, h, inTopic("/subscribe issue "+ref)); got.Subscription.Action != SubscriptionAlreadySubscribed {
		t.Fatalf("subscribe inside the reply topic = %+v", got)
	}
	if got := sendSubscription(t, h, inTopic("/unsubscribe issue "+ref)); got.Subscription.Action != SubscriptionUnsubscribed {
		t.Fatalf("unsubscribe inside the reply topic = %+v", got)
	}
	if rows := s.rows(t, s.public.ID); len(rows) != 0 {
		t.Fatalf("unsubscribe inside the topic left rows: %+v", rows)
	}
}

func TestRouter_SubscribeRejectsHiddenAndUnknownIssues_DB(t *testing.T) {
	s := newSubscriptionDB(t)
	h, _ := s.harness(t)
	for _, ref := range []string{
		fmt.Sprintf("SUB-%d", s.private.Number),
		"OTHER-" + fmt.Sprint(s.public.Number),
		"SUB-999999",
	} {
		got := sendSubscription(t, h, groupSubscribeMessage(t, "/subscribe "+ref))
		if got.Subscription.Action != SubscriptionNotFound || got.IssueTitle != "" {
			t.Fatalf("subscribe %s = %+v, want not found without issue details", ref, got)
		}
	}
	if rows := s.rows(t, s.private.ID); len(rows) != 0 {
		t.Fatalf("private issue subscribed: %+v", rows)
	}
}

func TestRouter_SubscribeFollowsSenderAccess_DB(t *testing.T) {
	s := newSubscriptionDB(t)
	h, _ := s.harness(t)
	ref := fmt.Sprintf("SUB-%d", s.private.Number)

	s.joinPrivateProject(t, s.userID)
	got := sendSubscription(t, h, groupSubscribeMessage(t, "/subscribe "+ref))
	if got.Subscription.Action != SubscriptionSubscribed || got.IssueTitle != s.private.Title {
		t.Fatalf("project member subscribe = %+v", got)
	}
	if rows := s.rows(t, s.private.ID); len(rows) != 1 || rows[0].CreatedBy != s.userID {
		t.Fatalf("private issue subscription = %+v", rows)
	}

	// A sender outside the project cannot follow the issue, but can stop the
	// updates this chat already gets.
	h.ident.id = ResolvedIdentity{UserID: s.otherMember(t)}
	other := groupSubscribeMessage(t, "/subscribe "+ref)
	other.Source.ChatID = "oc_other"
	if got := sendSubscription(t, h, other); got.Subscription.Action != SubscriptionNotFound || got.IssueTitle != "" {
		t.Fatalf("outsider subscribe = %+v", got)
	}
	other.Text = "/unsubscribe " + ref
	if got := sendSubscription(t, h, other); got.Subscription.Action != SubscriptionNotFound || got.IssueTitle != "" {
		t.Fatalf("outsider unsubscribe in an unsubscribed chat = %+v", got)
	}
	if got := sendSubscription(t, h, groupSubscribeMessage(t, "/unsubscribe "+ref)); got.Subscription.Action != SubscriptionUnsubscribed {
		t.Fatalf("outsider unsubscribe of a followed issue = %+v", got)
	}
	if rows := s.rows(t, s.private.ID); len(rows) != 0 {
		t.Fatalf("unsubscribe left rows: %+v", rows)
	}
}

func TestRouter_IssueCommandInGroupSubscribesChat_DB(t *testing.T) {
	s := newSubscriptionDB(t)
	h, _ := s.harness(t)
	h.binder.appendResult = AppendResult{DedupMarked: true, IssueCommand: &IssueCommand{Title: "Fix login"}}
	h.issues.result = service.IssueCreateResult{Issue: s.public}

	got := sendSubscription(t, h, groupSubscribeMessage(t, "/issue Fix login"))
	if !got.IssueID.Valid || !got.IssueChatSubscribed {
		t.Fatalf("group /issue result = %+v, want a subscribed confirmation", got)
	}
	if rows := s.rows(t, s.public.ID); len(rows) != 1 || rows[0].ChatID != "oc_chat" {
		t.Fatalf("group /issue subscription = %+v", rows)
	}

	// A direct message keeps the existing behavior: the creator follows the
	// issue through their inbox, nothing is posted back into the DM.
	dm := p2pMessage(t)
	dm.Text = "/issue Fix login"
	dm.Source.ChatID = "oc_dm"
	if got := sendSubscription(t, h, dm); got.IssueChatSubscribed {
		t.Fatalf("direct /issue must not subscribe the chat: %+v", got)
	}
	if rows := s.rows(t, s.public.ID); len(rows) != 1 {
		t.Fatalf("direct /issue added a subscription: %+v", rows)
	}

	h.issues.result = service.IssueCreateResult{Issue: s.private}
	if got := sendSubscription(t, h, groupSubscribeMessage(t, "/issue Secret work")); got.IssueChatSubscribed {
		t.Fatalf("/issue into a project the sender cannot see must not subscribe the group: %+v", got)
	}
	s.joinPrivateProject(t, s.userID)
	if got := sendSubscription(t, h, groupSubscribeMessage(t, "/issue Secret work")); !got.IssueChatSubscribed {
		t.Fatalf("/issue by a project member must subscribe the group: %+v", got)
	}
}

func TestRouter_PublishIssueUpdate_DB(t *testing.T) {
	s := newSubscriptionDB(t)
	h, poster := s.harness(t)
	ctx := context.Background()
	subscribe := func(issue db.Issue, installation pgtype.UUID, createdBy pgtype.UUID, chat, thread, anchor string) db.ChannelIssueSubscription {
		row, err := s.q.CreateChannelIssueSubscription(ctx, db.CreateChannelIssueSubscriptionParams{
			WorkspaceID: s.workspaceID, IssueID: issue.ID, InstallationID: installation,
			ChannelType: string(channel.TypeFeishu), ChatID: chat, ThreadID: thread, AnchorMessageID: anchor,
			CreatedBy: createdBy,
		})
		if err != nil {
			t.Fatalf("seed subscription: %v", err)
		}
		return row
	}
	subscribe(s.public, s.installation, s.userID, "oc_group", "omt_topic", "om_anchor")
	orphan := subscribe(s.public, util.MustParseUUID(uuid.NewString()), s.userID, "oc_gone", "", "")
	// The private issue's updates follow each subscription creator's access.
	s.joinPrivateProject(t, s.userID)
	subscribe(s.private, s.installation, s.userID, "oc_member", "", "")
	subscribe(s.private, s.installation, s.otherMember(t), "oc_outsider", "", "")

	h.router.PublishIssueUpdate(s.workspaceID, s.public.ID, func(context.Context) string { return "status changed" })
	h.router.PublishIssueUpdate(s.workspaceID, s.private.ID, func(context.Context) string { return "secret changed" })
	if !h.router.Drain(ctx) {
		t.Fatal("drain timed out")
	}

	posts := map[string]postedUpdate{}
	for _, p := range poster.calls() {
		posts[p.target.ChatID] = p
	}
	want := ChatTarget{ChatID: "oc_group", ThreadID: "omt_topic", AnchorMessageID: "om_anchor"}
	if len(posts) != 2 || posts["oc_group"].target != want || posts["oc_group"].text != "status changed" ||
		posts["oc_group"].installationID != util.UUIDToString(s.installation) ||
		posts["oc_member"].text != "secret changed" {
		t.Fatalf("posts = %+v, want the topic update and the project member's private update", posts)
	}
	for _, row := range s.rows(t, s.public.ID) {
		if row.ID == orphan.ID {
			t.Fatal("subscription of a missing installation must be deleted")
		}
	}
	if rows := s.rows(t, s.private.ID); len(rows) != 2 {
		t.Fatalf("an outsider's subscription must be kept while its updates pause: %+v", rows)
	}
}

func (s *subscriptionDB) projectRows(t *testing.T, projectID pgtype.UUID) []db.ChannelProjectSubscription {
	t.Helper()
	rows, err := s.q.ListChannelProjectSubscriptionsForProject(context.Background(), db.ListChannelProjectSubscriptionsForProjectParams{
		ProjectID: projectID, WorkspaceID: s.workspaceID,
	})
	if err != nil {
		t.Fatalf("list project subscriptions: %v", err)
	}
	return rows
}

func TestRouter_SubscribeProject_DB(t *testing.T) {
	s := newSubscriptionDB(t)
	h, _ := s.harness(t)

	got := sendSubscription(t, h, groupSubscribeMessage(t, "/subscribe project web app"))
	if got.Subscription.Action != SubscriptionSubscribed || got.Subscription.ProjectID != s.webProject || got.Subscription.ProjectTitle != "Web App" {
		t.Fatalf("subscribe by name = %+v", got)
	}
	if rows := s.projectRows(t, s.webProject); len(rows) != 1 || rows[0].ChatID != "oc_chat" || rows[0].CreatedBy != s.userID {
		t.Fatalf("stored project subscription = %+v", rows)
	}
	link := "https://app.example/sub/projects/" + util.UUIDToString(s.webProject)
	if got := sendSubscription(t, h, groupSubscribeMessage(t, "/subscribe "+link)); got.Subscription.Action != SubscriptionAlreadySubscribed {
		t.Fatalf("repeat subscribe by bare project link = %+v", got)
	}
	if got := sendSubscription(t, h, groupSubscribeMessage(t, "/unsubscribe project "+link)); got.Subscription.Action != SubscriptionUnsubscribed {
		t.Fatalf("unsubscribe by link = %+v", got)
	}
	if rows := s.projectRows(t, s.webProject); len(rows) != 0 {
		t.Fatalf("unsubscribe left rows: %+v", rows)
	}

	// A second project with the same name makes the name ambiguous.
	s.f.Project(t, "web APP")
	if got := sendSubscription(t, h, groupSubscribeMessage(t, "/subscribe project Web App")); got.Subscription.Action != SubscriptionAmbiguous {
		t.Fatalf("ambiguous name = %+v", got)
	}
}

func TestRouter_SubscribeProjectFollowsSenderAccess_DB(t *testing.T) {
	s := newSubscriptionDB(t)
	h, _ := s.harness(t)
	private := util.MustParseUUID(s.projectID)
	for _, ref := range []string{"Secret", "https://app.example/sub/projects/" + s.projectID, "Nope"} {
		got := sendSubscription(t, h, groupSubscribeMessage(t, "/subscribe project "+ref))
		if got.Subscription.Action != SubscriptionNotFound || got.Subscription.ProjectTitle != "" {
			t.Fatalf("subscribe %s without access = %+v", ref, got)
		}
	}
	s.joinPrivateProject(t, s.userID)
	if got := sendSubscription(t, h, groupSubscribeMessage(t, "/subscribe project Secret")); got.Subscription.Action != SubscriptionSubscribed {
		t.Fatalf("project member subscribe = %+v", got)
	}
	if rows := s.projectRows(t, private); len(rows) != 1 {
		t.Fatalf("private project subscription = %+v", rows)
	}
}

func TestRouter_PublishProjectFeed_DB(t *testing.T) {
	s := newSubscriptionDB(t)
	h, poster := s.harness(t)
	ctx := context.Background()
	follow := func(chat string) {
		if _, err := s.q.CreateChannelProjectSubscription(ctx, db.CreateChannelProjectSubscriptionParams{
			WorkspaceID: s.workspaceID, ProjectID: s.webProject, InstallationID: s.installation,
			ChannelType: string(channel.TypeFeishu), ChatID: chat, CreatedBy: s.userID,
		}); err != nil {
			t.Fatalf("seed project subscription: %v", err)
		}
	}
	follow("oc_project")
	follow("oc_both")
	if _, err := s.q.CreateChannelIssueSubscription(ctx, db.CreateChannelIssueSubscriptionParams{
		WorkspaceID: s.workspaceID, IssueID: s.webIssue.ID, InstallationID: s.installation,
		ChannelType: string(channel.TypeFeishu), ChatID: "oc_both", CreatedBy: s.userID,
	}); err != nil {
		t.Fatalf("seed issue subscription: %v", err)
	}

	h.router.PublishIssueUpdate(s.workspaceID, s.webIssue.ID, func(context.Context) string { return "updated" })
	h.router.PublishIssueUpdate(s.workspaceID, s.public.ID, func(context.Context) string { return "unrelated" })
	if !h.router.Drain(ctx) {
		t.Fatal("drain timed out")
	}
	counts := map[string]int{}
	for _, p := range poster.calls() {
		if p.text != "updated" {
			t.Fatalf("issue outside the project posted: %+v", p)
		}
		counts[p.target.ChatID]++
	}
	if len(counts) != 2 || counts["oc_project"] != 1 || counts["oc_both"] != 1 {
		t.Fatalf("project fanout = %v, want each chat once", counts)
	}
}

// fakeTopicPoster roots a topic per OpenTopic call, like Feishu.
type fakeTopicPoster struct {
	fakePoster
	mu     sync.Mutex
	opened []string
}

func (f *fakeTopicPoster) OpenTopic(_ context.Context, _ db.ChannelInstallation, chatID, text string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.opened = append(f.opened, chatID+"|"+text)
	return fmt.Sprintf("om_topic_%d", len(f.opened)), nil
}

func (f *fakeTopicPoster) topics() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.opened...)
}

func TestRouter_ProjectFeedOpensOneTopicPerIssue_DB(t *testing.T) {
	s := newSubscriptionDB(t)
	h, _ := s.harness(t)
	poster := &fakeTopicPoster{}
	h.router.Register(channel.TypeFeishu, ResolverSet{
		Installation: h.inst, Identity: h.ident, Dedup: h.dedup, Session: h.binder,
		Audit: h.audit, Replier: h.replier, Poster: poster, OriginType: "lark_chat",
	})
	ctx := context.Background()
	drain := func() {
		t.Helper()
		if !h.router.Drain(ctx) {
			t.Fatal("drain timed out")
		}
	}

	cmd := groupSubscribeMessage(t, "/subscribe project Web App")
	cmd.MessageID = "om-project-cmd"
	if got := sendSubscription(t, h, cmd); got.Subscription.Action != SubscriptionSubscribed {
		t.Fatalf("subscribe project = %+v", got)
	}

	h.router.PublishIssueCreated(s.workspaceID, s.webIssue.ID, func(context.Context) string { return "created" })
	drain()
	if topics := poster.topics(); len(topics) != 1 || topics[0] != "oc_chat|created" {
		t.Fatalf("opened topics = %v, want one chat-level post for the new issue", topics)
	}
	rows := s.rows(t, s.webIssue.ID)
	if len(rows) != 1 || rows[0].AnchorMessageID != "om_topic_1" || !rows[0].ProjectSubscriptionID.Valid || rows[0].ThreadID != "" {
		t.Fatalf("issue topic row = %+v", rows)
	}

	h.router.PublishIssueUpdate(s.workspaceID, s.webIssue.ID, func(context.Context) string { return "updated" })
	drain()
	posts := poster.calls()
	if len(posts) != 1 || posts[0].target.AnchorMessageID != "om_topic_1" || posts[0].text != "updated" || len(poster.topics()) != 1 {
		t.Fatalf("later update = posts %+v topics %v, want one reply in the issue topic", posts, poster.topics())
	}

	// /unsubscribe issue inside the issue topic mutes it; the project does not
	// reopen a topic, and /subscribe issue there resumes it.
	inTopic := func(text string) channel.InboundMessage {
		msg := groupSubscribeMessage(t, text)
		msg.MessageID = "om-" + strings.ReplaceAll(text, " ", "-")
		msg.Source.ThreadID = "omt_issue"
		msg.ReplyTo = &channel.ReplyCtx{MessageID: "om_topic_1", RootID: "om_topic_1"}
		return msg
	}
	ref := fmt.Sprintf("SUB-%d", s.webIssue.Number)
	if got := sendSubscription(t, h, inTopic("/unsubscribe issue "+ref)); got.Subscription.Action != SubscriptionUnsubscribed {
		t.Fatalf("unsubscribe in issue topic = %+v", got)
	}
	h.router.PublishIssueUpdate(s.workspaceID, s.webIssue.ID, func(context.Context) string { return "muted" })
	drain()
	if len(poster.calls()) != 1 || len(poster.topics()) != 1 {
		t.Fatalf("muted issue posted: posts %+v topics %v", poster.calls(), poster.topics())
	}
	if got := sendSubscription(t, h, inTopic("/subscribe issue "+ref)); got.Subscription.Action != SubscriptionSubscribed {
		t.Fatalf("resubscribe in issue topic = %+v", got)
	}
	h.router.PublishIssueUpdate(s.workspaceID, s.webIssue.ID, func(context.Context) string { return "resumed" })
	drain()
	if posts := poster.calls(); len(posts) != 2 || posts[1].text != "resumed" {
		t.Fatalf("resumed issue topic posts = %+v", posts)
	}

	// Unsubscribing the project retires its issue topics.
	if got := sendSubscription(t, h, groupSubscribeMessage(t, "/unsubscribe project Web App")); got.Subscription.Action != SubscriptionUnsubscribed {
		t.Fatalf("unsubscribe project = %+v", got)
	}
	h.router.PublishIssueUpdate(s.workspaceID, s.webIssue.ID, func(context.Context) string { return "after" })
	drain()
	if len(poster.calls()) != 2 || len(poster.topics()) != 1 {
		t.Fatalf("update after project unsubscribe: posts %+v topics %v", poster.calls(), poster.topics())
	}
	if rows := s.rows(t, s.webIssue.ID); len(rows) != 0 {
		t.Fatalf("stale issue topic kept: %+v", rows)
	}
}

// An issue the chat created with /issue already follows its reply topic, so
// the chat's project subscription does not open a second topic for it.
func TestRouter_ProjectFeedSkipsIssueTopicForOriginChat_DB(t *testing.T) {
	s := newSubscriptionDB(t)
	h, _ := s.harness(t)
	poster := &fakeTopicPoster{}
	h.router.Register(channel.TypeFeishu, ResolverSet{
		Installation: h.inst, Identity: h.ident, Dedup: h.dedup, Session: h.binder,
		Audit: h.audit, Replier: h.replier, Poster: poster, OriginType: "lark_chat",
	})
	ctx := context.Background()
	if _, err := s.q.CreateChannelProjectSubscription(ctx, db.CreateChannelProjectSubscriptionParams{
		WorkspaceID: s.workspaceID, ProjectID: s.webProject, InstallationID: s.installation,
		ChannelType: string(channel.TypeFeishu), ChatID: "oc_chat", CreatedBy: s.userID,
	}); err != nil {
		t.Fatalf("seed project subscription: %v", err)
	}
	sessionID := uuid.NewString()
	s.f.Insert(t, "channel_chat_session_binding", dbfx.Cols{
		"chat_session_id": sessionID, "installation_id": util.UUIDToString(s.installation),
		"channel_type": string(channel.TypeFeishu), "channel_chat_id": "oc_chat", "chat_type": "group",
	})
	s.f.Exec(t, `UPDATE issue SET origin_type = 'lark_chat', origin_id = $2 WHERE id = $1`, util.UUIDToString(s.webIssue.ID), sessionID)

	h.router.PublishIssueCreated(s.workspaceID, s.webIssue.ID, func(context.Context) string { return "created" })
	if !h.router.Drain(ctx) {
		t.Fatal("drain timed out")
	}
	if topics := poster.topics(); len(topics) != 0 {
		t.Fatalf("origin chat got a project topic: %v", topics)
	}
}
