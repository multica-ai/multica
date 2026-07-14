package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/issueposition"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

var (
	ErrExternalIdentityConflict       = errors.New("external identity conflict")
	ErrExternalIdentityTargetNotFound = errors.New("external identity target issue not found in this workspace")
	ErrExternalIdentityInvalid        = errors.New("invalid external identity upsert")
)

var (
	externalIdentityNamespaceRE   = regexp.MustCompile(`^[a-z][a-z0-9_.:-]{0,127}$`)
	externalIdentityMetadataKeyRE = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_.-]{0,63}$`)
)

func IsValidExternalIdentityNamespace(namespace string) bool {
	return externalIdentityNamespaceRE.MatchString(namespace)
}

type ExternalIdentityAlias struct {
	Namespace  string
	ExternalID string
}

type IssueExternalIdentityUpsertParams struct {
	WorkspaceID    pgtype.UUID
	Aliases        []ExternalIdentityAlias
	TargetIssueID  pgtype.UUID
	Create         IssueCreateParams
	MetadataPatch  []byte
	CreatorType    string
	CreatorID      pgtype.UUID
	IssueCreateOpt IssueCreateOpts
}

type IssueExternalIdentityUpsertResult struct {
	Issue           db.Issue
	Created         bool
	MetadataChanged bool
}

func (s *IssueService) UpsertExternalIdentity(ctx context.Context, p IssueExternalIdentityUpsertParams) (IssueExternalIdentityUpsertResult, error) {
	aliases, err := normalizeExternalIdentityAliases(p.Aliases)
	if err != nil {
		return IssueExternalIdentityUpsertResult{}, err
	}
	if len(p.MetadataPatch) > 0 {
		if err := validateExternalIdentityMetadataPatch(p.MetadataPatch); err != nil {
			return IssueExternalIdentityUpsertResult{}, err
		}
	}

	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return IssueExternalIdentityUpsertResult{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := s.Queries.WithTx(tx)

	for _, alias := range aliases {
		if err := qtx.LockIssueExternalIdentityKey(ctx, externalIdentityLockKey(p.WorkspaceID, alias)); err != nil {
			return IssueExternalIdentityUpsertResult{}, fmt.Errorf("lock external identity: %w", err)
		}
	}

	mapped := make(map[pgtype.UUID]struct{})
	mappedByAlias := make(map[ExternalIdentityAlias]pgtype.UUID)
	for _, alias := range aliases {
		row, err := qtx.GetIssueExternalIdentityForUpdate(ctx, db.GetIssueExternalIdentityForUpdateParams{
			WorkspaceID: p.WorkspaceID,
			Namespace:   alias.Namespace,
			ExternalID:  alias.ExternalID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				continue
			}
			return IssueExternalIdentityUpsertResult{}, fmt.Errorf("read external identity: %w", err)
		}
		mapped[row.IssueID] = struct{}{}
		mappedByAlias[alias] = row.IssueID
	}
	if len(mapped) > 1 {
		return IssueExternalIdentityUpsertResult{}, ErrExternalIdentityConflict
	}

	var effective db.Issue
	created := false
	for id := range mapped {
		if p.TargetIssueID.Valid && id != p.TargetIssueID {
			return IssueExternalIdentityUpsertResult{}, ErrExternalIdentityConflict
		}
		effective, err = qtx.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: id, WorkspaceID: p.WorkspaceID})
		if err != nil {
			return IssueExternalIdentityUpsertResult{}, fmt.Errorf("load mapped issue: %w", err)
		}
	}
	if !effective.ID.Valid {
		if p.TargetIssueID.Valid {
			effective, err = qtx.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: p.TargetIssueID, WorkspaceID: p.WorkspaceID})
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return IssueExternalIdentityUpsertResult{}, ErrExternalIdentityTargetNotFound
				}
				return IssueExternalIdentityUpsertResult{}, fmt.Errorf("load target issue: %w", err)
			}
		} else {
			createParams := p.Create
			createParams.WorkspaceID = p.WorkspaceID
			createParams.AllowDuplicate = true
			effective, err = s.createIssueRowForExternalIdentity(ctx, qtx, tx, createParams)
			if err != nil {
				return IssueExternalIdentityUpsertResult{}, err
			}
			created = true
		}
	}

	for _, alias := range aliases {
		if _, ok := mappedByAlias[alias]; ok {
			continue
		}
		if err := qtx.InsertIssueExternalIdentity(ctx, db.InsertIssueExternalIdentityParams{
			WorkspaceID: p.WorkspaceID,
			Namespace:   alias.Namespace,
			ExternalID:  alias.ExternalID,
			IssueID:     effective.ID,
		}); err != nil {
			return IssueExternalIdentityUpsertResult{}, fmt.Errorf("insert external identity: %w", err)
		}
		actual, err := qtx.GetIssueExternalIdentityForUpdate(ctx, db.GetIssueExternalIdentityForUpdateParams{
			WorkspaceID: p.WorkspaceID, Namespace: alias.Namespace, ExternalID: alias.ExternalID,
		})
		if err != nil {
			return IssueExternalIdentityUpsertResult{}, fmt.Errorf("verify external identity: %w", err)
		}
		if actual.IssueID != effective.ID {
			return IssueExternalIdentityUpsertResult{}, ErrExternalIdentityConflict
		}
	}

	metadataChanged := len(p.MetadataPatch) > 0
	if metadataChanged {
		effective, err = qtx.MergeIssueMetadataPatch(ctx, db.MergeIssueMetadataPatchParams{
			ID:          effective.ID,
			WorkspaceID: p.WorkspaceID,
			Patch:       p.MetadataPatch,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return IssueExternalIdentityUpsertResult{}, fmt.Errorf("%w: metadata exceeds bounds", ErrExternalIdentityInvalid)
			}
			return IssueExternalIdentityUpsertResult{}, fmt.Errorf("merge metadata: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return IssueExternalIdentityUpsertResult{}, fmt.Errorf("commit: %w", err)
	}

	actorID := p.IssueCreateOpt.ActorID
	if actorID == "" {
		if p.CreatorID.Valid {
			actorID = util.UUIDToString(p.CreatorID)
		} else {
			actorID = util.UUIDToString(effective.CreatorID)
		}
	}
	actorType := p.CreatorType
	if actorType == "" {
		actorType = effective.CreatorType
	}
	if created {
		s.publishIssueCreated(effective, nil, effective.CreatorType, actorID, p.IssueCreateOpt)
		s.captureCreatedAnalytics(effective, effective.CreatorType, actorID, p.IssueCreateOpt)
		s.maybeEnqueueOnAssign(ctx, effective, effective.CreatorType, actorID)
	} else if metadataChanged && s.Bus != nil {
		s.Bus.Publish(events.Event{
			Type:        protocol.EventIssueMetadataChanged,
			WorkspaceID: util.UUIDToString(effective.WorkspaceID),
			ActorType:   actorType,
			ActorID:     actorID,
			Payload: map[string]any{
				"issue_id": util.UUIDToString(effective.ID),
				"metadata": json.RawMessage(effective.Metadata),
			},
		})
	}

	return IssueExternalIdentityUpsertResult{Issue: effective, Created: created, MetadataChanged: metadataChanged}, nil
}

func normalizeExternalIdentityAliases(in []ExternalIdentityAlias) ([]ExternalIdentityAlias, error) {
	if len(in) == 0 {
		return nil, fmt.Errorf("%w: at least one alias is required", ErrExternalIdentityInvalid)
	}
	seen := make(map[ExternalIdentityAlias]struct{}, len(in))
	out := make([]ExternalIdentityAlias, 0, len(in))
	for _, alias := range in {
		alias.Namespace = strings.TrimSpace(alias.Namespace)
		if !externalIdentityNamespaceRE.MatchString(alias.Namespace) {
			return nil, fmt.Errorf("%w: invalid namespace", ErrExternalIdentityInvalid)
		}
		if alias.ExternalID == "" || len([]rune(alias.ExternalID)) > 512 || len(alias.ExternalID) > 1024 {
			return nil, fmt.Errorf("%w: invalid external_id", ErrExternalIdentityInvalid)
		}
		if _, ok := seen[alias]; ok {
			continue
		}
		seen[alias] = struct{}{}
		out = append(out, alias)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Namespace != out[j].Namespace {
			return out[i].Namespace < out[j].Namespace
		}
		return out[i].ExternalID < out[j].ExternalID
	})
	return out, nil
}

func validateExternalIdentityMetadataPatch(raw []byte) error {
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil || parsed == nil {
		return fmt.Errorf("%w: metadata patch must be a JSON object", ErrExternalIdentityInvalid)
	}
	if len(parsed) > 50 {
		return fmt.Errorf("%w: metadata cannot exceed 50 keys", ErrExternalIdentityInvalid)
	}
	for key, value := range parsed {
		if !externalIdentityMetadataKeyRE.MatchString(key) {
			return fmt.Errorf("%w: invalid metadata key", ErrExternalIdentityInvalid)
		}
		switch value.(type) {
		case string, bool, float64:
		default:
			return fmt.Errorf("%w: metadata values must be primitive", ErrExternalIdentityInvalid)
		}
	}
	if len(raw) > 8192 {
		return fmt.Errorf("%w: metadata exceeds the 8KB size limit", ErrExternalIdentityInvalid)
	}
	return nil
}

func externalIdentityLockKey(workspaceID pgtype.UUID, alias ExternalIdentityAlias) string {
	workspace := util.UUIDToString(workspaceID)
	return fmt.Sprintf("%d:%s%d:%s%d:%s", len(workspace), workspace, len(alias.Namespace), alias.Namespace, len(alias.ExternalID), alias.ExternalID)
}

func (s *IssueService) createIssueRowForExternalIdentity(ctx context.Context, qtx *db.Queries, tx pgx.Tx, p IssueCreateParams) (db.Issue, error) {
	if p.Title == "" {
		return db.Issue{}, fmt.Errorf("%w: title is required", ErrExternalIdentityInvalid)
	}
	projectID := p.ProjectID
	if p.ParentIssueID.Valid {
		parent, err := qtx.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{
			ID:          p.ParentIssueID,
			WorkspaceID: p.WorkspaceID,
		})
		if err != nil || !parent.ID.Valid {
			return db.Issue{}, ErrParentIssueNotFound
		}
		if !projectID.Valid {
			projectID = parent.ProjectID
		}
	}
	if projectID.Valid {
		if _, err := qtx.GetProjectInWorkspace(ctx, db.GetProjectInWorkspaceParams{ID: projectID, WorkspaceID: p.WorkspaceID}); err != nil {
			return db.Issue{}, ErrProjectNotFound
		}
	}
	issueNumber, err := qtx.IncrementIssueCounter(ctx, p.WorkspaceID)
	if err != nil {
		return db.Issue{}, fmt.Errorf("increment counter: %w", err)
	}
	newPosition, err := issueposition.NextTopPosition(ctx, tx, p.WorkspaceID, p.Status)
	if err != nil {
		return db.Issue{}, fmt.Errorf("next top position: %w", err)
	}
	if p.OriginType.Valid {
		return qtx.CreateIssueWithOrigin(ctx, db.CreateIssueWithOriginParams{
			WorkspaceID:   p.WorkspaceID,
			Title:         p.Title,
			Description:   p.Description,
			Status:        p.Status,
			Priority:      p.Priority,
			AssigneeType:  p.AssigneeType,
			AssigneeID:    p.AssigneeID,
			CreatorType:   p.CreatorType,
			CreatorID:     p.CreatorID,
			ParentIssueID: p.ParentIssueID,
			Position:      newPosition,
			StartDate:     p.StartDate,
			DueDate:       p.DueDate,
			Number:        issueNumber,
			ProjectID:     projectID,
			OriginType:    p.OriginType,
			OriginID:      p.OriginID,
			Stage:         p.Stage,
		})
	}
	return qtx.CreateIssue(ctx, db.CreateIssueParams{
		WorkspaceID:   p.WorkspaceID,
		Title:         p.Title,
		Description:   p.Description,
		Status:        p.Status,
		Priority:      p.Priority,
		AssigneeType:  p.AssigneeType,
		AssigneeID:    p.AssigneeID,
		CreatorType:   p.CreatorType,
		CreatorID:     p.CreatorID,
		ParentIssueID: p.ParentIssueID,
		Position:      newPosition,
		StartDate:     p.StartDate,
		DueDate:       p.DueDate,
		Number:        issueNumber,
		ProjectID:     projectID,
		Stage:         p.Stage,
	})
}
