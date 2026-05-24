package main

// CEREBRO-PATCH(router-capability-register): FIR-2129 wiring adapter for the
// capability register API.
//
// Bridges capabilityregistry.Service to handler.CapabilityRegisterService so
// the handler package never imports the cerebro-side registry types. Input
// validation lives here too, mapping to the handler's sentinel errors so the
// HTTP layer can return 400 vs 500 without knowing the SQL details.

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/cerebro/capabilityregistry"
	"github.com/multica-ai/multica/server/internal/handler"
)

// capabilityRegisterAdapter implements handler.CapabilityRegisterService.
type capabilityRegisterAdapter struct {
	svc *capabilityregistry.Service
}

func newCapabilityRegisterAdapter(svc *capabilityregistry.Service) *capabilityRegisterAdapter {
	return &capabilityRegisterAdapter{svc: svc}
}

func (a *capabilityRegisterAdapter) Report(
	ctx context.Context,
	workspaceID pgtype.UUID,
	reporter handler.CapabilitySubject,
	caps []handler.CapabilityReportInput,
) ([]handler.CapabilityView, error) {
	inputs := make([]capabilityregistry.ReportInput, 0, len(caps))
	for _, c := range caps {
		inputs = append(inputs, capabilityregistry.ReportInput{
			Key:         c.Key,
			Title:       c.Title,
			Category:    c.Category,
			Description: c.Description,
			Source:      c.Source,
			Metadata:    c.Metadata,
			Owners:      subjectsToRegistry(c.Owners),
			Users:       subjectsToRegistry(c.Users),
		})
	}
	views, err := a.svc.Report(ctx, workspaceID, subjectToRegistry(reporter), inputs)
	if err != nil {
		return nil, mapRegistryErr(err)
	}
	return viewsToHandler(views), nil
}

func (a *capabilityRegisterAdapter) List(
	ctx context.Context,
	workspaceID pgtype.UUID,
	subject *handler.CapabilitySubject,
	keys []string,
) ([]handler.CapabilityView, error) {
	var sub *capabilityregistry.Subject
	if subject != nil {
		s := subjectToRegistry(*subject)
		sub = &s
	}
	views, err := a.svc.List(ctx, workspaceID, sub, keys)
	if err != nil {
		return nil, mapRegistryErr(err)
	}
	return viewsToHandler(views), nil
}

func mapRegistryErr(err error) error {
	switch err {
	case capabilityregistry.ErrInvalidSubject:
		return handler.ErrCapabilityInvalidSubjectForAdapter()
	case capabilityregistry.ErrInvalidReport:
		return handler.ErrCapabilityInvalidReportForAdapter()
	default:
		return err
	}
}

func subjectToRegistry(s handler.CapabilitySubject) capabilityregistry.Subject {
	return capabilityregistry.Subject{
		Type:        s.Type,
		ID:          s.ID,
		DisplayName: s.DisplayName,
		Metadata:    s.Metadata,
	}
}

func subjectsToRegistry(in []handler.CapabilitySubject) []capabilityregistry.Subject {
	if len(in) == 0 {
		return nil
	}
	out := make([]capabilityregistry.Subject, 0, len(in))
	for _, s := range in {
		out = append(out, subjectToRegistry(s))
	}
	return out
}

func subjectToHandler(s capabilityregistry.Subject) handler.CapabilitySubject {
	return handler.CapabilitySubject{
		Type:        s.Type,
		ID:          s.ID,
		DisplayName: s.DisplayName,
		Metadata:    s.Metadata,
	}
}

func subjectsToHandler(in []capabilityregistry.Subject) []handler.CapabilitySubject {
	out := make([]handler.CapabilitySubject, 0, len(in))
	for _, s := range in {
		out = append(out, subjectToHandler(s))
	}
	return out
}

func viewsToHandler(in []capabilityregistry.View) []handler.CapabilityView {
	out := make([]handler.CapabilityView, 0, len(in))
	for _, v := range in {
		out = append(out, handler.CapabilityView{
			ID:             v.ID,
			WorkspaceID:    v.WorkspaceID,
			Key:            v.Key,
			Title:          v.Title,
			Category:       v.Category,
			Description:    v.Description,
			Source:         v.Source,
			Metadata:       v.Metadata,
			Owners:         subjectsToHandler(v.Owners),
			Users:          subjectsToHandler(v.Users),
			Reporters:      subjectsToHandler(v.Reporters),
			FirstSeenAt:    v.FirstSeenAt.UTC().Format("2006-01-02T15:04:05.000Z"),
			LastReportedAt: v.LastReportedAt.UTC().Format("2006-01-02T15:04:05.000Z"),
			UpdatedAt:      v.UpdatedAt.UTC().Format("2006-01-02T15:04:05.000Z"),
		})
	}
	return out
}
