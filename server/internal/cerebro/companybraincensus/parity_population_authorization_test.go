package companybraincensus

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestOwnerAdminParityPopulationAuthorizationGateConsumesExactApprovalOnce(t *testing.T) {
	request := validParityPopulationRequest(t)
	queryer := &parityApprovalQueryerStub{
		rows: []pgx.Row{
			parityApprovalRowStub{approvedBy: "44444444-4444-4444-8444-444444444444"},
			parityApprovalRowStub{err: pgx.ErrNoRows},
		},
	}
	gate := NewOwnerAdminParityPopulationAuthorizationGate(queryer)

	if err := gate.AuthorizeOnce(context.Background(), request); err != nil {
		t.Fatalf("authorize exact parity population: %v", err)
	}
	err := gate.AuthorizeOnce(context.Background(), request)
	if err == nil || !strings.Contains(err.Error(), "live, unused, single-use") {
		t.Fatalf("replayed authorization error = %v, want fail-closed denial", err)
	}
	if queryer.calls != 2 {
		t.Fatalf("authorization query calls = %d, want 2", queryer.calls)
	}

	wantArgs := []any{
		request.AuthorizationID,
		request.WorkspaceID,
		ParityPopulationApprovalCapability,
		ParityPopulationApprovalResource(request),
		ParityPopulationApprovalIssueID,
		ParityPopulationApprovalBoundary,
		request.FrozenCensusSHA256,
		request.CensusVersion,
		request.CompanyBrainConnectionID,
		request.ExpectedEligibleAgentCount,
		request.ExpectedTargetPermissionsSHA256,
	}
	if !reflect.DeepEqual(queryer.lastArgs, wantArgs) {
		t.Fatalf("authorization args = %#v, want %#v", queryer.lastArgs, wantArgs)
	}
	for _, clause := range []string{
		"approval.agent_id IS NULL",
		"approval.single_use = TRUE",
		"approval.consumed_at IS NULL",
		"approval.expires_at > now()",
		"approver.role IN ('owner', 'admin')",
		"'frozen_census_sha256'",
		"'census_version'",
		"'company_brain_connection_id'",
		"'eligible_agent_count'",
		"'target_permissions_sha256'",
	} {
		if !strings.Contains(queryer.lastSQL, clause) {
			t.Fatalf("authorization SQL missing %q", clause)
		}
	}
}

func TestOwnerAdminParityPopulationAuthorizationGateRejectsUnavailableApproval(t *testing.T) {
	tests := []string{
		"mismatched request",
		"expired approval",
		"replayed approval",
		"non-owner approval",
	}
	for _, name := range tests {
		t.Run(name, func(t *testing.T) {
			gate := NewOwnerAdminParityPopulationAuthorizationGate(
				&parityApprovalQueryerStub{
					rows: []pgx.Row{parityApprovalRowStub{err: pgx.ErrNoRows}},
				},
			)
			err := gate.AuthorizeOnce(
				context.Background(),
				validParityPopulationRequest(t),
			)
			if err == nil || !strings.Contains(err.Error(), "live, unused, single-use") {
				t.Fatalf("AuthorizeOnce() error = %v, want fail-closed denial", err)
			}
		})
	}
}

func TestOwnerAdminParityPopulationAuthorizationGateValidatesBeforeQuery(t *testing.T) {
	queryer := &parityApprovalQueryerStub{}
	gate := NewOwnerAdminParityPopulationAuthorizationGate(queryer)
	request := validParityPopulationRequest(t)
	request.ExpectedEligibleAgentCount = 0

	err := gate.AuthorizeOnce(context.Background(), request)
	if err == nil || !strings.Contains(err.Error(), "eligible agent count") {
		t.Fatalf("AuthorizeOnce() error = %v, want request validation error", err)
	}
	if queryer.calls != 0 {
		t.Fatalf("invalid request reached database %d times", queryer.calls)
	}
}

type parityApprovalQueryerStub struct {
	rows     []pgx.Row
	calls    int
	lastSQL  string
	lastArgs []any
}

func (s *parityApprovalQueryerStub) QueryRow(
	_ context.Context,
	sql string,
	args ...any,
) pgx.Row {
	s.calls++
	s.lastSQL = sql
	s.lastArgs = append([]any(nil), args...)
	if len(s.rows) == 0 {
		return parityApprovalRowStub{err: errors.New("unexpected authorization query")}
	}
	row := s.rows[0]
	s.rows = s.rows[1:]
	return row
}

type parityApprovalRowStub struct {
	approvedBy string
	err        error
}

func (r parityApprovalRowStub) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != 1 {
		return errors.New("unexpected authorization scan destination")
	}
	value, ok := dest[0].(*string)
	if !ok {
		return errors.New("unexpected authorization scan type")
	}
	*value = r.approvedBy
	return nil
}
