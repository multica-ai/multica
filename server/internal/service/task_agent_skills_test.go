package service

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// errInjectedSkillRead is the transient read failure these tests inject. The
// point of every failure case below is that it reaches the caller: a swallowed
// read here does not shrink the payload, it silently replaces it with a
// smaller one that hashes and validates like a correct one.
var errInjectedSkillRead = errors.New("injected skill read failure")

// sliceRows is a pgx.Rows over canned column values, so the skill loaders can
// be exercised without a database. Values must match the destination field
// types exactly; Scan assigns them positionally.
type sliceRows struct {
	rows [][]any
	i    int
}

func (r *sliceRows) Close()                                       {}
func (r *sliceRows) Err() error                                   { return nil }
func (r *sliceRows) CommandTag() pgconn.CommandTag                { return pgconn.NewCommandTag("") }
func (r *sliceRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *sliceRows) Values() ([]any, error)                       { return nil, nil }
func (r *sliceRows) RawValues() [][]byte                          { return nil }
func (r *sliceRows) Conn() *pgx.Conn                              { return nil }

func (r *sliceRows) Next() bool {
	if r.i >= len(r.rows) {
		return false
	}
	r.i++
	return true
}

func (r *sliceRows) Scan(dest ...any) error {
	if r.i == 0 || r.i > len(r.rows) {
		return fmt.Errorf("Scan called outside a row")
	}
	values := r.rows[r.i-1]
	if len(values) != len(dest) {
		return fmt.Errorf("scan: %d destinations for %d columns", len(dest), len(values))
	}
	for i, d := range dest {
		dv := reflect.ValueOf(d)
		if dv.Kind() != reflect.Pointer || dv.IsNil() {
			return fmt.Errorf("scan: destination %d is not a non-nil pointer", i)
		}
		sv := reflect.ValueOf(values[i])
		if !sv.IsValid() || !sv.Type().AssignableTo(dv.Elem().Type()) {
			return fmt.Errorf("scan: column %d is %T, destination is %s", i, values[i], dv.Elem().Type())
		}
		dv.Elem().Set(sv)
	}
	return nil
}

// skillReadDBTX serves canned skill / skill_file result sets and can fail one
// chosen query. perSkillFileCalls counts the pre-batch ListSkillFiles query so
// a test can prove the N+1 loop is gone rather than just assuming it.
type skillReadDBTX struct {
	skills           [][]any
	files            [][]any
	failQuery        string
	perSkillFileCall int
}

func (d *skillReadDBTX) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.NewCommandTag(""), nil
}

func (d *skillReadDBTX) QueryRow(context.Context, string, ...any) pgx.Row { return noRow{} }

func (d *skillReadDBTX) Query(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
	// Checked longest-name-first: the batch query's text also contains the
	// per-skill query's name as a prefix.
	switch {
	case strings.Contains(sql, "ListSkillFilesBySkillIDs"):
		if d.failQuery == "ListSkillFilesBySkillIDs" {
			return nil, errInjectedSkillRead
		}
		return &sliceRows{rows: d.files}, nil
	case strings.Contains(sql, "ListSkillFiles"):
		d.perSkillFileCall++
		return &sliceRows{}, nil
	case strings.Contains(sql, "ListAgentSkills"):
		if d.failQuery == "ListAgentSkills" {
			return nil, errInjectedSkillRead
		}
		return &sliceRows{rows: d.skills}, nil
	}
	return nil, fmt.Errorf("unexpected query: %s", sql)
}

// skillRow / skillFileRow build result rows in the column order the generated
// ListAgentSkills / ListSkillFilesBySkillIDs scanners read.
func skillRow(id pgtype.UUID, name, description, content string) []any {
	return []any{
		id, testUUID(0xF0), name, description, content,
		[]byte(nil), pgtype.UUID{}, pgtype.Timestamptz{}, pgtype.Timestamptz{}, pgtype.UUID{},
	}
}

func skillFileRow(skillID pgtype.UUID, path, content string) []any {
	return []any{
		testUUID(0xF1), skillID, path, content,
		pgtype.Timestamptz{}, pgtype.Timestamptz{},
	}
}

