package wecom

// media_download.go — fetching the bytes a callback points at.
//
// The URL is a pre-signed Tencent COS address: no access_token, no header,
// good for five minutes. That makes the fetch itself trivial and puts all the
// care somewhere else — the body arrives encrypted, its size is not declared
// anywhere in the callback, and the only description of what the file IS
// comes back in the response headers.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
	"unicode"
)

// maxMediaBytes is the ceiling on one downloaded body. WeCom caps smart-bot
// files and video at 100 MB and does not document a cap for images, so this
// is the ceiling for everything: whatever arrives above it is not something
// the callback was supposed to hand us.
//
// On this path the body is buffered whole — CBC decryption needs the tail
// before the head can be trusted — so the ceiling is also the per-download
// memory bound, multiplied by the router's media concurrency. This path is the
// fallback: a storage backend implementing UploadStream (both shipped ones do)
// goes through media_stream.go instead, where the same ceiling bounds disk.
const maxMediaBytes = 100 << 20

// mediaDownloadTimeout caps a single fetch. The router already runs media
// resolution under a 45s budget shared by every attachment on the message and
// by whatever is queued ahead of it, so one slow object must not eat all of
// it.
const mediaDownloadTimeout = 30 * time.Second

// errMediaTooLarge is returned for a body past maxMediaBytes, either declared
// in Content-Length or discovered while reading. Callers match on it to tell
// the user the file was too big rather than that something went wrong.
var errMediaTooLarge = fmt.Errorf("wecom: media exceeds the %d byte limit", maxMediaBytes)

// mediaHeaders is what the response said about the file. Both fetch paths
// return it, so the buffered and the streaming ingest learn the same things.
type mediaHeaders struct {
	// Filename is the display name parsed out of Content-Disposition, or
	// empty. The callback body carries no name, size or MIME type of its
	// own, so this header is the only place the original name exists.
	Filename string
	// Disposition is the Content-Disposition value exactly as it arrived,
	// kept for the trace and for nothing else. What COS puts in that header
	// is the one thing this package cannot check locally — the URL it came
	// from lapses after five minutes, so a name that looks wrong cannot be
	// re-fetched and must have been recorded at the time.
	Disposition string
}

// downloadedMedia is one fetched body plus what the response said about it.
type downloadedMedia struct {
	// Body is the raw response — still encrypted. decryptMedia turns it into
	// the file.
	Body []byte
	mediaHeaders
}

// downloadMedia GETs one media URL. Errors carry the reason (expired link,
// oversize body, stalled server) because the caller turns them into something
// a person reads.
func downloadMedia(ctx context.Context, hc *http.Client, rawURL string) (downloadedMedia, error) {
	if err := checkMediaURL(rawURL); err != nil {
		return downloadedMedia{}, err
	}
	// A missing client is a wiring bug, and the tempting recovery —
	// http.DefaultClient — is precisely the unguarded fetch media_guard.go
	// exists to prevent. Refusing turns that bug into a failed download
	// instead of an SSRF.
	if hc == nil {
		return downloadedMedia{}, errors.New("wecom: media download: no guarded http client configured")
	}
	ctx, cancel := context.WithTimeout(ctx, mediaDownloadTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return downloadedMedia{}, fmt.Errorf("wecom: media request: %w", stripURL(err))
	}
	resp, err := hc.Do(req)
	if err != nil {
		return downloadedMedia{}, fmt.Errorf("wecom: media download: %w", stripURL(err))
	}
	defer resp.Body.Close()

	if resp.ContentLength > maxMediaBytes {
		return downloadedMedia{}, errMediaTooLarge
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// A five-minute URL that has already lapsed is the ordinary failure
		// here, and COS explains itself in a short XML body. Carry a snippet
		// so the log says which kind of refusal it was.
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return downloadedMedia{}, fmt.Errorf("wecom: media download: http %d %s: %s",
			resp.StatusCode, http.StatusText(resp.StatusCode), strings.TrimSpace(string(snippet)))
	}

	// LimitReader with one byte of headroom: reading exactly the cap cannot
	// tell "the file is exactly at the limit" from "there is more coming".
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxMediaBytes+1))
	if err != nil {
		return downloadedMedia{}, fmt.Errorf("wecom: media download: read body: %w", err)
	}
	if len(body) > maxMediaBytes {
		return downloadedMedia{}, errMediaTooLarge
	}
	return downloadedMedia{
		Body:         body,
		mediaHeaders: mediaHeadersOf(resp),
	}, nil
}

