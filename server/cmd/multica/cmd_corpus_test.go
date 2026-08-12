package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/corpustransfer"
	"github.com/spf13/cobra"
)

const corpusCLIWorkspaceID = "11111111-1111-4111-8111-111111111111"

func TestParseCorpusSourceSpecPreservesWindowsDrivePath(t *testing.T) {
	root, err := parseCorpusSourceSpec(`codex=desktop:C:\Users\EDY\Desktop\exports`)
	if err != nil {
		t.Fatal(err)
	}
	if root.Type != "codex" || root.Name != "desktop" || root.Path != `C:\Users\EDY\Desktop\exports` {
		t.Fatalf("parsed source = %#v", root)
	}
}

func TestCorpusPackBuildsArchiveAndCanonicalSidecar(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("P0 Windows supports sending existing ZIPs; source packing is not hardened yet")
	}
	now := time.Now().UTC().Truncate(time.Second)
	rootA := t.TempDir()
	rootB := t.TempDir()
	writeCorpusCLIFile(t, rootA, "z/会话.jsonl", "raw secret-looking content", now)
	writeCorpusCLIFile(t, rootB, "a/copy.jsonl", "raw secret-looking content", now.Add(-time.Minute))
	writeCorpusCLIFile(t, rootA, "old.jsonl", "old", now.Add(-48*time.Hour))

	archive := filepath.Join(t.TempDir(), "package.zip")
	cmd := newCorpusPackCommand()
	cmd.SetContext(context.Background())
	var output bytes.Buffer
	cmd.SetOut(&output)
	if err := cmd.Flags().Set("source", "automation=secondary:"+rootB); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Flags().Set("source", "codex=desktop:"+rootA); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Flags().Set("cutoff", now.Add(-24*time.Hour).Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	if err := cmd.RunE(cmd, []string{archive}); err != nil {
		t.Fatal(err)
	}

	var result struct {
		ArchivePath    string   `json:"archive_path"`
		ManifestPath   string   `json:"manifest_path"`
		ArchiveSHA256  string   `json:"archive_sha256"`
		ManifestSHA256 string   `json:"manifest_sha256"`
		SizeBytes      int64    `json:"size_bytes"`
		EntryCount     int      `json:"entry_count"`
		MissingSources []string `json:"missing_sources"`
	}
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatalf("decode pack output: %v\n%s", err, output.String())
	}
	if result.ArchivePath != archive || result.ManifestPath != archive+".manifest.json" || result.EntryCount != 2 || result.SizeBytes < 1 {
		t.Fatalf("pack result = %#v", result)
	}
	manifestBytes, err := os.ReadFile(result.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest corpustransfer.Manifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Entries[0].Path >= manifest.Entries[1].Path || manifest.Entries[1].ReplicaOf != manifest.Entries[0].Path {
		t.Fatalf("canonical entries = %#v", manifest.Entries)
	}
	info, err := os.Stat(result.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("sidecar mode = %v, want 0600", info.Mode().Perm())
	}
	inspected, envelope, err := corpustransfer.InspectZIP(archive, manifest.Source)
	if err != nil {
		t.Fatal(err)
	}
	if inspected.PackageID != manifest.PackageID || envelope.SHA256 != result.ArchiveSHA256 || envelope.ManifestSHA256 != result.ManifestSHA256 {
		t.Fatalf("archive inspection mismatch: %#v %#v", inspected, envelope)
	}
}

