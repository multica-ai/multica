package projectplan

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/featureflags"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/featureflag"
)

const supportedPlanKind = "prd"

type TxStarter interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

type Service struct {
	Repository   *Repository
	TxStarter    TxStarter
	FeatureFlags *featureflag.Service
}

func NewService(repository *Repository, txStarter TxStarter, flags *featureflag.Service) *Service {
	return &Service{Repository: repository, TxStarter: txStarter, FeatureFlags: flags}
}

type Actor struct {
	Type string
	ID   pgtype.UUID
}

type CreateManualParams struct {
	WorkspaceID pgtype.UUID
	ProjectID   pgtype.UUID
	Kind        string
	Title       string
	Description string
	Attributes  []byte
	CreatedBy   Actor
}

type CreateFromIssueParams struct {
	WorkspaceID   pgtype.UUID
	ProjectID     pgtype.UUID
	SourceIssueID pgtype.UUID
	Kind          string
	Attributes    []byte
	CreatedBy     Actor
}

type PlanPatch struct {
	Title       *string
	Description *string
	Kind        *string
	Attributes  *json.RawMessage
}

type SupersedeParams struct {
	WorkspaceID pgtype.UUID
	PlanID      pgtype.UUID
	Patch       PlanPatch
	CreatedBy   Actor
}

type CreatePhaseParams struct {
	WorkspaceID pgtype.UUID
	PlanID      pgtype.UUID
	Title       string
	Description string
	Attributes  []byte
	Position    int32
}

type PhasePatch struct {
	Title       *string
	Description *string
	Attributes  *json.RawMessage
	Position    *int32
}

type CreatePartParams struct {
	WorkspaceID        pgtype.UUID
	PlanID             pgtype.UUID
	PhaseID            pgtype.UUID
	Title              string
	Description        string
	AcceptanceCriteria string
	Attributes         []byte
	Position           int32
}

type PartPatch struct {
	Title              *string
	Description        *string
	AcceptanceCriteria *string
	Attributes         *json.RawMessage
	Position           *int32
}

type DeleteImpact struct {
	MembershipRows     int64
	LiveIssuesUnlinked int64
}

func (s *Service) CreateManual(ctx context.Context, params CreateManualParams) (db.ProjectPlan, error) {
	if err := s.requireEnabled(ctx); err != nil {
		return db.ProjectPlan{}, err
	}
	if err := validateActor(params.CreatedBy); err != nil {
		return db.ProjectPlan{}, err
	}
	if err := validateTitle(params.Title); err != nil {
		return db.ProjectPlan{}, err
	}
	if err := validateAttributes(params.Attributes); err != nil {
		return db.ProjectPlan{}, err
	}

	return inTransaction(ctx, s, func(repository *Repository) (db.ProjectPlan, error) {
		if err := prepareCreate(ctx, repository, params.WorkspaceID, params.ProjectID, params.Kind); err != nil {
			return db.ProjectPlan{}, err
		}
		version, err := repository.queries.GetNextProjectPlanVersion(ctx, params.ProjectID)
		if err != nil {
			return db.ProjectPlan{}, domainError(ErrorUnavailable, "allocate project plan version", err)
		}
		plan, err := repository.createPlan(ctx, db.CreateProjectPlanParams{
			WorkspaceID:   params.WorkspaceID,
			ProjectID:     params.ProjectID,
			Version:       version,
			Kind:          params.Kind,
			Origin:        "manual",
			Title:         params.Title,
			Description:   params.Description,
			Attributes:    cloneBytes(params.Attributes),
			CreatedByType: params.CreatedBy.Type,
			CreatedByID:   params.CreatedBy.ID,
		})
		if err != nil {
			return db.ProjectPlan{}, translateWriteError(err)
		}
		return plan, nil
	})
}

func (s *Service) CreateFromIssue(ctx context.Context, params CreateFromIssueParams) (db.ProjectPlan, error) {
	if err := s.requireEnabled(ctx); err != nil {
		return db.ProjectPlan{}, err
	}
	if err := validateActor(params.CreatedBy); err != nil {
		return db.ProjectPlan{}, err
	}
	if err := validateAttributes(params.Attributes); err != nil {
		return db.ProjectPlan{}, err
	}

	return inTransaction(ctx, s, func(repository *Repository) (db.ProjectPlan, error) {
		if err := prepareCreate(ctx, repository, params.WorkspaceID, params.ProjectID, params.Kind); err != nil {
			return db.ProjectPlan{}, err
		}
		source, err := repository.queries.GetProjectPlanSourceIssueForWrite(ctx, db.GetProjectPlanSourceIssueForWriteParams{
			ID: params.SourceIssueID, WorkspaceID: params.WorkspaceID, ProjectID: params.ProjectID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return db.ProjectPlan{}, domainError(ErrorNotFound, "source issue not found in this project", err)
			}
			return db.ProjectPlan{}, domainError(ErrorUnavailable, "load source issue", err)
		}
		description := source.Description.String
		digest := sha256.Sum256([]byte(description))
		version, err := repository.queries.GetNextProjectPlanVersion(ctx, params.ProjectID)
		if err != nil {
			return db.ProjectPlan{}, domainError(ErrorUnavailable, "allocate project plan version", err)
		}
		plan, err := repository.createPlan(ctx, db.CreateProjectPlanParams{
			WorkspaceID:               params.WorkspaceID,
			ProjectID:                 params.ProjectID,
			Version:                   version,
			Kind:                      params.Kind,
			Origin:                    "issue",
			Title:                     source.Title,
			Description:               description,
			Attributes:                cloneBytes(params.Attributes),
			SourceIssueID:             source.ID,
			SourceIssueRevision:       pgtype.Int8{Int64: source.Revision, Valid: true},
			SourceDescriptionSnapshot: pgtype.Text{String: description, Valid: true},
			SourceContentSha256:       pgtype.Text{String: hex.EncodeToString(digest[:]), Valid: true},
			CreatedByType:             params.CreatedBy.Type,
			CreatedByID:               params.CreatedBy.ID,
		})
		if err != nil {
			return db.ProjectPlan{}, translateWriteError(err)
		}
		return plan, nil
	})
}

