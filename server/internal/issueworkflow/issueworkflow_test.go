package issueworkflow

import (
	"errors"
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
		"done":        {Key: "done", Name: "Done", Category: "done"},
		"cancelled":   {Key: "cancelled", Name: "Cancelled", Category: "closed"},
		"retired":     {Key: "retired", Name: "Retired", Category: "started", Archived: true},
	}
}

func devSteps() []Step {
	return []Step{
		{StatusKey: "backlog"},
		{StatusKey: "implement", Handler: Handler{Type: HandlerAgent, ID: testAgentID}, Instructions: "Implement it.", NextStatusKey: "code_review"},
		{StatusKey: "code_review", Handler: Handler{Type: HandlerProjectLead}, NextStatusKey: "done", BackStatusKey: "implement"},
		{StatusKey: "done"},
	}
}

func TestValidateAcceptsWellFormedWorkflow(t *testing.T) {
	name, desc, initial, steps := Normalize(" Delivery ", "", "implement", devSteps())
	if err := Validate(name, desc, initial, steps, testCatalog()); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if name != "Delivery" {
		t.Fatalf("name was not trimmed: %q", name)
	}
}

func TestValidateRejectsBrokenDefinitions(t *testing.T) {
	cases := map[string]struct {
		name    string
		initial string
		steps   func() []Step
		want    string
	}{
		"empty name":        {name: "", initial: "implement", steps: devSteps, want: "name"},
		"reserved name":     {name: "default", initial: "implement", steps: devSteps, want: "reserved"},
		"no steps":          {name: "x", initial: "implement", steps: func() []Step { return nil }, want: "1-50 steps"},
		"unknown status":    {name: "x", initial: "backlog", steps: func() []Step { return []Step{{StatusKey: "backlog"}, {StatusKey: "nope"}} }, want: "does not exist"},
		"archived status":   {name: "x", initial: "backlog", steps: func() []Step { return []Step{{StatusKey: "backlog"}, {StatusKey: "retired"}} }, want: "archived"},
		"duplicate status":  {name: "x", initial: "backlog", steps: func() []Step { return []Step{{StatusKey: "backlog"}, {StatusKey: "backlog"}} }, want: "more than once"},
		"initial not step":  {name: "x", initial: "todo", steps: devSteps, want: "starting status"},
		"handler needs id":  {name: "x", initial: "backlog", steps: func() []Step { return []Step{{StatusKey: "backlog", Handler: Handler{Type: HandlerAgent}}} }, want: "needs a agent"},
		"unknown handler":   {name: "x", initial: "backlog", steps: func() []Step { return []Step{{StatusKey: "backlog", Handler: Handler{Type: "robot"}}} }, want: "unknown handler"},
		"next outside flow": {name: "x", initial: "backlog", steps: func() []Step { return []Step{{StatusKey: "backlog", NextStatusKey: "todo"}} }, want: "not a step"},
	}
	for label, tc := range cases {
		t.Run(label, func(t *testing.T) {
			name, desc, initial, steps := Normalize(tc.name, "", tc.initial, tc.steps())
			err := Validate(name, desc, initial, steps, testCatalog())
			var verr *ValidationError
			if !errors.As(err, &verr) {
				t.Fatalf("Validate = %v, want a ValidationError", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

func TestNormalizeDropsStaleHandlerData(t *testing.T) {
	_, _, _, steps := Normalize("x", "", "a", []Step{
		{StatusKey: "a", Handler: Handler{Type: HandlerProjectLead, ID: testAgentID}, Instructions: " keep "},
		{StatusKey: "b", Handler: Handler{Type: HandlerNone, ID: testAgentID}, Instructions: "drop"},
		{StatusKey: "c"},
	})
	if steps[0].Handler.ID != "" || steps[0].Instructions != "keep" {
		t.Fatalf("project-lead step = %+v, want no id and trimmed instructions", steps[0])
	}
	if steps[1].Handler.ID != "" || steps[1].Instructions != "" {
		t.Fatalf("manual step = %+v, want no id and no instructions", steps[1])
	}
	if steps[2].Handler.Type != HandlerNone {
		t.Fatalf("empty handler type = %q, want none", steps[2].Handler.Type)
	}
}

func TestResolveHandler(t *testing.T) {
	var lead pgtype.UUID
	_ = lead.Scan(testAgentID)
	project := db.Project{LeadType: pgtype.Text{String: "member", Valid: true}, LeadID: lead}
	creator := lead

	steps := devSteps()
	if typ, id, ok := ResolveHandler(steps[1], project, "member", creator); !ok || typ != "agent" || id != lead {
		t.Fatalf("agent handler = %s %v %v", typ, id, ok)
	}
	if typ, _, ok := ResolveHandler(steps[2], project, "member", creator); !ok || typ != "member" {
		t.Fatalf("project lead handler = %s %v, want member", typ, ok)
	}
	if _, _, ok := ResolveHandler(steps[2], db.Project{}, "member", creator); ok {
		t.Fatal("a project without a lead must not resolve a handoff")
	}
	if _, _, ok := ResolveHandler(Step{StatusKey: "x", Handler: Handler{Type: HandlerCreator}}, project, "agent", creator); !ok {
		t.Fatal("creator handler should resolve to the issue creator")
	}
	if _, _, ok := ResolveHandler(steps[0], project, "member", creator); ok {
		t.Fatal("a manual step must not resolve a handoff")
	}
}

func TestMoveTargetPrefersSameStatusThenCategory(t *testing.T) {
	def := Definition{Name: "Delivery", InitialStatusKey: "implement", Steps: devSteps()}
	category := func(key string) string { return testCatalog()[key].Category }
	if got := MoveTarget(def, "done", category); got != "done" {
		t.Fatalf("listed status moved to %q", got)
	}
	if got := MoveTarget(def, "todo", category); got != "backlog" {
		t.Fatalf("todo moved to %q, want the first unstarted step", got)
	}
	if got := MoveTarget(def, "cancelled", category); got != "implement" {
		t.Fatalf("a category the workflow lacks moved to %q, want the starting status", got)
	}
}

func TestCheckStatusListsAllowedKeys(t *testing.T) {
	def := &Definition{Name: "Delivery", Steps: devSteps()}
	if err := CheckStatus(nil, "anything"); err != nil {
		t.Fatalf("the Default workflow accepts any status, got %v", err)
	}
	err := CheckStatus(def, "in_review")
	var notIn *NotInWorkflowError
	if !errors.As(err, &notIn) {
		t.Fatalf("CheckStatus = %v, want NotInWorkflowError", err)
	}
	if !strings.Contains(err.Error(), "backlog, implement, code_review, done") {
		t.Fatalf("error %q should list the workflow's keys for agents", err)
	}
}

func TestBriefNamesStepCommandsAndHandlers(t *testing.T) {
	def := Definition{Name: "Delivery", InitialStatusKey: "implement", Steps: devSteps()}
	brief := Brief(BriefInput{
		Workflow:        def,
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
		"This project's workflow — Delivery\n",
		`You are handling the "Code review" step of MUL-7.`,
		"- `implement` Implement — hands off to Forge\n",
		"- `code_review` Code review — hands off to Lin (project lead)   ← current\n",
		"- `done` Done\n",
		"Step done      → multica issue status MUL-7 done\n",
		"Needs changes  → multica issue status MUL-7 implement\n",
	} {
		if !strings.Contains(brief, want) {
			t.Fatalf("brief is missing %q:\n%s", want, brief)
		}
	}
	if strings.Contains(brief, "Can't proceed") {
		t.Fatalf("brief offers blocked, which this workflow does not list:\n%s", brief)
	}
}

func TestBriefPrintsBuiltInKeysOnce(t *testing.T) {
	def := Definition{Name: "Flow", InitialStatusKey: "todo", Steps: []Step{{StatusKey: "todo"}, {StatusKey: "in_review", Handler: Handler{Type: HandlerProjectLead}}}}
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
}
