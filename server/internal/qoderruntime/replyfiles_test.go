package qoderruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type qoderFile struct {
	data         string
	downloadable bool
	// scope is the owning session; empty means the run's own session.
	scope string
}

type replyUpload struct {
	name, taskID, data string
	afterComplete      bool
}

func chatTask(h *harness) {
	h.task.IssueID = ""
	h.task.ChatSessionID = "chat-1"
	h.task.ChatMessage = "Write the report"
}

// deliver appends an agent.artifact_delivered event and the file Qoder keeps.
func (h *harness) deliver(eventID, fileID, name, data string, size int, downloadable bool) {
	if h.qoderFiles == nil {
		h.qoderFiles = map[string]qoderFile{}
	}
	h.qoderFiles[fileID] = qoderFile{data: data, downloadable: downloadable}
	h.events = append(h.events, map[string]any{"id": eventID, "type": "agent.artifact_delivered", "file_id": fileID, "original_filename": name, "size": size, "content_type": "application/octet-stream"})
}

// stepUntilDone steps the bridge until the run is reported, as the ticker would.
func (h *harness) stepUntilDone(t *testing.T) {
	t.Helper()
	for i := 0; i < 40 && h.complete == nil && h.failure == nil; i++ {
		if err := h.b.step(context.Background()); err != nil {
			t.Logf("step %d: %v", i, err)
		}
	}
	if h.complete == nil {
		t.Fatalf("run not completed: %#v", h.failure)
	}
}

func TestChatReplyCarriesDeliveredFiles(t *testing.T) {
	h := setup(t)
	chatTask(h)
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.prompt, "A file over 10 MB is not attached; tell the user so instead of saying it was sent.") {
		t.Fatalf("prompt does not describe reply files: %s", h.prompt)
	}
	h.deliver("evt_a1", "file_report", "report.pdf", "%PDF-1.7", 8, true)
	h.deliver("evt_a2", "file_data", "data.csv", "a,b\n1,2\n", 8, true)
	// Delivering the same file again does not attach it twice.
	h.deliver("evt_a3", "file_report", "report.pdf", "%PDF-1.7", 8, true)
	h.deliver("evt_a4", "file_internal", "scratch.bin", "x", 1, false)
	h.deliver("evt_a5", "file_big", "big.zip", "", maxReplyFileBytes+1, true)
	// A file of another session, such as another chat's upload, is not passed on.
	h.deliver("evt_a6", "file_other", "other.txt", "secret", 6, true)
	f := h.qoderFiles["file_other"]
	f.scope = "sess_other"
	h.qoderFiles["file_other"] = f
	h.finish("end_turn")
	h.stepUntilDone(t)
	want := []replyUpload{{"report.pdf", "task-1", "%PDF-1.7", false}, {"data.csv", "task-1", "a,b\n1,2\n", false}}
	if fmt.Sprint(h.replyUploads) != fmt.Sprint(want) {
		t.Fatalf("reply uploads: %#v", h.replyUploads)
	}
	if !strings.Contains(fmt.Sprint(h.complete["output"]), "Tests passed") {
		t.Fatalf("result: %#v", h.complete)
	}
}

func TestReplyFileUploadFailuresAreNotRetried(t *testing.T) {
	h := setup(t)
	chatTask(h)
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.deliver("evt_a1", "file_report", "report.pdf", "%PDF-1.7", 8, true)
	h.deliver("evt_a2", "file_data", "data.csv", "a,b", 3, true)
	h.deliver("evt_a3", "file_notes", "notes.txt", "notes", 5, true)
	// Neither an ambiguous 502 nor a definite 400 replays the upload. The
	// next file must still arrive, with notices for both skipped files.
	h.replyUploadStatus = []int{502, 400}
	h.finish("end_turn")
	h.stepUntilDone(t)
	if len(h.replyUploads) != 1 || h.replyUploads[0].name != "notes.txt" {
		t.Fatalf("reply uploads: %#v", h.replyUploads)
	}
	output := fmt.Sprint(h.complete["output"])
	if !strings.Contains(output, "report.pdf") || !strings.Contains(output, "data.csv") || strings.Contains(output, "notes.txt") {
		t.Fatalf("wrong failed-file notice: %s", output)
	}
}