// mediaHeadersOf reads the one response header that describes the file.
func mediaHeadersOf(resp *http.Response) mediaHeaders {
	raw := resp.Header.Get("Content-Disposition")
	return mediaHeaders{Filename: mediaFilenameFromDisposition(raw), Disposition: raw}
}

// stripURL removes the request URL from a transport error before it can be
// logged.
//
// net/http wraps every failure in a *url.Error whose Error() prints the URL
// it was fetching — and the URL here is a pre-signed COS link: a five-minute
// bearer credential for a colleague's private attachment, good to anyone who
// presents it. A DNS hiccup, a TCP reset, a TLS error or the download timeout
// would each have written one into the application log, and from there into
// whatever ships those logs onward. The wrapped cause carries the same
// diagnosis with none of the credential, so that is what we keep.
func stripURL(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) && urlErr.Err != nil {
		return urlErr.Err
	}
	return err
}

// checkMediaURL refuses anything the transport should not be pointed at. The
// URL arrives over the authenticated socket, so this is a guard rail rather
// than a defence, but it is a string from outside naming a host we then GET.
func checkMediaURL(rawURL string) error {
	s := strings.TrimSpace(rawURL)
	if s == "" {
		return errors.New("wecom: media url is empty")
	}
	u, err := url.Parse(s)
	if err != nil {
		// url.Parse's error text quotes the whole input, and the input here
		// is a live pre-signed link. Say what was wrong, not what it was.
		return errors.New("wecom: media url is unparseable")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("wecom: media url scheme %q is not fetchable", u.Scheme)
	}
	if u.Host == "" {
		return errors.New("wecom: media url has no host")
	}
	return nil
}

// mediaFilenameFromDisposition reads the display name out of
// Content-Disposition, preferring RFC 5987's extended `filename*` form over the
// plain `filename=` beside it. Servers that send both put the real (non-ASCII)
// name in the extended form and a mangled ASCII approximation in the plain
// one, so taking the plain one would rename every Chinese attachment to
// underscores. mime.ParseMediaType already resolves that preference for us
// regardless of the order the two appear in; the tests pin it because it is a
// property we depend on, not one we implement.
//
// A plain `filename=` from COS arrives form-urlencoded, and ParseMediaType
// does not undo that (it percent-decodes only the extended form), so the
// value needs decodeFormEncodedFilename before it is fit to show anyone. The
// extended form is skipped there, because a server that sent `filename*`
// declared its own encoding and ParseMediaType has already applied it —
// decoding a second time would be guessing on top of a declaration.
//
// The decode runs BEFORE the base-name reduction, not after: an escaped
// separator (`..%2F..%2Fetc%2Fpasswd`) is a path only once it is decoded, and
// a cleaner that ran first would hand a traversal straight through.
//
// The result goes through cleanMediaFilename, which reduces it to a base name
// and strips control characters: the header is remote input, and once the
// escapes above are undone it can carry a separator or a NUL that the raw
// header could not.
func mediaFilenameFromDisposition(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	_, params, err := mime.ParseMediaType(raw)
	if err != nil {
		return ""
	}
	name := params["filename"]
	if !hasExtendedFilename(raw) {
		name = decodeFormEncodedFilename(name)
	}
	return cleanMediaFilename(name)
}

// hasExtendedFilename reports whether the header offered an RFC 5987
// `filename*` at all. mime.ParseMediaType folds the extended parameter into
// params["filename"] and does not say which form it came from, so the raw
// header is the only place left to ask.
//
// A plain filename whose VALUE contains the text "filename*" reads as a false
// positive here. That costs nothing: the only consequence is that the value
// is left exactly as it arrived.
func hasExtendedFilename(raw string) bool {
	return strings.Contains(strings.ToLower(raw), "filename*")
}

