package mention

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// mockResolver implements Resolver for testing.
type mockResolver struct {
	prefix string
	issues map[int32]db.Issue
}

func (m *mockResolver) GetWorkspace(_ context.Context, _ pgtype.UUID) (db.Workspace, error) {
	return db.Workspace{IssuePrefix: m.prefix}, nil
}

func (m *mockResolver) GetIssueByNumber(_ context.Context, arg db.GetIssueByNumberParams) (db.Issue, error) {
	if issue, ok := m.issues[arg.Number]; ok {
		return issue, nil
	}
	return db.Issue{}, fmt.Errorf("not found")
}

func makeUUID(id string) pgtype.UUID {
	var u pgtype.UUID
	u.Valid = true
	// Simple deterministic UUID from a short string for testing.
	copy(u.Bytes[:], []byte(fmt.Sprintf("%-16s", id)))
	return u
}

func TestExpandIssueIdentifiers(t *testing.T) {
	ctx := context.Background()
	wsID := makeUUID("ws1")
	issueID := makeUUID("issue117")

	resolver := &mockResolver{
		prefix: "MUL",
		issues: map[int32]db.Issue{
			117: {ID: issueID, Number: 117},
		},
	}

	tests := []struct {
		name    string
		input   string
		want    string
	}{
		{
			name:  "basic replacement",
			input: "See MUL-117 for details",
			want:  "See [MUL-117](mention://issue/" + uuidToString(issueID) + ") for details",
		},
		{
			name:  "at start of line",
			input: "MUL-117 is important",
			want:  "[MUL-117](mention://issue/" + uuidToString(issueID) + ") is important",
		},
		{
			name:  "at end of line",
			input: "Check out MUL-117",
			want:  "Check out [MUL-117](mention://issue/" + uuidToString(issueID) + ")",
		},
		{
			name:  "already a mention link",
			input: "[MUL-117](mention://issue/some-id)",
			want:  "[MUL-117](mention://issue/some-id)",
		},
		{
			name:  "inside inline code",
			input: "Run `MUL-117` to test",
			want:  "Run `MUL-117` to test",
		},
		{
			name:  "inside fenced code block",
			input: "```\nMUL-117\n```",
			want:  "```\nMUL-117\n```",
		},
		{
			name:  "non-existent issue unchanged",
			input: "See MUL-999 for details",
			want:  "See MUL-999 for details",
		},
		{
			name:  "no match",
			input: "No issues here",
			want:  "No issues here",
		},
		{
			name:  "already a markdown link text",
			input: "[MUL-117](https://example.com)",
			want:  "[MUL-117](https://example.com)",
		},
		{
			name:  "multiple references",
			input: "MUL-117 and also MUL-117 again",
			want:  "[MUL-117](mention://issue/" + uuidToString(issueID) + ") and also [MUL-117](mention://issue/" + uuidToString(issueID) + ") again",
		},
		{
			name:  "with parentheses",
			input: "(MUL-117)",
			want:  "([MUL-117](mention://issue/" + uuidToString(issueID) + "))",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExpandIssueIdentifiers(ctx, resolver, wsID, tt.input)
			if got != tt.want {
				t.Errorf("ExpandIssueIdentifiers() =\n  %q\nwant:\n  %q", got, tt.want)
			}
		})
	}
}
