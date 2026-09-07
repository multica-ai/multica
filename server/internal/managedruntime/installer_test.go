package managedruntime

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestInstallerInstallsVerifiedDirectoryAndReusesIt(t *testing.T) {
	archive := testTarGz(t, map[string]testArchiveFile{
		"pi/pi":             {body: "#!/bin/sh\ntest \"$PI_TELEMETRY\" = 0 || exit 3\nprintf '0.83.0\\n'\n", mode: 0o755},
		"pi/docs/readme.md": {body: "bundled docs", mode: 0o644},
	})
	digest := sha256.Sum256(archive)
	var archiveRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch filepath.Base(r.URL.Path) {
		case "SHA256SUMS":
			fmt.Fprintf(w, "%s  pi-linux-x64.tar.gz\n", hex.EncodeToString(digest[:]))
		case "pi-linux-x64.tar.gz":
			archiveRequests.Add(1)
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	installer := testInstaller(t, server.URL)
	result, err := installer.Install(context.Background(), "pi")
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if !result.Installed || result.Version != "0.83.0" {
		t.Fatalf("result = %+v", result)
	}
	if _, err := os.Stat(result.Path); err != nil {
		t.Fatalf("managed executable missing: %v", err)
	}
	docs := filepath.Join(filepath.Dir(result.Path), "docs", "readme.md")
	if body, err := os.ReadFile(docs); err != nil || string(body) != "bundled docs" {
		t.Fatalf("whole runtime directory was not installed: body=%q err=%v", body, err)
	}

	again, err := installer.Install(context.Background(), "pi")
	if err != nil {
		t.Fatalf("second Install: %v", err)
	}
	if again.Installed {
		t.Fatalf("second result should reuse the verified version: %+v", again)
	}
	if got := archiveRequests.Load(); got != 1 {
		t.Fatalf("archive requests = %d, want 1", got)
	}
}

func TestInstallerCoalescesConcurrentProcessesThroughLock(t *testing.T) {
	archive := testTarGz(t, map[string]testArchiveFile{
		"pi/pi": {body: "#!/bin/sh\nprintf '0.83.0\\n'\n", mode: 0o755},
	})
	digest := sha256.Sum256(archive)
	var archiveRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/SHA256SUMS") {
			fmt.Fprintf(w, "%s  pi-linux-x64.tar.gz\n", hex.EncodeToString(digest[:]))
			return
		}
		archiveRequests.Add(1)
		_, _ = w.Write(archive)
	}))
	defer server.Close()

	installer := testInstaller(t, server.URL)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := installer.Install(context.Background(), "pi")
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent Install: %v", err)
		}
	}
	if got := archiveRequests.Load(); got != 1 {
		t.Fatalf("archive requests = %d, want 1", got)
	}
}

func TestInstallerRejectsChecksumMismatchBeforeExtraction(t *testing.T) {
	archive := testTarGz(t, map[string]testArchiveFile{
		"pi/pi": {body: "#!/bin/sh\nprintf '0.83.0\\n'\n", mode: 0o755},
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/SHA256SUMS") {
			fmt.Fprintf(w, "%064d  pi-linux-x64.tar.gz\n", 0)
			return
		}
		_, _ = w.Write(archive)
	}))
	defer server.Close()

	installer := testInstaller(t, server.URL)
	_, err := installer.Install(context.Background(), "pi")
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("Install error = %v, want checksum mismatch", err)
	}
	if _, statErr := os.Stat(filepath.Join(installer.RootDir, "pi", "versions", "0.83.0")); !os.IsNotExist(statErr) {
		t.Fatalf("unverified version must not be installed: %v", statErr)
	}
}

func TestInstallerRejectsArchivePathTraversal(t *testing.T) {
	archive := testTarGz(t, map[string]testArchiveFile{
		"pi/pi":      {body: "#!/bin/sh\nprintf '0.83.0\\n'\n", mode: 0o755},
		"../escaped": {body: "owned", mode: 0o644},
	})
	digest := sha256.Sum256(archive)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/SHA256SUMS") {
			fmt.Fprintf(w, "%s  pi-linux-x64.tar.gz\n", hex.EncodeToString(digest[:]))
			return
		}
		_, _ = w.Write(archive)
	}))
	defer server.Close()

	installer := testInstaller(t, server.URL)
	_, err := installer.Install(context.Background(), "pi")
	if err == nil || !strings.Contains(err.Error(), "escapes the destination") {
		t.Fatalf("Install error = %v, want traversal rejection", err)
	}
	if _, statErr := os.Stat(filepath.Join(installer.RootDir, "escaped")); !os.IsNotExist(statErr) {
		t.Fatalf("archive wrote outside staging: %v", statErr)
	}
}

func TestChecksumForAssetRequiresSHA256(t *testing.T) {
	if _, err := checksumForAsset([]byte("abcd  pi-linux-x64.tar.gz\n"), "pi-linux-x64.tar.gz"); err == nil {
		t.Fatal("expected malformed checksum to fail closed")
	}
	if _, err := checksumForAsset([]byte(strings.Repeat("a", 64)+"  other.tar.gz\n"), "pi-linux-x64.tar.gz"); err == nil {
		t.Fatal("expected missing asset checksum to fail closed")
	}
}

func testInstaller(t *testing.T, releaseBaseURL string) *Installer {
	t.Helper()
	d := descriptors["pi"]
	d.Source.ReleaseBaseURL = releaseBaseURL
	return &Installer{
		RootDir:     t.TempDir(),
		Client:      http.DefaultClient,
		GOOS:        "linux",
		GOARCH:      "amd64",
		Descriptors: map[string]Descriptor{"pi": d},
	}
}

type testArchiveFile struct {
	body string
	mode int64
}

func testTarGz(t *testing.T, files map[string]testArchiveFile) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, file := range files {
		header := &tar.Header{
			Name: name,
			Mode: file.mode,
			Size: int64(len(file.body)),
		}
		if err := tw.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(file.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
