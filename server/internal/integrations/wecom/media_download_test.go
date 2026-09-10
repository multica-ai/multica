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
	"unicode"
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
			if got.Disposition != tc.disposition {
				t.Errorf("raw disposition = %q, want it kept verbatim for the trace", got.Disposition)
			}
		})
	}
}

// TestTheDispositionFilenameIsFormDecoded pins the encoding COS actually puts
// in the plain filename parameter, and the exact line drawn around undoing
// it. A live tenant sent "PC D&T Strategy 2026.docx" and the object was
// stored as "PC+D%26T+Strategy+2026.docx": the header arrives
// form-urlencoded, mime.ParseMediaType percent-decodes only the extended
// filename* form, and nothing was undoing the rest.
//
// The three groups matter as much as the individual rows. The first is what
// the fix buys. The second is what a bare url.QueryUnescape would have cost —
// those rows fail on the obvious implementation, not on the absent one. The
// third is what this choice knowingly gives up, written down as expected
// values so the trade is on the record instead of in a reviewer's memory.
func TestTheDispositionFilenameIsFormDecoded(t *testing.T) {
	cases := []struct {
		name        string
		disposition string
		want        string
	}{
		// -- decoded, because re-encoding reproduces the header exactly --
		{"the reported case", `attachment; filename="PC+D%26T+Strategy+2026.docx"`, "PC D&T Strategy 2026.docx"},
		{"spaces on their own", `attachment; filename="Meeting+notes+2026.docx"`, "Meeting notes 2026.docx"},
		{"non-ascii, upper-case hex", `attachment; filename="%E5%AD%A3%E6%8A%A5.png"`, "季报.png"},
		{"non-ascii, lower-case hex", `attachment; filename="%e5%ad%a3%e6%8a%a5.png"`, "季报.png"},
		{"an escaped plus survives the decode", `attachment; filename="C%2B%2B+notes.docx"`, "C++ notes.docx"},
		{"escaped separators are decoded, then reduced", `attachment; filename="..%2F..%2Fetc%2Fpasswd"`, "passwd"},

		// -- left alone, because the value cannot have been form-encoded --
		{"a plus beside a real space is a real plus", `attachment; filename="C++ notes.docx"`, "C++ notes.docx"},
		{"a percent that begins no escape", `attachment; filename="100%.docx"`, "100%.docx"},
		{"filename* is already decoded and is not decoded twice", `attachment; filename*=UTF-8''A%2BB.docx`, "A+B.docx"},

		// -- the sacrifices, named --
		{
			// "C++.docx" is byte-identical to a form-encoding of "C  .docx",
			// so no rule reading only this header can tell them apart. We
			// decode, and this name loses its pluses.
			"a plus with no space anywhere is decoded as a space",
			`attachment; filename="C++.docx"`, "C  .docx",
		},
		{
			// The same trade, and the rows are here because "C++.docx" reads
			// as a curiosity while the class it stands for does not: every
			// name whose only encoder-touched character is a '+' loses it.
			"a product name loses its plus too",
			`attachment; filename="React+Redux.md"`, "React Redux.md",
		},
		{
			"and so does a version number",
			`attachment; filename="C++11.pdf"`, "C  11.pdf",
		},
		{
			// RFC 3986 spelling rather than the form one. Re-encoding gives
			// "Meeting+notes.docx", the check fails, and the escapes stay.
			"a %20-encoded name keeps its escapes",
			`attachment; filename="Meeting%20notes.docx"`, "Meeting%20notes.docx",
		},
		{
			// The round trip re-encodes with url.QueryEscape, whose unreserved
			// set is A-Za-z0-9-_.~ — so it assumes the server agreed on that
			// set. Java's URLEncoder also passes '*', and PHP's urlencode
			// escapes '~' as %7E, which Go would have left alone. Re-encoding
			// "backup~.docx" gives back the tilde, the lengths differ, and
			// nothing is decoded.
			"a tilde escaped by a non-Go encoder keeps its escape",
			`attachment; filename="backup%7E.docx"`, "backup%7E.docx",
		},
		{
			// The reason the row above is worth pinning: the check is
			// all-or-nothing. One character outside Go's set and the WHOLE
			// name stays escaped — including the '+' this decode exists to
			// remove — so the user is shown exactly the unreadable string the
			// fix was written to stop showing them.
			"one foreign escape keeps the whole name escaped, plus and all",
			`attachment; filename="Q1+report%7E.docx"`, "Q1+report%7E.docx",
		},
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

// TestControlCharactersNeverReachTheFilename covers what the decode widened.
//
// Undoing the escapes widened the byte domain of this name. Exactly one
// control character used to get here: a raw TAB, which net/http passes
// through in a header value where it rejects the whole response for a NUL, a
// CR, an LF, an ESC or a DEL. Since mime.ParseMediaType never percent-decodes
// the plain filename= form, `filename="a%00b.docx"` stayed the printable
// string it looks like. Decoding turns those escapes into the bytes they name.
//
// NUL is the one that costs something. The name is written to
// attachments.filename, which is Postgres TEXT, and Postgres cannot store a
// NUL in a text value — so a file whose bytes downloaded and decrypted
// perfectly is lost on the insert. CR and LF are the header-injection shape;
// internal/storage's ContentDisposition already replaces them when it builds
// the download header, so nothing is injectable either way, but the row is
// written before that and there is no reason to carry them.
//
// Every escaped row's value really does survive the round-trip check — that
// is why it reaches cleanMediaFilename decoded at all — so these assert the
// strip, not the decode declining to run. The last row is the one that needed
// no decode to get here and was never handled.
func TestControlCharactersNeverReachTheFilename(t *testing.T) {
	cases := []struct {
		name        string
		disposition string
		want        string
	}{
		{"a NUL, which Postgres TEXT cannot store", `attachment; filename="a%00b.docx"`, "ab.docx"},
		{"a CRLF, the header-injection shape", `attachment; filename="a%0D%0Ab.docx"`, "ab.docx"},
		{"a tab", `attachment; filename="a%09b.docx"`, "ab.docx"},
		{"an ESC, which a terminal reading the log would act on", `attachment; filename="a%1Bb.docx"`, "ab.docx"},
		{"a DEL", `attachment; filename="a%7Fb.docx"`, "ab.docx"},
		{"nothing but control characters leaves no name at all", `attachment; filename="%00%01%02"`, ""},
		{"a raw tab, the one that never needed the decode", "attachment; filename=\"a\tb.docx\"", "ab.docx"},
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
			for _, r := range got.Filename {
				if unicode.IsControl(r) {
					t.Fatalf("filename %q still carries a control character %U", got.Filename, r)
				}
			}
		})
	}
}
