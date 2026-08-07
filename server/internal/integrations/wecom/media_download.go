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
)

// maxMediaBytes is the ceiling on one downloaded body. WeCom caps smart-bot
// files and video at 100 MB and does not document a cap for images, so this
// is the ceiling for everything: whatever arrives above it is not something
// the callback was supposed to hand us.
//
// The body is buffered whole — CBC decryption needs the tail before the head
// can be trusted — so this is also the per-download memory bound, multiplied
// by the router's media concurrency.
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

// downloadedMedia is one fetched body plus what the response said about it.
type downloadedMedia struct {
	// Body is the raw response — still encrypted. decryptMedia turns it into
	// the file.
	Body []byte
	// Filename is the display name parsed out of Content-Disposition, or
	// empty. The callback body carries no name, size or MIME type of its
	// own, so this header is the only place the original name exists.
	Filename string
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
		Body:     body,
		Filename: mediaFilenameFromDisposition(resp.Header.Get("Content-Disposition")),
	}, nil
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
// The result is reduced to a base name: the header is remote input and a
// filename with path separators in it has no business reaching a storage key
// or a download header.
func mediaFilenameFromDisposition(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	_, params, err := mime.ParseMediaType(raw)
	if err != nil {
		return ""
	}
	return cleanMediaFilename(params["filename"])
}

// cleanMediaFilename reduces a name from anywhere to a single path segment,
// or empty when nothing usable is left.
func cleanMediaFilename(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	name = path.Base(strings.ReplaceAll(name, "\\", "/"))
	if name == "." || name == "/" || name == ".." {
		return ""
	}
	return name
}

// openMedia is downloadMedia without the buffer: it returns the body as a
// stream so the caller can decrypt it as it arrives. Same guard, same status
// handling, same ceiling — the ceiling is enforced by the LimitReader the
// caller reads through, so a body that lies about its length still cannot
// run away with the process.
//
// The caller closes the reader. It owns the underlying response, so an early
// return without a Close leaks a connection.
func openMedia(ctx context.Context, hc *http.Client, rawURL string) (io.ReadCloser, string, error) {
	if err := checkMediaURL(rawURL); err != nil {
		return nil, "", err
	}
	if hc == nil {
		return nil, "", errors.New("wecom: media download: no guarded http client configured")
	}

	// No context timeout here the way downloadMedia has one: the caller reads
	// this body over the length of a decrypt, and a deadline that covers the
	// fetch would cut the read off mid-file. The router's own media budget
	// bounds the whole operation.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("wecom: media request: %w", stripURL(err))
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("wecom: media download: %w", stripURL(err))
	}
	if resp.ContentLength > maxMediaBytes {
		resp.Body.Close()
		return nil, "", errMediaTooLarge
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		resp.Body.Close()
		return nil, "", fmt.Errorf("wecom: media download: http %d %s: %s",
			resp.StatusCode, http.StatusText(resp.StatusCode), strings.TrimSpace(string(snippet)))
	}
	return &cappedBody{
		ReadCloser: resp.Body,
		remaining:  maxMediaBytes + 1, // one byte of headroom, as downloadMedia has
	}, mediaFilenameFromDisposition(resp.Header.Get("Content-Disposition")), nil
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
