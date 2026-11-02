package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// The brief push sends a published brief to the members who opted in to a chat
// app (spec 2.7, plan 9.10). Publishing only queues a delivery row per member;
// the retry job sends it. A delivery failure never fails the brief, and a
// publish never waits on the chat platform.
const (
	// briefPushMaxAttempts is the number of sends tried before a row is failed.
	briefPushMaxAttempts = 5
	// briefPushClaimBatch bounds the rows one run takes, so a backlog drains in
	// steps and one run stays inside its timeout.
	briefPushClaimBatch = 20
	// briefPushLease is how long a claimed row is held. A run that dies leaves
	// its rows to come back after this long.
	briefPushLease = 10 * time.Minute
	// briefPushSendTimeout bounds one platform call.
	briefPushSendTimeout = 20 * time.Second
	// briefPushDecisionLines is how many pending decisions the message names.
	briefPushDecisionLines = 3
	// briefPushTitleRunes and briefPushHeadlineRunes keep each line short.
	briefPushTitleRunes    = 120
	briefPushHeadlineRunes = 280
	// briefPushErrorRunes caps last_error, which the table also limits.
	briefPushErrorRunes = 500
)

// The delivery states, as the brief_delivery.state CHECK spells them.
const (
	briefStatePending  = "pending"
	briefStateSent     = "sent"
	briefStateFailed   = "failed"
	briefStateDeferred = "deferred"
)

// briefPushBackoff is the wait after each failed attempt: 1, 2, 4 and 8
// minutes. The fifth failure is final.
var briefPushBackoff = []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute}

// Reasons recorded on a row the job fails before it reaches the platform.
const (
	briefPushReasonOff       = "brief push is off or the chat choice changed after this brief was queued"
	briefPushReasonNotMember = "the member is no longer in the workspace"
	briefPushReasonNoBinding = "the chat binding no longer belongs to this member"
	briefPushReasonNoBrief   = "the brief no longer exists"
	briefPushReasonNoChannel = "no notifier is installed for this chat app"
)

// BriefPushService queues brief deliveries on publish and sends them from the
// retry job. Notifiers are keyed by chat app; a chat app with no notifier
// receives nothing, and the preference API refuses to choose it.
type BriefPushService struct {
	Queries   *db.Queries
	Notifiers map[channel.Type]channel.Notifier
	// AppURL is the web app origin the message's links are built from.
	AppURL string
	Logger *slog.Logger

	now func() time.Time
}

// NewBriefPushService returns a service with no notifiers. Add each chat app's
// notifier with RegisterNotifier.
func NewBriefPushService(q *db.Queries, appURL string) *BriefPushService {
	return &BriefPushService{
		Queries:   q,
		Notifiers: map[channel.Type]channel.Notifier{},
		AppURL:    appURL,
		Logger:    slog.Default(),
		now:       time.Now,
	}
}

// RegisterNotifier makes a chat app able to receive briefs.
func (s *BriefPushService) RegisterNotifier(t channel.Type, n channel.Notifier) {
	s.Notifiers[t] = n
}

// ChatAppSupported reports whether a chat app can receive a brief push. A nil
// service supports nothing, so a deployment that never wired it refuses cleanly.
func (s *BriefPushService) ChatAppSupported(t channel.Type) bool {
	if s == nil {
		return false
	}
	_, ok := s.Notifiers[t]
	return ok
}

func (s *BriefPushService) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

// EnqueueForBrief writes one pending delivery per member who has brief push on
// with a chat chosen. Running it twice for one brief writes nothing new. It is
// called from the brief:created subscriber; it does no network I/O.
func (s *BriefPushService) EnqueueForBrief(ctx context.Context, workspaceID, briefID pgtype.UUID) (int, error) {
	if len(s.Notifiers) == 0 {
		return 0, nil
	}
	recipients, err := s.Queries.ListBriefPushRecipients(ctx, workspaceID)
	if err != nil {
		return 0, fmt.Errorf("list brief push recipients: %w", err)
	}
	var errs []error
	queued := 0
	for _, r := range recipients {
		n, err := s.Queries.CreateBriefDelivery(ctx, db.CreateBriefDeliveryParams{
			ID:          dbid.NewV7(),
			WorkspaceID: workspaceID,
			BriefID:     briefID,
			MemberID:    r.MemberID,
			BindingID:   r.BindingID,
			Now:         briefPushTimestamp(s.clock()),
		})
		if err != nil {
			errs = append(errs, fmt.Errorf("queue brief delivery for member %s: %w", util.UUIDToString(r.MemberID), err))
			continue
		}
		queued += int(n)
	}
	return queued, errors.Join(errs...)
}