func TestCorpusPackOutputInsideSourceDoesNotScanPrivateWorkspace(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("P0 Windows supports sending existing ZIPs; source packing is not hardened yet")
	}
	root := t.TempDir()
	writeCorpusCLIFile(t, root, "!.jsonl", "source payload", time.Now().UTC())
	archive := filepath.Join(root, "package.zip")
	command := newCorpusPackCommand()
	command.SetContext(context.Background())
	command.SetOut(io.Discard)
	if err := command.Flags().Set("source", "codex=desktop:"+root); err != nil {
		t.Fatal(err)
	}
	if err := command.RunE(command, []string{archive}); err != nil {
		t.Fatal(err)
	}
	manifest, _, err := corpustransfer.InspectZIP(archive, corpustransfer.SourceInfo{})
	if err != nil {
		t.Fatal(err)
	}
	if manifest.EntryCount != 1 || manifest.Entries[0].Path != "codex/desktop/!.jsonl" {
		t.Fatalf("packaged entries = %#v", manifest.Entries)
	}
}

func TestCreateCorpusPackWorkspaceIsOutsideSourceRoots(t *testing.T) {
	root := t.TempDir()
	workspace, err := createCorpusPackWorkspace([]corpustransfer.SourceRoot{{Path: root}})
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(workspace)
	inside, err := pathWithinCorpusRoot(root, workspace)
	if err != nil {
		t.Fatal(err)
	}
	if inside {
		t.Fatalf("pack workspace %q is inside source root %q", workspace, root)
	}
}

func TestCorpusPackSendStatusReceiveEndToEnd(t *testing.T) {
	archive, manifest, envelope := packCorpusCLIArchive(t)
	archiveBytes, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}

	transferID := "22222222-2222-4222-8222-222222222222"
	fixture := &corpusHTTPFixture{t: t, transferID: transferID, archive: archiveBytes, manifest: manifest, envelope: envelope}
	server := httptest.NewServer(http.HandlerFunc(fixture.serveHTTP))
	defer server.Close()
	setCorpusCLIAuthEnv(t, server.URL)

	send := newCorpusSendCommand()
	send.SetContext(context.Background())
	var sendOutput bytes.Buffer
	send.SetOut(&sendOutput)
	if err := send.Flags().Set("source-type", "codex-export"); err != nil {
		t.Fatal(err)
	}
	if err := send.RunE(send, []string{archive}); err != nil {
		t.Fatal(err)
	}
	var receipt map[string]any
	if err := json.Unmarshal(sendOutput.Bytes(), &receipt); err != nil || receipt["state"] != "confirmed" {
		t.Fatalf("send receipt/error = %s/%v", sendOutput.String(), err)
	}
	if retryReason, ok := receipt["retry_reason"]; !ok || retryReason != nil {
		t.Fatalf("send retry_reason = %#v, present = %v", retryReason, ok)
	}

	status := newCorpusStatusCommand()
	status.SetContext(context.Background())
	var statusOutput bytes.Buffer
	status.SetOut(&statusOutput)
	if err := status.RunE(status, []string{transferID}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(statusOutput.String(), `"missing": false`) {
		t.Fatalf("status output = %s", statusOutput.String())
	}

	outputDir := t.TempDir()
	fixture.downloadDir = outputDir
	receive := newCorpusReceiveCommand()
	receive.SetContext(context.Background())
	var receiveOutput bytes.Buffer
	receive.SetOut(&receiveOutput)
	if err := receive.Flags().Set("output-dir", outputDir); err != nil {
		t.Fatal(err)
	}
	if err := receive.Flags().Set("sink-id", "fixture-sink"); err != nil {
		t.Fatal(err)
	}
	if err := receive.RunE(receive, []string{transferID}); err != nil {
		t.Fatal(err)
	}
	installed := filepath.Join(outputDir, manifest.PackageID, filepath.FromSlash(manifest.Entries[0].Path))
	body, err := os.ReadFile(installed)
	if err != nil || string(body) != "same payload" {
		t.Fatalf("installed body/error = %q/%v", body, err)
	}
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	if fixture.createCalls != 1 || fixture.uploadCalls != 1 || fixture.completeCalls != 1 || fixture.ackCalls != 1 || fixture.lastSink != "fixture-sink" {
		t.Fatalf("fixture calls create/upload/complete/ack/sink = %d/%d/%d/%d/%q", fixture.createCalls, fixture.uploadCalls, fixture.completeCalls, fixture.ackCalls, fixture.lastSink)
	}
	if !bytes.Equal(fixture.uploaded, archiveBytes) {
		t.Fatal("uploaded bytes differ from archive")
	}
}

