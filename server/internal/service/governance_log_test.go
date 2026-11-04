package service

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

func TestTruncateGovernanceSummaryKeepsShortAndCutsLong(t *testing.T) {
	if got := TruncateGovernanceSummary("  The boss paused the company.  "); got != "The boss paused the company." {
		t.Fatalf("short summary = %q, want it trimmed and kept", got)
	}
	long := strings.Repeat("é", GovernanceSummaryMax+100)
	got := TruncateGovernanceSummary(long)
	if n := utf8.RuneCountInString(got); n != GovernanceSummaryMax {
		t.Fatalf("cut summary has %d characters, want %d", n, GovernanceSummaryMax)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("a cut summary must say so with an ellipsis, got suffix %q", got[len(got)-3:])
	}
	exact := strings.Repeat("a", GovernanceSummaryMax)
	if TruncateGovernanceSummary(exact) != exact {
		t.Fatal("a summary of exactly the maximum length is kept whole")
	}
}

func TestGovernanceRecordRefusesUnknownKindsAndBadFieldsBeforeWriting(t *testing.T) {
	// Every refusal happens before the query handle is used, so a nil handle is safe.
	ctx := context.Background()
	ws := dbid.NewV7()
	log := NewGovernanceLog()

	if err := log.Record(ctx, nil, GovernanceEvent{WorkspaceID: ws, Kind: "made_up", ActorType: "member", SubjectType: "workspace"}); !errors.Is(err, ErrGovernanceKindUnknown) {
		t.Fatalf("unknown kind: err = %v, want ErrGovernanceKindUnknown", err)
	}
	if err := log.Record(ctx, nil, GovernanceEvent{WorkspaceID: ws, Kind: GovernanceKindCompanyPaused, ActorType: "robot", SubjectType: "workspace"}); !errors.Is(err, ErrGovernanceEventInvalid) {
		t.Fatalf("unknown actor type: err = %v, want ErrGovernanceEventInvalid", err)
	}
	if err := log.Record(ctx, nil, GovernanceEvent{Kind: GovernanceKindCompanyPaused, ActorType: "member", SubjectType: "workspace"}); !errors.Is(err, ErrGovernanceEventInvalid) {
		t.Fatalf("no workspace: err = %v, want ErrGovernanceEventInvalid", err)
	}
	if err := log.Record(ctx, nil, GovernanceEvent{WorkspaceID: ws, Kind: GovernanceKindCompanyPaused, ActorType: "member"}); !errors.Is(err, ErrGovernanceEventInvalid) {
		t.Fatalf("no subject type: err = %v, want ErrGovernanceEventInvalid", err)
	}
}

func TestGovernanceKindsCoverTheSpecList(t *testing.T) {
	// Spec 4.5 names these kinds; the log must accept every one of them.
	spec := []string{
		"policy_auto_approve", "policy_created", "policy_revoked", "budget_set", "budget_extended",
		"budget_hold", "budget_release", "autonomy_changed", "org_change_applied", "org_change_reverted",
		"auto_publish", "initiative_forced", "company_paused", "company_resumed", "goal_activated", "review_published",
	}
	got := map[string]bool{}
	for _, k := range GovernanceKinds() {
		got[k] = true
	}
	for _, k := range spec {
		if !got[k] {
			t.Errorf("kind %q from spec 4.5 is not accepted", k)
		}
	}
	if len(got) != len(spec) {
		t.Errorf("the log accepts %d kinds, spec 4.5 names %d", len(got), len(spec))
	}
}

func TestGovernanceCursorRoundTripsAndRejectsGarbage(t *testing.T) {
	at := time.Date(2026, 3, 4, 5, 6, 7, 123456789, time.UTC)
	id := dbid.NewV7()
	gotAt, gotID, err := decodeGovernanceCursor(encodeGovernanceCursor(at, id))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !gotAt.Equal(at) || util.UUIDToString(gotID) != util.UUIDToString(id) {
		t.Fatalf("round trip gave %v %v, want %v %v", gotAt, gotID, at, util.UUIDToString(id))
	}
	for _, bad := range []string{
		"", "!!!", base64.RawURLEncoding.EncodeToString([]byte("no-separator")),
		base64.RawURLEncoding.EncodeToString([]byte("not-a-time|" + util.UUIDToString(id))),
		base64.RawURLEncoding.EncodeToString([]byte(at.Format(time.RFC3339Nano) + "|not-a-uuid")),
	} {
		if _, _, err := decodeGovernanceCursor(bad); !errors.Is(err, ErrGovernanceCursorInvalid) {
			t.Errorf("cursor %q: err = %v, want ErrGovernanceCursorInvalid", bad, err)
		}
	}
}

