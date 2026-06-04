package github

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Patcher pushes local Multica changes back out to the GitHub board. It
// subscribes to the in-process event bus (the same mechanism the Lark
// Patcher uses) and translates issue-status changes into ProjectV2
// single-select writes, and new comments into GitHub issue comments.
//
// Loop guard: every inbound import records the remote status it observed
// (binding.RemoteStatus) and every outbound push records what it wrote
// (binding.LastPushedStatus). The patcher refuses to push a status that
// equals the value GitHub already holds, so an imported change never
// bounces straight back.
type Patcher struct {
	sync *Sync
	log  *slog.Logger
}

// NewPatcher builds an outbound patcher bound to a Sync.
func NewPatcher(s *Sync) *Patcher {
	return &Patcher{sync: s, log: s.log.With("role", "patcher")}
}

// Register wires the patcher onto the event bus. Mirrors lark.Patcher.Register.
func (p *Patcher) Register(bus *events.Bus) {
	bus.Subscribe(protocol.EventIssueUpdated, p.handleEvent)
	bus.Subscribe(protocol.EventCommentCreated, p.handleEvent)
}

func (p *Patcher) handleEvent(e events.Event) {
	// Bus delivery is synchronous; use a bounded background ctx so a slow
	// GitHub call never wedges the publishing request.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if e.WorkspaceID != p.sync.cfg.WorkspaceID {
		return // not our workspace
	}
	var err error
	switch e.Type {
	case protocol.EventIssueUpdated:
		err = p.onIssueUpdated(ctx, e)
	case protocol.EventCommentCreated:
		err = p.onCommentCreated(ctx, e)
	}
	if err != nil {
		p.log.Warn("outbound push failed", "event", e.Type, "error", err)
	}
}

// payload shapes (parsed via JSON round-trip; the in-process bus carries
// the handler structs as `any`, so we re-marshal and pull just our fields).
type issueUpdatedPayload struct {
	Issue struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	} `json:"issue"`
	StatusChanged bool `json:"status_changed"`
}

type commentCreatedPayload struct {
	Comment struct {
		IssueID    string `json:"issue_id"`
		AuthorType string `json:"author_type"`
		Content    string `json:"content"`
		Type       string `json:"type"`
	} `json:"comment"`
}

func decodePayload(p any, out any) error {
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

func (p *Patcher) onIssueUpdated(ctx context.Context, e events.Event) error {
	var pl issueUpdatedPayload
	if err := decodePayload(e.Payload, &pl); err != nil {
		return err
	}
	if !pl.StatusChanged || pl.Issue.ID == "" {
		return nil
	}
	binding, err := p.sync.store.GetBindingByIssue(ctx, p.sync.inst.ID, pl.Issue.ID)
	if err != nil || binding == nil {
		return err // not a synced issue (or lookup error)
	}
	ghStatus, ok := MapStatusToGitHub(pl.Issue.Status)
	if !ok {
		return nil
	}
	// Loop guard: GitHub already holds this status — nothing to push.
	if ghStatus == binding.RemoteStatus {
		return nil
	}
	statusField, ok := p.sync.schema.Fields["Status"]
	if !ok {
		return fmt.Errorf("board has no Status field")
	}
	optionID, ok := statusField.Options[ghStatus]
	if !ok {
		return fmt.Errorf("board Status has no option %q", ghStatus)
	}
	if err := p.sync.client.SetSingleSelectValue(ctx, p.sync.inst.ProjectNodeID, binding.GitHubItemID, statusField.ID, optionID); err != nil {
		return fmt.Errorf("set status on board: %w", err)
	}
	if err := p.sync.store.SetLastPushedStatus(ctx, p.sync.inst.ID, pl.Issue.ID, ghStatus); err != nil {
		p.log.Debug("record pushed status failed", "error", err)
	}
	p.log.Info("pushed status to github", "issue", pl.Issue.ID, "status", ghStatus, "item", binding.GitHubItemID)
	return nil
}

func (p *Patcher) onCommentCreated(ctx context.Context, e events.Event) error {
	var pl commentCreatedPayload
	if err := decodePayload(e.Payload, &pl); err != nil {
		return err
	}
	c := pl.Comment
	if c.IssueID == "" || c.Content == "" {
		return nil
	}
	// System/status-change rows are not user-authored prose; don't mirror.
	if c.AuthorType == "system" || (c.Type != "" && c.Type != "comment") {
		return nil
	}
	binding, err := p.sync.store.GetBindingByIssue(ctx, p.sync.inst.ID, c.IssueID)
	if err != nil || binding == nil {
		return err
	}
	if binding.GitHubRepo == "" || binding.GitHubIssueNumber == 0 {
		return nil // draft item with no backing repo issue
	}
	body := strings.TrimSpace(c.Content) + "\n\n_— posted from Multica_"
	if err := p.sync.client.AddIssueComment(ctx, binding.GitHubRepo, binding.GitHubIssueNumber, body); err != nil {
		return fmt.Errorf("add github comment: %w", err)
	}
	p.log.Info("pushed comment to github", "issue", c.IssueID, "repo", binding.GitHubRepo, "number", binding.GitHubIssueNumber)
	return nil
}
