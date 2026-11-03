package localhost

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/localconversation/store"
	"github.com/multica-ai/multica/server/internal/localconversation/timing"
)

type timedV2 struct {
	srv                  string
	deviceLog, reportLog *timing.Log
	devicePath, reports  string
	v                    *V2
}

// newTimedRemote serves a device-link entry with timing on, from a phone by default.
func newTimedRemote(t *testing.T, connection string) *timedV2 {
	t.Helper()
	_, _, v := newV2Fixture(t)
	dir := t.TempDir()
	devicePath, reportsPath := filepath.Join(dir, "device.jsonl"), filepath.Join(dir, "reports.jsonl")
	f := &timedV2{deviceLog: timing.Open(devicePath, timing.Options{}), reportLog: timing.Open(reportsPath, timing.Options{}), devicePath: devicePath, reports: reportsPath, v: v}
	t.Cleanup(f.deviceLog.Close)
	t.Cleanup(f.reportLog.Close)
	v.SetTiming(timing.NewMarks(f.deviceLog, timing.SourceDaemon), f.reportLog)
	var open atomic.Bool
	open.Store(true)
	f.srv = remoteServer(t, v, RemotePeer{ID: "peer-phone", DeviceName: "Phone", Connection: connection}, &open).URL
	return f
}

func (f *timedV2) read(t *testing.T, log *timing.Log, path string) []timing.Record {
	t.Helper()
	log.Flush()
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var out []timing.Record
	for _, line := range bytes.Split(raw, []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var r timing.Record
		if err := json.Unmarshal(line, &r); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

func (f *timedV2) post(t *testing.T, path, kind string, body any) (int, map[string]any) {
	t.Helper()
	data, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, f.srv+"/local/v2"+path, bytes.NewReader(data))
	req.Header = remoteHeaders("client-1")
	if kind != "" {
		req.Header.Set(headerClientKind, kind)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(res.Body).Decode(&out)
	return res.StatusCode, out
}

func report(at string, stage string) map[string]any {
	return map[string]any{"version": 2, "source": "mobile", "at": at, "stage": stage, "event": "mark", "elapsedMs": 0}
}

func TestRemoteCallersAreRecordedWithTheirOwnStream(t *testing.T) {
	t.Parallel()

	got := caller{kind: callerRemote, clientKind: timing.ClientMobile, connection: timing.ConnectionRelayed}.timing()
	if got.Connection != timing.ConnectionRelayed || got.Origin != timing.OriginP2P || got.ClientKind != timing.ClientMobile {
		t.Fatalf("caller = %+v", got)
	}
}

func TestOnlyRemoteDevicesMayReport(t *testing.T) {
	t.Parallel()

	for _, kind := range []callerKind{callerMain, callerRenderer} {
		if allowed(kind, http.MethodPost, timing.ReportsPath, "") {
			t.Fatalf("local caller %d may report", kind)
		}
	}
	if !allowed(callerRemote, http.MethodPost, timing.ReportsPath, "") {
		t.Fatal("a remote device may report")
	}
	f := newTimedRemote(t, timing.ConnectionDirect)
	if status, body := f.post(t, timing.ReportsPath, "desktop", map[string]any{"batch_id": "b1", "records": []any{report("2026-11-02T10:00:00.000Z", "client.send")}}); status != http.StatusForbidden {
		t.Fatalf("a device that is not a phone = %d %v", status, body)
	}
}

func TestHelloOffersReportingOnlyToRemoteDevices(t *testing.T) {
	t.Parallel()

	f := newTimedRemote(t, timing.ConnectionDirect)
	req, _ := http.NewRequest(http.MethodGet, f.srv+"/local/v2/hello", nil)
	req.Header = remoteHeaders("c")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	if !strings.Contains(string(data), TimingReportCapability) {
		t.Fatalf("hello = %s", data)
	}
}

func TestReportsAreStampedScopedAndKeptApart(t *testing.T) {
	t.Parallel()

	f := newTimedRemote(t, timing.ConnectionRelayed)
	ctx := context.Background()
	for id, workspace := range map[string]string{"conv-1": "ws-1", "conv-other": "another-workspace"} {
		scope, err := f.v.svc.Store().EnsureScope(ctx, store.Scope{APIOrigin: "https://api.multica.ai", UserID: "user-1", WorkspaceID: workspace})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.v.svc.Store().CreateConversation(ctx, store.NewConversation{ID: id, ScopeID: scope, Provider: "claude", CreatedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	// A wrong clock must not cost the record: it keeps its own at and gains received_at.
	skewed := "2001-01-01T00:00:00.000Z"
	own := report(skewed, "client.send")
	own["conversationId"], own["requestId"], own["reporter"], own["receivedAt"] = "conv-1", "req-1", "someone-else", "1999-01-01T00:00:00.000Z"
	foreign := report(skewed, "client.send")
	foreign["conversationId"] = "conv-other"
	missing := report(skewed, "client.send")
	missing["conversationId"] = "conv-gone"
	daemonMark := report(skewed, "turn.finished")
	otherSource := report(skewed, "client.send")
	otherSource["source"] = "daemon"
	malformed := report(skewed, "client.send")
	malformed["reused"] = "yes"
	device := report(skewed, "connection.direct")
	device["peer"] = "peer-desktop"
	secret := report(skewed, "ws.closed")
	secret["reason"] = "dial tcp 10.0.0.1"

	status, ack := f.post(t, timing.ReportsPath, "mobile", map[string]any{"batch_id": "b1", "records": []any{own, foreign, missing, daemonMark, otherSource, malformed, device, secret}})
	if status != http.StatusOK || ack["accepted"] != float64(3) || ack["rejected"] != float64(5) || ack["duplicate"] != false {
		t.Fatalf("ack = %d %v", status, ack)
	}
	stored := f.read(t, f.reportLog, f.reports)
	if len(stored) != 3 {
		t.Fatalf("stored = %+v", stored)
	}
	first := stored[0]
	if first.At != skewed || first.Reporter != "peer-phone" || first.ReceivedAt == "" || strings.HasPrefix(first.ReceivedAt, "1999") || first.Source != timing.SourceMobile || first.ClientKind != timing.ClientMobile || first.Origin != timing.OriginP2P {
		t.Fatalf("report = %+v", first)
	}
	if stored[2].Reason != "" {
		t.Fatalf("free text survived: %+v", stored[2])
	}
	for _, r := range f.read(t, f.deviceLog, f.devicePath) {
		if r.Stage == "client.send" || r.Stage == timing.StageProxyRequest {
			t.Fatalf("a report reached this device's own log: %+v", r)
		}
	}
}

func TestAReportRetriedAfterALostAcknowledgementStoresNothing(t *testing.T) {
	t.Parallel()

	f := newTimedRemote(t, timing.ConnectionDirect)
	body := map[string]any{"batch_id": "b1", "records": []any{report("2026-11-02T10:00:00.000Z", "ws.open")}}
	f.post(t, timing.ReportsPath, "mobile", body)
	status, ack := f.post(t, timing.ReportsPath, "mobile", body)
	if status != http.StatusOK || ack["duplicate"] != true {
		t.Fatalf("retry = %d %v", status, ack)
	}
	if stored := f.read(t, f.reportLog, f.reports); len(stored) != 1 {
		t.Fatalf("stored = %+v", stored)
	}
}

func TestReportsAreRateLimitedPerDevice(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)
	limits := newReportLimits(func() time.Time { return now })
	for i := 0; i < reportBurst; i++ {
		if !limits.allow("a") {
			t.Fatalf("batch %d refused inside the burst", i)
		}
	}
	if limits.allow("a") {
		t.Fatal("the burst has an end")
	}
	if !limits.allow("b") {
		t.Fatal("one device must not use up another's reports")
	}
	now = now.Add(time.Minute)
	if !limits.allow("a") {
		t.Fatal("the bucket refills")
	}

	f := newTimedRemote(t, timing.ConnectionDirect)
	var last int
	for i := 0; i <= reportBurst; i++ {
		last, _ = f.post(t, timing.ReportsPath, "mobile", map[string]any{"batch_id": "b" + string(rune('a'+i)), "records": []any{}})
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("status = %d", last)
	}
}

func TestMalformedReportsAreRefused(t *testing.T) {
	t.Parallel()

	f := newTimedRemote(t, timing.ConnectionDirect)
	many := make([]any, reportMaxRecords+1)
	for i := range many {
		many[i] = report("2026-11-02T10:00:00.000Z", "ws.open")
	}
	for name, body := range map[string]any{
		"no batch":  map[string]any{"records": []any{}},
		"bad batch": map[string]any{"batch_id": "a/b c", "records": []any{}},
		"not list":  map[string]any{"batch_id": "b", "records": "x"},
		"too many":  map[string]any{"batch_id": "b", "records": many},
	} {
		if status, _ := f.post(t, timing.ReportsPath, "mobile", body); status != http.StatusBadRequest {
			t.Errorf("%s: status = %d", name, status)
		}
	}
}

func TestRemoteRequestsRecordTheStreamAndTheirOwnTraffic(t *testing.T) {
	t.Parallel()

	f := newTimedRemote(t, timing.ConnectionRelayed)
	req, _ := http.NewRequest(http.MethodGet, f.srv+"/local/v2/sessions", nil)
	req.Header = remoteHeaders("c")
	req.Header.Set(timing.HeaderRequestID, "req-1")
	req.Header.Set(headerClientKind, "mobile")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	records := f.read(t, f.deviceLog, f.devicePath)
	want := map[string]bool{timing.StageProxyRequest: false, timing.StageProxyFirstByte: false, timing.StageLinkRequestReceived: false, timing.StageLinkResponseSent: false}
	for _, r := range records {
		if _, ok := want[r.Stage]; ok {
			want[r.Stage] = true
			if r.Connection != timing.ConnectionRelayed || r.Role != timing.RoleExecutor || r.RequestID != "req-1" || r.Peer != "peer-phone" {
				t.Fatalf("record = %+v", r)
			}
		}
	}
	for stage, seen := range want {
		if !seen {
			t.Errorf("no %s", stage)
		}
	}
}

func TestReportsLeaveNoRecordsForTheNextReport(t *testing.T) {
	t.Parallel()

	f := newTimedRemote(t, timing.ConnectionDirect)
	f.post(t, timing.ReportsPath, "mobile", map[string]any{"batch_id": "b1", "records": []any{}})
	if records := f.read(t, f.deviceLog, f.devicePath); len(records) != 0 {
		t.Fatalf("records = %+v", records)
	}
}

func TestAWebSocketThatMovesToDirectIsRecordedOnTheExecutor(t *testing.T) {
	t.Parallel()

	f := newTimedRemote(t, timing.ConnectionRelayed)
	v := f.v
	relayed := &wsConn{caller: caller{kind: callerRemote, clientKey: "peer-phone/c", peer: "peer-phone", connection: timing.ConnectionRelayed}}
	direct := &wsConn{caller: caller{kind: callerRemote, clientKey: "peer-phone/c", peer: "peer-phone", connection: timing.ConnectionDirect}}
	v.noteResubscribed(direct)
	if records := f.read(t, f.deviceLog, f.devicePath); len(records) != 0 {
		t.Fatalf("a first direct socket is no migration: %+v", records)
	}
	v.noteSocketClosed(relayed)
	v.noteResubscribed(direct)
	v.noteResubscribed(direct)
	count := 0
	for _, r := range f.read(t, f.deviceLog, f.devicePath) {
		if r.Stage == timing.StageMigrateResubscribed {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("resubscribed %d times", count)
	}
}
