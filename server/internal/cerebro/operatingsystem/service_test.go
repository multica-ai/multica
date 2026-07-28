package operatingsystem

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestValidateStrategyInput(t *testing.T) {
	tests := []struct {
		name    string
		input   StrategyItemInput
		wantErr bool
	}{
		{name: "core value", input: StrategyItemInput{Kind: "core_value", Title: "Care deeply"}},
		{name: "horizon goal", input: StrategyItemInput{Kind: "horizon_goal", Title: "Expand", HorizonUnit: "year", HorizonCount: 3}},
		{name: "invalid kind", input: StrategyItemInput{Kind: "objective", Title: "Expand"}, wantErr: true},
		{name: "missing title", input: StrategyItemInput{Kind: "core_focus"}, wantErr: true},
		{name: "missing horizon", input: StrategyItemInput{Kind: "horizon_goal", Title: "Expand"}, wantErr: true},
		{name: "invalid horizon", input: StrategyItemInput{Kind: "horizon_goal", Title: "Expand", HorizonUnit: "quarter", HorizonCount: 1}, wantErr: true},
		{name: "horizon on core value", input: StrategyItemInput{Kind: "core_value", Title: "Care", HorizonUnit: "year", HorizonCount: 1}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateStrategyInput(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateStrategyInput() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

const pageUUID = "550e8400-e29b-41d4-a716-446655440000"

func TestValidateVisionPlanSectionInput(t *testing.T) {
	tests := []struct {
		name    string
		input   VisionPlanSectionInput
		wantErr bool
	}{
		{name: "list", input: VisionPlanSectionInput{Name: "Core Values", SectionType: "list", PageID: pageUUID}},
		{name: "structured", input: VisionPlanSectionInput{Name: "Marketing Strategy", SectionType: "structured", PageID: pageUUID}},
		{name: "process", input: VisionPlanSectionInput{Name: "Core Processes", SectionType: "process", PageID: pageUUID}},
		{name: "goals", input: VisionPlanSectionInput{Name: "Goals", SectionType: "goals", PageID: pageUUID, ColumnIndex: 2}},
		{name: "missing name", input: VisionPlanSectionInput{SectionType: "list", PageID: pageUUID}, wantErr: true},
		{name: "invalid type", input: VisionPlanSectionInput{Name: "Custom", SectionType: "cards", PageID: pageUUID}, wantErr: true},
		{name: "missing page", input: VisionPlanSectionInput{Name: "Custom", SectionType: "list"}, wantErr: true},
		{name: "column out of range", input: VisionPlanSectionInput{Name: "Custom", SectionType: "list", PageID: pageUUID, ColumnIndex: 3}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateVisionPlanSectionInput(tt.input); (err != nil) != tt.wantErr {
				t.Fatalf("ValidateVisionPlanSectionInput(%#v) error = %v", tt.input, err)
			}
		})
	}
}

func TestValidateVisionPlanPageInput(t *testing.T) {
	tests := []struct {
		name    string
		input   VisionPlanPageInput
		wantErr bool
	}{
		{name: "three columns", input: VisionPlanPageInput{Name: "Traction", ColumnCount: 3}},
		{name: "one column", input: VisionPlanPageInput{Name: "Vision", ColumnCount: 1}},
		{name: "missing name", input: VisionPlanPageInput{ColumnCount: 2}, wantErr: true},
		{name: "no columns", input: VisionPlanPageInput{Name: "Traction"}, wantErr: true},
		{name: "too many columns", input: VisionPlanPageInput{Name: "Traction", ColumnCount: 4}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateVisionPlanPageInput(tt.input); (err != nil) != tt.wantErr {
				t.Fatalf("ValidateVisionPlanPageInput(%#v) error = %v", tt.input, err)
			}
		})
	}
}

func TestValidateVisionPlanItemInput(t *testing.T) {
	valid := VisionPlanItemInput{SectionID: testWorkspaceID, Title: "Serve a clear niche"}
	if err := ValidateVisionPlanItemInput(valid); err != nil {
		t.Fatalf("valid item rejected: %v", err)
	}

	tests := []VisionPlanItemInput{
		{SectionID: "bad", Title: valid.Title},
		{SectionID: valid.SectionID},
		{SectionID: valid.SectionID, Title: valid.Title, OwnerType: "member"},
		{SectionID: valid.SectionID, Title: valid.Title, OwnerType: "team", OwnerID: testMemberID},
	}
	for _, input := range tests {
		if err := ValidateVisionPlanItemInput(input); err == nil {
			t.Fatalf("invalid item accepted: %#v", input)
		}
	}
}

func TestValidateMeetingInput(t *testing.T) {
	valid := MeetingConfigInput{
		CadenceUnit: "week", CadenceCount: 1,
		Agenda: []MeetingAgendaSectionInput{{ID: "check-in", Name: "Check-in", Binding: "none"}},
	}
	if err := ValidateMeetingInput(valid); err != nil {
		t.Fatalf("valid meeting rejected: %v", err)
	}
	invalid := []MeetingConfigInput{
		{CadenceUnit: "fortnight", CadenceCount: 1},
		{CadenceUnit: "week", CadenceCount: 0},
		{CadenceUnit: "week", CadenceCount: 1, Agenda: []MeetingAgendaSectionInput{{Name: "", Binding: "none"}}},
		{CadenceUnit: "week", CadenceCount: 1, Agenda: []MeetingAgendaSectionInput{{Name: "Review", Binding: "unknown"}}},
	}
	for _, input := range invalid {
		if err := ValidateMeetingInput(input); err == nil {
			t.Fatalf("invalid meeting accepted: %#v", input)
		}
	}
}

func TestApplyMeetingNoteTypeUsesRecurringNoteCadence(t *testing.T) {
	response := MeetingConfigResponse{
		NoteTypeID: "note-weekly", CadenceUnit: "month", CadenceCount: 3,
	}
	applyMeetingNoteType(&response, []MeetingNoteTypeResponse{{
		ID: "note-weekly", Name: "Business Review", CadenceUnit: "week", CadenceCount: 1, Enabled: true, CurrentNoteID: "note-current",
	}})

	if response.NoteTypeName != "Business Review" || response.CurrentNoteID != "note-current" || response.CadenceUnit != "week" || response.CadenceCount != 1 {
		t.Fatalf("meeting timing did not follow recurring note: %#v", response)
	}
}

func TestValidateOrgChartSeatInput(t *testing.T) {
	valid := OrgChartSeatInput{Name: "Operations", Responsibilities: []string{"Run the weekly plan"}}
	if err := ValidateOrgChartSeatInput(valid); err != nil {
		t.Fatalf("valid seat rejected: %v", err)
	}
	invalid := []OrgChartSeatInput{
		{Name: ""},
		{Name: "Operations", OwnerType: "member"},
		{Name: "Operations", OwnerType: "team", OwnerID: testMemberID},
		{Name: "Operations", OwnerType: "member", OwnerID: "not-a-uuid"},
	}
	for _, input := range invalid {
		if err := ValidateOrgChartSeatInput(input); err == nil {
			t.Fatalf("invalid seat accepted: %#v", input)
		}
	}
}

func TestValidateRockInput(t *testing.T) {
	valid := RockInput{
		Title:          "Cut fulfilment cost 12%",
		PeriodID:       "550e8400-e29b-41d4-a716-446655440010",
		Confidence:     70,
		ReportedHealth: "on_track",
	}
	tests := []struct {
		name   string
		mutate func(*RockInput)
	}{
		{name: "missing title", mutate: func(v *RockInput) { v.Title = "" }},
		{name: "bad period id", mutate: func(v *RockInput) { v.PeriodID = "not-a-uuid" }},
		{name: "low confidence", mutate: func(v *RockInput) { v.Confidence = -1 }},
		{name: "high confidence", mutate: func(v *RockInput) { v.Confidence = 101 }},
		{name: "bad health", mutate: func(v *RockInput) { v.ReportedHealth = "green" }},
		{name: "bad owner type", mutate: func(v *RockInput) { v.OwnerType = "team"; v.OwnerID = testMemberID }},
		{name: "owner without id", mutate: func(v *RockInput) { v.OwnerType = "member" }},
	}

	if err := ValidateRockInput(valid); err != nil {
		t.Fatalf("valid RockInput rejected: %v", err)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := valid
			tt.mutate(&input)
			if err := ValidateRockInput(input); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestValidateRockInputAcceptsOptionalConnections(t *testing.T) {
	input := RockInput{
		Title: "Independent Rock", PeriodID: "550e8400-e29b-41d4-a716-446655440010",
		Confidence: 50, ReportedHealth: "unset",
	}
	if err := ValidateRockInput(input); err != nil {
		t.Fatalf("standalone Rock rejected: %v", err)
	}

	input.ProjectIDs = []string{"550e8400-e29b-41d4-a716-446655440011"}
	input.IssueIDs = []string{"550e8400-e29b-41d4-a716-446655440012", "550e8400-e29b-41d4-a716-446655440013"}
	if err := ValidateRockInput(input); err != nil {
		t.Fatalf("connected Rock rejected: %v", err)
	}
}

func TestNormalizeTerminologyFallsBackPerField(t *testing.T) {
	got := NormalizeTerminology([]byte(`{"strategy":"Direction","rock":12,"rocks":""}`))
	if got.Strategy != "Direction" || got.Rock != "Goal" || got.Rocks != "Goals" {
		t.Fatalf("unexpected terminology: %#v", got)
	}

	if got := NormalizeTerminology([]byte(`not-json`)); got != DefaultTerminology() {
		t.Fatalf("malformed JSON fallback = %#v", got)
	}
}

func TestDefaultTerminologyUsesNeutralNamesForEveryElement(t *testing.T) {
	got := DefaultTerminology()
	want := Terminology{
		Strategy: "Strategy", Rock: "Goal", Rocks: "Goals",
		VisionPlan: "Vision Plan", Meetings: "Cycles", OrgChart: "Roles",
		Scorecard: "Scorecard", IssuesList: "Issues List", StrategyMap: "Strategy Map",
	}
	if got != want {
		t.Fatalf("DefaultTerminology() = %#v", got)
	}
}

func TestNormalizeTerminologyPreservesStoredLegacyLabels(t *testing.T) {
	got := NormalizeTerminology([]byte(`{"strategy":"Strategy","rock":"Rock","rocks":"Rocks"}`))
	if got.Rock != "Rock" || got.Rocks != "Rocks" {
		t.Fatalf("stored labels must win over neutral defaults: %#v", got)
	}
	if got.VisionPlan != "Vision Plan" || got.StrategyMap != "Strategy Map" {
		t.Fatalf("missing element labels must fall back to neutral defaults: %#v", got)
	}
}

func TestElementRegistryEnablesTodaysElementsByDefault(t *testing.T) {
	elements := MergeElementSettings(nil)
	byKey := make(map[string]OsElementResponse, len(elements))
	for _, element := range elements {
		byKey[element.Key] = element
	}
	for _, key := range []string{"vision_plan", "goals", "meetings", "org_chart", "scorecard", "issues_list", "strategy_map"} {
		if _, ok := byKey[key]; !ok {
			t.Fatalf("element registry missing %q", key)
		}
	}
	if !byKey["goals"].Enabled || !byKey["vision_plan"].Enabled {
		t.Fatal("elements with a shipped interface must default to enabled")
	}
	if !byKey["meetings"].Enabled || !byKey["org_chart"].Enabled || byKey["scorecard"].Enabled {
		t.Fatal("shipped Stage 4 elements must default to enabled while scorecard remains disabled")
	}
}

func TestMergeElementSettingsAppliesWorkspaceOverrides(t *testing.T) {
	elements := MergeElementSettings(map[string]bool{"goals": false, "scorecard": true})
	byKey := make(map[string]OsElementResponse, len(elements))
	for _, element := range elements {
		byKey[element.Key] = element
	}
	if byKey["goals"].Enabled || !byKey["scorecard"].Enabled {
		t.Fatalf("overrides not applied: %#v", byKey)
	}
	if !byKey["goals"].DefaultEnabled || byKey["scorecard"].DefaultEnabled {
		t.Fatal("default_enabled must keep reporting the registry default")
	}
}

func TestIsRegisteredElementRejectsUnknownKeys(t *testing.T) {
	if !IsRegisteredElement("goals") {
		t.Fatal("goals must be a registered element")
	}
	if IsRegisteredElement("people_analyzer") {
		t.Fatal("unknown keys must be rejected")
	}
}

func TestValidateGoalTypeInput(t *testing.T) {
	tests := []struct {
		name    string
		input   GoalTypeInput
		wantErr bool
	}{
		{name: "valid", input: GoalTypeInput{Name: "Company", Color: "#6366F1", ScopeLabel: "company-wide"}},
		{name: "color and scope optional", input: GoalTypeInput{Name: "Team"}},
		{name: "blank name", input: GoalTypeInput{Name: "  "}, wantErr: true},
		{name: "malformed color", input: GoalTypeInput{Name: "Team", Color: "blue"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateGoalTypeInput(tt.input); (err != nil) != tt.wantErr {
				t.Fatalf("ValidateGoalTypeInput(%#v) error = %v", tt.input, err)
			}
		})
	}
}

func TestResolvePeriodInputComputesBoundsAndNames(t *testing.T) {
	month, err := ResolvePeriodInput(OperatingPeriodInput{Unit: "month", StartsOn: "2026-08-01"})
	if err != nil || month.Name != "August 2026" || date(month.EndsOn) != "2026-08-31" {
		t.Fatalf("month period = %#v, err %v", month, err)
	}

	quarter, err := ResolvePeriodInput(OperatingPeriodInput{Unit: "quarter", StartsOn: "2026-10-01"})
	if err != nil || quarter.Name != "Q4 2026" || date(quarter.EndsOn) != "2026-12-31" {
		t.Fatalf("quarter period = %#v, err %v", quarter, err)
	}

	custom, err := ResolvePeriodInput(OperatingPeriodInput{Unit: "custom", Name: "H1 2027", StartsOn: "2027-01-01", EndsOn: "2027-06-30"})
	if err != nil || custom.Name != "H1 2027" || date(custom.EndsOn) != "2027-06-30" {
		t.Fatalf("custom period = %#v, err %v", custom, err)
	}

	if _, err := ResolvePeriodInput(OperatingPeriodInput{Unit: "custom", StartsOn: "2027-01-01", EndsOn: "2027-06-30"}); err == nil {
		t.Fatal("custom periods must require a name")
	}
	if _, err := ResolvePeriodInput(OperatingPeriodInput{Unit: "custom", Name: "Broken", StartsOn: "2027-06-30", EndsOn: "2027-01-01"}); err == nil {
		t.Fatal("period end must not precede start")
	}
	if _, err := ResolvePeriodInput(OperatingPeriodInput{Unit: "sprint", StartsOn: "2026-08-01"}); err == nil {
		t.Fatal("unknown units must be rejected")
	}
	if _, err := ResolvePeriodInput(OperatingPeriodInput{Unit: "month", StartsOn: "not-a-date"}); err == nil {
		t.Fatal("malformed starts_on must be rejected")
	}

	midMonth, err := ResolvePeriodInput(OperatingPeriodInput{Unit: "month", StartsOn: "2026-08-17"})
	if err != nil || date(midMonth.StartsOn) != "2026-08-01" {
		t.Fatalf("month periods must snap to calendar boundaries: %#v, err %v", midMonth, err)
	}

	named, err := ResolvePeriodInput(OperatingPeriodInput{Unit: "month", Name: "Sprint August", StartsOn: "2026-08-01"})
	if err != nil || named.Name != "Sprint August" {
		t.Fatalf("explicit names must win over generated ones: %#v, err %v", named, err)
	}
}

func TestValidateRockInputAcceptsGoalType(t *testing.T) {
	input := RockInput{
		Title: "Typed goal", PeriodID: "550e8400-e29b-41d4-a716-446655440010",
		Confidence: 50, ReportedHealth: "unset", GoalTypeID: "550e8400-e29b-41d4-a716-446655440020",
	}
	if err := ValidateRockInput(input); err != nil {
		t.Fatalf("goal-typed input rejected: %v", err)
	}
	input.GoalTypeID = "not-a-uuid"
	if err := ValidateRockInput(input); err == nil {
		t.Fatal("malformed goal_type_id must be rejected")
	}
}

func TestDeriveHealthExplainsIssueContributors(t *testing.T) {
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		total   int32
		done    int32
		blocked int32
		want    string
	}{
		{name: "blocked wins", total: 4, done: 1, blocked: 1, want: "off_track"},
		{name: "all done", total: 4, done: 4, want: "on_track"},
		{name: "work remains", total: 4, done: 1, want: "at_risk"},
		{name: "no issues", want: "unset"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DeriveHealth(tt.total, tt.done, tt.blocked, now)
			if got.State != tt.want || got.Reason == "" || got.CalculatedAt != now.Format(time.RFC3339) {
				t.Fatalf("DeriveHealth() = %#v", got)
			}
		})
	}
}

func TestEnsureProjectInWorkspaceRejectsCrossWorkspaceProject(t *testing.T) {
	projectID := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	workspaceID := pgtype.UUID{Bytes: [16]byte{2}, Valid: true}
	reader := fakeProjectReader{err: pgx.ErrNoRows}

	err := EnsureProjectInWorkspace(context.Background(), reader, projectID, workspaceID)
	if !errors.Is(err, ErrProjectNotInWorkspace) {
		t.Fatalf("error = %v, want ErrProjectNotInWorkspace", err)
	}
}

func TestEnsureOwnerInWorkspaceRejectsCrossWorkspaceOwners(t *testing.T) {
	workspaceID := pgtype.UUID{Bytes: [16]byte{2}, Valid: true}
	ownerID := pgtype.UUID{Bytes: [16]byte{3}, Valid: true}

	t.Run("member", func(t *testing.T) {
		reader := fakeProjectReader{project: db.Project{WorkspaceID: pgtype.UUID{Bytes: [16]byte{4}, Valid: true}}}
		if err := EnsureOwnerInWorkspace(context.Background(), reader, "member", ownerID, workspaceID); !errors.Is(err, ErrOwnerNotInWorkspace) {
			t.Fatalf("error = %v, want ErrOwnerNotInWorkspace", err)
		}
	})

	t.Run("agent", func(t *testing.T) {
		reader := fakeProjectReader{err: pgx.ErrNoRows}
		if err := EnsureOwnerInWorkspace(context.Background(), reader, "agent", ownerID, workspaceID); !errors.Is(err, ErrOwnerNotInWorkspace) {
			t.Fatalf("error = %v, want ErrOwnerNotInWorkspace", err)
		}
	})
}

func TestDuplicateConnectionDetection(t *testing.T) {
	if !IsDuplicateConnection(&pgconn.PgError{Code: "23505"}) {
		t.Fatal("unique violation must be reported as duplicate connection")
	}
	if IsDuplicateConnection(errors.New("other")) {
		t.Fatal("ordinary error must not be reported as duplicate")
	}
}

func TestQuarterPeriodUsesCalendarQuarter(t *testing.T) {
	name, start, end := quarterPeriod(time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC))
	if name != "Q3 2026" || start != (time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)) || end != (time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("quarterPeriod() = %q, %s, %s", name, start, end)
	}
}

type fakeProjectReader struct {
	project db.Project
	err     error
}

func (f fakeProjectReader) GetProjectInWorkspace(context.Context, db.GetProjectInWorkspaceParams) (db.Project, error) {
	return f.project, f.err
}

func (f fakeProjectReader) GetIssueInWorkspace(context.Context, db.GetIssueInWorkspaceParams) (db.Issue, error) {
	return db.Issue{}, f.err
}

func (f fakeProjectReader) GetMember(context.Context, pgtype.UUID) (db.Member, error) {
	return db.Member{WorkspaceID: f.project.WorkspaceID}, f.err
}

func (f fakeProjectReader) GetAgentInWorkspace(context.Context, db.GetAgentInWorkspaceParams) (db.Agent, error) {
	return db.Agent{}, f.err
}