func (s *Service) UpdatePlan(
	ctx context.Context,
	workspaceID pgtype.UUID,
	planID pgtype.UUID,
	patch PlanPatch,
) (db.ProjectPlan, error) {
	if err := s.requireEnabled(ctx); err != nil {
		return db.ProjectPlan{}, err
	}
	if patch.Title != nil {
		if err := validateTitle(*patch.Title); err != nil {
			return db.ProjectPlan{}, err
		}
	}
	if patch.Attributes != nil {
		if err := validateAttributes(*patch.Attributes); err != nil {
			return db.ProjectPlan{}, err
		}
	}

	return inTransaction(ctx, s, func(repository *Repository) (db.ProjectPlan, error) {
		plan, err := activePlanForWrite(ctx, repository, workspaceID, planID)
		if err != nil {
			return db.ProjectPlan{}, err
		}
		if patch.Kind != nil {
			if err := requireSupportedKind(ctx, repository, *patch.Kind); err != nil {
				return db.ProjectPlan{}, err
			}
			plan, err = repository.queries.UpdateProjectPlanKind(ctx, db.UpdateProjectPlanKindParams{
				ID: planID, WorkspaceID: workspaceID, Kind: *patch.Kind,
			})
			if err != nil {
				return db.ProjectPlan{}, translateWriteError(err)
			}
		}
		if patch.Title != nil {
			plan, err = repository.queries.UpdateProjectPlanTitle(ctx, db.UpdateProjectPlanTitleParams{
				ID: planID, WorkspaceID: workspaceID, Title: *patch.Title,
			})
			if err != nil {
				return db.ProjectPlan{}, translateWriteError(err)
			}
		}
		if patch.Description != nil {
			plan, err = repository.queries.UpdateProjectPlanDescription(ctx, db.UpdateProjectPlanDescriptionParams{
				ID: planID, WorkspaceID: workspaceID, Description: *patch.Description,
			})
			if err != nil {
				return db.ProjectPlan{}, translateWriteError(err)
			}
		}
		if patch.Attributes != nil {
			plan, err = repository.queries.UpdateProjectPlanAttributes(ctx, db.UpdateProjectPlanAttributesParams{
				ID: planID, WorkspaceID: workspaceID, Attributes: cloneBytes(*patch.Attributes),
			})
			if err != nil {
				return db.ProjectPlan{}, translateWriteError(err)
			}
		}
		return plan, nil
	})
}

