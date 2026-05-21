package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/integration/linear"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// IntegrationService handles creating mirror issues from external sources
// and syncing status back when work completes.
type IntegrationService struct {
	Queries      *db.Queries
	TxStarter    TxStarter
	Bus          *events.Bus
	TaskSvc      *TaskService
	LinearClient *linear.Client
}

func NewIntegrationService(q *db.Queries, tx TxStarter, bus *events.Bus, taskSvc *TaskService) *IntegrationService {
	return &IntegrationService{
		Queries:      q,
		TxStarter:    tx,
		Bus:          bus,
		TaskSvc:      taskSvc,
		LinearClient: linear.NewClient(),
	}
}

// ExternalIssue represents a normalized issue from an external source.
type ExternalIssue struct {
	Provider    string // "linear" or "github"
	ExternalID  string // Linear issue UUID or GitHub issue node_id
	Identifier  string // "LIN-123" or "owner/repo#42"
	Title       string
	Description string
	URL         string
	Priority    string
	Status      string
}

// ImportExternalIssue creates a mirror Multica issue for an external issue.
// It handles deduplication via the external_issue_link table.
// Returns the Multica issue and whether it was newly created.
func (s *IntegrationService) ImportExternalIssue(
	ctx context.Context,
	workspaceID pgtype.UUID,
	integration db.WorkspaceIntegration,
	ext ExternalIssue,
) (db.Issue, bool, error) {
	// Check for existing link (dedup).
	existing, err := s.Queries.GetExternalIssueLinkByExternalID(ctx, db.GetExternalIssueLinkByExternalIDParams{
		WorkspaceID: workspaceID,
		Provider:    ext.Provider,
		ExternalID:  ext.ExternalID,
	})
	if err == nil {
		// Already imported — return existing issue.
		issue, err := s.Queries.GetIssue(ctx, existing.IssueID)
		if err != nil {
			return db.Issue{}, false, fmt.Errorf("load linked issue: %w", err)
		}
		return issue, false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return db.Issue{}, false, fmt.Errorf("check existing link: %w", err)
	}

	// Find the agent to assign. Prefer configured default, fall back to first available agent.
	var agentID pgtype.UUID
	if integration.DefaultAgentID.Valid {
		agentID = integration.DefaultAgentID
	} else {
		agents, err := s.Queries.ListAgents(ctx, workspaceID)
		if err != nil || len(agents) == 0 {
			return db.Issue{}, false, fmt.Errorf("no agents available in workspace for %s integration", ext.Provider)
		}
		agentID = pgtype.UUID{Bytes: agents[0].ID.Bytes, Valid: true}
		slog.Warn("integration: no default agent, using first available",
			"agent", agents[0].Name, "provider", ext.Provider)
	}

	agent, err := s.Queries.GetAgent(ctx, agentID)
	if err != nil {
		return db.Issue{}, false, fmt.Errorf("load agent: %w", err)
	}

	// Map external priority to Multica priority.
	priority := mapPriority(ext.Priority)

	// Build description with external link.
	description := ext.Description
	if ext.URL != "" {
		description = fmt.Sprintf("_Imported from [%s](%s)_\n\n%s", ext.Identifier, ext.URL, description)
	}

	// Create issue in a transaction (need to increment counter).
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return db.Issue{}, false, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	qtx := s.Queries.WithTx(tx)
	issueNumber, err := qtx.IncrementIssueCounter(ctx, workspaceID)
	if err != nil {
		return db.Issue{}, false, fmt.Errorf("increment issue counter: %w", err)
	}

	issue, err := qtx.CreateIssue(ctx, db.CreateIssueParams{
		WorkspaceID:   workspaceID,
		Title:         ext.Title,
		Description:   pgtype.Text{String: description, Valid: description != ""},
		Status:        "todo",
		Priority:      priority,
		AssigneeType:  pgtype.Text{String: "agent", Valid: true},
		AssigneeID:    agentID,
		CreatorType:   "agent",
		CreatorID:     agent.ID,
		ParentIssueID: pgtype.UUID{},
		Position:      0,
		DueDate:       pgtype.Timestamptz{},
		Number:        issueNumber,
		ProjectID:     pgtype.UUID{},
	})
	if err != nil {
		return db.Issue{}, false, fmt.Errorf("create issue: %w", err)
	}

	// Create the external issue link.
	_, err = qtx.CreateExternalIssueLink(ctx, db.CreateExternalIssueLinkParams{
		WorkspaceID:        workspaceID,
		IssueID:            issue.ID,
		Provider:           ext.Provider,
		ExternalID:         ext.ExternalID,
		ExternalIdentifier: pgtype.Text{String: ext.Identifier, Valid: ext.Identifier != ""},
		ExternalUrl:        pgtype.Text{String: ext.URL, Valid: ext.URL != ""},
		SyncDirection:      "bidirectional",
	})
	if err != nil {
		return db.Issue{}, false, fmt.Errorf("create external link: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return db.Issue{}, false, fmt.Errorf("commit: %w", err)
	}

	slog.Info("imported external issue",
		"provider", ext.Provider,
		"external_id", ext.ExternalID,
		"identifier", ext.Identifier,
		"issue_id", util.UUIDToString(issue.ID),
		"workspace_id", util.UUIDToString(workspaceID),
	)

	return issue, true, nil
}

// SyncStatusToExternal syncs a Multica issue's terminal status back to the
// external source (Linear or GitHub).
func (s *IntegrationService) SyncStatusToExternal(ctx context.Context, issue db.Issue) error {
	link, err := s.Queries.GetExternalIssueLinkByIssueID(ctx, issue.ID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // Not a linked issue — nothing to sync.
		}
		return fmt.Errorf("load external link: %w", err)
	}

	if link.SyncDirection == "inbound" {
		return nil // Inbound-only — don't push back.
	}

	switch link.Provider {
	case "linear":
		return s.syncToLinear(ctx, issue, link)
	case "github":
		return s.syncToGitHub(ctx, issue, link)
	default:
		return fmt.Errorf("unknown provider: %s", link.Provider)
	}
}

