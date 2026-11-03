package mobilediag

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/localconversation/timing"
)

const testUser = "account-a"

func openAt(t *testing.T, dir string, at *time.Time) *Store {
	t.Helper()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return *at }
	t.Cleanup(s.Close)
	return s
}

func appLine(at time.Time, message string) []byte {
	return fmt.Appendf(nil, `{"at":%q,"source":"app","level":"info","message":%q}`, at.UTC().Format(time.RFC3339Nano), message)
}

func read(t *testing.T, s *Store, user string, minutes int) ([]map[string]any, Summary) {
	t.Helper()
	var out bytes.Buffer
	summary, err := s.WriteRead(&out, user, minutes)
	if err != nil {
		t.Fatal(err)
	}
	lines := []map[string]any{}
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		var value map[string]any
		if err := json.Unmarshal([]byte(line), &value); err != nil {
			t.Fatalf("not JSON: %s", line)
		}
		lines = append(lines, value)
	}
	return lines, summary
}

func messages(lines []map[string]any) []string {
	out := []string{}
	for _, line := range lines {
		if message, ok := line["message"].(string); ok {
			out = append(out, message)
		}
	}
	return out
}

func TestReadIsFramedAndFiltersByTimeAcrossDays(t *testing.T) {
	now := time.Date(2026, 11, 3, 0, 30, 0, 0, time.UTC)
	s := openAt(t, t.TempDir(), &now)
	s.SetAccount(testUser)
	s.SetRuntime([]byte(`{"appVersion":"1.2.3"}`))
	s.Append(SourceApp, appLine(now, "too old"))
	now = now.Add(23 * time.Hour)
	s.Append(SourceApp, appLine(now, "yesterday"))
	now = now.Add(time.Hour)
	s.Append(SourceApp, appLine(now, "today"))
	if len(s.files(SourceApp)) != 2 {
		t.Fatalf("files = %+v", s.files(SourceApp))
	}
	lines, summary := read(t, s, testUser, 120)
	if got := messages(lines); strings.Join(got, ",") != "yesterday,today" {
		t.Fatalf("messages = %v", got)
	}
	if lines[0]["kind"] != headerKind || lines[0]["runtime"].(map[string]any)["appVersion"] != "1.2.3" {
		t.Fatalf("header = %v", lines[0])
	}
	if last := lines[len(lines)-1]; last["kind"] != summaryKind {
		t.Fatalf("summary line = %v", last)
	}
	if summary.Sources[SourceApp].Records != 2 || summary.Sources[SourceApp].Status != statusAvailable || summary.Sources[SourceLink].Status != statusEmpty {
		t.Fatalf("summary = %+v", summary)
	}
}

func TestReadSkipsALineStillBeingWritten(t *testing.T) {
	now := time.Now()
	dir := t.TempDir()
	s := openAt(t, dir, &now)
	s.SetAccount(testUser)
	s.Append(SourceApp, appLine(now, "whole"))
	path := s.files(SourceApp)[0].path
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = file.WriteString(`{"at":"2026-11-03T`)
	file.Close()
	lines, _ := read(t, s, testUser, 60)
	if got := messages(lines); len(got) != 1 || got[0] != "whole" {
		t.Fatalf("messages = %v", got)
	}
}

func TestReadKeepsTheNewestWithinBudgetAndStaysUnderTheLimit(t *testing.T) {
	now := time.Now()
	s := openAt(t, t.TempDir(), &now)
	s.SetAccount(testUser)
	padding := strings.Repeat("x", 200<<10)
	for i := range 40 {
		now = now.Add(time.Second)
		s.Append(SourceApp, appLine(now, fmt.Sprintf("app-%d %s", i, padding)))
		s.Append(SourceMarks, appLine(now, fmt.Sprintf("mark-%d %s", i, padding)))
		s.write(SourceLink, append(appLine(now, fmt.Sprintf("link-%d %s", i, padding)), '\n'))
	}
	var out bytes.Buffer
	summary, err := s.WriteRead(&out, testUser, 60)
	if err != nil {
		t.Fatal(err)
	}
	if out.Len() > MaxReadBytes {
		t.Fatalf("read is %d bytes", out.Len())
	}
	if !summary.Sources[SourceApp].Truncated || !summary.Sources[SourceLink].Truncated || !summary.Sources[SourceMarks].Truncated {
		t.Fatalf("summary = %+v", summary)
	}
	if text := out.String(); !strings.Contains(text, "app-39 ") || strings.Contains(text, "app-0 ") {
		t.Fatal("did not keep the newest records")
	}
}

