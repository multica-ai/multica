package issueworkflow

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const testAgentID = "0f0e0d0c-0b0a-4909-8807-060504030201"

func testCatalog() map[string]CatalogEntry {
	return map[string]CatalogEntry{
		"backlog":     {Key: "backlog", Name: "Backlog", Category: "unstarted"},
		"todo":        {Key: "todo", Name: "Todo", Category: "unstarted"},
		"implement":   {Key: "implement", Name: "Implement", Category: "started"},
		"code_review": {Key: "code_review", Name: "Code review", Category: "started"},
		"blocked":     {Key: "blocked", Name: "Blocked", Category: "started"},
		"done":        {Key: "done", Name: "Done", Category: "done"},
		"cancelled":   {Key: "cancelled", Name: "Cancelled", Category: "closed"},
		"retired":     {Key: "retired", Name: "Retired", Category: "started", Archived: true},
	}
}

// withHandoffs replaces the steps of def that handoffs name.
func withHandoffs(def Definition, handoffs []Step) Definition {
	for _, h := range handoffs {
		for i, s := range def.Steps {
			if s.StatusKey == h.StatusKey {
				def.Steps[i] = h
			}
		}
	}
	return def
}

func devDefinition() Definition {
	def := Definition{Name: "Delivery", Steps: stepsFor(ArrangeKeys([]string{"backlog", "implement", "code_review", "blocked"}))}
	return withHandoffs(def, []Step{
		{StatusKey: "implement", Handler: Handler{Type: HandlerAgent, ID: testAgentID}, Instructions: "Implement it."},
		{StatusKey: "code_review", Handler: Handler{Type: HandlerProjectLead}, NextStatusKey: "done", BackStatusKey: "implement"},
	})
}

func TestNormalizePlacesFixedStatusesAroundOwnSteps(t *testing.T) {
	name, _, steps := Normalize(" Delivery ", "", stepsFor([]string{"done", " implement ", "blocked", "todo", "code_review", "backlog"}))
	if name != "Delivery" {
		t.Fatalf("name was not trimmed: %q", name)
	}
	want := []string{"backlog", "todo", "implement", "code_review", "blocked", "done", "cancelled"}
	if got := keysOf(steps); !reflect.DeepEqual(got, want) {
		t.Fatalf("keys = %v, want %v", got, want)
	}
	// Backlog and blocked are optional; the required statuses are added.
	_, _, steps = Normalize("Lean", "", stepsFor([]string{"implement"}))
	if got := keysOf(steps); !reflect.DeepEqual(got, []string{"todo", "implement", "done", "cancelled"}) {
		t.Fatalf("lean keys = %v", got)
	}
	if got := BuiltInDefaultKeys(); !reflect.DeepEqual(got, []string{"backlog", "todo", "in_progress", "in_review", "blocked", "done", "cancelled"}) {
		t.Fatalf("built-in Default = %v", got)
	}
}