func TestReplyFilesAfterRestartCompleteWithoutThem(t *testing.T) {
	h := setup(t)
	chatTask(h)
	ctx := context.Background()
	if err := h.b.step(ctx); err != nil {
		t.Fatal(err)
	}
	h.deliver("evt_a1", "file_report", "report.pdf", "%PDF-1.7", 8, true)
	j, err := readJournal(h.b.path, "test")
	if err != nil {
		t.Fatal(err)
	}
	h.b.state = j
	h.finish("end_turn")
	h.stepUntilDone(t)
	if len(h.replyUploads) != 0 {
		t.Fatalf("uploaded without the task credential: %#v", h.replyUploads)
	}
	if output := fmt.Sprint(h.complete["output"]); !strings.Contains(output, "report.pdf") || !strings.Contains(output, "not attached") {
		t.Fatalf("restart did not disclose missing file: %s", output)
	}
}

func TestIssueRunDoesNotAttachDeliveredFiles(t *testing.T) {
	h := setup(t)
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(h.prompt, "DeliverArtifacts") {
		t.Fatalf("issue prompt promises reply files: %s", h.prompt)
	}
	h.deliver("evt_a1", "file_report", "report.pdf", "%PDF-1.7", 8, true)
	h.finish("end_turn")
	h.stepUntilDone(t)
	if len(h.replyUploads) != 0 {
		t.Fatalf("issue run uploaded reply files: %#v", h.replyUploads)
	}
}

type replyTransport func(*http.Request) (*http.Response, error)

func (f replyTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func replyResponse(r *http.Request, status int, body string) *http.Response {
	if r.Body != nil {
		r.Body.Close()
	}
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}
}

func TestReplyFileReadFailuresAreBounded(t *testing.T) {
	for _, stage := range []string{"metadata", "content", "download", "timeout"} {
		t.Run(stage, func(t *testing.T) {
			h := setup(t)
			chatTask(h)
			if err := h.b.step(context.Background()); err != nil {
				t.Fatal(err)
			}
			h.deliver("evt_a1", "file_report", "report.pdf", "%PDF-1.7", 8, true)
			h.deliver("evt_a2", "file_data", "data.csv", "a,b", 3, true)
			h.finish("end_turn")
			upstream := h.b.qoder.client.Transport
			attempts := 0
			h.b.qoder.client.Transport = replyTransport(func(req *http.Request) (*http.Response, error) {
				if stage == "metadata" && req.URL.Path == "/files/file_report" || stage == "content" && req.URL.Path == "/files/file_report/content" || (stage == "download" || stage == "timeout") && req.URL.Path == "/blob/file_report" {
					attempts++
					if stage == "timeout" {
						return nil, context.DeadlineExceeded
					}
					return replyResponse(req, 502, `{}`), nil
				}
				return upstream.RoundTrip(req)
			})
			// Enter final first, then expire the execution deadline: attachment
			// failures need their own bound even after remote execution has ended.
			_ = h.b.step(context.Background())
			h.b.state.Runs[0].Deadline = time.Now().Add(-time.Hour)
			h.stepUntilDone(t)
			if attempts != 3 || len(h.replyUploads) != 1 || h.replyUploads[0].name != "data.csv" {
				t.Fatalf("attempts=%d uploads=%v", attempts, h.replyUploads)
			}
			if !strings.Contains(fmt.Sprint(h.complete["output"]), "report.pdf") {
				t.Fatalf("missing failed-file notice: %v", h.complete)
			}
		})
	}
}

