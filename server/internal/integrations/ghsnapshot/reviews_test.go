package ghsnapshot

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func reviewEntry(id int, state, login, kind string) map[string]any {
	return map[string]any{"id": id, "state": state, "submitted_at": "2026-11-03T01:00:00Z",
		"user": map[string]any{"login": login, "type": kind}, "body": "private feedback"}
}

func TestFetchPRReviewsPaginatesAndFiltersIdentities(t *testing.T) {
	pages := 0
	srv := snapshotServer(t, func(w http.ResponseWriter, r *http.Request) {
		pages++
		if r.URL.Path != "/repos/o/r/pulls/5/reviews" || r.URL.Query().Get("per_page") != "100" {
			t.Errorf("review request = %s", r.URL)
		}
		entries := []map[string]any{}
		if r.URL.Query().Get("page") == "1" {
			for i := 1; i <= 100; i++ {
				entries = append(entries, reviewEntry(i, "APPROVED", "robot", "Bot"))
			}
		} else {
			entries = append(entries,
				reviewEntry(101, "APPROVED", "reviewer", "User"),
				reviewEntry(102, "CHANGES_REQUESTED", "reviewer", "User"),
				reviewEntry(103, "COMMENTED", "reviewer", "User"),
				reviewEntry(104, "APPROVED", "AUTHOR", "User"),
				reviewEntry(105, "DISMISSED", "reviewer", "User"),
				reviewEntry(106, "PENDING", "reviewer", "User"),
				reviewEntry(107, "APPROVED", "unknown", ""),
				reviewEntry(108, "APPROVED", "", "User"))
		}
		_ = json.NewEncoder(w).Encode(entries)
	})
	defer srv.Close()
	reviews, err := fetchPRReviews(context.Background(), newTestClient(t, srv.URL), 1, "/repos/o/r/pulls/5/reviews", "author")
	if err != nil || pages != 2 || len(reviews) != 2 || reviews[0].ID != 101 || reviews[1].State != "CHANGES_REQUESTED" {
		t.Fatalf("reviews=%+v pages=%d err=%v", reviews, pages, err)
	}
}

func TestFetchPRReviewsRejectsPartialOrMalformedResults(t *testing.T) {
	for _, failure := range []string{"missing author", "request failed", "null", "not array", "duplicate", "missing timestamp", "invalid ID", "page limit"} {
		t.Run(failure, func(t *testing.T) {
			srv := snapshotServer(t, func(w http.ResponseWriter, r *http.Request) {
				switch failure {
				case "request failed":
					w.WriteHeader(http.StatusForbidden)
				case "null":
					_, _ = w.Write([]byte(`null`))
				case "not array":
					_, _ = w.Write([]byte(`{}`))
				default:
					entry := reviewEntry(1, "APPROVED", "reviewer", "User")
					if failure == "missing timestamp" {
						delete(entry, "submitted_at")
					}
					if failure == "invalid ID" {
						entry["id"] = 0
					}
					entries := []map[string]any{entry}
					if failure == "duplicate" {
						entries = append(entries, entry)
					}
					if failure == "page limit" {
						var page int
						_, _ = fmt.Sscan(r.URL.Query().Get("page"), &page)
						entries = nil
						for i := 1; i <= 100; i++ {
							entries = append(entries, reviewEntry(page*100+i, "APPROVED", "reviewer", "User"))
						}
					}
					_ = json.NewEncoder(w).Encode(entries)
				}
			})
			defer srv.Close()
			author := "author"
			if failure == "missing author" {
				author = ""
			}
			reviews, err := fetchPRReviews(context.Background(), newTestClient(t, srv.URL), 1, "/repos/o/r/pulls/5/reviews", author)
			if err == nil || reviews != nil {
				t.Fatalf("partial reviews=%+v err=%v", reviews, err)
			}
		})
	}
}

func TestApplyReviewSnapshotIsAtomicAndHeadGuarded(t *testing.T) {
	pool := testDBPool(t)
	q := db.New(pool)
	pr := seedPR(t, pool, q, "a")
	defer func() { _, _ = pool.Exec(context.Background(), "DELETE FROM github_pull_request WHERE id=$1", pr.ID) }()
	m := NewManager(nil, q, pool, nil)
	snap := &PRSnapshot{HeadSHA: "a", Mergeable: "MERGEABLE", Reviews: []PRReview{{ID: 1, State: "APPROVED", SubmittedAt: time.Now()}}}
	if ok, err := m.applySnapshot(context.Background(), pr.ID, snap); err != nil || !ok {
		t.Fatalf("apply: %t %v", ok, err)
	}
	if _, err := pool.Exec(context.Background(), "UPDATE github_pull_request SET head_sha='b' WHERE id=$1", pr.ID); err != nil {
		t.Fatal(err)
	}
	snap.Reviews = nil
	if ok, err := m.applySnapshot(context.Background(), pr.ID, snap); err != nil || ok {
		t.Fatalf("old head applied: %t %v", ok, err)
	}
	var raw []byte
	if err := pool.QueryRow(context.Background(), "SELECT review_snapshot FROM github_pull_request WHERE id=$1", pr.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var reviews []PRReview
	if err := json.Unmarshal(raw, &reviews); err != nil || len(reviews) != 1 || reviews[0].ID != 1 {
		t.Fatalf("stale response overwrote reviews: %s %v", raw, err)
	}
	snap.HeadSHA = "b"
	if ok, err := m.applySnapshot(context.Background(), pr.ID, snap); err != nil || !ok {
		t.Fatalf("empty snapshot apply: %t %v", ok, err)
	}
	if err := pool.QueryRow(context.Background(), "SELECT review_snapshot FROM github_pull_request WHERE id=$1", pr.ID).Scan(&raw); err != nil || string(raw) != "[]" {
		t.Fatalf("empty result must replace dismissed reviews with []: %s %v", raw, err)
	}
}