func TestValidateRejectsBrokenWorkflows(t *testing.T) {
	cases := map[string]struct {
		name      string
		middle    []string
		isDefault bool
		want      string
	}{
		"empty name":       {name: "", want: "name"},
		"reserved name":    {name: "default", want: "reserved"},
		"unknown status":   {name: "x", middle: []string{"nope"}, want: "does not exist"},
		"archived status":  {name: "x", middle: []string{"retired"}, want: "archived"},
		"duplicate status": {name: "x", middle: []string{"implement", "implement"}, want: "more than once"},
	}
	broken := stepsFor([]string{"implement"})
	broken[0].Handler = Handler{Type: HandlerSquad, ID: testAgentID}
	name, desc, steps := Normalize("x", "", broken)
	if err := Validate(name, desc, steps, false, testCatalog()); err == nil || !strings.Contains(err.Error(), "hand off to an agent") {
		t.Fatalf("Validate must check handoffs, got %v", err)
	}
	for label, tc := range cases {
		t.Run(label, func(t *testing.T) {
			name, desc, steps := Normalize(tc.name, "", stepsFor(tc.middle))
			err := Validate(name, desc, steps, tc.isDefault, testCatalog())
			var verr *ValidationError
			if !errors.As(err, &verr) {
				t.Fatalf("Validate = %v, want a ValidationError", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", err, tc.want)
			}
		})
	}
	name, desc, steps = Normalize(DefaultName, "", stepsFor([]string{"implement"}))
	if err := Validate(name, desc, steps, true, testCatalog()); err != nil {
		t.Fatalf("Default keeps its reserved name: %v", err)
	}
}

func TestValidateHandoff(t *testing.T) {
	def := devDefinition()
	cases := map[string]struct {
		step Step
		want string
	}{
		"not a column":      {step: Step{StatusKey: "nope", Handler: Handler{Type: HandlerProjectLead}}, want: "not part"},
		"needs an id":       {step: Step{StatusKey: "implement", Handler: Handler{Type: HandlerAgent}}, want: "choose a agent"},
		"unknown handler":   {step: Step{StatusKey: "implement", Handler: Handler{Type: "robot"}}, want: "unknown handler"},
		"backlog parks":     {step: Step{StatusKey: "backlog", Handler: Handler{Type: HandlerProjectLead}}, want: "cannot be handed off"},
		"done finishes":     {step: Step{StatusKey: "done", Handler: Handler{Type: HandlerProjectLead}}, want: "cannot be handed off"},
		"next outside flow": {step: Step{StatusKey: "implement", Handler: Handler{Type: HandlerProjectLead}, NextStatusKey: "in_review"}, want: "not another status"},
		"next is itself":    {step: Step{StatusKey: "implement", Handler: Handler{Type: HandlerProjectLead}, NextStatusKey: "implement"}, want: "not another status"},
	}
	for label, tc := range cases {
		t.Run(label, func(t *testing.T) {
			err := ValidateHandoff(NormalizeHandoff(tc.step), def)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ValidateHandoff = %v, want it to mention %q", err, tc.want)
			}
		})
	}
	if err := ValidateHandoff(NormalizeHandoff(Step{StatusKey: "todo", Handler: Handler{Type: HandlerProjectLead}}), def); err != nil {
		t.Fatalf("todo may hand off: %v", err)
	}
}

func TestNormalizeHandoffDropsStaleHandlerData(t *testing.T) {
	lead := NormalizeHandoff(Step{StatusKey: "a", Handler: Handler{Type: HandlerProjectLead, ID: testAgentID}, Instructions: " keep "})
	if lead.Handler.ID != "" || lead.Instructions != "keep" {
		t.Fatalf("project-lead handoff = %+v, want no id and trimmed instructions", lead)
	}
	none := NormalizeHandoff(Step{StatusKey: "b", Handler: Handler{Type: HandlerNone, ID: testAgentID}, Instructions: "drop"})
	if none.Handler.ID != "" || none.Instructions != "" {
		t.Fatalf("no-handoff = %+v, want no id and no instructions", none)
	}
	if empty := NormalizeHandoff(Step{StatusKey: "c"}); empty.Handler.Type != HandlerNone {
		t.Fatalf("empty handler type = %q, want none", empty.Handler.Type)
	}
}