// DeliverDue sends the due deliveries. It is the retry job's handler. It
// returns how many rows it took; a row that fails to send is recorded and does
// not stop the run. The error is for the run as a whole (a failed claim, or a
// row whose outcome could not be written).
func (s *BriefPushService) DeliverDue(ctx context.Context) (int, error) {
	now := s.clock()
	rows, err := s.Queries.ClaimDueBriefDeliveries(ctx, db.ClaimDueBriefDeliveriesParams{
		DueAt:      briefPushTimestamp(now),
		LeaseUntil: briefPushTimestamp(now.Add(briefPushLease)),
		RowLimit:   briefPushClaimBatch,
	})
	if err != nil {
		return 0, fmt.Errorf("claim brief deliveries: %w", err)
	}
	var errs []error
	for _, d := range rows {
		if ctx.Err() != nil {
			// The run is over. Unsent rows keep their lease and come back.
			errs = append(errs, ctx.Err())
			break
		}
		if err := s.deliver(ctx, d); err != nil {
			s.Logger.Warn("brief push: could not record delivery outcome",
				"delivery_id", util.UUIDToString(d.ID), "error", err)
			errs = append(errs, err)
		}
	}
	return len(rows), errors.Join(errs...)
}

// deliver makes one attempt. Each check that fails before the platform is
// reached settles the row as failed without counting an attempt. The row is
// reloaded from the preference and the membership each time, so a member who
// switched the push off after publish does not receive the brief.
func (s *BriefPushService) deliver(ctx context.Context, d db.BriefDelivery) error {
	ws := d.WorkspaceID
	now := s.clock()

	pref, err := s.Queries.GetBriefPushPreference(ctx, db.GetBriefPushPreferenceParams{
		WorkspaceID: ws, MemberID: d.MemberID,
	})
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("load brief push preference: %w", err)
		}
		return s.settleUnsent(ctx, d, briefStateFailed, briefPushReasonOff, time.Time{})
	}
	if !pref.BriefPush || util.UUIDToString(pref.BindingID) != util.UUIDToString(d.BindingID) {
		return s.settleUnsent(ctx, d, briefStateFailed, briefPushReasonOff, time.Time{})
	}

	if _, err := s.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID: d.MemberID, WorkspaceID: ws,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return s.settleUnsent(ctx, d, briefStateFailed, briefPushReasonNotMember, time.Time{})
		}
		return fmt.Errorf("check membership: %w", err)
	}

	binding, err := s.Queries.GetChannelUserBindingInWorkspace(ctx, db.GetChannelUserBindingInWorkspaceParams{
		ID: d.BindingID, WorkspaceID: ws,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return s.settleUnsent(ctx, d, briefStateFailed, briefPushReasonNoBinding, time.Time{})
		}
		return fmt.Errorf("load chat binding: %w", err)
	}
	if util.UUIDToString(binding.MulticaUserID) != util.UUIDToString(d.MemberID) {
		return s.settleUnsent(ctx, d, briefStateFailed, briefPushReasonNoBinding, time.Time{})
	}

	if pref.QuietHoursStart.Valid && pref.QuietHoursEnd.Valid {
		loc := s.memberLocation(ctx, d.MemberID)
		if until, quiet := quietWindowEnd(now, loc, int(pref.QuietHoursStart.Int16), int(pref.QuietHoursEnd.Int16)); quiet {
			return s.settleUnsent(ctx, d, briefStateDeferred, "", until)
		}
	}

	brief, err := s.Queries.GetBriefInWorkspace(ctx, db.GetBriefInWorkspaceParams{ID: d.BriefID, WorkspaceID: ws})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return s.settleUnsent(ctx, d, briefStateFailed, briefPushReasonNoBrief, time.Time{})
		}
		return fmt.Errorf("load brief: %w", err)
	}

	notifier, ok := s.Notifiers[channel.Type(binding.ChannelType)]
	if !ok {
		return s.settleUnsent(ctx, d, briefStateFailed, briefPushReasonNoChannel, time.Time{})
	}

	msg, err := s.composeMessage(ctx, ws, brief)
	if err != nil {
		return err
	}

	sendCtx, cancel := context.WithTimeout(ctx, briefPushSendTimeout)
	sendErr := notifier.Send(sendCtx, channel.Binding{
		ID:             binding.ID,
		WorkspaceID:    binding.WorkspaceID,
		MulticaUserID:  binding.MulticaUserID,
		InstallationID: binding.InstallationID,
		ChannelType:    channel.Type(binding.ChannelType),
		ChannelUserID:  binding.ChannelUserID,
	}, msg)
	cancel()

	return s.recordAttempt(ctx, d, now, sendErr)
}