func TestReplyFileLostUploadAcknowledgementIsNotReplayed(t *testing.T) {
	h := setup(t)
	chatTask(h)
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.deliver("evt_a1", "file_report", "report.pdf", "%PDF-1.7", 8, true)
	h.deliver("evt_a2", "file_data", "data.csv", "a,b", 3, true)
	h.finish("end_turn")
	upstream := h.b.multica.client.Transport
	lost := false
	h.b.multica.client.Transport = replyTransport(func(req *http.Request) (*http.Response, error) {
		resp, err := upstream.RoundTrip(req)
		if err == nil && req.URL.Path == "/api/upload-file" && !lost {
			lost = true
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			return nil, errors.New("lost acknowledgement after upload committed")
		}
		return resp, err
	})
	h.stepUntilDone(t)
	if len(h.replyUploads) != 2 || h.replyUploads[0].name != "report.pdf" || h.replyUploads[1].name != "data.csv" {
		t.Fatalf("upload replayed or next file lost: %v", h.replyUploads)
	}
	output := fmt.Sprint(h.complete["output"])
	if !strings.Contains(output, "report.pdf") || !strings.Contains(output, "could not be confirmed") {
		t.Fatalf("missing ambiguous-delivery notice: %s", output)
	}
}

func TestReplyFileUploadRequiresAttachmentID(t *testing.T) {
	for _, body := range []string{`{"id":"","url":"https://storage.example/report.pdf"}`, `{"id":" "}`, `{}`, `not json`} {
		t.Run(body, func(t *testing.T) {
			h := setup(t)
			chatTask(h)
			if err := h.b.step(context.Background()); err != nil {
				t.Fatal(err)
			}
			h.deliver("evt_a1", "file_report", "report.pdf", "%PDF-1.7", 8, true)
			h.finish("end_turn")
			upstream := h.b.multica.client.Transport
			attempts := 0
			h.b.multica.client.Transport = replyTransport(func(req *http.Request) (*http.Response, error) {
				if req.URL.Path == "/api/upload-file" {
					attempts++
					return replyResponse(req, 200, body), nil
				}
				return upstream.RoundTrip(req)
			})
			h.stepUntilDone(t)
			if attempts != 1 || !strings.Contains(fmt.Sprint(h.complete["output"]), "report.pdf") {
				t.Fatalf("attempts=%d complete=%v", attempts, h.complete)
			}
		})
	}
}

func TestReplyFileRefusalAddsDeliveryNotice(t *testing.T) {
	h := setup(t)
	chatTask(h)
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.deliver("evt_a1", "file_report", "report.pdf", "%PDF-1.7", 8, false)
	h.finish("end_turn")
	h.events[len(h.events)-2]["content"] = []map[string]string{{"type": "text", "text": "Report attached."}}
	h.stepUntilDone(t)
	output := fmt.Sprint(h.complete["output"])
	if len(h.replyUploads) != 0 || !strings.HasPrefix(output, "Report attached.") || !strings.Contains(output, "report.pdf") || !strings.Contains(output, "not attached") {
		t.Fatalf("missing truthful delivery notice: %s", output)
	}
}

func TestReplyFileReadRecoversWithoutDeliveryNotice(t *testing.T) {
	h := setup(t)
	chatTask(h)
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.deliver("evt_a1", "file_report", "report.pdf", "%PDF-1.7", 8, true)
	h.finish("end_turn")
	upstream := h.b.qoder.client.Transport
	attempts := 0
	h.b.qoder.client.Transport = replyTransport(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/files/file_report" {
			attempts++
			if attempts == 1 {
				return replyResponse(req, 502, `{}`), nil
			}
		}
		return upstream.RoundTrip(req)
	})
	h.stepUntilDone(t)
	if attempts != 2 || len(h.replyUploads) != 1 || h.complete["output"] != "Tests passed. PR: https://example.com/pr/1" {
		t.Fatalf("read recovery: attempts=%d uploaded=%v complete=%v", attempts, h.replyUploads, h.complete)
	}
}

func TestReplyFileTransferSharesOneTimeBudget(t *testing.T) {
	h := setup(t)
	chatTask(h)
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.deliver("evt_a1", "file_report", "report.pdf", "%PDF-1.7", 8, true)
	h.finish("end_turn")
	var deadlines []time.Time
	for _, api := range []*api{h.b.qoder, h.b.multica} {
		upstream := api.client.Transport
		api.client.Transport = replyTransport(func(req *http.Request) (*http.Response, error) {
			if strings.HasPrefix(req.URL.Path, "/files/") || strings.HasPrefix(req.URL.Path, "/blob/") || req.URL.Path == "/api/upload-file" {
				deadline, ok := req.Context().Deadline()
				if !ok || time.Until(deadline) > 20*time.Second {
					t.Errorf("file request has no 20-second budget: %v %v", deadline, ok)
				}
				deadlines = append(deadlines, deadline)
			}
			return upstream.RoundTrip(req)
		})
	}
	h.stepUntilDone(t)
	if len(deadlines) != 4 || len(h.replyUploads) != 1 {
		t.Fatalf("file requests=%d uploads=%v", len(deadlines), h.replyUploads)
	}
	for _, deadline := range deadlines {
		if !deadline.Equal(deadlines[0]) {
			t.Fatalf("transfer reset its time budget: %v", deadlines)
		}
	}
}