func (s *IntegrationService) syncToLinear(ctx context.Context, issue db.Issue, link db.ExternalIssueLink) error {
	if !s.LinearClient.Available() {
		slog.Warn("linear sync skipped: no API key", "issue_id", util.UUIDToString(issue.ID))
		return nil
	}

	var targetState string
	switch issue.Status {
	case "done":
		targetState = "Done"
	case "cancelled":
		targetState = "Cancelled"
	case "in_progress":
		targetState = "In Progress"
	case "in_review":
		targetState = "In Review"
	default:
		return nil // Only sync known states.
	}

	if err := s.LinearClient.UpdateIssueState(link.ExternalID, targetState); err != nil {
		slog.Warn("linear sync failed", "issue_id", util.UUIDToString(issue.ID), "external_id", link.ExternalID, "error", err)
		return err
	}

	s.Queries.UpdateExternalIssueLinkSyncedAt(ctx, link.ID)
	slog.Info("synced status to linear", "issue_id", util.UUIDToString(issue.ID), "state", targetState)
	return nil
}

func (s *IntegrationService) syncToGitHub(ctx context.Context, issue db.Issue, link db.ExternalIssueLink) error {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		slog.Warn("github sync skipped: no GITHUB_TOKEN", "issue_id", util.UUIDToString(issue.ID))
		return nil
	}

	if issue.Status != "done" && issue.Status != "cancelled" {
		return nil // Only close issues on terminal states.
	}

	// Parse config to get owner/repo from the link's external_identifier (e.g., "owner/repo#42").
	var config GitHubIntegrationConfig
	integration, err := s.Queries.GetWorkspaceIntegrationByProvider(ctx, db.GetWorkspaceIntegrationByProviderParams{
		WorkspaceID: link.WorkspaceID,
		Provider:    "github",
	})
	if err == nil {
		json.Unmarshal(integration.Config, &config)
	}

	if config.Owner == "" || config.Repo == "" {
		slog.Warn("github sync skipped: missing owner/repo config", "issue_id", util.UUIDToString(issue.ID))
		return nil
	}

	// Close the GitHub issue via REST API.
	issueNumber := extractGitHubIssueNumber(link.ExternalIdentifier.String)
	if issueNumber == "" {
		return nil
	}

	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/issues/%s", config.Owner, config.Repo, issueNumber)
	body := []byte(`{"state":"closed"}`)
	req, err := http.NewRequestWithContext(ctx, "PATCH", url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("github api: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 500))
		slog.Warn("github close issue failed", "status", resp.StatusCode, "body", string(respBody))
		return fmt.Errorf("github api status %d", resp.StatusCode)
	}

	s.Queries.UpdateExternalIssueLinkSyncedAt(ctx, link.ID)
	slog.Info("synced status to github (closed)", "issue_id", util.UUIDToString(issue.ID), "external", link.ExternalIdentifier.String)
	return nil
}

// GitHubIntegrationConfig holds the config for a GitHub integration.
type GitHubIntegrationConfig struct {
	Owner  string   `json:"owner"`
	Repo   string   `json:"repo"`
	Labels []string `json:"labels,omitempty"`
}

// LinearIntegrationConfig holds the config for a Linear integration.
type LinearIntegrationConfig struct {
	TeamID       string   `json:"team_id,omitempty"`
	ProjectID    string   `json:"project_id,omitempty"`
	ActiveStates []string `json:"active_states,omitempty"`
}

func mapPriority(external string) string {
	switch external {
	case "urgent", "1":
		return "urgent"
	case "high", "2":
		return "high"
	case "medium", "3":
		return "medium"
	case "low", "4":
		return "low"
	default:
		return "medium"
	}
}

// extractGitHubIssueNumber extracts "42" from "owner/repo#42".
func extractGitHubIssueNumber(identifier string) string {
	for i := len(identifier) - 1; i >= 0; i-- {
		if identifier[i] == '#' {
			return identifier[i+1:]
		}
	}
	return ""
}