func TestCorpusSendInspectsArchiveWithoutSidecar(t *testing.T) {
	archive, manifest, envelope := buildCorpusCLIArchive(t)
	if _, err := os.Stat(archive + ".manifest.json"); !os.IsNotExist(err) {
		t.Fatalf("unexpected sidecar: %v", err)
	}
	archiveBytes, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	fixture := &corpusHTTPFixture{
		t: t, transferID: "44444444-4444-4444-8444-444444444444",
		archive: archiveBytes, manifest: manifest, envelope: envelope,
	}
	server := httptest.NewServer(http.HandlerFunc(fixture.serveHTTP))
	defer server.Close()
	setCorpusCLIAuthEnv(t, server.URL)

	send := newCorpusSendCommand()
	send.SetContext(context.Background())
	send.SetOut(io.Discard)
	if err := send.Flags().Set("source-type", "codex-export"); err != nil {
		t.Fatal(err)
	}
	if err := send.RunE(send, []string{archive}); err != nil {
		t.Fatal(err)
	}
	if fixture.createCalls != 1 || fixture.uploadCalls != 1 || fixture.completeCalls != 1 {
		t.Fatalf("fixture calls create/upload/complete = %d/%d/%d", fixture.createCalls, fixture.uploadCalls, fixture.completeCalls)
	}
}

func TestCorpusSendExistingZIPRetryReusesStableManifest(t *testing.T) {
	archive := buildCorpusCLILegacyArchive(t)
	manifest, envelope, err := corpustransfer.InspectZIP(archive, corpustransfer.SourceInfo{
		Adapter: "zip", Version: "v1", Type: "codex-export", Name: filepath.Base(archive),
	})
	if err != nil {
		t.Fatal(err)
	}
	archiveBytes, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	fixture := &corpusHTTPFixture{
		t: t, transferID: "99999999-9999-4999-8999-999999999999",
		archive: archiveBytes, manifest: manifest, envelope: envelope, failCreateOnce: true,
	}
	server := httptest.NewServer(http.HandlerFunc(fixture.serveHTTP))
	defer server.Close()
	setCorpusCLIAuthEnv(t, server.URL)

	newSend := func() *cobra.Command {
		command := newCorpusSendCommand()
		command.SetContext(context.Background())
		command.SetOut(io.Discard)
		_ = command.Flags().Set("source-type", "codex-export")
		_ = command.Flags().Set("idempotency-key", "controlled-retry")
		return command
	}
	first := newSend()
	if err := first.RunE(first, []string{archive}); err == nil || !strings.Contains(err.Error(), "create corpus transfer") {
		t.Fatalf("ambiguous first create error = %v", err)
	}
	retry := newSend()
	if err := retry.RunE(retry, []string{archive}); err != nil {
		t.Fatalf("controlled retry: %v", err)
	}
	if fixture.createCalls != 2 || fixture.uploadCalls != 1 || fixture.completeCalls != 1 {
		t.Fatalf("fixture calls create/upload/complete = %d/%d/%d", fixture.createCalls, fixture.uploadCalls, fixture.completeCalls)
	}
}