func TestReplyFileParentDeadlineStillBoundsReadRetries(t *testing.T) {
	h := setup(t)
	chatTask(h)
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.deliver("evt_a1", "file_report", "report.pdf", "%PDF-1.7", 8, true)
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	r := h.b.state.Runs[0]
	upstream := h.b.qoder.client.Transport
	attempts := 0
	h.b.qoder.client.Transport = replyTransport(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/files/file_report" {
			attempts++
			<-req.Context().Done()
			return nil, req.Context().Err()
		}
		return upstream.RoundTrip(req)
	})
	// A busy poll can have less time left than the file's own budget. These
	// deadlines must count as failed attempts too, or a slow file retries forever.
	for i := 0; i < 3; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		_, err := h.b.attachReplyFile(ctx, r)
		cancel()
		if err != nil && !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
	}
	if attempts != 3 || r.Reply.Handled != 1 {
		t.Fatalf("parent deadlines not bounded: attempts=%d handled=%d", attempts, r.Reply.Handled)
	}
	h.finish("end_turn")
	h.stepUntilDone(t)
	if !strings.Contains(fmt.Sprint(h.complete["output"]), "report.pdf") {
		t.Fatalf("missing timeout notice: %v", h.complete)
	}
}

func TestReplyFileNoticeSurvivesRestartAndCompletionRetry(t *testing.T) {
	h := setup(t)
	chatTask(h)
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.deliver("evt_a1", "file_report", "report.pdf", "%PDF-1.7", 8, false)
	h.finish("end_turn")
	upstream := h.b.multica.client.Transport
	var first map[string]any
	h.b.multica.client.Transport = replyTransport(func(req *http.Request) (*http.Response, error) {
		if strings.HasSuffix(req.URL.Path, "/complete") {
			if err := json.NewDecoder(req.Body).Decode(&first); err != nil {
				t.Error(err)
			}
			return replyResponse(req, 502, `{}`), nil
		}
		return upstream.RoundTrip(req)
	})
	// The poll that observes end_turn schedules the transfer for its own step.
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := h.b.step(context.Background()); err == nil {
		t.Fatal("completion failure was not injected")
	}
	j, err := readJournal(h.b.path, "test")
	if err != nil {
		t.Fatal(err)
	}
	if j.Runs[0].Reply.token != "" {
		t.Fatal("task credential persisted")
	}
	h.b.state = j
	h.b.multica.client.Transport = upstream
	h.stepUntilDone(t)
	output := fmt.Sprint(h.complete["output"])
	if output != first["output"] || strings.Count(output, "Attachment delivery notice:") != 1 || !strings.Contains(output, "report.pdf") || len(h.replyUploads) != 0 {
		t.Fatalf("notice changed after retry/restart: first=%v final=%v uploads=%v", first, h.complete, h.replyUploads)
	}
}

func TestReplyFileTransferHasItsOwnGuardedStep(t *testing.T) {
	h := setup(t)
	chatTask(h)
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.deliver("evt_a1", "file_report", "report.pdf", "%PDF-1.7", 8, true)
	h.finish("end_turn")
	guards, released, transferGuard := 0, 0, 0
	h.b.cfg.BeforeStep = func(context.Context) (func(), error) {
		guards++
		return func() { released++ }, nil
	}
	upstream := h.b.qoder.client.Transport
	h.b.qoder.client.Transport = replyTransport(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/files/file_report" {
			transferGuard = guards
			if released != 1 {
				t.Errorf("transfer started before the event poll released its guard: %d", released)
			}
			deadline, ok := req.Context().Deadline()
			if !ok || time.Until(deadline) < 19*time.Second {
				t.Errorf("transfer did not receive its full budget: %v %v", deadline, ok)
			}
		}
		return upstream.RoundTrip(req)
	})
	if err := h.b.guardedStep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(h.replyUploads) != 0 {
		t.Fatal("file transfer shared the event poll's time budget")
	}
	if err := h.b.guardedStep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if transferGuard != 2 || guards != 2 || released != 2 || len(h.replyUploads) != 1 || h.complete == nil {
		t.Fatalf("guards=%d releases=%d transfer=%d uploads=%v completed=%v", guards, released, transferGuard, h.replyUploads, h.complete)
	}
}

