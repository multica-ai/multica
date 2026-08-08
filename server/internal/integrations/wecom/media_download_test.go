package wecom

// media_download_test.go — the fetch half, against a real HTTP server.
// A callback gives us a Tencent COS URL that is good for five minutes and
// carries no auth; everything we learn about the file beyond its bytes comes
// out of the response headers, so those are what these tests pin.

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDownloadMediaReturnsTheBytes(t *testing.T) {
	payload := strings.Repeat("ciphertext", 100)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if auth := r.Header.Get("Authorization"); auth != "" {
			t.Errorf("the COS url is pre-signed; sending %q leaks a credential", auth)
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte(payload))
	}))
	defer srv.Close()

	got, err := downloadMedia(context.Background(), srv.Client(), srv.URL)
	if err != nil {
		t.Fatalf("downloadMedia: %v", err)
	}
	if string(got.Body) != payload {
		t.Fatalf("got %d bytes, want %d", len(got.Body), len(payload))
	}
	if got.Filename != "" {
		t.Fatalf("no Content-Disposition means no filename, got %q", got.Filename)
	}
}

// TestDownloadMediaReadsTheFilename covers both header forms and, crucially,
// which one wins when a server sends both: RFC 5987's filename* carries the
// non-ASCII name, and the plain filename beside it is the mangled fallback
// for old clients. Taking the fallback would name every Chinese attachment
// something like "____.pdf".
func TestDownloadMediaReadsTheFilename(t *testing.T) {
	cases := []struct {
		name        string
		disposition string
		want        string
	}{
		{"plain filename", `attachment; filename="quarterly report.pdf"`, "quarterly report.pdf"},
		{"rfc 5987 only", `attachment; filename*=UTF-8''%E5%AD%A3%E6%8A%A5.pdf`, "季报.pdf"},
		{"both, rfc 5987 wins", `attachment; filename="____.pdf"; filename*=UTF-8''%E5%AD%A3%E6%8A%A5.pdf`, "季报.pdf"},
		{"both, reversed order", `attachment; filename*=UTF-8''%E5%AD%A3%E6%8A%A5.pdf; filename="____.pdf"`, "季报.pdf"},
		{"unquoted", `attachment;filename=notes.txt`, "notes.txt"},
		{"no filename param", `inline`, ""},
		{"unparseable", `attachment; filename=`, ""},
		{"path traversal is stripped to the base name", `attachment; filename="../../etc/passwd"`, "passwd"},
		{"windows separators too", `attachment; filename="C:\\Users\\alex\\a.docx"`, "a.docx"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Disposition", tc.disposition)
				_, _ = w.Write([]byte("bytes"))
			}))
			defer srv.Close()

			got, err := downloadMedia(context.Background(), srv.Client(), srv.URL)
			if err != nil {
				t.Fatalf("downloadMedia: %v", err)
			}
			if got.Filename != tc.want {
				t.Fatalf("filename = %q, want %q", got.Filename, tc.want)
			}
		})
	}
}

// TestDownloadMediaFailsLoudlyOn404: a five-minute URL that has already
// expired is the ordinary failure here, and it must not look like an empty
// file.
func TestDownloadMediaFailsLoudlyOnAnErrorStatus(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusForbidden, http.StatusInternalServerError} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte("<Error><Code>AccessDenied</Code></Error>"))
		}))
		_, err := downloadMedia(context.Background(), srv.Client(), srv.URL)
		srv.Close()
		if err == nil {
			t.Fatalf("http %d returned no error", status)
		}
		if !strings.Contains(err.Error(), http.StatusText(status)) && !strings.Contains(err.Error(), "http "+itoa(status)) {
			t.Fatalf("http %d error does not name the status: %v", status, err)
		}
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// TestDownloadMediaGivesUpWhenTheServerStalls: COS is normally fast, but a
// download that never finishes would otherwise hold a media slot for the
// whole 45s router budget and starve everything queued behind it.
func TestDownloadMediaGivesUpWhenTheServerStalls(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer func() {
		close(release)
		srv.Close()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := downloadMedia(ctx, srv.Client(), srv.URL)
	if err == nil {
		t.Fatal("a stalled download must return an error")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("gave up after %s — the caller's deadline was not honoured", elapsed)
	}
}

// TestDownloadMediaRefusesAnOversizeBody, both ways a server can present one:
// an honest Content-Length we can reject before reading a byte, and a
// chunked response that only reveals its size as it arrives.
func TestDownloadMediaRefusesAnOversizeBody(t *testing.T) {
	t.Run("declared up front", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Length", "209715200") // 200 MB
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()
		_, err := downloadMedia(context.Background(), srv.Client(), srv.URL)
		if !errors.Is(err, errMediaTooLarge) {
			t.Fatalf("err = %v, want errMediaTooLarge", err)
		}
	})

	t.Run("only discovered while reading", func(t *testing.T) {
		chunk := make([]byte, 1<<20)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// No Content-Length: the body streams until we stop it.
			for i := 0; i < 200; i++ {
				if _, err := w.Write(chunk); err != nil {
					return
				}
				if r.Context().Err() != nil {
					return
				}
			}
		}))
		defer srv.Close()
		_, err := downloadMedia(context.Background(), srv.Client(), srv.URL)
		if !errors.Is(err, errMediaTooLarge) {
			t.Fatalf("err = %v, want errMediaTooLarge", err)
		}
	})
}

// TestDownloadMediaRefusesAUrlItShouldNotFetch: the URL arrives over the
// authenticated socket, but it is still a string from outside naming a host
// we then GET. Anything that is not http(s) is refused rather than handed to
// the transport.
func TestDownloadMediaRefusesAUrlItShouldNotFetch(t *testing.T) {
	for _, raw := range []string{"", "   ", "file:///etc/passwd", "ftp://example.invalid/a", "not a url at all"} {
		if _, err := downloadMedia(context.Background(), http.DefaultClient, raw); err == nil {
			t.Fatalf("downloadMedia(%q) returned no error", raw)
		}
	}
}
