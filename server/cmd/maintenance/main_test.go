package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/maintenance"
)

const testID = "00000000-0000-4000-8000-000000000001"

func testJob(revision int64, status string) *maintenance.Job {
	return &maintenance.Job{ID: testID, Revision: revision, Status: status, NextAllowedAt: time.Now().Add(-time.Second)}
}

func TestRunThousandsOfBatchesAndCompletion(t *testing.T) {
	var advances int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			var body struct{ Revision int64 }
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body.Revision != advances {
				t.Errorf("revision=%d advances=%d", body.Revision, advances)
			}
			advances++
		}
		status := "ready"
		if advances == 2402 {
			status = "completed"
		}
		_ = json.NewEncoder(w).Encode(response{Job: testJob(advances, status)})
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	var out bytes.Buffer
	err := run(context.Background(), []string{"run", "--job", testID, "--max-batches", "3000", "--max-seconds", "30"}, u.Port(), &out, io.Discard)
	if err != nil || advances != 2402 || exitCode(err) != 0 {
		t.Fatalf("advances=%d err=%v", advances, err)
	}
	if !strings.Contains(out.String(), `"status":"completed"`) {
		t.Fatal("missing completion report")
	}
}

func TestRunBudgetsAreIncomplete(t *testing.T) {
	for _, tc := range []struct {
		name      string
		next      time.Time
		batches   string
		wantPosts int
	}{
		{"batch", time.Now().Add(-time.Hour), "1", 1},
		{"time", time.Now().Add(time.Hour), "3000", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			posts := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "POST" {
					posts++
				}
				j := testJob(int64(posts), "ready")
				j.NextAllowedAt = tc.next
				_ = json.NewEncoder(w).Encode(response{Job: j})
			}))
			defer srv.Close()
			u, _ := url.Parse(srv.URL)
			err := run(context.Background(), []string{"run", "--job", testID, "--max-batches", tc.batches, "--max-seconds", "1"}, u.Port(), io.Discard, io.Discard)
			if !errors.Is(err, errIncomplete) || exitCode(err) != 2 || posts != tc.wantPosts {
				t.Fatalf("posts=%d err=%v", posts, err)
			}
		})
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestAdvanceRetriesOriginalRevisionAfterLostResponse(t *testing.T) {
	var revisions []int64
	c := client{base: "http://127.0.0.1", http: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		var body struct{ Revision int64 }
		_ = json.NewDecoder(r.Body).Decode(&body)
		revisions = append(revisions, body.Revision)
		if len(revisions) == 1 {
			return nil, io.ErrUnexpectedEOF
		}
		data, _ := json.Marshal(response{Job: testJob(8, "completed")})
		return &http.Response{StatusCode: 409, Body: io.NopCloser(bytes.NewReader(data)), Header: make(http.Header)}, nil
	})}}
	j, err := c.advance(context.Background(), "/maintenance/jobs/"+testID, testJob(7, "ready"), 1)
	if err != nil || j.Revision != 8 || !reflect.DeepEqual(revisions, []int64{7, 7}) {
		t.Fatalf("revisions=%v job=%+v err=%v", revisions, j, err)
	}
}

func TestRetryableFailureResumesButOperatorPauseStops(t *testing.T) {
	var actions []string
	var revisions []int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		actions = append(actions, r.URL.Path)
		var body struct{ Revision int64 }
		_ = json.NewDecoder(r.Body).Decode(&body)
		revisions = append(revisions, body.Revision)
		switch len(actions) {
		case 1:
			w.WriteHeader(503)
			_ = json.NewEncoder(w).Encode(response{Job: testJob(1, "paused"), Retryable: true})
		case 2:
			_ = json.NewEncoder(w).Encode(response{Job: testJob(2, "ready")})
		case 3:
			_ = json.NewEncoder(w).Encode(response{Job: testJob(3, "completed")})
		default:
			t.Error("unexpected retry")
		}
	}))
	defer srv.Close()
	c := client{base: srv.URL, http: srv.Client(), out: io.Discard}
	j, err := c.advance(context.Background(), "/job", testJob(0, "ready"), 1)
	if err != nil || j.Status != "completed" || !reflect.DeepEqual(actions, []string{"/job/advance", "/job/resume", "/job/advance"}) || !reflect.DeepEqual(revisions, []int64{0, 1, 2}) {
		t.Fatalf("actions=%v revisions=%v err=%v", actions, revisions, err)
	}

	posts := 0
	paused := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			posts++
		}
		_ = json.NewEncoder(w).Encode(response{Job: testJob(1, "paused")})
	}))
	defer paused.Close()
	c.base = paused.URL
	if err := c.drive(context.Background(), "/job", 10, 3, false); err == nil || posts != 0 {
		t.Fatalf("posts=%d err=%v", posts, err)
	}
}

func TestRetryBudgetAndCancellation(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(409)
		_ = json.NewEncoder(w).Encode(response{Error: "busy", Retryable: true})
	}))
	defer srv.Close()
	c := client{base: srv.URL, http: srv.Client()}
	if _, err := c.advance(context.Background(), "/job", testJob(0, "ready"), 1); err == nil || calls != 2 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sleep(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestCommandsAndExplicitLimits(t *testing.T) {
	var paths []string
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		var body map[string]any
		if r.Method == "POST" {
			_ = json.NewDecoder(r.Body).Decode(&body)
		}
		bodies = append(bodies, body)
		_ = json.NewEncoder(w).Encode(response{Job: testJob(4, "paused")})
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	opts := []string{"--batch-size", "500", "--delay-ms", "100", "--lock-timeout-ms", "500", "--statement-timeout-ms", "3000"}
	for _, cmd := range []string{"create", "status", "pause", "resume", "cancel", "configure"} {
		args := []string{cmd}
		if cmd != "create" {
			args = append(args, "--job", testID)
		}
		if cmd != "create" && cmd != "status" {
			args = append(args, "--revision", "3")
		}
		if cmd == "create" {
			args = append(args, "--idempotency-key", "test", "--apply", "--parameters", `{"writers_upgraded":true}`)
		}
		if cmd == "create" || cmd == "configure" {
			args = append(args, opts...)
		}
		if err := run(context.Background(), args, u.Port(), io.Discard, io.Discard); err != nil {
			t.Fatal(err)
		}
	}
	if len(paths) != 6 || paths[0] != "POST /maintenance/jobs" || paths[1] != "GET /maintenance/jobs/"+testID || bodies[0]["dry_run"] != false || bodies[5]["revision"] != float64(3) {
		t.Fatalf("paths=%v bodies=%v", paths, bodies)
	}
	for _, args := range [][]string{
		{"create", "--idempotency-key", "bad", "--apply"},
		{"configure", "--job", testID, "--revision", "3"},
		{"run", "--job", testID},
		{"run", "--job", testID, "--batch-size", "10", "--max-batches", "3000", "--max-seconds", "1"},
		{"run", "--job", testID, "--max-batches", "3000", "--max-seconds", "0"},
		{"cancel", "--job", testID},
		{"status", "--job", "not-a-uuid"},
		{"status", "--job", testID, "--port", "0.0.0.0:6061"},
	} {
		if err := run(context.Background(), args, u.Port(), io.Discard, io.Discard); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	if len(paths) != 6 {
		t.Fatal("invalid arguments reached API")
	}
}