func TestEncodeStepsRoundTripsHandoffs(t *testing.T) {
	def := devDefinition()
	raw, err := EncodeSteps(def.Steps)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"status_key":"todo","handler"`) {
		t.Fatalf("a step without a handoff should store only its key: %s", raw)
	}
	got, err := DecodeSteps(raw)
	if err != nil || !reflect.DeepEqual(got, def.Steps) {
		t.Fatalf("DecodeSteps = %+v, %v; want %+v", got, err, def.Steps)
	}
}

func TestNextStatusFallsBackToTheNextColumn(t *testing.T) {
	def := devDefinition()
	if got := def.NextStatus("implement"); got != "code_review" {
		t.Fatalf("implement next = %q, want the next column", got)
	}
	if got := def.NextStatus("code_review"); got != "done" {
		t.Fatalf("code_review next = %q, want its own choice", got)
	}
	// The column after the last own step is blocked, which is skipped.
	plain := Definition{Steps: stepsFor(ArrangeKeys([]string{"implement", "blocked"}))}
	if got := plain.NextStatus("implement"); got != "done" {
		t.Fatalf("last step next = %q, want done", got)
	}
}

func TestResolveHandler(t *testing.T) {
	var lead pgtype.UUID
	_ = lead.Scan(testAgentID)
	project := db.Project{LeadType: pgtype.Text{String: "member", Valid: true}, LeadID: lead}
	creator := lead

	def := devDefinition()
	implement, _ := def.Step("implement")
	review, _ := def.Step("code_review")
	todo, _ := def.Step("todo")
	if typ, id, ok := ResolveHandler(implement, project, "member", creator); !ok || typ != "agent" || id != lead {
		t.Fatalf("agent handler = %s %v %v", typ, id, ok)
	}
	if typ, _, ok := ResolveHandler(review, project, "member", creator); !ok || typ != "member" {
		t.Fatalf("project lead handler = %s %v, want member", typ, ok)
	}
	if _, _, ok := ResolveHandler(review, db.Project{}, "member", creator); ok {
		t.Fatal("a project without a lead must not resolve a handoff")
	}
	if _, _, ok := ResolveHandler(Step{StatusKey: "x", Handler: Handler{Type: HandlerCreator}}, project, "agent", creator); !ok {
		t.Fatal("creator handler should resolve to the issue creator")
	}
	if _, _, ok := ResolveHandler(todo, project, "member", creator); ok {
		t.Fatal("a column without a handoff must not resolve one")
	}
}

func TestMoveTargetPrefersSameStatusThenCategory(t *testing.T) {
	def := devDefinition()
	category := func(key string) string {
		if key == "in_review" {
			return "started"
		}
		if key == "elsewhere" {
			return "unknown"
		}
		return testCatalog()[key].Category
	}
	if got := MoveTarget(def, "done", category); got != "done" {
		t.Fatalf("listed status moved to %q", got)
	}
	if got := MoveTarget(def, "in_review", category); got != "implement" {
		t.Fatalf("in_review moved to %q, want the first started step", got)
	}
	if got := MoveTarget(def, "elsewhere", category); got != StartStatus {
		t.Fatalf("a category the workflow lacks moved to %q, want todo", got)
	}
}

func TestCheckStatusListsAllowedKeys(t *testing.T) {
	def := devDefinition()
	if err := CheckStatus(nil, "anything"); err != nil {
		t.Fatalf("an issue outside projects accepts any status, got %v", err)
	}
	if err := CheckStatus(&Definition{Name: DefaultName, IsDefault: true, Steps: stepsFor(BuiltInDefaultKeys())}, "code_review"); err != nil {
		t.Fatalf("Default accepts any status, got %v", err)
	}
	err := CheckStatus(&def, "in_review")
	var notIn *NotInWorkflowError
	if !errors.As(err, &notIn) {
		t.Fatalf("CheckStatus = %v, want NotInWorkflowError", err)
	}
	if !strings.Contains(err.Error(), "backlog, todo, implement, code_review, blocked, done, cancelled") {
		t.Fatalf("error %q should list the workflow's keys for agents", err)
	}
}

func TestBriefNamesStepCommandsAndHandlers(t *testing.T) {
	brief := Brief(BriefInput{
		Workflow:        devDefinition(),
		ProjectName:     "Payments",
		StatusKey:       "code_review",
		IssueIdentifier: "MUL-7",
		StatusName:      func(key string) string { return testCatalog()[key].Name },
		HandlerName: func(step Step) string {
			if step.Handler.Type == HandlerAgent {
				return "Forge"
			}
			if step.Handler.Type == HandlerProjectLead {
				return "Lin (project lead)"
			}
			return ""
		},
	})
	for _, want := range []string{
		"This project's workflow — Delivery (Payments)\n",
		`You are handling the "Code review" step of MUL-7.`,
		"- `implement` Implement — hands off to Forge\n",
		"- `code_review` Code review — hands off to Lin (project lead)   ← current\n",
		"- `done` Done\n",
		"Step done      → multica issue status MUL-7 done\n",
		// Each move names who the target step hands off to.
		"Needs changes  → multica issue status MUL-7 implement — hands off to Forge\n",
		"\nYour moves:\n",
		"Can't proceed  → multica issue status MUL-7 blocked",
	} {
		if !strings.Contains(brief, want) {
			t.Fatalf("brief is missing %q:\n%s", want, brief)
		}
	}
}

// A current daemon's runtime brief states no built-in status rules and owns
// the exit check, so the brief neither overrides nor repeats them; an older
// daemon's still states them, so its brief keeps the override lines (MUL-8145).
func TestBriefOverridesRuntimeRulesOnlyForLegacyDaemons(t *testing.T) {
	legacyOnly := []string{
		"replaces the status rules in your runtime instructions",
		"These commands are the only status moves for your step",
		"Before your run ends, either run one of them",
		"The system does not advance the status when your run ends.",
	}
	for _, observer := range []bool{false, true} {
		current := Brief(BriefInput{Workflow: devDefinition(), StatusKey: "code_review", IssueIdentifier: "MUL-7", Observer: observer})
		for _, line := range legacyOnly {
			if strings.Contains(current, line) {
				t.Errorf("observer=%v: current-daemon brief carries %q:\n%s", observer, line, current)
			}
		}
		for _, word := range []string{"`in_progress`", "`in_review`"} {
			if strings.Contains(current, word) {
				t.Errorf("observer=%v: current-daemon brief names %s outside the workflow", observer, word)
			}
		}
	}
	legacy := Brief(BriefInput{Workflow: devDefinition(), StatusKey: "code_review", IssueIdentifier: "MUL-7", LegacyRuntimeRules: true})
	for _, line := range legacyOnly {
		if !strings.Contains(legacy, line) {
			t.Errorf("legacy-daemon brief is missing %q:\n%s", line, legacy)
		}
	}
}

func TestBriefForObserverLeavesTheStepToItsAssignee(t *testing.T) {
	brief := Brief(BriefInput{
		Workflow:        devDefinition(),
		StatusKey:       "code_review",
		IssueIdentifier: "MUL-7",
		Observer:        true,
		AssigneeName:    "Forge",
	})
	if !strings.Contains(brief, `MUL-7 is at the "code_review" step and is assigned to Forge. You are not its assignee`) {
		t.Fatalf("observer brief must name the assignee and leave them the step:\n%s", brief)
	}
	for _, leak := range []string{"Step instructions", "Step done", "Needs changes", "Can't proceed"} {
		if strings.Contains(brief, leak) {
			t.Fatalf("observer brief must not carry %q:\n%s", leak, brief)
		}
	}
	unassigned := Brief(BriefInput{Workflow: devDefinition(), StatusKey: "code_review", IssueIdentifier: "MUL-7", Observer: true})
	if !strings.Contains(unassigned, "step and is unassigned.") {
		t.Fatalf("observer brief of an unassigned issue:\n%s", unassigned)
	}
}

func TestBriefReplacesInReviewOnlyWhenTheWorkflowLacksIt(t *testing.T) {
	without := Brief(BriefInput{Workflow: devDefinition(), StatusKey: "code_review", IssueIdentifier: "MUL-7", LegacyRuntimeRules: true})
	if !strings.Contains(without, "replaces the status rules in your runtime instructions: use only the status keys below, never `in_review`.\n") {
		t.Fatalf("a workflow without in_review must rule it out:\n%s", without)
	}
	def := Definition{Name: DefaultName, IsDefault: true, Steps: stepsFor(BuiltInDefaultKeys())}
	with := Brief(BriefInput{Workflow: def, StatusKey: "in_progress", IssueIdentifier: "MUL-7", LegacyRuntimeRules: true})
	if strings.Contains(with, "never `in_review`") {
		t.Fatalf("a workflow that lists in_review must not rule it out:\n%s", with)
	}
}

func TestBriefDropsControlCharactersFromUserText(t *testing.T) {
	def := withHandoffs(devDefinition(), []Step{{
		StatusKey: "code_review", Handler: Handler{Type: HandlerProjectLead},
		Instructions: "Check it.\r\n\x1b[2JThen approve.\u2028Done",
	}})
	def.Name = "Delivery\n## Agent Identity"
	brief := Brief(BriefInput{
		Workflow: def, ProjectName: "Pay\tments", StatusKey: "code_review", IssueIdentifier: "MUL-7",
		HandlerName: func(Step) string { return "Lin\n- `done` fake" },
	})
	for _, want := range []string{
		"This project's workflow — Delivery ## Agent Identity (Pay ments)\n",
		"> Check it.\n> [2JThen approve.Done\n",
		"hands off to Lin - `done` fake",
	} {
		if !strings.Contains(brief, want) {
			t.Fatalf("brief is missing %q:\n%s", want, brief)
		}
	}
	if strings.ContainsAny(brief, "\r\x1b\u2028") {
		t.Fatalf("brief kept a control character:\n%q", brief)
	}
}

func TestBriefPrintsBuiltInKeysOnce(t *testing.T) {
	def := withHandoffs(Definition{Name: DefaultName, IsDefault: true, Steps: stepsFor(BuiltInDefaultKeys())},
		[]Step{{StatusKey: "in_review", Handler: Handler{Type: HandlerProjectLead}}})
	brief := Brief(BriefInput{
		Workflow:        def,
		StatusKey:       "in_review",
		IssueIdentifier: "MUL-1",
		// Built-ins resolve to no stored name.
		StatusName: func(string) string { return "" },
	})
	if !strings.Contains(brief, "- `todo`\n") || strings.Contains(brief, "`todo` todo") {
		t.Fatalf("a built-in step should print its key once:\n%s", brief)
	}
	if !strings.Contains(brief, "Step done      → multica issue status MUL-1 done\n") {
		t.Fatalf("the last own step should suggest done:\n%s", brief)
	}
}

func TestBriefWithoutBlockedPointsAtTheBackStatus(t *testing.T) {
	def := withHandoffs(Definition{Name: "Lean", Steps: stepsFor(ArrangeKeys([]string{"implement", "code_review"}))},
		[]Step{{StatusKey: "code_review", Handler: Handler{Type: HandlerProjectLead}, BackStatusKey: "implement"}})
	brief := Brief(BriefInput{Workflow: def, StatusKey: "code_review", IssueIdentifier: "MUL-2"})
	if strings.Contains(brief, "MUL-2 blocked") || !strings.Contains(brief, "Can't proceed  → comment why, then run Needs changes\n") || !strings.Contains(brief, "Needs changes  → multica issue status MUL-2 implement\n") {
		t.Fatalf("a workflow without blocked should not suggest it:\n%s", brief)
	}
}

// A step without a handler is a progress marker: the assignee owns the issue's
// whole job until it reaches a step that hands off, so the brief must not
// frame the column as a stage of its own (MUL-8231).
func TestBriefTreatsHandlerlessStepsAsOneSpan(t *testing.T) {
	leadName := func(step Step) string {
		if step.Handler.Type == HandlerProjectLead {
			return "Lin (project lead)"
		}
		return ""
	}
	reviewedDefault := withHandoffs(Definition{Name: DefaultName, IsDefault: true, Steps: stepsFor(BuiltInDefaultKeys())},
		[]Step{{StatusKey: "in_review", Handler: Handler{Type: HandlerProjectLead}}})
	plain := Definition{Name: "Plain", Steps: stepsFor(ArrangeKeys([]string{"in_progress", "in_review"}))}
	short := Definition{Name: "Short", Steps: stepsFor(ArrangeKeys([]string{"in_progress"}))}

	cases := []struct {
		name    string
		def     Definition
		status  string
		want    []string
		wantNot []string
	}{
		{
			name:   "todo before a reviewed in_review",
			def:    reviewedDefault,
			status: "todo",
			want: []string{
				`You are handling MUL-7. Its "todo" step has no handler, so the work the issue asks for is yours until it reaches the "in_review" step — hands off to Lin (project lead).`,
				"Starting work  → multica issue status MUL-7 in_progress\n",
				"Step done      → multica issue status MUL-7 in_review — hands off to Lin (project lead)\n",
				"finish all of the issue's work in this run before Step done",
			},
			wantNot: []string{"You are handling the", "MUL-7 done"},
		},
		{
			name:   "already in_progress",
			def:    reviewedDefault,
			status: "in_progress",
			want: []string{
				"Step done      → multica issue status MUL-7 in_review — hands off to Lin (project lead)\n",
			},
			wantNot: []string{"Starting work", "You are handling the"},
		},
		{
			name:   "no handoff ahead leaves done to a human",
			def:    plain,
			status: "todo",
			want: []string{
				`Its "todo" step has no handler, so the work the issue asks for is yours to finish.`,
				"Starting work  → multica issue status MUL-7 in_progress\n",
				"Step done      → multica issue status MUL-7 in_review\n",
			},
			wantNot: []string{"MUL-7 done"},
		},
		{
			name:   "only in_progress before done",
			def:    short,
			status: "todo",
			want: []string{
				"Starting work  → multica issue status MUL-7 in_progress\n",
				"Step done      → multica issue status MUL-7 done\n",
			},
		},
		{
			name:   "no in_progress in the workflow",
			def:    devDefinition(),
			status: "todo",
			want: []string{
				`until it reaches the "implement" step.`,
				"Step done      → multica issue status MUL-7 implement\n",
			},
			wantNot: []string{"Starting work", "in_progress"},
		},
		{
			// Default with a handoff elsewhere: the unconfigured columns keep
			// the built-in arc — start in_progress, deliver in_review, and a
			// re-run at in_review leaves done to a human.
			name:   "Default handoff elsewhere, from todo",
			def:    blockedOnlyDefault(),
			status: "todo",
			want: []string{
				"Starting work  → multica issue status MUL-7 in_progress\n",
				"Step done      → multica issue status MUL-7 in_review\n",
			},
			wantNot: []string{"MUL-7 done"},
		},
		{
			name:   "Default handoff elsewhere, at in_review",
			def:    blockedOnlyDefault(),
			status: "in_review",
			want: []string{
				"Step done      → leave the issue at `in_review`; `done` is set by a human\n",
			},
			wantNot: []string{"MUL-7 done", "Starting work"},
		},
		{
			name:   "a step that hands off keeps its stage wording",
			def:    reviewedDefault,
			status: "in_review",
			want: []string{
				`You are handling the "in_review" step of MUL-7.`,
				"Step done      → multica issue status MUL-7 done\n",
			},
			wantNot: []string{"Starting work", "Steps without a handler"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			brief := Brief(BriefInput{Workflow: tc.def, StatusKey: tc.status, IssueIdentifier: "MUL-7", HandlerName: leadName})
			for _, want := range tc.want {
				if !strings.Contains(brief, want) {
					t.Errorf("brief is missing %q:\n%s", want, brief)
				}
			}
			for _, not := range tc.wantNot {
				if strings.Contains(brief, not) {
					t.Errorf("brief carries %q:\n%s", not, brief)
				}
			}
		})
	}
}

// An older daemon's runtime brief states its own in_progress rule, and an
// observer leaves the status alone: neither gets the span wording.
func TestBriefKeepsStepWordingForLegacyDaemonsAndObservers(t *testing.T) {
	def := Definition{Name: "Plain", Steps: stepsFor(ArrangeKeys([]string{"in_progress", "in_review"}))}
	legacy := Brief(BriefInput{Workflow: def, StatusKey: "todo", IssueIdentifier: "MUL-7", LegacyRuntimeRules: true})
	if !strings.Contains(legacy, `You are handling the "todo" step of MUL-7.`) || strings.Contains(legacy, "Starting work") ||
		!strings.Contains(legacy, "Step done      → multica issue status MUL-7 in_progress\n") {
		t.Fatalf("legacy brief must keep the per-step wording:\n%s", legacy)
	}
	observer := Brief(BriefInput{Workflow: def, StatusKey: "todo", IssueIdentifier: "MUL-7", Observer: true})
	if strings.Contains(observer, "Starting work") || strings.Contains(observer, "has no handler") {
		t.Fatalf("observer brief must not carry the span wording:\n%s", observer)
	}
}

// blockedOnlyDefault is the Default workflow with a handoff only on blocked,
// so every other column is unconfigured.
func blockedOnlyDefault() Definition {
	return withHandoffs(Definition{Name: DefaultName, IsDefault: true, Steps: stepsFor(BuiltInDefaultKeys())},
		[]Step{{StatusKey: "blocked", Handler: Handler{Type: HandlerProjectLead}}})
}
