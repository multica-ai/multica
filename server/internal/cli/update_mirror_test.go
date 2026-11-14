package cli

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// Only a temporary copy of this test executable may replace itself.
func TestFileMirrorUpdateHelper(t *testing.T) {
	if os.Getenv("MULTICA_TEST_FILE_MIRROR_UPDATE") != "1" {
		return
	}
	_, err := UpdateViaDownloadWithTimeout("v1.2.3", 3*time.Second)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

func TestUpdateFromFileMirrorWithoutReleaseAPI(t *testing.T) {
	binaryName := "multica"
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}
	payload := []byte("synthetic updated executable, never run")
	var archive bytes.Buffer
	if runtime.GOOS == "windows" {
		z := zip.NewWriter(&archive)
		w, err := z.Create(binaryName)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = w.Write(payload); err != nil {
			t.Fatal(err)
		}
		if err = z.Close(); err != nil {
			t.Fatal(err)
		}
	} else {
		gz := gzip.NewWriter(&archive)
		tw := tar.NewWriter(gz)
		if err := tw.WriteHeader(&tar.Header{Name: binaryName, Mode: 0755, Size: int64(len(payload))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(payload); err != nil {
			t.Fatal(err)
		}
		if err := tw.Close(); err != nil {
			t.Fatal(err)
		}
		if err := gz.Close(); err != nil {
			t.Fatal(err)
		}
	}
	sum := fmt.Sprintf("%x", sha256.Sum256(archive.Bytes()))
	versioned := fmt.Sprintf("multica-cli-1.2.3-%s-%s.%s", runtime.GOOS, runtime.GOARCH, releaseArchiveExtension(runtime.GOOS))
	legacy := fmt.Sprintf("multica_%s_%s.%s", runtime.GOOS, runtime.GOARCH, releaseArchiveExtension(runtime.GOOS))
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, asset, manifest, wantError string }{
		{"versioned", versioned, fmt.Sprintf("%s  %s\n", sum, versioned), ""},
		{"legacy", legacy, fmt.Sprintf("%s  %s\n", sum, legacy), ""},
		{"prefer_versioned", versioned, fmt.Sprintf("%s  %s\n%s  %s\n", sum, legacy, sum, versioned), ""},
		{"wrong_checksum", versioned, fmt.Sprintf("%064d  %s\n", 0, versioned), "checksum mismatch"},
		{"wrong_platform", "", fmt.Sprintf("%s  unrelated-archive.tar.gz\n", sum), "no matching release asset"},
		{"missing_manifest", "", "", "download checksum manifest"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			var requests []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				requests = append(requests, r.URL.Path)
				mu.Unlock()
				switch r.URL.Path {
				case "/releases/v1.2.3/checksums.txt":
					if tc.name == "missing_manifest" {
						http.NotFound(w, r)
						return
					}
					fmt.Fprint(w, tc.manifest)
				case "/releases/v1.2.3/" + tc.asset:
					w.Write(archive.Bytes())
				default:
					http.Error(w, "unexpected release API or external asset request", 500)
				}
			}))
			defer server.Close()
			childPath := filepath.Join(t.TempDir(), "updater-test")
			if runtime.GOOS == "windows" {
				childPath += ".exe"
			}
			if err := os.WriteFile(childPath, original, 0755); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(childPath, "-test.run=^TestFileMirrorUpdateHelper$")
			cmd.Env = append(os.Environ(), "MULTICA_TEST_FILE_MIRROR_UPDATE=1", releaseDownloadBaseURLEnv+"="+server.URL+"/releases/", releaseAPIBaseURLEnv+"="+server.URL+"/forbidden-api")
			output, runErr := cmd.CombinedOutput()
			installed, err := os.ReadFile(childPath)
			if err != nil {
				t.Fatal(err)
			}
			if tc.wantError == "" {
				if runErr != nil {
					t.Fatalf("update: %v: %s", runErr, output)
				}
				if !bytes.Equal(installed, payload) {
					t.Fatal("temporary executable was not replaced by verified payload")
				}
			} else {
				if runErr == nil || !strings.Contains(string(output), tc.wantError) {
					t.Fatalf("update error=%v, output=%s, want %q", runErr, output, tc.wantError)
				}
				if !bytes.Equal(installed, original) {
					t.Fatal("failed update changed executable")
				}
			}
			want := []string{"/releases/v1.2.3/checksums.txt"}
			if tc.asset != "" {
				want = append(want, "/releases/v1.2.3/"+tc.asset)
			}
			mu.Lock()
			defer mu.Unlock()
			if !reflect.DeepEqual(requests, want) {
				t.Fatalf("requests=%v, want only %v", requests, want)
			}
		})
	}
}

func TestFileMirrorDoesNotProbeHomebrew(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Homebrew is a Unix installation method")
	}
	dir := t.TempDir()
	probe := filepath.Join(dir, "brew-probe")
	script := []byte("#!/bin/sh\nprintf '/\\n'\nprintf x > \"$MULTICA_TEST_BREW_PROBE\"\n")
	if err := os.WriteFile(filepath.Join(dir, "brew"), script, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("MULTICA_TEST_BREW_PROBE", probe)
	t.Setenv(releaseDownloadBaseURLEnv, "https://mirror.example/releases")
	if IsBrewInstall() {
		t.Fatal("mirror update selected Homebrew")
	}
	if _, err := os.Stat(probe); !os.IsNotExist(err) {
		t.Fatalf("mirror update probed Homebrew: %v", err)
	}
}