func (s *Service) Supersede(ctx context.Context, params SupersedeParams) (db.ProjectPlan, error) {
	if err := s.requireEnabled(ctx); err != nil {
		return db.ProjectPlan{}, err
	}
	if err := validateActor(params.CreatedBy); err != nil {
		return db.ProjectPlan{}, err
	}
	if params.Patch.Title != nil {
		if err := validateTitle(*params.Patch.Title); err != nil {
			return db.ProjectPlan{}, err
		}
	}
	if params.Patch.Attributes != nil {
		if err := validateAttributes(*params.Patch.Attributes); err != nil {
			return db.ProjectPlan{}, err
		}
	}

	return inTransaction(ctx, s, func(repository *Repository) (db.ProjectPlan, error) {
		oldPlan, err := lockPlanAndProject(ctx, repository, params.WorkspaceID, params.PlanID)
		if err != nil {
			return db.ProjectPlan{}, err
		}
		if err := requireActive(oldPlan); err != nil {
			return db.ProjectPlan{}, err
		}
		kind := oldPlan.Kind
		if params.Patch.Kind != nil {
			kind = *params.Patch.Kind
		}
		if err := requireSupportedKind(ctx, repository, kind); err != nil {
			return db.ProjectPlan{}, err
		}
		if oldPlan.SourceIssueID.Valid {
			if _, err := repository.queries.GetProjectPlanSourceIssueForWrite(ctx, db.GetProjectPlanSourceIssueForWriteParams{
				ID: oldPlan.SourceIssueID, WorkspaceID: params.WorkspaceID, ProjectID: oldPlan.ProjectID,
			}); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return db.ProjectPlan{}, domainError(ErrorNotFound, "source issue no longer exists in this project", err)
				}
				return db.ProjectPlan{}, domainError(ErrorUnavailable, "validate source issue", err)
			}
		}

		phases, err := repository.queries.ListProjectPlanPhasesForClone(ctx, oldPlan.ID)
		if err != nil {
			return db.ProjectPlan{}, domainError(ErrorUnavailable, "load phases for supersede", err)
		}
		parts, err := repository.queries.ListProjectPlanPartsForClone(ctx, oldPlan.ID)
		if err != nil {
			return db.ProjectPlan{}, domainError(ErrorUnavailable, "load parts for supersede", err)
		}
		links, err := repository.queries.ListProjectPlanPartIssuesForClone(ctx, oldPlan.ID)
		if err != nil {
			return db.ProjectPlan{}, domainError(ErrorUnavailable, "load issue links for supersede", err)
		}
		dependencies, err := repository.queries.ListProjectPlanDependenciesForClone(ctx, oldPlan.ID)
		if err != nil {
			return db.ProjectPlan{}, domainError(ErrorUnavailable, "load dependencies for supersede", err)
		}
		for _, link := range links {
			if !link.IssueID.Valid {
				continue
			}
			if _, err := repository.queries.GetProjectPlanSourceIssueForWrite(ctx, db.GetProjectPlanSourceIssueForWriteParams{
				ID: link.IssueID, WorkspaceID: params.WorkspaceID, ProjectID: oldPlan.ProjectID,
			}); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return db.ProjectPlan{}, domainError(ErrorNotFound, "linked issue no longer exists in this project", err)
				}
				return db.ProjectPlan{}, domainError(ErrorUnavailable, "validate linked issue", err)
			}
		}

		version, err := repository.queries.GetNextProjectPlanVersion(ctx, oldPlan.ProjectID)
		if err != nil {
			return db.ProjectPlan{}, domainError(ErrorUnavailable, "allocate project plan version", err)
		}
		if _, err := repository.queries.SupersedeProjectPlan(ctx, db.SupersedeProjectPlanParams{
			ID: oldPlan.ID, WorkspaceID: params.WorkspaceID,
		}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return db.ProjectPlan{}, domainError(ErrorNotActive, "only the active plan can be superseded", err)
			}
			return db.ProjectPlan{}, translateWriteError(err)
		}

		newPlanParams := db.CreateProjectPlanParams{
			WorkspaceID:               oldPlan.WorkspaceID,
			ProjectID:                 oldPlan.ProjectID,
			Version:                   version,
			Kind:                      kind,
			Origin:                    oldPlan.Origin,
			Title:                     oldPlan.Title,
			Description:               oldPlan.Description,
			Attributes:                cloneBytes(oldPlan.Attributes),
			SourceIssueID:             oldPlan.SourceIssueID,
			SourceIssueRevision:       oldPlan.SourceIssueRevision,
			SourceDescriptionSnapshot: oldPlan.SourceDescriptionSnapshot,
			SourceContentSha256:       oldPlan.SourceContentSha256,
			CreatedByType:             params.CreatedBy.Type,
			CreatedByID:               params.CreatedBy.ID,
		}
		applyPlanPatch(&newPlanParams, params.Patch)
		newPlan, err := repository.createPlan(ctx, newPlanParams)
		if err != nil {
			return db.ProjectPlan{}, translateWriteError(err)
		}
		if err := cloneStructure(ctx, repository, newPlan.ID, phases, parts, links, dependencies); err != nil {
			return db.ProjectPlan{}, err
		}
		return newPlan, nil
	})
}

func (s *Service) AddPhase(ctx context.Context, params CreatePhaseParams) (db.ProjectPlanPhase, error) {
	if err := s.requireEnabled(ctx); err != nil {
		return db.ProjectPlanPhase{}, err
	}
	if err := validateTitle(params.Title); err != nil {
		return db.ProjectPlanPhase{}, err
	}
	if params.Position < 0 {
		return db.ProjectPlanPhase{}, domainError(ErrorInvalid, "phase position must be non-negative", nil)
	}
	if err := validateAttributes(params.Attributes); err != nil {
		return db.ProjectPlanPhase{}, err
	}
	return inTransaction(ctx, s, func(repository *Repository) (db.ProjectPlanPhase, error) {
		if _, err := activePlanForWrite(ctx, repository, params.WorkspaceID, params.PlanID); err != nil {
			return db.ProjectPlanPhase{}, err
		}
		phase, err := repository.createPhase(ctx, db.CreateProjectPlanPhaseParams{
			ProjectPlanID: params.PlanID, Title: params.Title, Description: params.Description,
			Attributes: cloneBytes(params.Attributes), Position: params.Position,
		})
		if err != nil {
			return db.ProjectPlanPhase{}, translateWriteError(err)
		}
		return phase, nil
	})
}