func TestPruneRemovesExpiredDaysAndHoldsTheCap(t *testing.T) {
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	s := openAt(t, dir, &now)
	s.Append(SourceApp, appLine(now, "first day"))
	first := s.files(SourceApp)[0].path
	now = now.AddDate(0, 0, retentionDays)
	s.Append(SourceApp, appLine(now, "kept on the last day"))
	if _, err := os.Stat(first); err != nil {
		t.Fatalf("removed a file still within retention: %v", err)
	}
	now = now.AddDate(0, 0, 1)
	s.Append(SourceApp, appLine(now, "a day later"))
	if _, err := os.Stat(first); !os.IsNotExist(err) {
		t.Fatalf("kept an expired file: %v", err)
	}
	line := appLine(now, strings.Repeat("x", 512<<10))
	for range 60 {
		s.Append(SourceApp, line)
	}
	var total int64
	for _, f := range s.files(SourceApp) {
		if f.size > maxFileBytes {
			t.Fatalf("%s is %d bytes", f.path, f.size)
		}
		total += f.size
	}
	if total > sourceCaps[SourceApp]+maxFileBytes {
		t.Fatalf("source holds %d bytes", total)
	}
}

func TestOpenRemovesExpiredLogsAndLeftoverExports(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "app-2001-01-01.jsonl"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, exportsDir), 0o700); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	openAt(t, dir, &now)
	for _, gone := range []string{"app-2001-01-01.jsonl", exportsDir} {
		if _, err := os.Stat(filepath.Join(dir, gone)); !os.IsNotExist(err) {
			t.Fatalf("%s remains: %v", gone, err)
		}
	}
}

func TestAccountChangesClearLogsAndKeepTheDevicesChoice(t *testing.T) {
	now := time.Now()
	s := openAt(t, t.TempDir(), &now)
	s.Append(SourceApp, appLine(now, "before sign-in"))
	s.SetAccount(testUser)
	lines, _ := read(t, s, testUser, 60)
	if got := messages(lines); len(got) != 1 {
		t.Fatalf("logs from before sign-in were lost: %v", got)
	}
	s.SetAccount(testUser)
	if lines, _ := read(t, s, testUser, 60); len(messages(lines)) != 1 {
		t.Fatal("the same account cleared its logs")
	}
	s.SetRemoteReadAllowed(false)
	s.SetAccount("account-b")
	lines, summary := read(t, s, "account-b", 60)
	if len(messages(lines)) != 0 || summary.Sources[SourceApp].Status != statusEmpty {
		t.Fatalf("another account's logs remain: %v", lines)
	}
	if s.RemoteReadAllowed() {
		t.Fatal("the device's choice was cleared with the logs")
	}
	s.Append(SourceApp, appLine(now, "b"))
	s.Clear()
	if lines, _ := read(t, s, "", 60); len(messages(lines)) != 0 {
		t.Fatal("clear left logs behind")
	}
}

func TestReadNeverAnswersForAnotherAccount(t *testing.T) {
	now := time.Now()
	s := openAt(t, t.TempDir(), &now)
	s.SetAccount("account-b")
	s.Append(SourceApp, appLine(now, "account b"))
	lines, summary := read(t, s, testUser, 60)
	if len(messages(lines)) != 0 || summary.Sources[SourceApp].Status != statusOtherOwner {
		t.Fatalf("read another account's logs: %v", lines)
	}
}

func TestLoggerWritesRecordsARead(t *testing.T) {
	now := time.Now()
	s := openAt(t, t.TempDir(), &now)
	s.SetAccount(testUser)
	s.Logger().Debug("p2p: connect failed", "error", "dial failed /ip4/203.0.113.9/tcp/1")
	lines, _ := read(t, s, testUser, 60)
	if got := messages(lines); len(got) != 1 || got[0] != "p2p: connect failed" {
		t.Fatalf("messages = %v", got)
	}
	record := lines[1]
	if record["source"] != SourceLink || !strings.Contains(record["error"].(string), "203.0.113.9") {
		t.Fatalf("record = %v", record)
	}
}

type recordingSink struct{ records []timing.Record }

func (s *recordingSink) Write(record timing.Record) { s.records = append(s.records, record) }

func TestRetainedMarksStayReadableAfterTheNextSinkHasThem(t *testing.T) {
	now := time.Now()
	s := openAt(t, t.TempDir(), &now)
	s.SetAccount(testUser)
	next := &recordingSink{}
	timing.NewMarks(s.RetainMarks(next), timing.SourceMobile).Write(timing.Record{Stage: timing.StageLinkRegistered})
	if len(next.records) != 1 {
		t.Fatalf("next = %+v", next.records)
	}
	lines, summary := read(t, s, testUser, 60)
	if summary.Sources[SourceMarks].Records != 1 || lines[1]["record"].(map[string]any)["stage"] != timing.StageLinkRegistered {
		t.Fatalf("lines = %v", lines)
	}
}

func TestExportFileIsAReadInAPrivateFile(t *testing.T) {
	now := time.Now()
	s := openAt(t, t.TempDir(), &now)
	s.SetAccount(testUser)
	s.Append(SourceApp, appLine(now, "exported"))
	path, summary, err := s.ExportFile(60)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), "exported") || summary.Sources[SourceApp].Records != 1 {
		t.Fatalf("export = %s, %+v, %v", data, summary, err)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v", info.Mode())
	}
	s.Clear()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("clear left an export: %v", err)
	}
}