// recordAttempt settles a row after a send. A success is sent. A permanent
// failure is failed at once. Anything else is retried with backoff, honouring a
// retry_after the platform gave, and is failed once the attempt cap is reached.
func (s *BriefPushService) recordAttempt(ctx context.Context, d db.BriefDelivery, now time.Time, sendErr error) error {
	attempts := int(d.Attempts) + 1
	if sendErr == nil {
		_, err := s.Queries.RecordBriefDeliveryOutcome(ctx, db.RecordBriefDeliveryOutcomeParams{
			ID:            d.ID,
			State:         briefStateSent,
			Attempts:      int32(attempts),
			NextAttemptAt: briefPushTimestamp(now),
			LastError:     "",
			DeliveredAt:   briefPushTimestamp(now),
		})
		return wrapOutcome(err)
	}
	lastError := cutRunes(sendErr.Error(), briefPushErrorRunes)
	if channel.IsPermanentDeliveryError(sendErr) || attempts >= briefPushMaxAttempts {
		_, err := s.Queries.RecordBriefDeliveryOutcome(ctx, db.RecordBriefDeliveryOutcomeParams{
			ID:            d.ID,
			State:         briefStateFailed,
			Attempts:      int32(attempts),
			NextAttemptAt: briefPushTimestamp(now),
			LastError:     lastError,
		})
		return wrapOutcome(err)
	}
	wait := briefPushBackoffFor(attempts)
	if retryAfter := channel.DeliveryRetryAfter(sendErr); retryAfter > wait {
		wait = retryAfter
	}
	_, err := s.Queries.RecordBriefDeliveryOutcome(ctx, db.RecordBriefDeliveryOutcomeParams{
		ID:            d.ID,
		State:         briefStatePending,
		Attempts:      int32(attempts),
		NextAttemptAt: briefPushTimestamp(now.Add(wait)),
		LastError:     lastError,
	})
	return wrapOutcome(err)
}

// settleUnsent records a row that never reached the platform. It keeps the
// attempt count. A zero until means the row is failed; otherwise it is deferred
// to until.
func (s *BriefPushService) settleUnsent(ctx context.Context, d db.BriefDelivery, state, reason string, until time.Time) error {
	params := db.RecordBriefDeliveryOutcomeParams{
		ID:            d.ID,
		State:         state,
		Attempts:      d.Attempts,
		NextAttemptAt: briefPushTimestamp(until),
		LastError:     reason,
	}
	if until.IsZero() {
		params.NextAttemptAt = briefPushTimestamp(s.clock())
	}
	_, err := s.Queries.RecordBriefDeliveryOutcome(ctx, params)
	return wrapOutcome(err)
}

func wrapOutcome(err error) error {
	if err != nil {
		return fmt.Errorf("record brief delivery outcome: %w", err)
	}
	return nil
}

// memberLocation is the member's saved timezone, or UTC when there is none or
// it does not load.
func (s *BriefPushService) memberLocation(ctx context.Context, userID pgtype.UUID) *time.Location {
	user, err := s.Queries.GetUser(ctx, userID)
	if err != nil || !user.Timezone.Valid {
		return time.UTC
	}
	loc, err := time.LoadLocation(user.Timezone.String)
	if err != nil {
		return time.UTC
	}
	return loc
}

// composeMessage builds the push: the headline, the pending decisions the brief
// raises, and a link to the brief. It reads the proposals at send time, so a
// decision answered since publish is not named.
func (s *BriefPushService) composeMessage(ctx context.Context, ws pgtype.UUID, brief db.Brief) (channel.Message, error) {
	ordered := make([]pgtype.UUID, 0, len(brief.ProposalIds))
	seen := map[string]bool{}
	for _, id := range brief.ProposalIds {
		if key := util.UUIDToString(id); !seen[key] {
			seen[key] = true
			ordered = append(ordered, id)
		}
	}
	var decisions []briefPushDecision
	if len(ordered) > 0 {
		rows, err := s.Queries.ListBriefPendingDecisions(ctx, db.ListBriefPendingDecisionsParams{
			WorkspaceID: ws, ProposalIds: ordered, Now: briefPushTimestamp(s.clock()),
		})
		if err != nil {
			return channel.Message{}, fmt.Errorf("load pending decisions: %w", err)
		}
		byID := make(map[string]string, len(rows))
		for _, r := range rows {
			byID[util.UUIDToString(r.ID)] = r.Title
		}
		for _, id := range ordered {
			if title, ok := byID[util.UUIDToString(id)]; ok {
				decisions = append(decisions, briefPushDecision{ID: util.UUIDToString(id), Title: title})
			}
		}
	}

	workspace, err := s.Queries.GetWorkspace(ctx, ws)
	if err != nil {
		return channel.Message{}, fmt.Errorf("load workspace: %w", err)
	}
	office := briefPushOfficeURL(s.AppURL, workspace.Slug)
	headline := BriefHeadline(brief.Body)
	if headline == "" {
		headline = brief.Title
	}
	return renderBriefPush(headline, decisions, util.UUIDToString(brief.ID), office), nil
}