func (s *Service) UpdatePhase(
	ctx context.Context,
	workspaceID pgtype.UUID,
	planID pgtype.UUID,
	phaseID pgtype.UUID,
	patch PhasePatch,
) (db.ProjectPlanPhase, error) {
	if err := s.requireEnabled(ctx); err != nil {
		return db.ProjectPlanPhase{}, err
	}
	if patch.Title != nil {
		if err := validateTitle(*patch.Title); err != nil {
			return db.ProjectPlanPhase{}, err
		}
	}
	if patch.Position != nil && *patch.Position < 0 {
		return db.ProjectPlanPhase{}, domainError(ErrorInvalid, "phase position must be non-negative", nil)
	}
	if patch.Attributes != nil {
		if err := validateAttributes(*patch.Attributes); err != nil {
			return db.ProjectPlanPhase{}, err
		}
	}
	return inTransaction(ctx, s, func(repository *Repository) (db.ProjectPlanPhase, error) {
		if _, err := activePlanForWrite(ctx, repository, workspaceID, planID); err != nil {
			return db.ProjectPlanPhase{}, err
		}
		phase, err := repository.queries.GetProjectPlanPhaseForWrite(ctx, db.GetProjectPlanPhaseForWriteParams{
			ID: phaseID, ProjectPlanID: planID, WorkspaceID: workspaceID,
		})
		if err != nil {
			return db.ProjectPlanPhase{}, notFound("phase", err)
		}
		if patch.Title != nil {
			phase, err = repository.queries.UpdateProjectPlanPhaseTitle(ctx, db.UpdateProjectPlanPhaseTitleParams{
				ID: phaseID, ProjectPlanID: planID, Title: *patch.Title,
			})
			if err != nil {
				return db.ProjectPlanPhase{}, translateWriteError(err)
			}
		}
		if patch.Description != nil {
			phase, err = repository.queries.UpdateProjectPlanPhaseDescription(ctx, db.UpdateProjectPlanPhaseDescriptionParams{
				ID: phaseID, ProjectPlanID: planID, Description: *patch.Description,
			})
			if err != nil {
				return db.ProjectPlanPhase{}, translateWriteError(err)
			}
		}
		if patch.Attributes != nil {
			phase, err = repository.queries.UpdateProjectPlanPhaseAttributes(ctx, db.UpdateProjectPlanPhaseAttributesParams{
				ID: phaseID, ProjectPlanID: planID, Attributes: cloneBytes(*patch.Attributes),
			})
			if err != nil {
				return db.ProjectPlanPhase{}, translateWriteError(err)
			}
		}
		if patch.Position != nil {
			phase, err = repository.queries.UpdateProjectPlanPhasePosition(ctx, db.UpdateProjectPlanPhasePositionParams{
				ID: phaseID, ProjectPlanID: planID, Position: *patch.Position,
			})
			if err != nil {
				return db.ProjectPlanPhase{}, translateWriteError(err)
			}
		}
		return phase, nil
	})
}

func (s *Service) ReorderPhases(
	ctx context.Context,
	workspaceID pgtype.UUID,
	planID pgtype.UUID,
	orderedIDs []pgtype.UUID,
) error {
	if err := s.requireEnabled(ctx); err != nil {
		return err
	}
	_, err := inTransaction(ctx, s, func(repository *Repository) (struct{}, error) {
		if _, err := activePlanForWrite(ctx, repository, workspaceID, planID); err != nil {
			return struct{}{}, err
		}
		phases, err := repository.queries.ListProjectPlanPhasesForClone(ctx, planID)
		if err != nil {
			return struct{}{}, domainError(ErrorUnavailable, "list phases for reorder", err)
		}
		current := make([]pgtype.UUID, len(phases))
		positions := make([]int32, len(phases))
		for i, phase := range phases {
			current[i], positions[i] = phase.ID, phase.Position
		}
		if err := validateExactOrder(current, orderedIDs); err != nil {
			return struct{}{}, err
		}
		if err := reorderPositions(ctx, positions, orderedIDs, func(id pgtype.UUID, position int32) error {
			_, err := repository.queries.UpdateProjectPlanPhasePosition(ctx, db.UpdateProjectPlanPhasePositionParams{
				ID: id, ProjectPlanID: planID, Position: position,
			})
			return err
		}); err != nil {
			return struct{}{}, translateWriteError(err)
		}
		return struct{}{}, nil
	})
	return err
}

func (s *Service) DeletePhase(ctx context.Context, workspaceID, planID, phaseID pgtype.UUID) error {
	if err := s.requireEnabled(ctx); err != nil {
		return err
	}
	_, err := inTransaction(ctx, s, func(repository *Repository) (struct{}, error) {
		if _, err := activePlanForWrite(ctx, repository, workspaceID, planID); err != nil {
			return struct{}{}, err
		}
		if _, err := repository.queries.GetProjectPlanPhaseForWrite(ctx, db.GetProjectPlanPhaseForWriteParams{
			ID: phaseID, ProjectPlanID: planID, WorkspaceID: workspaceID,
		}); err != nil {
			return struct{}{}, notFound("phase", err)
		}
		if err := repository.deletePhaseStructure(ctx, planID, phaseID); err != nil {
			return struct{}{}, domainError(ErrorUnavailable, "delete phase structure", err)
		}
		return struct{}{}, nil
	})
	return err
}

func (s *Service) AddPart(ctx context.Context, params CreatePartParams) (db.ProjectPlanPart, error) {
	if err := s.requireEnabled(ctx); err != nil {
		return db.ProjectPlanPart{}, err
	}
	if err := validateTitle(params.Title); err != nil {
		return db.ProjectPlanPart{}, err
	}
	if params.Position < 0 {
		return db.ProjectPlanPart{}, domainError(ErrorInvalid, "part position must be non-negative", nil)
	}
	if err := validateAttributes(params.Attributes); err != nil {
		return db.ProjectPlanPart{}, err
	}
	return inTransaction(ctx, s, func(repository *Repository) (db.ProjectPlanPart, error) {
		if _, err := activePlanForWrite(ctx, repository, params.WorkspaceID, params.PlanID); err != nil {
			return db.ProjectPlanPart{}, err
		}
		if _, err := repository.queries.GetProjectPlanPhaseForWrite(ctx, db.GetProjectPlanPhaseForWriteParams{
			ID: params.PhaseID, ProjectPlanID: params.PlanID, WorkspaceID: params.WorkspaceID,
		}); err != nil {
			return db.ProjectPlanPart{}, notFound("phase", err)
		}
		part, err := repository.createPart(ctx, db.CreateProjectPlanPartParams{
			ProjectPlanID: params.PlanID, ProjectPlanPhaseID: params.PhaseID,
			Title: params.Title, Description: params.Description,
			AcceptanceCriteria: params.AcceptanceCriteria,
			Attributes:         cloneBytes(params.Attributes), Position: params.Position,
		})
		if err != nil {
			return db.ProjectPlanPart{}, translateWriteError(err)
		}
		return part, nil
	})
}

