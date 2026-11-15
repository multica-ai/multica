package qoderruntime

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type leaderTransport func(*http.Request) (*http.Response, error)

func (f leaderTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func pendingLeader(t *testing.T, reply string) *harness {
	t.Helper()
	h := setup(t)
	h.task.IsLeaderTask = true
	h.task.TriggerCommentID = "comment-1"
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.draft(reply)
	return h
}

const leaderDelegation = "OUTCOME: action\nREASON: API work\n[@Bob](mention://agent/bob) fix the handler."

func TestSquadLeaderQuickCreateAssignsSquad(t *testing.T) {
	h := setup(t)
	quickCreateTask(h)
	h.task.SquadID = "squad-1"
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.draft(`{"title":"Settings page","description":"Add settings"}`)
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if h.complete == nil || h.failure != nil || h.issue["assignee_type"] != "squad" || h.issue["assignee_id"] != "squad-1" {
		t.Fatalf("created issue must keep the selected squad: complete=%v failure=%v issue=%v", h.complete, h.failure, h.issue)
	}
}

func TestSquadLeaderLostCommentResponseDoesNotRepost(t *testing.T) {
	h := pendingLeader(t, leaderDelegation)
	base := h.b.multica.client.Transport
	lost := false
	h.b.multica.client.Transport = leaderTransport(func(r *http.Request) (*http.Response, error) {
		resp, err := base.RoundTrip(r)
		if err == nil && !lost && r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/comments") {
			j, journalErr := readJournal(h.b.path, "test")
			if journalErr != nil || len(j.Runs) != 1 || !j.Runs[0].Leader.CommentSubmitted || j.Runs[0].Leader.token != "" {
				t.Fatalf("write intent must be durable and credentials excluded before POST: journal=%+v error=%v", j, journalErr)
			}
			lost = true
			resp.Body.Close()
			return nil, errors.New("response lost after comment was saved")
		}
		return resp, err
	})
	_ = h.b.step(context.Background())
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(h.comments) != 1 || h.failure == nil || h.complete != nil || !strings.Contains(h.failure["error"].(string), "could not be confirmed") {
		t.Fatalf("ambiguous comment must fail without replay: comments=%d failure=%v complete=%v", len(h.comments), h.failure, h.complete)
	}
	if len(h.evaluations) != 1 || h.evaluations[0]["outcome"] != "failed" {
		t.Fatalf("evaluation must report the uncertain delivery: %v", h.evaluations)
	}
}

func TestSquadLeaderReportsBlockedDelegation(t *testing.T) {
	const bobID = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
	const aliceID = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	const reply = "OUTCOME: action\nREASON: API work\n[@Bob[API]](mention://agent/" + bobID + ") fix the handler."
	for _, tc := range []struct {
		name, reply, outcomes, wantTarget string
	}{
		{"agent", reply, `[{"target_type":"agent","target_id":"` + bobID + `","status":"blocked","reason_code":"invocation_not_allowed"}]`, "Bob[API] [" + bobID + "]"},
		{"partial dispatch", reply, `[{"target_type":"agent","target_id":"` + aliceID + `","status":"queued"},{"target_type":"agent","target_id":"` + bobID + `","status":"blocked","reason_code":"invocation_not_allowed"}]`, "Bob[API] [" + bobID + "]"},
		{"squad", reply + "\n[@Platform Squad](mention://squad/" + bobID + ") help.", `[{"target_type":"squad","target_id":"` + bobID + `","status":"blocked","reason_code":"invocation_not_allowed"}]`, "Platform Squad [" + bobID + "]"},
		{"unknown label", reply, `[{"target_type":"agent","target_id":"` + aliceID + `","status":"blocked","reason_code":"invocation_not_allowed"}]`, aliceID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := pendingLeader(t, tc.reply)
			base := h.b.multica.client.Transport
			h.b.multica.client.Transport = leaderTransport(func(r *http.Request) (*http.Response, error) {
				resp, err := base.RoundTrip(r)
				if err == nil && r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/comments") {
					resp.Body.Close()
					resp.Body = io.NopCloser(strings.NewReader(`{"trigger_outcomes":` + tc.outcomes + `}`))
				}
				return resp, err
			})
			if err := h.b.step(context.Background()); err != nil {
				t.Fatal(err)
			}
			if len(h.comments) != 1 || h.complete != nil || h.failure == nil || !strings.Contains(h.failure["error"].(string), "invocation_not_allowed") || !strings.Contains(h.failure["error"].(string), tc.wantTarget) {
				t.Fatalf("blocked delegation must fail visibly: comments=%v complete=%v failure=%v", h.comments, h.complete, h.failure)
			}
			if len(h.evaluations) != 1 || h.evaluations[0]["outcome"] != "failed" || !strings.Contains(h.evaluations[0]["reason"].(string), tc.wantTarget) {
				t.Fatalf("evaluation must identify the blocked target: %v", h.evaluations)
			}
		})
	}
}