// TestLoadAgentSkills_GroupsBatchedFilesBySkillID pins the batch assembly.
// ListAgentSkills orders by name and ListSkillFilesBySkillIDs orders by
// skill_id, so the two result sets are in DIFFERENT orders — the fixture makes
// them disagree deliberately. Grouping must be by skill_id; a positional zip
// of the two would hand each skill another skill's files.
func TestLoadAgentSkills_GroupsBatchedFilesBySkillID(t *testing.T) {
	alpha, beta, gamma := testUUID(3), testUUID(1), testUUID(2)

	fake := &skillReadDBTX{
		// ListAgentSkills order: name ASC.
		skills: [][]any{
			skillRow(alpha, "alpha", "first", "alpha body"),
			skillRow(beta, "beta", "second", "beta body"),
			skillRow(gamma, "gamma", "third", "gamma body"),
		},
		// ListSkillFilesBySkillIDs order: skill_id, path ASC. gamma has none.
		files: [][]any{
			skillFileRow(beta, "a.md", "beta a"),
			skillFileRow(beta, "z.md", "beta z"),
			skillFileRow(alpha, "docs/one.md", "alpha one"),
			skillFileRow(alpha, "docs/two.md", "alpha two"),
		},
	}
	svc := &TaskService{Queries: db.New(fake)}

	got, err := svc.LoadAgentSkills(context.Background(), testUUID(9))
	if err != nil {
		t.Fatalf("LoadAgentSkills: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("loaded %d skills, want 3", len(got))
	}

	// Skill order follows ListAgentSkills, not the file result set.
	wantNames := []string{"alpha", "beta", "gamma"}
	for i, want := range wantNames {
		if got[i].Name != want {
			t.Fatalf("skill %d is %q, want %q", i, got[i].Name, want)
		}
	}

	wantFiles := map[string][]AgentSkillFileData{
		"alpha": {{Path: "docs/one.md", Content: "alpha one"}, {Path: "docs/two.md", Content: "alpha two"}},
		"beta":  {{Path: "a.md", Content: "beta a"}, {Path: "z.md", Content: "beta z"}},
		"gamma": nil,
	}
	for _, skill := range got {
		if !reflect.DeepEqual(skill.Files, wantFiles[skill.Name]) {
			t.Fatalf("skill %q files = %+v, want %+v", skill.Name, skill.Files, wantFiles[skill.Name])
		}
	}
	// A skill with no files keeps the nil (omitempty) list it had before the
	// batch load, not an empty slice that would serialize as `"files": []`.
	if got[2].Files != nil {
		t.Fatalf("skill without files got %v, want nil", got[2].Files)
	}

	if fake.perSkillFileCall != 0 {
		t.Fatalf("per-skill ListSkillFiles ran %d times, want 0 (the N+1 loop is what this replaced)", fake.perSkillFileCall)
	}
}

// TestLoadAgentSkills_ReportsReadFailures is the review regression for #7689:
// batching made ONE query own every skill's files, so swallowing its error
// strips attachments from ALL of them at once — and the caller cannot tell
// that apart from an agent whose skills genuinely have no files, because the
// bundle hash is then computed over the truncated content. Both reads must
// surface the failure instead.
func TestLoadAgentSkills_ReportsReadFailures(t *testing.T) {
	tests := []struct {
		name      string
		failQuery string
	}{
		{name: "batch file query fails", failQuery: "ListSkillFilesBySkillIDs"},
		{name: "agent skill query fails", failQuery: "ListAgentSkills"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fake := &skillReadDBTX{
				skills:    [][]any{skillRow(testUUID(1), "alpha", "first", "alpha body")},
				files:     [][]any{skillFileRow(testUUID(1), "a.md", "alpha a")},
				failQuery: tc.failQuery,
			}
			svc := &TaskService{Queries: db.New(fake)}

			got, err := svc.LoadAgentSkills(context.Background(), testUUID(9))
			if err == nil {
				t.Fatalf("LoadAgentSkills returned nil error and %d skills; a failed read must not look like a smaller skill set", len(got))
			}
			if !errors.Is(err, errInjectedSkillRead) {
				t.Fatalf("error %v does not wrap the injected read failure", err)
			}
			if got != nil {
				t.Fatalf("LoadAgentSkills returned %d skills alongside an error, want none", len(got))
			}
		})
	}
}

// TestLoadAgentSkillBundles_FailsClosedOnReadFailure covers the same failure
// one level up. Returning the built-ins alone would be the worst outcome: a
// well-formed bundle set with valid hashes and refs that the daemon accepts
// and caches, for an agent that is missing every workspace skill it owns.
func TestLoadAgentSkillBundles_FailsClosedOnReadFailure(t *testing.T) {
	fake := &skillReadDBTX{
		skills:    [][]any{skillRow(testUUID(1), "alpha", "first", "alpha body")},
		failQuery: "ListSkillFilesBySkillIDs",
	}
	svc := &TaskService{Queries: db.New(fake)}

	bundles, refs, err := svc.LoadAgentSkillBundles(context.Background(), testUUID(9), "", false)
	if err == nil {
		t.Fatalf("LoadAgentSkillBundles returned nil error with %d bundles / %d refs", len(bundles), len(refs))
	}
	if !errors.Is(err, errInjectedSkillRead) {
		t.Fatalf("error %v does not wrap the injected read failure", err)
	}
	if bundles != nil || refs != nil {
		t.Fatalf("returned %d bundles / %d refs alongside an error, want none", len(bundles), len(refs))
	}
}