func (s *Service) UpdatePart(
	ctx context.Context,
	workspaceID pgtype.UUID,
	planID pgtype.UUID,
	partID pgtype.UUID,
	patch PartPatch,
) (db.ProjectPlanPart, error) {
	if err := s.requireEnabled(ctx); err != nil {
		return db.ProjectPlanPart{}, err
	}
	if patch.Title != nil {
		if err := validateTitle(*patch.Title); err != nil {
			return db.ProjectPlanPart{}, err
		}
	}
	if patch.Position != nil && *patch.Position < 0 {
		return db.ProjectPlanPart{}, domainError(ErrorInvalid, "part position must be non-negative", nil)
	}
	if patch.Attributes != nil {
		if err := validateAttributes(*patch.Attributes); err != nil {
			return db.ProjectPlanPart{}, err
		}
	}
	return inTransaction(ctx, s, func(repository *Repository) (db.ProjectPlanPart, error) {
		if _, err := activePlanForWrite(ctx, repository, workspaceID, planID); err != nil {
			return db.ProjectPlanPart{}, err
		}
		part, err := repository.queries.GetProjectPlanPartForWrite(ctx, db.GetProjectPlanPartForWriteParams{
			ID: partID, ProjectPlanID: planID, WorkspaceID: workspaceID,
		})
		if err != nil {
			return db.ProjectPlanPart{}, notFound("part", err)
		}
		if patch.Title != nil {
			part, err = repository.queries.UpdateProjectPlanPartTitle(ctx, db.UpdateProjectPlanPartTitleParams{
				ID: partID, ProjectPlanID: planID, Title: *patch.Title,
			})
			if err != nil {
				return db.ProjectPlanPart{}, translateWriteError(err)
			}
		}
		if patch.Description != nil {
			part, err = repository.queries.UpdateProjectPlanPartDescription(ctx, db.UpdateProjectPlanPartDescriptionParams{
				ID: partID, ProjectPlanID: planID, Description: *patch.Description,
			})
			if err != nil {
				return db.ProjectPlanPart{}, translateWriteError(err)
			}
		}
		if patch.AcceptanceCriteria != nil {
			part, err = repository.queries.UpdateProjectPlanPartAcceptanceCriteria(ctx, db.UpdateProjectPlanPartAcceptanceCriteriaParams{
				ID: partID, ProjectPlanID: planID, AcceptanceCriteria: *patch.AcceptanceCriteria,
			})
			if err != nil {
				return db.ProjectPlanPart{}, translateWriteError(err)
			}
		}
		if patch.Attributes != nil {
			part, err = repository.queries.UpdateProjectPlanPartAttributes(ctx, db.UpdateProjectPlanPartAttributesParams{
				ID: partID, ProjectPlanID: planID, Attributes: cloneBytes(*patch.Attributes),
			})
			if err != nil {
				return db.ProjectPlanPart{}, translateWriteError(err)
			}
		}
		if patch.Position != nil {
			part, err = repository.queries.UpdateProjectPlanPartPosition(ctx, db.UpdateProjectPlanPartPositionParams{
				ID: partID, ProjectPlanID: planID, Position: *patch.Position,
			})
			if err != nil {
				return db.ProjectPlanPart{}, translateWriteError(err)
			}
		}
		return part, nil
	})
}

func (s *Service) ReorderParts(
	ctx context.Context,
	workspaceID pgtype.UUID,
	planID pgtype.UUID,
	phaseID pgtype.UUID,
	orderedIDs []pgtype.UUID,
) error {
	if err := s.requireEnabled(ctx); err != nil {
		return err
	}
	_, err := inTransaction(ctx, s, func(repository *Repository) (struct{}, error) {
		if _, err := activePlanForWrite(ctx, repository, workspaceID, planID); err != nil {
			return struct{}{}, err
		}
		if _, err := repository.queries.GetProjectPlanPhaseForWrite(ctx, db.GetProjectPlanPhaseForWriteParams{
			ID: phaseID, ProjectPlanID: planID, WorkspaceID: workspaceID,
		}); err != nil {
			return struct{}{}, notFound("phase", err)
		}
		allParts, err := repository.queries.ListProjectPlanPartsForClone(ctx, planID)
		if err != nil {
			return struct{}{}, domainError(ErrorUnavailable, "list parts for reorder", err)
		}
		current := make([]pgtype.UUID, 0, len(allParts))
		positions := make([]int32, 0, len(allParts))
		for _, part := range allParts {
			if part.ProjectPlanPhaseID == phaseID {
				current = append(current, part.ID)
				positions = append(positions, part.Position)
			}
		}
		if err := validateExactOrder(current, orderedIDs); err != nil {
			return struct{}{}, err
		}
		if err := reorderPositions(ctx, positions, orderedIDs, func(id pgtype.UUID, position int32) error {
			_, err := repository.queries.UpdateProjectPlanPartPosition(ctx, db.UpdateProjectPlanPartPositionParams{
				ID: id, ProjectPlanID: planID, Position: position,
			})
			return err
		}); err != nil {
			return struct{}{}, translateWriteError(err)
		}
		return struct{}{}, nil
	})
	return err
}