func TestCorpusStatusPrintsDurableFailureReason(t *testing.T) {
	transferID := "55555555-5555-4555-8555-555555555555"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"transfer_id": transferID, "workspace_id": corpusCLIWorkspaceID,
			"state": "failed", "retry_reason": "verification_mismatch",
		})
	}))
	defer server.Close()
	setCorpusCLIAuthEnv(t, server.URL)

	status := newCorpusStatusCommand()
	status.SetContext(context.Background())
	var output bytes.Buffer
	status.SetOut(&output)
	if err := status.RunE(status, []string{transferID}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"retry_reason": "verification_mismatch"`) {
		t.Fatalf("status output = %s", output.String())
	}
}

func TestCorpusSendHelpIncludesCopyReadyWindowsExistingZIPCommand(t *testing.T) {
	command := newCorpusSendCommand()
	want := `multica corpus send "C:\Users\EDY\Desktop\codex-export-皮皮-30d.zip" --source-type codex-export`
	if !strings.Contains(command.Example, want) {
		t.Fatalf("send example = %q, want %q", command.Example, want)
	}
}

func TestInspectCorpusArchiveRejectsOversizeBeforeZIPParsing(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "oversize.zip")
	file, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maxCorpusCLIArchiveBytes + 1); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	_, _, err = inspectCorpusArchive(archive, "", corpustransfer.SourceInfo{Adapter: "zip", Type: "test", Name: "oversize"})
	if err == nil || !strings.Contains(err.Error(), "2 GiB") {
		t.Fatalf("oversize archive error = %v", err)
	}
}

func TestCorpusReceiveDigestMismatchDoesNotInstallOrACK(t *testing.T) {
	archive, manifest, envelope := buildCorpusCLIArchive(t)
	archiveBytes, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	envelope.SHA256 = strings.Repeat("0", 64)
	fixture := &corpusHTTPFixture{t: t, transferID: "33333333-3333-4333-8333-333333333333", archive: archiveBytes, manifest: manifest, envelope: envelope, confirmed: true}
	server := httptest.NewServer(http.HandlerFunc(fixture.serveHTTP))
	defer server.Close()
	setCorpusCLIAuthEnv(t, server.URL)

	outputDir := t.TempDir()
	receive := newCorpusReceiveCommand()
	receive.SetContext(context.Background())
	receive.SetOut(io.Discard)
	_ = receive.Flags().Set("output-dir", outputDir)
	if err := receive.RunE(receive, []string{fixture.transferID}); err == nil || !strings.Contains(err.Error(), "sha256") {
		t.Fatalf("digest mismatch error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(outputDir, manifest.PackageID)); !os.IsNotExist(err) {
		t.Fatalf("destination exists after failed verification: %v", err)
	}
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	if fixture.ackCalls != 0 {
		t.Fatalf("ACK calls = %d, want 0", fixture.ackCalls)
	}
}

func TestCorpusReceiveEntryDigestMismatchDoesNotInstallOrACK(t *testing.T) {
	archive, manifest, envelope := buildCorpusCLIArchive(t)
	archiveBytes, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	for i := range manifest.Entries {
		manifest.Entries[i].SHA256 = strings.Repeat("0", 64)
	}
	manifestJSON, err := manifest.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	envelope.ManifestSHA256 = sha256HexBytes(manifestJSON)
	fixture := &corpusHTTPFixture{
		t: t, transferID: "66666666-6666-4666-8666-666666666666",
		archive: archiveBytes, manifest: manifest, envelope: envelope, confirmed: true,
	}
	server := httptest.NewServer(http.HandlerFunc(fixture.serveHTTP))
	defer server.Close()
	setCorpusCLIAuthEnv(t, server.URL)

	outputDir := t.TempDir()
	receive := newCorpusReceiveCommand()
	receive.SetContext(context.Background())
	receive.SetOut(io.Discard)
	_ = receive.Flags().Set("output-dir", outputDir)
	if err := receive.RunE(receive, []string{fixture.transferID}); err == nil || !strings.Contains(err.Error(), "sha256 mismatch") {
		t.Fatalf("entry digest mismatch error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(outputDir, manifest.PackageID)); !os.IsNotExist(err) {
		t.Fatalf("destination exists after failed extraction: %v", err)
	}
	if fixture.ackCalls != 0 {
		t.Fatalf("ACK calls = %d, want 0", fixture.ackCalls)
	}
}

func TestCorpusReceiveRejectsInvalidSinkBeforeDownloadOrInstall(t *testing.T) {
	archive, manifest, envelope := buildCorpusCLIArchive(t)
	archiveBytes, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	fixture := &corpusHTTPFixture{
		t: t, transferID: "77777777-7777-4777-8777-777777777777",
		archive: archiveBytes, manifest: manifest, envelope: envelope, confirmed: true,
	}
	server := httptest.NewServer(http.HandlerFunc(fixture.serveHTTP))
	defer server.Close()
	setCorpusCLIAuthEnv(t, server.URL)

	outputDir := t.TempDir()
	receive := newCorpusReceiveCommand()
	receive.SetContext(context.Background())
	receive.SetOut(io.Discard)
	_ = receive.Flags().Set("output-dir", outputDir)
	_ = receive.Flags().Set("sink-id", "bad\x00sink")
	if err := receive.RunE(receive, []string{fixture.transferID}); err == nil || !strings.Contains(err.Error(), "sink-id is invalid") {
		t.Fatalf("invalid sink error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(outputDir, manifest.PackageID)); !os.IsNotExist(err) {
		t.Fatalf("destination exists after invalid sink: %v", err)
	}
	if fixture.downloadCalls != 0 || fixture.ackCalls != 0 {
		t.Fatalf("download/ACK calls = %d/%d, want 0/0", fixture.downloadCalls, fixture.ackCalls)
	}
}

func TestCorpusReceiveRetriesACKForVerifiedExistingInstall(t *testing.T) {
	archive, manifest, envelope := buildCorpusCLIArchive(t)
	archiveBytes, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	fixture := &corpusHTTPFixture{
		t: t, transferID: "88888888-8888-4888-8888-888888888888",
		archive: archiveBytes, manifest: manifest, envelope: envelope,
		confirmed: true, failACKOnce: true,
	}
	server := httptest.NewServer(http.HandlerFunc(fixture.serveHTTP))
	defer server.Close()
	setCorpusCLIAuthEnv(t, server.URL)
	outputDir := t.TempDir()

	receive := newCorpusReceiveCommand()
	receive.SetContext(context.Background())
	receive.SetOut(io.Discard)
	_ = receive.Flags().Set("output-dir", outputDir)
	_ = receive.Flags().Set("sink-id", "retry-sink")
	if err := receive.RunE(receive, []string{fixture.transferID}); err == nil || !strings.Contains(err.Error(), "ACK failed") {
		t.Fatalf("first receive error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(outputDir, manifest.PackageID)); err != nil {
		t.Fatalf("installed package missing after ACK failure: %v", err)
	}

	retry := newCorpusReceiveCommand()
	retry.SetContext(context.Background())
	retry.SetOut(io.Discard)
	_ = retry.Flags().Set("output-dir", outputDir)
	_ = retry.Flags().Set("sink-id", "retry-sink")
	if err := retry.RunE(retry, []string{fixture.transferID}); err != nil {
		t.Fatalf("retry receive: %v", err)
	}
	if fixture.downloadCalls != 1 || fixture.ackCalls != 2 {
		t.Fatalf("download/ACK calls = %d/%d, want 1/2", fixture.downloadCalls, fixture.ackCalls)
	}
}

func TestCountingWriterCapsBytesDuringStreaming(t *testing.T) {
	var destination bytes.Buffer
	writer := &countingWriter{writer: &destination, max: 3}
	n, err := writer.Write([]byte("four"))
	if err == nil || !strings.Contains(err.Error(), "exceeds declared size") {
		t.Fatalf("bounded write error = %v", err)
	}
	if n != 3 || writer.count != 3 || destination.String() != "fou" {
		t.Fatalf("bounded write n/count/body = %d/%d/%q", n, writer.count, destination.String())
	}
}

func writeCorpusCLIFile(t *testing.T, root, rel, body string, mtime time.Time) {
	t.Helper()
	filename := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filename, mtime, mtime); err != nil {
		t.Fatal(err)
	}
}

func buildCorpusCLIArchive(t *testing.T) (string, corpustransfer.Manifest, corpustransfer.ArchiveEnvelope) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	root := t.TempDir()
	writeCorpusCLIFile(t, root, "皮皮/会话.jsonl", "same payload", now)
	writeCorpusCLIFile(t, root, "copy.jsonl", "same payload", now)
	stage := filepath.Join(t.TempDir(), "stage")
	manifest, err := corpustransfer.StageAndBuildManifest(context.Background(), []corpustransfer.SourceRoot{{Type: "codex-export", Name: "pipi", Path: root}}, time.Time{}, stage)
	if err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(t.TempDir(), "codex-export-皮皮-30d.zip")
	envelope, err := corpustransfer.BuildZIP(stage, archive, manifest)
	if err != nil {
		t.Fatal(err)
	}
	return archive, manifest, envelope
}

func packCorpusCLIArchive(t *testing.T) (string, corpustransfer.Manifest, corpustransfer.ArchiveEnvelope) {
	t.Helper()
	root := t.TempDir()
	writeCorpusCLIFile(t, root, "皮皮/会话.jsonl", "same payload", time.Now().UTC())
	writeCorpusCLIFile(t, root, "copy.jsonl", "same payload", time.Now().UTC())
	archive := filepath.Join(t.TempDir(), "codex-export-皮皮-30d.zip")
	command := newCorpusPackCommand()
	command.SetContext(context.Background())
	command.SetOut(io.Discard)
	if err := command.Flags().Set("source", "codex-export=pipi:"+root); err != nil {
		t.Fatal(err)
	}
	if err := command.RunE(command, []string{archive}); err != nil {
		t.Fatal(err)
	}
	manifest, envelope, err := corpustransfer.InspectZIP(archive, corpustransfer.SourceInfo{})
	if err != nil {
		t.Fatal(err)
	}
	return archive, manifest, envelope
}

func buildCorpusCLILegacyArchive(t *testing.T) string {
	t.Helper()
	archive := filepath.Join(t.TempDir(), "codex-export-皮皮-30d.zip")
	file, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	entry, err := writer.Create("皮皮/会话.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte("legacy payload")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return archive
}

func setCorpusCLIAuthEnv(t *testing.T, serverURL string) {
	t.Helper()
	t.Setenv("MULTICA_SERVER_URL", serverURL)
	t.Setenv("MULTICA_WORKSPACE_ID", corpusCLIWorkspaceID)
	t.Setenv("MULTICA_TOKEN", "mat_corpus_test")
	t.Setenv("MULTICA_AGENT_ID", "")
	t.Setenv("MULTICA_TASK_ID", "")
}

type corpusHTTPFixture struct {
	t              *testing.T
	mu             sync.Mutex
	transferID     string
	archive        []byte
	manifest       corpustransfer.Manifest
	envelope       corpustransfer.ArchiveEnvelope
	confirmed      bool
	createCalls    int
	uploadCalls    int
	completeCalls  int
	downloadCalls  int
	ackCalls       int
	uploaded       []byte
	lastSink       string
	downloadDir    string
	failACKOnce    bool
	failCreateOnce bool
	createManifest []byte
	idempotencyKey string
}

func (f *corpusHTTPFixture) serveHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	base := "/api/workspaces/" + corpusCLIWorkspaceID + "/corpus-transfers"
	item := base + "/" + f.transferID
	w.Header().Set("Content-Type", "application/json")
	if r.Header.Get("Authorization") != "Bearer mat_corpus_test" || r.Header.Get("X-Workspace-ID") != corpusCLIWorkspaceID {
		f.t.Errorf("auth/workspace headers = %q/%q", r.Header.Get("Authorization"), r.Header.Get("X-Workspace-ID"))
	}
	switch {
	case r.Method == http.MethodPost && r.URL.Path == base:
		f.createCalls++
		var request struct {
			IdempotencyKey string                         `json:"idempotency_key"`
			Manifest       corpustransfer.Manifest        `json:"manifest"`
			Archive        corpustransfer.ArchiveEnvelope `json:"archive"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			f.t.Error(err)
		}
		if request.Manifest.PackageID != f.manifest.PackageID || request.Archive.SHA256 != f.envelope.SHA256 {
			f.t.Errorf("create request = %#v", request)
		}
		manifestJSON, err := request.Manifest.CanonicalJSON()
		if err != nil {
			f.t.Errorf("canonical create manifest: %v", err)
		} else if f.createManifest == nil {
			f.createManifest = manifestJSON
			f.idempotencyKey = request.IdempotencyKey
		} else if !bytes.Equal(f.createManifest, manifestJSON) || f.idempotencyKey != request.IdempotencyKey {
			f.t.Errorf("controlled retry changed manifest or idempotency key")
		}
		if f.failCreateOnce {
			f.failCreateOnce = false
			http.Error(w, "ambiguous create response", http.StatusServiceUnavailable)
			return
		}
		f.writeReceipt(w, "created")
	case r.Method == http.MethodPut && r.URL.Path == item+"/content":
		f.uploadCalls++
		if r.ContentLength != int64(len(f.archive)) {
			f.t.Errorf("upload length = %d, want %d", r.ContentLength, len(f.archive))
		}
		f.uploaded, _ = io.ReadAll(r.Body)
		f.writeReceipt(w, "uploaded")
	case r.Method == http.MethodPost && r.URL.Path == item+"/complete":
		f.completeCalls++
		f.confirmed = true
		f.writeReceipt(w, "confirmed")
	case r.Method == http.MethodGet && r.URL.Path == item:
		state := "created"
		if f.confirmed {
			state = "confirmed"
		}
		f.writeReceipt(w, state)
	case r.Method == http.MethodGet && r.URL.Path == item+"/content":
		f.downloadCalls++
		if f.downloadDir != "" {
			matches, err := filepath.Glob(filepath.Join(f.downloadDir, ".multica-corpus-download-*.zip"))
			if err != nil || len(matches) != 1 {
				f.t.Errorf("private download file matches/error = %v/%v", matches, err)
			} else {
				info, statErr := os.Stat(matches[0])
				if statErr != nil {
					f.t.Errorf("stat private download file: %v", statErr)
				} else if info.Mode().Perm() != 0o600 {
					f.t.Errorf("private download mode = %v, want 0600", info.Mode().Perm())
				}
			}
		}
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Length", fmt.Sprint(len(f.archive)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(f.archive)
	case r.Method == http.MethodPost && r.URL.Path == item+"/acks":
		f.ackCalls++
		var ack struct {
			SinkID string `json:"sink_id"`
			Digest string `json:"confirmed_sha256"`
		}
		if err := json.NewDecoder(r.Body).Decode(&ack); err != nil {
			f.t.Error(err)
		}
		f.lastSink = ack.SinkID
		if ack.Digest != f.envelope.SHA256 {
			f.t.Errorf("ACK digest = %q, want %q", ack.Digest, f.envelope.SHA256)
		}
		if f.failACKOnce {
			f.failACKOnce = false
			http.Error(w, "temporary ACK failure", http.StatusServiceUnavailable)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"transfer_id": f.transferID, "sink_id": ack.SinkID, "confirmed_sha256": ack.Digest, "acknowledged_at": time.Now().UTC()})
	default:
		http.NotFound(w, r)
	}
}

func (f *corpusHTTPFixture) writeReceipt(w http.ResponseWriter, state string) {
	now := time.Now().UTC()
	_ = json.NewEncoder(w).Encode(map[string]any{
		"transfer_id": f.transferID, "workspace_id": corpusCLIWorkspaceID, "state": state,
		"manifest": f.manifest, "archive": f.envelope,
		"expires_at": now.Add(time.Hour), "created_at": now.Add(-time.Minute), "confirmed_at": now,
		"last_success_at": now, "late": false, "missing": false,
	})
}