// briefPushDecision is one pending decision named in the push.
type briefPushDecision struct {
	ID    string
	Title string
}

// renderBriefPush lays out the message. office is the Office URL, or "" when
// no app URL is configured, in which case the message has no links. It is pure,
// so the format is tested without a database.
func renderBriefPush(headline string, decisions []briefPushDecision, briefID, office string) channel.Message {
	var b strings.Builder
	b.WriteString(cutRunes(headline, briefPushHeadlineRunes))

	if len(decisions) > 0 {
		b.WriteString("\n\n")
		if len(decisions) == 1 {
			b.WriteString("1 decision needs you:")
		} else {
			fmt.Fprintf(&b, "%d decisions need you:", len(decisions))
		}
		shown := decisions
		if len(shown) > briefPushDecisionLines {
			shown = shown[:briefPushDecisionLines]
		}
		for _, d := range shown {
			b.WriteString("\n- ")
			b.WriteString(oneLine(cutRunes(d.Title, briefPushTitleRunes)))
			if office != "" {
				b.WriteString(" ")
				b.WriteString(office + "?" + url.Values{"decision": {d.ID}}.Encode())
			}
		}
		if extra := len(decisions) - len(shown); extra > 0 {
			fmt.Fprintf(&b, "\n...and %d more in the Office.", extra)
		}
	}

	msg := channel.Message{Text: b.String()}
	if office != "" {
		briefURL := office + "?" + url.Values{"brief": {briefID}}.Encode()
		msg.Text += "\n\nBrief: " + briefURL
		msg.Buttons = []channel.Button{{Label: "Open the brief", URL: briefURL}}
	}
	return msg
}

// briefPushOfficeURL is the Office page for a workspace, or "" when either part
// is unknown.
func briefPushOfficeURL(appURL, slug string) string {
	appURL = strings.TrimRight(strings.TrimSpace(appURL), "/")
	if appURL == "" || slug == "" {
		return ""
	}
	return appURL + "/" + url.PathEscape(slug) + "/office"
}

// BriefHeadline is the Headline section of a brief, its lines joined and cut to
// the inbox length. Empty when the body has no Headline.
func BriefHeadline(body string) string {
	var lines []string
	in := false
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "## ") {
			if in {
				break
			}
			in = strings.EqualFold(strings.TrimSpace(line[3:]), "Headline")
			continue
		}
		if in {
			lines = append(lines, strings.TrimSpace(line))
		}
	}
	return cutRunes(strings.TrimSpace(strings.Join(lines, " ")), briefPushHeadlineRunes)
}

// quietWindowEnd reports whether now is inside the quiet window and, if so, the
// instant the window ends. Hours are local to loc. The window is [start, end),
// wrapping midnight when start is after end. start equal to end is no window.
func quietWindowEnd(now time.Time, loc *time.Location, start, end int) (time.Time, bool) {
	if start == end || start < 0 || start > 23 || end < 0 || end > 23 {
		return time.Time{}, false
	}
	local := now.In(loc)
	h := local.Hour()
	var inside bool
	if start < end {
		inside = h >= start && h < end
	} else {
		inside = h >= start || h < end
	}
	if !inside {
		return time.Time{}, false
	}
	until := time.Date(local.Year(), local.Month(), local.Day(), end, 0, 0, 0, loc)
	if !until.After(now) {
		until = until.AddDate(0, 0, 1)
	}
	return until, true
}

// briefPushBackoffFor is the wait after the given failed attempt, which is
// between 1 and briefPushMaxAttempts-1.
func briefPushBackoffFor(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > len(briefPushBackoff) {
		return briefPushBackoff[len(briefPushBackoff)-1]
	}
	return briefPushBackoff[attempt-1]
}

func briefPushTimestamp(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}

// cutRunes cuts s to at most n runes, marking the cut with an ellipsis.
func cutRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}

// oneLine keeps a decision title on one line.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