func (s *Service) DeletePart(ctx context.Context, workspaceID, planID, partID pgtype.UUID) error {
	if err := s.requireEnabled(ctx); err != nil {
		return err
	}
	_, err := inTransaction(ctx, s, func(repository *Repository) (struct{}, error) {
		if _, err := activePlanForWrite(ctx, repository, workspaceID, planID); err != nil {
			return struct{}{}, err
		}
		if _, err := repository.queries.GetProjectPlanPartForWrite(ctx, db.GetProjectPlanPartForWriteParams{
			ID: partID, ProjectPlanID: planID, WorkspaceID: workspaceID,
		}); err != nil {
			return struct{}{}, notFound("part", err)
		}
		if err := repository.deletePartStructure(ctx, planID, partID); err != nil {
			return struct{}{}, domainError(ErrorUnavailable, "delete part structure", err)
		}
		return struct{}{}, nil
	})
	return err
}

func (s *Service) LinkIssue(
	ctx context.Context,
	workspaceID pgtype.UUID,
	planID pgtype.UUID,
	partID pgtype.UUID,
	issueID pgtype.UUID,
) (db.ProjectPlanPartIssue, error) {
	if err := s.requireEnabled(ctx); err != nil {
		return db.ProjectPlanPartIssue{}, err
	}
	return inTransaction(ctx, s, func(repository *Repository) (db.ProjectPlanPartIssue, error) {
		plan, err := activePlanForWrite(ctx, repository, workspaceID, planID)
		if err != nil {
			return db.ProjectPlanPartIssue{}, err
		}
		if _, err := repository.queries.GetProjectPlanPartForWrite(ctx, db.GetProjectPlanPartForWriteParams{
			ID: partID, ProjectPlanID: planID, WorkspaceID: workspaceID,
		}); err != nil {
			return db.ProjectPlanPartIssue{}, notFound("part", err)
		}
		issue, err := repository.queries.GetProjectPlanSourceIssueForWrite(ctx, db.GetProjectPlanSourceIssueForWriteParams{
			ID: issueID, WorkspaceID: workspaceID, ProjectID: plan.ProjectID,
		})
		if err != nil {
			return db.ProjectPlanPartIssue{}, notFound("issue", err)
		}
		link, err := repository.createPartIssue(ctx, db.CreateProjectPlanPartIssueParams{
			ProjectPlanID: planID, ProjectPlanPartID: partID, IssueID: issue.ID,
			IssueNumberSnapshot: issue.Number, IssueTitleSnapshot: issue.Title,
		})
		if err != nil {
			return db.ProjectPlanPartIssue{}, translateWriteError(err)
		}
		return link, nil
	})
}

func (s *Service) UnlinkIssue(
	ctx context.Context,
	workspaceID pgtype.UUID,
	planID pgtype.UUID,
	partID pgtype.UUID,
	issueID pgtype.UUID,
) error {
	if err := s.requireEnabled(ctx); err != nil {
		return err
	}
	_, err := inTransaction(ctx, s, func(repository *Repository) (struct{}, error) {
		plan, err := activePlanForWrite(ctx, repository, workspaceID, planID)
		if err != nil {
			return struct{}{}, err
		}
		if _, err := repository.queries.GetProjectPlanPartForWrite(ctx, db.GetProjectPlanPartForWriteParams{
			ID: partID, ProjectPlanID: planID, WorkspaceID: workspaceID,
		}); err != nil {
			return struct{}{}, notFound("part", err)
		}
		if _, err := repository.queries.GetProjectPlanSourceIssueForWrite(ctx, db.GetProjectPlanSourceIssueForWriteParams{
			ID: issueID, WorkspaceID: workspaceID, ProjectID: plan.ProjectID,
		}); err != nil {
			return struct{}{}, notFound("issue", err)
		}
		if _, err := repository.queries.DeleteProjectPlanPartIssue(ctx, db.DeleteProjectPlanPartIssueParams{
			ProjectPlanID: planID, ProjectPlanPartID: partID, IssueID: issueID,
		}); err != nil {
			return struct{}{}, notFound("issue link", err)
		}
		return struct{}{}, nil
	})
	return err
}

func (s *Service) DeleteImpact(
	ctx context.Context,
	workspaceID pgtype.UUID,
	planID pgtype.UUID,
) (DeleteImpact, error) {
	if err := s.requireEnabled(ctx); err != nil {
		return DeleteImpact{}, err
	}
	return inTransaction(ctx, s, func(repository *Repository) (DeleteImpact, error) {
		if _, err := repository.getPlan(ctx, workspaceID, planID); err != nil {
			return DeleteImpact{}, notFound("plan", err)
		}
		impact, err := repository.queries.CountProjectPlanDeleteImpact(ctx, planID)
		if err != nil {
			return DeleteImpact{}, domainError(ErrorUnavailable, "count plan delete impact", err)
		}
		return DeleteImpact{
			MembershipRows: impact.MembershipRows, LiveIssuesUnlinked: impact.LiveIssuesUnlinked,
		}, nil
	})
}