func TestSquadLeaderEvaluationRateLimitDoesNotRepostComment(t *testing.T) {
	h := pendingLeader(t, leaderDelegation)
	base := h.b.multica.client.Transport
	limited := false
	h.b.multica.client.Transport = leaderTransport(func(r *http.Request) (*http.Response, error) {
		if !limited && strings.HasSuffix(r.URL.Path, "/squad-evaluated") {
			limited = true
			return &http.Response{StatusCode: 429, Body: io.NopCloser(strings.NewReader(`{}`)), Header: http.Header{}}, nil
		}
		return base.RoundTrip(r)
	})
	if err := h.b.step(context.Background()); err == nil {
		t.Fatal("expected rate limit")
	}
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(h.comments) != 1 || len(h.evaluations) != 1 || h.complete == nil {
		t.Fatalf("retry must record without redelegating: comments=%v evaluations=%v complete=%v", h.comments, h.evaluations, h.complete)
	}
}

func TestSquadLeaderLostEvaluationResponseDoesNotReplay(t *testing.T) {
	h := pendingLeader(t, leaderDelegation)
	base := h.b.multica.client.Transport
	lost := false
	h.b.multica.client.Transport = leaderTransport(func(r *http.Request) (*http.Response, error) {
		resp, err := base.RoundTrip(r)
		if err == nil && !lost && strings.HasSuffix(r.URL.Path, "/squad-evaluated") {
			lost = true
			resp.Body.Close()
			return nil, errors.New("response lost after evaluation was saved")
		}
		return resp, err
	})
	_ = h.b.step(context.Background())
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if h.complete != nil || h.failure == nil || len(h.comments) != 1 || len(h.evaluations) != 1 {
		t.Fatalf("unconfirmed evaluation must not replay writes: complete=%v failure=%v comments=%v evaluations=%v", h.complete, h.failure, h.comments, h.evaluations)
	}
}

func TestSquadLeaderRejectedNoActionEvaluationFailsVisibly(t *testing.T) {
	h := pendingLeader(t, "OUTCOME: no_action\nREASON: progress only")
	base := h.b.multica.client.Transport
	h.b.multica.client.Transport = leaderTransport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/squad-evaluated") {
			return &http.Response{StatusCode: 403, Body: io.NopCloser(strings.NewReader(`{}`)), Header: http.Header{}}, nil
		}
		return base.RoundTrip(r)
	})
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if h.complete != nil || h.failure == nil || len(h.comments) != 0 || !strings.Contains(h.failure["error"].(string), "evaluation") {
		t.Fatalf("refused evaluation must not silently complete: complete=%v failure=%v comments=%v", h.complete, h.failure, h.comments)
	}
}

func TestSquadLeaderCompletedDeliverySurvivesRestart(t *testing.T) {
	h := pendingLeader(t, leaderDelegation)
	base := h.b.multica.client.Transport
	refused := false
	h.b.multica.client.Transport = leaderTransport(func(r *http.Request) (*http.Response, error) {
		if !refused && strings.HasSuffix(r.URL.Path, "/complete") {
			refused = true
			return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader(`{}`)), Header: http.Header{}}, nil
		}
		return base.RoundTrip(r)
	})
	if err := h.b.step(context.Background()); err == nil {
		t.Fatal("expected terminal report error")
	}
	j, err := readJournal(h.b.path, "test")
	if err != nil {
		t.Fatal(err)
	}
	h.b.state = j
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if h.complete == nil || h.failure != nil || len(h.comments) != 1 || len(h.evaluations) != 1 {
		t.Fatalf("restart must only retry completion: complete=%v failure=%v comments=%v evaluations=%v", h.complete, h.failure, h.comments, h.evaluations)
	}
}