func TestReplyFileStepRechecksCancellation(t *testing.T) {
	h := setup(t)
	chatTask(h)
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.deliver("evt_a1", "file_report", "report.pdf", "%PDF-1.7", 8, true)
	h.finish("end_turn")
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.status = "cancelled"
	upstream := h.b.qoder.client.Transport
	h.b.qoder.client.Transport = replyTransport(func(req *http.Request) (*http.Response, error) {
		if strings.HasPrefix(req.URL.Path, "/files/") {
			t.Error("cancelled run started a file transfer")
		}
		return upstream.RoundTrip(req)
	})
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(h.replyUploads) != 0 || len(h.b.state.Runs) != 0 || h.cancelAcks != 1 {
		t.Fatalf("cancelled file step: uploads=%v runs=%d acks=%d", h.replyUploads, len(h.b.state.Runs), h.cancelAcks)
	}
}

func TestConcurrentReplyFilesUseSeparatePolls(t *testing.T) {
	a, b := newTask("task-a", ""), newTask("task-b", "")
	a.IssueID, b.IssueID = "", ""
	a.ChatSessionID, b.ChatSessionID = "chat-a", "chat-b"
	a.ChatMessage, b.ChatMessage = "Make a report", "Make another report"
	h := setupMulti(t, 2, a, b)
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, session := range []string{"sess_new_1", "sess_new_2"} {
		h.sessions[session] = append(h.sessions[session], map[string]any{"id": "artifact_" + session, "type": "agent.artifact_delivered", "file_id": "file_" + session, "original_filename": "report.txt", "size": 3})
		h.finish(session)
	}
	upstreamQoder := h.b.qoder.client.Transport
	h.b.qoder.client.Transport = replyTransport(func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.HasPrefix(req.URL.Path, "/files/"):
			id, content := strings.CutSuffix(strings.TrimPrefix(req.URL.Path, "/files/"), "/content")
			if content {
				return replyResponse(req, 200, fmt.Sprintf(`{"url":%q}`, h.b.qoder.base+"/blob/"+id)), nil
			}
			return replyResponse(req, 200, fmt.Sprintf(`{"scope":{"id":%q}}`, strings.TrimPrefix(id, "file_"))), nil
		case strings.HasPrefix(req.URL.Path, "/blob/"):
			return replyResponse(req, 200, "abc"), nil
		}
		return upstreamQoder.RoundTrip(req)
	})
	upstreamMultica := h.b.multica.client.Transport
	var uploaded []string
	h.b.multica.client.Transport = replyTransport(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/api/upload-file" {
			if err := req.ParseMultipartForm(1 << 20); err != nil {
				t.Fatal(err)
			}
			defer req.MultipartForm.RemoveAll()
			uploaded = append(uploaded, req.FormValue("task_id"))
			return replyResponse(req, 200, `{"id":"attachment"}`), nil
		}
		return upstreamMultica.RoundTrip(req)
	})
	for poll, wantUploads := range []int{0, 1, 1, 2} {
		if err := h.b.guardedStep(context.Background()); err != nil {
			t.Fatal(err)
		}
		if len(uploaded) != wantUploads {
			t.Fatalf("poll %d: uploads=%v, want %d; normal polls and transfers must alternate", poll, uploaded, wantUploads)
		}
	}
	if uploaded[0] == uploaded[1] || len(h.completed) != 2 {
		t.Fatalf("concurrent file replies lost fairness: uploads=%v completed=%v", uploaded, h.completed)
	}
}