func (s *Service) DeletePlan(
	ctx context.Context,
	workspaceID pgtype.UUID,
	planID pgtype.UUID,
) (DeleteImpact, error) {
	if err := s.requireEnabled(ctx); err != nil {
		return DeleteImpact{}, err
	}
	return inTransaction(ctx, s, func(repository *Repository) (DeleteImpact, error) {
		if _, err := lockPlanAndProject(ctx, repository, workspaceID, planID); err != nil {
			return DeleteImpact{}, err
		}
		impact, err := repository.queries.CountProjectPlanDeleteImpact(ctx, planID)
		if err != nil {
			return DeleteImpact{}, domainError(ErrorUnavailable, "count plan delete impact", err)
		}
		if err := repository.deletePlanStructure(ctx, workspaceID, planID); err != nil {
			return DeleteImpact{}, domainError(ErrorUnavailable, "delete plan structure", err)
		}
		return DeleteImpact{
			MembershipRows: impact.MembershipRows, LiveIssuesUnlinked: impact.LiveIssuesUnlinked,
		}, nil
	})
}

func (s *Service) requireEnabled(ctx context.Context) error {
	if featureflags.ProjectPlansEnabled(ctx, s.FeatureFlags) {
		return nil
	}
	return domainError(ErrorDisabled, "project plans are disabled", nil)
}

func prepareCreate(
	ctx context.Context,
	repository *Repository,
	workspaceID pgtype.UUID,
	projectID pgtype.UUID,
	kind string,
) error {
	if err := repository.lockWorkspaceAndProject(ctx, workspaceID, projectID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domainError(ErrorNotFound, "workspace or project not found", err)
		}
		return domainError(ErrorUnavailable, "lock workspace and project", err)
	}
	if err := requireSupportedKind(ctx, repository, kind); err != nil {
		return err
	}
	if _, err := repository.getActivePlan(ctx, workspaceID, projectID); err == nil {
		return domainError(ErrorActivePlanExists, "this project already has an active plan", nil)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return domainError(ErrorUnavailable, "check active project plan", err)
	}
	return nil
}

func requireSupportedKind(ctx context.Context, repository *Repository, kind string) error {
	if _, err := repository.queries.GetProjectPlanKindForWrite(ctx, kind); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domainError(ErrorNotFound, "project plan kind does not exist", err)
		}
		return domainError(ErrorUnavailable, "validate project plan kind", err)
	}
	if kind != supportedPlanKind {
		return domainError(ErrorInvalid, "only prd plans are supported in this release", nil)
	}
	return nil
}

func activePlanForWrite(
	ctx context.Context,
	repository *Repository,
	workspaceID pgtype.UUID,
	planID pgtype.UUID,
) (db.ProjectPlan, error) {
	plan, err := repository.getPlan(ctx, workspaceID, planID)
	if err != nil {
		return db.ProjectPlan{}, notFound("plan", err)
	}
	if err := requireActive(plan); err != nil {
		return db.ProjectPlan{}, err
	}
	return plan, nil
}

func lockPlanAndProject(
	ctx context.Context,
	repository *Repository,
	workspaceID pgtype.UUID,
	planID pgtype.UUID,
) (db.ProjectPlan, error) {
	plan, err := repository.queries.GetProjectPlan(ctx, db.GetProjectPlanParams{
		ID: planID, WorkspaceID: workspaceID,
	})
	if err != nil {
		return db.ProjectPlan{}, notFound("plan", err)
	}
	if err := repository.lockWorkspaceAndProject(ctx, workspaceID, plan.ProjectID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.ProjectPlan{}, domainError(ErrorNotFound, "workspace or project not found", err)
		}
		return db.ProjectPlan{}, domainError(ErrorUnavailable, "lock workspace and project", err)
	}
	plan, err = repository.getPlan(ctx, workspaceID, planID)
	if err != nil {
		return db.ProjectPlan{}, notFound("plan", err)
	}
	return plan, nil
}

func requireActive(plan db.ProjectPlan) error {
	if plan.SupersededAt.Valid {
		return domainError(ErrorNotActive, "superseded plans are immutable", nil)
	}
	return nil
}

func validateActor(actor Actor) error {
	if actor.Type != "member" && actor.Type != "agent" {
		return domainError(ErrorInvalid, "creator type must be member or agent", nil)
	}
	if !actor.ID.Valid {
		return domainError(ErrorInvalid, "creator id is required", nil)
	}
	return nil
}

func validateTitle(title string) error {
	if strings.TrimSpace(title) == "" {
		return domainError(ErrorInvalid, "title is required", nil)
	}
	return nil
}

func validateAttributes(attributes []byte) error {
	if attributes == nil {
		return nil
	}
	var object map[string]any
	if err := json.Unmarshal(attributes, &object); err != nil || object == nil {
		return domainError(ErrorInvalid, "attributes must be a JSON object or null", err)
	}
	return nil
}

func validateExactOrder(current, requested []pgtype.UUID) error {
	if len(current) != len(requested) {
		return domainError(ErrorInvalid, "reorder must name every item exactly once", nil)
	}
	want := make(map[pgtype.UUID]struct{}, len(current))
	for _, id := range current {
		want[id] = struct{}{}
	}
	for _, id := range requested {
		if _, exists := want[id]; !exists {
			return domainError(ErrorInvalid, "reorder contains an unknown or duplicate item", nil)
		}
		delete(want, id)
	}
	if len(want) != 0 {
		return domainError(ErrorInvalid, "reorder must name every item exactly once", nil)
	}
	return nil
}

