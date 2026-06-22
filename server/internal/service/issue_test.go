package service

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestComputeIssueChanges_NoChanges(t *testing.T) {
	now := time.Now()
	issue := db.Issue{
		Title:        "same",
		Status:       "todo",
		Priority:     "high",
		AssigneeType: pgtype.Text{String: "agent", Valid: true},
		AssigneeID:   pgtype.UUID{Bytes: [16]byte{1, 2, 3}, Valid: true},
		Description:  pgtype.Text{String: "desc", Valid: true},
		StartDate:    pgtype.Date{Time: now, Valid: true},
		DueDate:      pgtype.Date{Valid: false},
	}
	ch := computeIssueChanges(issue, issue)
	if ch.AssigneeChanged || ch.StatusChanged || ch.PriorityChanged ||
		ch.TitleChanged || ch.DescriptionChanged ||
		ch.StartDateChanged || ch.DueDateChanged {
		t.Fatalf("expected no changes, got %+v", ch)
	}
}

func TestComputeIssueChanges_StatusChanged(t *testing.T) {
	prev := db.Issue{Status: "todo"}
	cur := db.Issue{Status: "done"}
	ch := computeIssueChanges(prev, cur)
	if !ch.StatusChanged {
		t.Fatal("expected StatusChanged=true")
	}
}

func TestComputeIssueChanges_AssigneeChanged(t *testing.T) {
	t.Run("type_change", func(t *testing.T) {
		prev := db.Issue{
			AssigneeType: pgtype.Text{String: "agent", Valid: true},
			AssigneeID:   pgtype.UUID{Bytes: [16]byte{1}, Valid: true},
		}
		cur := db.Issue{
			AssigneeType: pgtype.Text{String: "member", Valid: true},
			AssigneeID:   pgtype.UUID{Bytes: [16]byte{1}, Valid: true},
		}
		ch := computeIssueChanges(prev, cur)
		if !ch.AssigneeChanged {
			t.Fatal("expected AssigneeChanged=true on type change")
		}
	})
	t.Run("id_change", func(t *testing.T) {
		prev := db.Issue{
			AssigneeType: pgtype.Text{String: "agent", Valid: true},
			AssigneeID:   pgtype.UUID{Bytes: [16]byte{1}, Valid: true},
		}
		cur := db.Issue{
			AssigneeType: pgtype.Text{String: "agent", Valid: true},
			AssigneeID:   pgtype.UUID{Bytes: [16]byte{2}, Valid: true},
		}
		ch := computeIssueChanges(prev, cur)
		if !ch.AssigneeChanged {
			t.Fatal("expected AssigneeChanged=true on id change")
		}
	})
}

func TestComputeIssueChanges_DescriptionChanged(t *testing.T) {
	t.Run("same_text", func(t *testing.T) {
		prev := db.Issue{Description: pgtype.Text{String: "hello", Valid: true}}
		cur := db.Issue{Description: pgtype.Text{String: "hello", Valid: true}}
		ch := computeIssueChanges(prev, cur)
		if ch.DescriptionChanged {
			t.Fatal("expected DescriptionChanged=false when text is identical")
		}
	})
	t.Run("different_text", func(t *testing.T) {
		prev := db.Issue{Description: pgtype.Text{String: "hello", Valid: true}}
		cur := db.Issue{Description: pgtype.Text{String: "world", Valid: true}}
		ch := computeIssueChanges(prev, cur)
		if !ch.DescriptionChanged {
			t.Fatal("expected DescriptionChanged=true when text differs")
		}
	})
	t.Run("cleared", func(t *testing.T) {
		prev := db.Issue{Description: pgtype.Text{String: "hello", Valid: true}}
		cur := db.Issue{Description: pgtype.Text{Valid: false}}
		ch := computeIssueChanges(prev, cur)
		if !ch.DescriptionChanged {
			t.Fatal("expected DescriptionChanged=true when description cleared")
		}
	})
}

func TestComputeIssueChanges_DateChanged(t *testing.T) {
	day1 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	day2 := time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC)

	t.Run("start_date_changed", func(t *testing.T) {
		prev := db.Issue{StartDate: pgtype.Date{Time: day1, Valid: true}}
		cur := db.Issue{StartDate: pgtype.Date{Time: day2, Valid: true}}
		ch := computeIssueChanges(prev, cur)
		if !ch.StartDateChanged {
			t.Fatal("expected StartDateChanged=true")
		}
	})
	t.Run("start_date_cleared", func(t *testing.T) {
		prev := db.Issue{StartDate: pgtype.Date{Time: day1, Valid: true}}
		cur := db.Issue{StartDate: pgtype.Date{Valid: false}}
		ch := computeIssueChanges(prev, cur)
		if !ch.StartDateChanged {
			t.Fatal("expected StartDateChanged=true when cleared")
		}
	})
}
