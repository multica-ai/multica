package qoderruntime

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// A delivered file is downloaded from Qoder and uploaded to Multica within one
// 30-second bridge step, so it is kept small.
const (
	maxReplyFileBytes    = 10 << 20
	maxReplyFileAttempts = 3
	replyFileTimeout     = 20 * time.Second
)

// replyFiles carries the files a chat agent delivers with DeliverArtifacts.
// They are attached to the reply before the run completes.
type replyFiles struct {
	Delivered []deliveredFile `json:"delivered,omitempty"`
	// Handled counts delivered files already attached or skipped, so a retry
	// does not upload one twice.
	Handled int `json:"handled,omitempty"`
	// Attempts counts failed reads of the next file, never upload attempts.
	Attempts int             `json:"attempts,omitempty"`
	Skipped  []replyFileSkip `json:"skipped,omitempty"`
	// The task credential is never persisted; after a restart the remaining
	// files are not attached and the run completes without them.
	token string
}

type replyFileSkip struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

type deliveredFile struct {
	FileID string `json:"file_id"`
	Name   string `json:"name"`
	Size   int64  `json:"size"`
}

func (f *replyFiles) add(e event) {
	for _, d := range f.Delivered {
		if d.FileID == e.FileID {
			return
		}
	}
	f.Delivered = append(f.Delivered, deliveredFile{FileID: e.FileID, Name: e.OriginalFilename, Size: e.Size})
}

// attachReplyFile handles the next delivered file and reports whether any
// remain, one per step so each transfer has the step to itself. A file that
// cannot be attached is logged and skipped; the reply is still delivered.
func (b *Bridge) attachReplyFile(ctx context.Context, r *run) (bool, error) {
	f := r.Reply
	if f.Handled >= len(f.Delivered) {
		return false, nil
	}
	if err := ctx.Err(); err != nil {
		return true, err
	}
	d := f.Delivered[f.Handled]
	transferCtx, cancel := context.WithTimeout(ctx, replyFileTimeout)
	defer cancel()
	reason, err := b.transferReplyFile(transferCtx, r, d)
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return true, ctx.Err()
		}
		f.Attempts++
		if f.Attempts < maxReplyFileAttempts {
			if persistErr := b.persist(); persistErr != nil {
				return true, persistErr
			}
			return true, err
		}
		reason = fmt.Sprintf("not attached because the file transfer failed after %d attempts", maxReplyFileAttempts)
	}
	if reason != "" {
		name := d.Name
		if name == "" {
			name = d.FileID
		}
		f.Skipped = append(f.Skipped, replyFileSkip{Name: name, Reason: reason})
		b.log.WarnContext(ctx, "Qoder file delivery was not confirmed", "task_id", r.TaskID, "file", name, "reason", reason)
	}
	f.Handled++
	f.Attempts = 0
	return f.Handled < len(f.Delivered), b.persist()
}

// transferReplyFile retries only reads. Once an upload is attempted, an
// ambiguous result must not replay a write without an idempotency key.
func (b *Bridge) transferReplyFile(ctx context.Context, r *run, d deliveredFile) (string, error) {
	if r.Reply.token == "" {
		return "not attached because the bridge restarted during the run", nil
	}
	if d.Size > maxReplyFileBytes {
		return "not attached because it exceeds the 10 MB limit", nil
	}
	refused := func(err error) bool {
		var ae *apiError
		return errors.As(err, &ae) && (ae.Status == http.StatusNotFound || ae.Status == http.StatusForbidden)
	}
	// The token reads every file of the Qoder account, so only a file scoped
	// to this run's session is passed on, never another chat's.
	filePath := "/files/" + url.PathEscape(d.FileID)
	var meta struct {
		Scope *struct {
			ID string `json:"id"`
		} `json:"scope"`
	}
	if err := b.qoder.call(ctx, http.MethodGet, filePath, nil, &meta); refused(err) {
		return "not attached because Qoder did not offer the file for download", nil
	} else if err != nil {
		return "", err
	}
	if meta.Scope == nil || meta.Scope.ID != r.SessionID {
		return "not attached because access to the file could not be verified", nil
	}
	// Qoder answers 403 for a file it keeps for internal use only.
	var link struct {
		URL string `json:"url"`
	}
	if err := b.qoder.call(ctx, http.MethodGet, filePath+"/content", nil, &link); refused(err) {
		return "not attached because Qoder did not offer the file for download", nil
	} else if err != nil {
		return "", err
	}
	data, err := b.qoder.download(ctx, link.URL, maxReplyFileBytes)
	if errors.Is(err, errTooLarge) {
		return "not attached because it exceeds the 10 MB limit", nil
	} else if definitelyRejected(err) {
		return "not attached because the download was refused", nil
	} else if err != nil {
		return "", err
	}
	name := d.Name
	if name == "" {
		name = d.FileID
	}
	taskAPI := *b.multica
	taskAPI.token = r.Reply.token
	if err := ctx.Err(); err != nil {
		return "", err
	}
	var attachment struct {
		ID string `json:"id"`
	}
	if err := taskAPI.upload(ctx, "/api/upload-file", name, data, map[string]string{"task_id": r.TaskID}, &attachment); definitelyRejected(err) {
		return "not attached because Multica refused the upload", nil
	} else if err != nil {
		return "delivery could not be confirmed; check the attachments before retrying", nil
	}
	if strings.TrimSpace(attachment.ID) == "" {
		return "not attached because Multica could not save the attachment", nil
	}
	return "", nil
}

// output leaves the agent's text unchanged and adds persisted delivery notices.
// Building it without mutating run.Output keeps completion retries identical.
func (f *replyFiles) output(text string) string {
	if len(f.Skipped) == 0 {
		return text
	}
	var out strings.Builder
	out.WriteString(text)
	if text != "" {
		out.WriteString("\n\n")
	}
	out.WriteString("Attachment delivery notice:\n\n")
	for _, file := range f.Skipped {
		// Names are data, not links, markup or additional lines in the notice.
		name := strings.NewReplacer("\\", "\\\\", "`", "\\`", "*", "\\*", "_", "\\_", "[", "\\[", "]", "\\]", "<", "\\<", ">", "\\>").Replace(strconv.Quote(file.Name))
		fmt.Fprintf(&out, "- %s: %s.\n", name, file.Reason)
	}
	return strings.TrimSuffix(out.String(), "\n")
}