// TestGovernanceEventHasNoUpdateOrDeleteQuery is the append-only guard (spec 4.5,
// 4.9): the generated queries for governance_event expose no update and no delete
// except the workspace teardown, and no query file names an UPDATE or a DELETE on
// the table outside that teardown.
func TestGovernanceEventHasNoUpdateOrDeleteQuery(t *testing.T) {
	const teardown = "DeleteWorkspaceGovernanceEvents"
	// CountGovernanceEvents is a read: the CSV export's row-cap pre-check.
	allowedMethods := map[string]bool{"InsertGovernanceEvent": true, "ListGovernanceEvents": true, "CountGovernanceEvents": true, teardown: true}
	queriesType := reflect.TypeOf(&db.Queries{})
	for i := 0; i < queriesType.NumMethod(); i++ {
		name := queriesType.Method(i).Name
		if strings.Contains(name, "GovernanceEvent") && !allowedMethods[name] {
			t.Errorf("generated query %s touches governance_event and is neither an insert, a list nor the teardown", name)
		}
	}

	mutating := regexp.MustCompile(`(?is)\b(update\s+governance_event|delete\s+from\s+governance_event)\b`)
	files, err := filepath.Glob("../../pkg/db/queries/*.sql")
	if err != nil || len(files) == 0 {
		t.Fatalf("list query files: %v", err)
	}
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, block := range strings.Split(string(raw), "-- name: ")[1:] {
			name, _, _ := strings.Cut(block, "\n")
			name = strings.Fields(name)[0]
			if !mutating.MatchString(block) {
				continue
			}
			if name != teardown {
				t.Errorf("%s: query %s mutates governance_event; the log is append-only", filepath.Base(file), name)
			}
		}
	}
}

func TestGovernanceLogRecordsAndListsNewestFirstWithFiltersAndPaging(t *testing.T) {
	pool := sharedTestPool(t)
	ctx := context.Background()
	q := db.New(pool)
	fx := testutil.New(pool, "", "")
	userID := fx.User(t, "Gov Reader", fmt.Sprintf("gov-reader-%d@example.test", time.Now().UnixNano()))
	wsID := fx.Workspace(t, "Governance WS", fmt.Sprintf("gov-%d", time.Now().UnixNano()))
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM governance_event WHERE workspace_id = $1`, wsID)
	})
	ws := util.MustParseUUID(wsID)
	user := util.MustParseUUID(userID)
	log := NewGovernanceLog()

	subject := dbid.NewV7()
	written := make([]string, 0, 5)
	for i := 0; i < 5; i++ {
		kind := GovernanceKindCompanyPaused
		subjectID := subject
		if i%2 == 1 {
			kind = GovernanceKindBudgetHold
			subjectID = dbid.NewV7()
		}
		ev := GovernanceEvent{
			WorkspaceID: ws, Kind: kind, ActorType: GovernanceActorMember, ActorID: user,
			SubjectType: "workspace", SubjectID: subjectID,
			Summary: fmt.Sprintf("event %d", i), Details: map[string]any{"n": i},
		}
		if err := log.Record(ctx, q, ev); err != nil {
			t.Fatalf("record %d: %v", i, err)
		}
		time.Sleep(2 * time.Millisecond)
	}

	all, err := log.List(ctx, q, ws, GovernanceFilter{Limit: 2})
	if err != nil {
		t.Fatalf("list page 1: %v", err)
	}
	if len(all.Events) != 2 || all.NextCursor == "" {
		t.Fatalf("page 1: %d events, cursor %q; want 2 and a cursor", len(all.Events), all.NextCursor)
	}
	seen := map[string]bool{}
	var order []db.GovernanceEvent
	cursor := ""
	for pages := 0; pages < 10; pages++ {
		page, err := log.List(ctx, q, ws, GovernanceFilter{Limit: 2, Cursor: cursor})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		for _, e := range page.Events {
			if seen[util.UUIDToString(e.ID)] {
				t.Fatalf("event %s listed twice across pages", util.UUIDToString(e.ID))
			}
			seen[util.UUIDToString(e.ID)] = true
			order = append(order, e)
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	if len(order) != 5 {
		t.Fatalf("paging returned %d events, want 5", len(order))
	}
	for i := 1; i < len(order); i++ {
		if order[i].CreatedAt.Time.After(order[i-1].CreatedAt.Time) {
			t.Fatalf("event %d is newer than the one before it: the log is newest first", i)
		}
	}
	for _, e := range order {
		written = append(written, e.Summary)
	}
	if len(written) != 5 || written[0] != "event 4" {
		t.Fatalf("summaries newest first = %v, want the last recorded first", written)
	}

	paused, err := log.List(ctx, q, ws, GovernanceFilter{Kind: GovernanceKindCompanyPaused})
	if err != nil || len(paused.Events) != 3 {
		t.Fatalf("kind filter: %d events, err %v; want 3", len(paused.Events), err)
	}
	bySubject, err := log.List(ctx, q, ws, GovernanceFilter{SubjectID: subject})
	if err != nil || len(bySubject.Events) != 3 {
		t.Fatalf("subject filter: %d events, err %v; want 3", len(bySubject.Events), err)
	}
	byActor, err := log.List(ctx, q, ws, GovernanceFilter{ActorType: GovernanceActorAgent})
	if err != nil || len(byActor.Events) != 0 {
		t.Fatalf("actor filter for agents: %d events, err %v; want 0", len(byActor.Events), err)
	}
	if _, err := log.List(ctx, q, ws, GovernanceFilter{Cursor: "garbage!"}); !errors.Is(err, ErrGovernanceCursorInvalid) {
		t.Fatalf("garbage cursor: err = %v, want ErrGovernanceCursorInvalid", err)
	}
}
