package qoderruntime

import (
	"context"
	"errors"
	"net/http"
	"net/url"
)

// A delivered file is downloaded from Qoder and uploaded to Multica within one
// 30-second bridge step, so it is kept small.
const maxReplyFileBytes = 10 << 20

// replyFiles carries the files a chat agent delivers with DeliverArtifacts.
// They are attached to the reply before the run completes.
type replyFiles struct {
	Delivered []deliveredFile `json:"delivered,omitempty"`
	// Handled counts delivered files already attached or skipped, so a retry
	// does not upload one twice.
	Handled int `json:"handled,omitempty"`
	// The task credential is never persisted; after a restart the remaining
	// files are not attached and the run completes without them.
	token string
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
	d := f.Delivered[f.Handled]
	if reason, err := b.transferReplyFile(ctx, r, d); err != nil {
		return true, err
	} else if reason != "" {
		b.log.WarnContext(ctx, "Qoder delivered a file that is not attached to the chat reply", "task_id", r.TaskID, "file", d.Name, "reason", reason)
	}
	f.Handled++
	return f.Handled < len(f.Delivered), b.persist()
}

// transferReplyFile returns why the file is skipped, or an error to retry.
func (b *Bridge) transferReplyFile(ctx context.Context, r *run, d deliveredFile) (string, error) {
	if r.Reply.token == "" {
		return "the bridge restarted during the run", nil
	}
	if d.Size > maxReplyFileBytes {
		return "larger than the reply file limit", nil
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
		return "Qoder does not offer the file for download", nil
	} else if err != nil {
		return "", err
	}
	if meta.Scope == nil || meta.Scope.ID != r.SessionID {
		return "the file does not belong to this run's Qoder session", nil
	}
	// Qoder answers 403 for a file it keeps for internal use only.
	var link struct {
		URL string `json:"url"`
	}
	if err := b.qoder.call(ctx, http.MethodGet, filePath+"/content", nil, &link); refused(err) {
		return "Qoder does not offer the file for download", nil
	} else if err != nil {
		return "", err
	}
	data, err := b.qoder.download(ctx, link.URL, maxReplyFileBytes)
	if errors.Is(err, errTooLarge) {
		return "larger than the reply file limit", nil
	} else if definitelyRejected(err) {
		return "the download was refused", nil
	} else if err != nil {
		return "", err
	}
	name := d.Name
	if name == "" {
		name = d.FileID
	}
	taskAPI := *b.multica
	taskAPI.token = r.Reply.token
	if err := taskAPI.upload(ctx, "/api/upload-file", name, data, map[string]string{"task_id": r.TaskID}, nil); definitelyRejected(err) {
		return "Multica refused the upload", nil
	} else if err != nil {
		return "", err
	}
	return "", nil
}