// decodeFormEncodedFilename undoes application/x-www-form-urlencoding on a
// filename that provably carries it, and leaves every other name byte for
// byte.
//
// Why this is needed: a live tenant sent "PC D&T Strategy 2026.docx" and it
// was stored as "PC+D%26T+Strategy+2026.docx" — space as '+', '&' as %26,
// which is exactly url.QueryEscape of the original. COS form-encodes the
// plain filename parameter because that parameter is ASCII-only by
// specification, and Chinese names take the same route: a non-ASCII name
// cannot legally sit there, so it arrives as a run of percent escapes, and
// without this the user is shown the escapes.
//
// Why it is conditional: url.QueryUnescape on its own reads '+' as a space,
// so it would rewrite "C++ notes.docx" to "C   notes.docx". Both readings of
// a bare '+' are legitimate and no header field distinguishes them. What does
// distinguish them is self-consistency — a genuine form-encoding survives a
// round trip through the encoder, and an accidental one usually does not:
// "C++ notes.docx" re-encodes to "C+++notes.docx" (the literal space would
// have been a '+' too) and is therefore left alone. So the rule is: decode
// only when re-encoding the result reproduces the header value.
//
// What this does NOT handle, and why:
//
//   - Any name whose only encoder-touched character is a '+' IS a canonical
//     encoding of the same name with spaces, so the round trip cannot rule it
//     out and this decodes it, losing the pluses. That is a wider class than
//     one odd example: "C++.docx" → "C  .docx", and so do "React+Redux.md",
//     "Q1+Q2.xlsx", "C++11.pdf", "10+1.pdf". The ambiguity is irreducible from
//     the header alone, and this is the side chosen deliberately — a name with
//     a space is far more common than one with a '+' and no space, and a name
//     with both is already safe by the round trip above.
//   - A name percent-encoded in the RFC 3986 style, with %20 for space rather
//     than '+' ("PC%20D&T.docx"), re-encodes to "PC+D%26T.docx", fails the
//     check, and keeps its escapes.
//   - A name from a server whose unreserved set is not Go's. The round trip
//     re-encodes with url.QueryEscape, which leaves A-Za-z0-9-_.~ alone, so
//     the check silently assumes the sender agreed on that set. Java's
//     URLEncoder also passes '*', and PHP's urlencode escapes '~' as %7E, so
//     "backup~.docx" from either arrives as "backup%7E.docx", re-encodes to
//     "backup~.docx", and the lengths no longer match. The failure is
//     all-or-nothing rather than partial: ONE character outside Go's set and
//     the entire name keeps its escapes, so "Q1 report~.docx" is shown as
//     "Q1+report%7E.docx" — the unreadable state this decode exists to
//     remove, with the '+' still in it. Widening the comparison to tolerate
//     an escape on either side would mean deciding which of several encoders
//     wrote the header, and the evidence for that decision does not exist
//     yet.
//
// The gaps stay open because one stored filename is all the evidence there
// is, and the URL that produced it is long expired. traceMediaHeaders records
// the header as it arrives, so the next live run settles what COS really
// sends with bytes rather than with another inference from one sample.
func decodeFormEncodedFilename(name string) string {
	// Nothing an encoder produces looks like this, so there is nothing to
	// undo and no round trip worth running.
	if !strings.ContainsAny(name, "+%") {
		return name
	}
	decoded, err := url.QueryUnescape(name)
	if err != nil {
		// A stray '%' that begins no valid escape ("100%.docx") means this is
		// not an encoding at all, so there is nothing to undo.
		return name
	}
	if decoded == name || !sameFormEncoding(url.QueryEscape(decoded), name) {
		return name
	}
	return decoded
}

// sameFormEncoding reports whether two form-encodings of the same value agree,
// allowing only for the case of the hex digits inside percent escapes.
//
// The tolerance is not cosmetic. url.QueryEscape writes upper-case hex and
// plenty of servers write lower-case, so a byte comparison would reject
// "%e5%ad%a3%e6%8a%a5.png" — a Chinese filename, the case this decode matters
// most for, since a non-ASCII name is nothing but escapes — and quietly do
// nothing.
//
// Hex case is the ONLY tolerance, which is what makes the round trip assume
// the sender's unreserved set is url.QueryEscape's. A server that escapes one
// character Go leaves alone fails the comparison outright; see the third gap
// listed on decodeFormEncodedFilename for what that costs.
func sameFormEncoding(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); {
		if a[i] == '%' && b[i] == '%' && i+2 < len(a) {
			if !strings.EqualFold(a[i+1:i+3], b[i+1:i+3]) {
				return false
			}
			i += 3
			continue
		}
		if a[i] != b[i] {
			return false
		}
		i++
	}
	return true
}

