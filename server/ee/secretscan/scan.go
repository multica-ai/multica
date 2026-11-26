// Package secretscan finds API keys, access tokens and private keys pasted
// into comments (MUL-10283).
//
// Every comment written through the comment endpoints is checked as it is
// saved, against the high-confidence rules in pkg/redact. By default a hit is
// recorded and shown to the comment's author and the workspace admins, who can
// redact it or mark it as not a secret; a workspace can instead have hits
// replaced with a placeholder before the comment is stored. A finding keeps
// the kind of secret, a masked preview and a salted fingerprint, never the
// secret itself. Admins can also scan the comments written before scanning
// was on.
package secretscan

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/ee/secretscan/scandb"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/extension"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
	"github.com/multica-ai/multica/server/pkg/redact"
)

// EventChanged tells clients to refetch findings. Its payload names the
// issue when the change is about one, which also keeps it to the people who
// can see that issue.
const EventChanged = "secret_scan:changed"

const (
	ModeNotify = "notify"
	ModeRedact = "redact"

	StatusOpen      = "open"
	StatusRedacted  = "redacted"
	StatusDismissed = "dismissed"
	StatusResolved  = "resolved"
)

func init() {
	extension.RegisterWorkspaceRoutes(Mount)
}

// Mount registers the routes on r and starts scanning comments.
func Mount(r chi.Router, h *handler.Handler) {
	s := New(h)
	h.SetCommentScanner(s)
	s.subscribe(h.Bus)
	r.Route("/api/secret-scan", func(r chi.Router) {
		r.Get("/settings", s.GetSettings)
		r.Put("/settings", s.UpdateSettings)
		r.Post("/backfill", s.StartBackfill)
		r.Get("/findings", s.ListFindings)
		r.Post("/findings/redact", s.Redact)
		r.Post("/findings/dismiss", s.Dismiss)
		r.Get("/issues/{issueId}/findings", s.ListIssueFindings)
	})
}

type Service struct {
	h *handler.Handler
	q *scandb.Queries
}

func New(h *handler.Handler) *Service {
	return &Service{h: h, q: scandb.New(h.DB)}
}

type finding struct {
	rule        string
	masked      string
	fingerprint string
}

// fingerprint tells two findings of the same secret apart from findings of
// different ones without keeping anything that leads back to the secret.
func fingerprint(salt []byte, m redact.Match) string {
	mac := hmac.New(sha256.New, salt)
	mac.Write([]byte(m.Rule))
	mac.Write([]byte{0})
	mac.Write([]byte(m.Value))
	return hex.EncodeToString(mac.Sum(nil))[:32]
}

func findingsOf(salt []byte, ms []redact.Match) []finding {
	seen := make(map[string]bool, len(ms))
	out := make([]finding, 0, len(ms))
	for _, m := range ms {
		fp := fingerprint(salt, m)
		if seen[fp] {
			continue
		}
		seen[fp] = true
		out = append(out, finding{rule: m.Rule, masked: redact.Mask(m), fingerprint: fp})
	}
	return out
}

// ScanComment implements handler.CommentScanner. A scan that cannot read the
// workspace's setting lets the comment through unscanned: a comment is never
// refused because of the scanner.
func (s *Service) ScanComment(ctx context.Context, workspaceID pgtype.UUID, content string, edit bool) (string, func(context.Context, db.Comment)) {
	ms := redact.Find(content)
	if len(ms) == 0 {
		if !edit {
			return content, nil
		}
		return content, func(ctx context.Context, c db.Comment) { s.record(ctx, c, nil, "", true) }
	}
	setting, err := s.q.EnsureSetting(ctx, workspaceID)
	if err != nil {
		slog.Warn("secret scan: load setting", "workspace_id", util.UUIDToString(workspaceID), "error", err)
		return content, nil
	}
	found := findingsOf(setting.Salt, ms)
	status := StatusOpen
	if setting.Mode == ModeRedact {
		content = redact.Replace(content, ms)
		status = StatusRedacted
	}
	return content, func(ctx context.Context, c db.Comment) { s.record(ctx, c, found, status, edit) }
}

func (s *Service) record(ctx context.Context, c db.Comment, found []finding, status string, edit bool) {
	ctx = context.WithoutCancel(ctx)
	changed := len(found) > 0
	for _, f := range found {
		if err := s.q.RecordFinding(ctx, scandb.RecordFindingParams{
			WorkspaceID: c.WorkspaceID,
			IssueID:     c.IssueID,
			CommentID:   c.ID,
			AuthorType:  c.AuthorType,
			AuthorID:    c.AuthorID,
			Rule:        f.rule,
			Masked:      f.masked,
			Fingerprint: f.fingerprint,
			Status:      status,
		}); err != nil {
			slog.Warn("secret scan: record finding", "comment_id", util.UUIDToString(c.ID), "error", err)
		}
	}
	if edit {
		keep := make([]string, len(found))
		for i, f := range found {
			keep[i] = f.fingerprint
		}
		n, err := s.q.ResolveMissingFindings(ctx, scandb.ResolveMissingFindingsParams{CommentID: c.ID, Keep: keep})
		if err != nil {
			slog.Warn("secret scan: resolve findings", "comment_id", util.UUIDToString(c.ID), "error", err)
		}
		changed = changed || n > 0
	}
	if changed {
		s.publishIssue(util.UUIDToString(c.WorkspaceID), c.IssueID, "system", "")
	}
}

func (s *Service) publishIssue(workspaceID string, issueID pgtype.UUID, actorType, actorID string) {
	s.h.Publish(EventChanged, workspaceID, actorType, actorID, map[string]any{"issue_id": util.UUIDToString(issueID)})
}

// subscribe drops the findings of deleted comments and issues, so a finding
// never outlives the text it was about.
func (s *Service) subscribe(bus *events.Bus) {
	if bus == nil {
		return
	}
	bus.Subscribe(protocol.EventCommentDeleted, func(e events.Event) {
		id, ok := payloadUUID(e.Payload, "comment_id")
		if !ok {
			return
		}
		if err := s.q.DeleteCommentFindings(context.Background(), []pgtype.UUID{id}); err != nil {
			slog.Warn("secret scan: drop comment findings", "comment_id", util.UUIDToString(id), "error", err)
		}
	})
	bus.Subscribe(protocol.EventIssueDeleted, func(e events.Event) {
		id, ok := payloadUUID(e.Payload, "issue_id")
		if !ok {
			return
		}
		if err := s.q.DeleteIssueFindings(context.Background(), id); err != nil {
			slog.Warn("secret scan: drop issue findings", "issue_id", util.UUIDToString(id), "error", err)
		}
	})
}

func payloadUUID(payload any, key string) (pgtype.UUID, bool) {
	m, ok := payload.(map[string]any)
	if !ok {
		return pgtype.UUID{}, false
	}
	s, ok := m[key].(string)
	if !ok {
		return pgtype.UUID{}, false
	}
	id, err := util.ParseUUID(s)
	return id, err == nil
}