func reorderPositions(
	ctx context.Context,
	currentPositions []int32,
	orderedIDs []pgtype.UUID,
	update func(pgtype.UUID, int32) error,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var maxPosition int32 = -1
	for _, position := range currentPositions {
		if position > maxPosition {
			maxPosition = position
		}
	}
	if int64(maxPosition)+int64(len(orderedIDs)) >= math.MaxInt32 {
		return domainError(ErrorInvalid, "positions are too large to reorder safely", nil)
	}
	temporaryStart := maxPosition + 1
	for index, id := range orderedIDs {
		if err := update(id, temporaryStart+int32(index)); err != nil {
			return err
		}
	}
	for index, id := range orderedIDs {
		if err := update(id, int32(index)); err != nil {
			return err
		}
	}
	return nil
}

func applyPlanPatch(params *db.CreateProjectPlanParams, patch PlanPatch) {
	if patch.Title != nil {
		params.Title = *patch.Title
	}
	if patch.Description != nil {
		params.Description = *patch.Description
	}
	if patch.Kind != nil {
		params.Kind = *patch.Kind
	}
	if patch.Attributes != nil {
		params.Attributes = cloneBytes(*patch.Attributes)
	}
}

func cloneStructure(
	ctx context.Context,
	repository *Repository,
	newPlanID pgtype.UUID,
	phases []db.ProjectPlanPhase,
	parts []db.ProjectPlanPart,
	links []db.ProjectPlanPartIssue,
	dependencies []db.ProjectPlanDependency,
) error {
	phaseIDs := make(map[pgtype.UUID]pgtype.UUID, len(phases))
	for _, phase := range phases {
		cloned, err := repository.createPhase(ctx, db.CreateProjectPlanPhaseParams{
			ProjectPlanID: newPlanID, Title: phase.Title, Description: phase.Description,
			Attributes: cloneBytes(phase.Attributes), Position: phase.Position,
		})
		if err != nil {
			return translateWriteError(err)
		}
		phaseIDs[phase.ID] = cloned.ID
	}

	partIDs := make(map[pgtype.UUID]pgtype.UUID, len(parts))
	for _, part := range parts {
		phaseID, ok := phaseIDs[part.ProjectPlanPhaseID]
		if !ok {
			return domainError(ErrorUnavailable, "part references a phase outside its plan", nil)
		}
		cloned, err := repository.createPart(ctx, db.CreateProjectPlanPartParams{
			ProjectPlanID: newPlanID, ProjectPlanPhaseID: phaseID,
			Title: part.Title, Description: part.Description,
			AcceptanceCriteria: part.AcceptanceCriteria,
			Attributes:         cloneBytes(part.Attributes), Position: part.Position,
		})
		if err != nil {
			return translateWriteError(err)
		}
		partIDs[part.ID] = cloned.ID
	}

	for _, link := range links {
		partID, ok := partIDs[link.ProjectPlanPartID]
		if !ok {
			return domainError(ErrorUnavailable, "issue link references a part outside its plan", nil)
		}
		if _, err := repository.createPartIssue(ctx, db.CreateProjectPlanPartIssueParams{
			ProjectPlanID: newPlanID, ProjectPlanPartID: partID, IssueID: link.IssueID,
			IssueNumberSnapshot: link.IssueNumberSnapshot, IssueTitleSnapshot: link.IssueTitleSnapshot,
		}); err != nil {
			return translateWriteError(err)
		}
	}

	for _, dependency := range dependencies {
		blockedPhaseID, err := remapNullableID(dependency.BlockedPhaseID, phaseIDs)
		if err != nil {
			return err
		}
		blockedPartID, err := remapNullableID(dependency.BlockedPartID, partIDs)
		if err != nil {
			return err
		}
		blockingPhaseID, err := remapNullableID(dependency.BlockingPhaseID, phaseIDs)
		if err != nil {
			return err
		}
		blockingPartID, err := remapNullableID(dependency.BlockingPartID, partIDs)
		if err != nil {
			return err
		}
		if _, err := repository.queries.CreateProjectPlanDependency(ctx, db.CreateProjectPlanDependencyParams{
			ProjectPlanID: newPlanID, BlockedPhaseID: blockedPhaseID, BlockedPartID: blockedPartID,
			BlockingPhaseID: blockingPhaseID, BlockingPartID: blockingPartID,
		}); err != nil {
			return translateWriteError(err)
		}
	}
	return nil
}

func remapNullableID(id pgtype.UUID, mapping map[pgtype.UUID]pgtype.UUID) (pgtype.UUID, error) {
	if !id.Valid {
		return pgtype.UUID{}, nil
	}
	mapped, ok := mapping[id]
	if !ok {
		return pgtype.UUID{}, domainError(ErrorUnavailable, "dependency references a node outside its plan", nil)
	}
	return mapped, nil
}

func notFound(resource string, err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return domainError(ErrorNotFound, resource+" not found", err)
	}
	return domainError(ErrorUnavailable, "load "+resource, err)
}

func cloneBytes(value []byte) []byte {
	return append([]byte(nil), value...)
}

func inTransaction[T any](ctx context.Context, service *Service, fn func(*Repository) (T, error)) (T, error) {
	var zero T
	if service.Repository == nil || service.TxStarter == nil {
		return zero, domainError(ErrorUnavailable, "project plan service is not configured", nil)
	}
	tx, err := service.TxStarter.Begin(ctx)
	if err != nil {
		return zero, domainError(ErrorUnavailable, "begin project plan transaction", err)
	}
	defer tx.Rollback(ctx)

	result, err := fn(service.Repository.withTx(tx))
	if err != nil {
		return zero, err
	}
	if err := tx.Commit(ctx); err != nil {
		return zero, domainError(ErrorUnavailable, "commit project plan transaction", err)
	}
	return result, nil
}