// cleanMediaFilename reduces a name from anywhere to a single path segment
// with no control characters in it, or empty when nothing usable is left.
//
// The control-character strip is here because decodeFormEncodedFilename
// widened what can reach this function. Of the control characters, exactly one
// used to get this far: measured against net/http, a raw TAB in a header value
// is passed through and every other control byte — NUL, CR, LF, ESC, DEL —
// makes the transport reject the whole response. Since ParseMediaType never
// percent-decodes the plain filename= form, `filename="a%00b.docx"` stayed the
// printable string it looks like. Undoing the escapes turns all of them into
// the bytes they name: %00 into a real NUL, %0D%0A into a real CRLF.
//
// NUL is the one that costs something. This name is written to
// attachments.filename, which is Postgres TEXT, and Postgres cannot store a
// NUL in a text value at all — the insert fails and the whole attachment is
// lost, for a file whose bytes downloaded and decrypted perfectly. CR and LF
// are the header-injection shape; internal/storage's ContentDisposition
// already replaces them when it builds the download header, so that end is
// covered, but the attachments row is written before that and there is no
// reason to carry them to it.
//
// Reaching any of this needs a malicious or buggy CDN — a user cannot type a
// NUL into a filename. That is the right bar anyway: this header is remote
// input, and this function is the single choke point both fetch paths reach it
// through.
func cleanMediaFilename(name string) string {
	name = strings.TrimSpace(stripControlRunes(name))
	if name == "" {
		return ""
	}
	name = path.Base(strings.ReplaceAll(name, "\\", "/"))
	if name == "." || name == "/" || name == ".." {
		return ""
	}
	return name
}

// stripControlRunes drops every control character, dropping rather than
// substituting: a placeholder would put a character in the name that the
// sender did not type, and an attachment called "a_b.docx" that was really
// called "ab.docx" is its own small lie. Runes that decoded to invalid UTF-8
// are left as they are — RuneError is not a control character, and mangling a
// name further is not an improvement.
func stripControlRunes(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
}

// openMedia is downloadMedia without the buffer: it returns the body as a
// stream so the caller can decrypt it as it arrives, plus the same
// mediaHeaders the buffered path reads. Same guard, same status handling,
// same ceiling — the ceiling is enforced by the LimitReader the caller reads
// through, so a body that lies about its length still cannot run away with
// the process.
//
// The caller closes the reader. It owns the underlying response, so an early
// return without a Close leaks a connection.
func openMedia(ctx context.Context, hc *http.Client, rawURL string) (io.ReadCloser, mediaHeaders, error) {
	if err := checkMediaURL(rawURL); err != nil {
		return nil, mediaHeaders{}, err
	}
	if hc == nil {
		return nil, mediaHeaders{}, errors.New("wecom: media download: no guarded http client configured")
	}

	// No context timeout here the way downloadMedia has one: the caller reads
	// this body over the length of a decrypt, and a deadline that covers the
	// fetch would cut the read off mid-file. The router's own media budget
	// bounds the whole operation.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, mediaHeaders{}, fmt.Errorf("wecom: media request: %w", stripURL(err))
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, mediaHeaders{}, fmt.Errorf("wecom: media download: %w", stripURL(err))
	}
	if resp.ContentLength > maxMediaBytes {
		resp.Body.Close()
		return nil, mediaHeaders{}, errMediaTooLarge
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		resp.Body.Close()
		return nil, mediaHeaders{}, fmt.Errorf("wecom: media download: http %d %s: %s",
			resp.StatusCode, http.StatusText(resp.StatusCode), strings.TrimSpace(string(snippet)))
	}
	return &cappedBody{
		ReadCloser: resp.Body,
		remaining:  maxMediaBytes + 1, // one byte of headroom, as downloadMedia has
	}, mediaHeadersOf(resp), nil
}

// cappedBody stops a response that keeps going past the ceiling, so an
// undeclared length cannot be used to fill the disk the way it used to be
// able to fill the heap.
type cappedBody struct {
	io.ReadCloser
	remaining int64
}

func (c *cappedBody) Read(p []byte) (int, error) {
	if c.remaining <= 0 {
		return 0, errMediaTooLarge
	}
	if int64(len(p)) > c.remaining {
		p = p[:c.remaining]
	}
	n, err := c.ReadCloser.Read(p)
	c.remaining -= int64(n)
	if c.remaining <= 0 && err == nil {
		return n, errMediaTooLarge
	}
	return n, err
}
