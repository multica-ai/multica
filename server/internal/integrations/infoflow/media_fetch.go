package infoflow

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/dolfly/infoflow"
	"github.com/dolfly/infoflow/bot/im"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

// sdkMediaDownloader is the production mediaDownloader. An inbound attachment is
// read back through the robot's own file API — never through a URL the platform
// put in the message body.
//
// Two routes, because the platform's own payloads differ:
//
//   - an FID resolves through im.File.Download, which asks the open platform
//     for a short-lived URL and fetches the bytes with the app token. This is
//     the route the SDK documents for chat files, and the only one that needs
//     no egress decision here.
//   - an object key (what the outbound image API speaks) resolves through
//     im.File.GetURL to a presigned URL, which is then fetched over the guarded
//     client in media_http.go. A presigned URL is a bearer value that came off
//     the wire, so it is treated exactly like the ones WeCom and DingTalk hand
//     over: public addresses only, no proxy, no cross-origin downgrade.
type sdkMediaDownloader struct {
	client *infoflow.Client
	http   *http.Client
}

var _ mediaDownloader = sdkMediaDownloader{}

func (d sdkMediaDownloader) Fetch(ctx context.Context, target sendTarget, messageID string, desc mediaDescriptor) (mediaPayload, error) {
	refs := desc.references()
	if len(refs) == 0 {
		return mediaPayload{}, errors.New("attachment carries no resolvable reference")
	}
	// An explicit FID goes through the chat-file route first, which is the one
	// that needs no egress decision. Anything else is tried both ways: a
	// reference the payload did not type is an FID for a group body part and an
	// object key for an image, and guessing wrong costs one rejected request.
	var firstErr error
	for _, ref := range refs {
		got, fidErr := d.fetchByFID(ctx, target, messageID, desc, ref)
		if fidErr == nil {
			return got, nil
		}
		got, keyErr := d.fetchByObjectKey(ctx, desc, ref)
		if keyErr == nil {
			return got, nil
		}
		if firstErr == nil {
			firstErr = fmt.Errorf("fid route: %v; object-key route: %w", fidErr, keyErr)
		}
	}
	return mediaPayload{}, firstErr
}

func (d sdkMediaDownloader) fetchByFID(ctx context.Context, target sendTarget, messageID string, desc mediaDescriptor, fid string) (mediaPayload, error) {
	if d.client == nil {
		return mediaPayload{}, errors.New("infoflow client is not configured")
	}
	opts := im.DownloadFileOptions{
		FID:      fid,
		ChatType: im.FileChatPrivate,
		MaxBytes: maxInboundMediaBytes,
	}
	if target.ChatType == channel.ChatTypeGroup {
		// A group file's download URL is bound to the group AND to the message
		// that carried it; the platform rejects a request that names only one.
		opts.ChatType = im.FileChatGroup
		opts.ChatID = target.ChatID
		opts.FileMsgID = messageID
	}
	got, err := d.client.Bot.IM.File.Download(ctx, opts)
	if err != nil {
		return mediaPayload{}, fmt.Errorf("download chat file: %w", err)
	}
	return mediaPayload{
		Content:     got.Content,
		ContentType: got.ContentType,
		Filename:    firstNonEmpty(desc.Filename, got.FileName),
		SizeBytes:   int64(len(got.Content)),
	}, nil
}

func (d sdkMediaDownloader) fetchByObjectKey(ctx context.Context, desc mediaDescriptor, key string) (mediaPayload, error) {
	if d.client == nil {
		return mediaPayload{}, errors.New("infoflow client is not configured")
	}
	urls, err := d.client.Bot.IM.File.GetURL(ctx, im.BosFileURLOptions{
		ObjectKey:         key,
		ExpirationSeconds: mediaURLExpirationSeconds,
	})
	if err != nil {
		return mediaPayload{}, fmt.Errorf("resolve object url: %w", err)
	}
	return d.fetchBytes(ctx, urls.URL, desc)
}

// fetchBytes GETs one presigned URL under the guarded client. The response is
// capped at the same ceiling the FID route enforces, and the query string — a
// short-lived credential — is never logged.
func (d sdkMediaDownloader) fetchBytes(ctx context.Context, rawURL string, desc mediaDescriptor) (mediaPayload, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return mediaPayload{}, errors.New("build media download request")
	}
	resp, err := d.http.Do(req)
	if err != nil {
		return mediaPayload{}, fmt.Errorf("fetch media: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return mediaPayload{}, fmt.Errorf("fetch media: status %d", resp.StatusCode)
	}
	content, err := io.ReadAll(io.LimitReader(resp.Body, maxInboundMediaBytes+1))
	if err != nil {
		return mediaPayload{}, fmt.Errorf("read media: %w", err)
	}
	if int64(len(content)) > maxInboundMediaBytes {
		return mediaPayload{}, fmt.Errorf("media is larger than %d bytes", maxInboundMediaBytes)
	}
	return mediaPayload{
		Content:     content,
		ContentType: resp.Header.Get("Content-Type"),
		Filename:    fileNameFromDisposition(resp.Header.Get("Content-Disposition"), desc.Filename),
		SizeBytes:   int64(len(content)),
	}, nil
}
