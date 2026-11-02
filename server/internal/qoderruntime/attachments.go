package qoderruntime

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"path"
	"strings"
)

type issueAttachment struct {
	ID          string `json:"id"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	SizeBytes   int64  `json:"size_bytes"`
	DownloadURL string `json:"download_url"`
}

const (
	// Qoder's file upload accepts about 5 MB of text.
	maxAttachmentFileBytes = 5 << 20
	// Base64 image blocks are limited to 10 MiB of encoded text.
	maxAttachmentImageBytes = 7 << 20
	// Images are kept in the journal until the message is sent.
	maxAttachmentImageTotal = 20 << 20
	attachmentMountDir      = "/data/attachments/"
)

var imageMediaTypes = map[string]bool{"image/png": true, "image/jpeg": true, "image/webp": true, "image/gif": true}

var textMediaTypes = map[string]bool{"application/json": true, "application/xml": true, "application/javascript": true, "application/x-yaml": true, "application/x-toml": true}

// Extensions Qoder's file upload accepts when the stored MIME type is generic.
var textExtensions = map[string]bool{}

func init() {
	for _, ext := range strings.Fields(".txt .md .csv .json .xml .yaml .yml .toml .ini .conf .cfg .env .log .html .htm .css .scss .less .js .jsx .ts .tsx .vue .svelte .py .go .rs .java .kt .scala .c .cpp .cc .h .hpp .rb .php .swift .r .lua .pl .sh .bash .zsh .fish .ps1 .sql .graphql .gql .proto .dockerfile .makefile .gitignore .editorconfig .eslintrc .prettierrc .tex .rst .adoc .org .svg") {
		textExtensions[ext] = true
	}
}

func isTextAttachment(a issueAttachment) bool {
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(a.ContentType, ";")[0]))
	return strings.HasPrefix(mediaType, "text/") || textMediaTypes[mediaType] || textExtensions[strings.ToLower(path.Ext(a.Filename))]
}

// attachIssueFiles hands issue attachments to the Qoder session: text files are
// uploaded through Qoder Files and mounted into the session, and images are
// sent as image blocks with the task message. Other files are listed in the
// prompt as unavailable so the agent knows they exist.
func (b *Bridge) attachIssueFiles(ctx context.Context, taskAPI *api, attachments []issueAttachment, r *run, p *strings.Builder) error {
	if len(attachments) == 0 {
		return nil
	}
	var mounted, images, skipped []string
	used := map[string]bool{}
	imageBytes := 0
	for _, a := range attachments {
		name := attachmentName(a, used)
		mediaType := strings.ToLower(strings.TrimSpace(strings.Split(a.ContentType, ";")[0]))
		switch {
		case imageMediaTypes[mediaType]:
			data, err := b.downloadAttachment(ctx, taskAPI, a, maxAttachmentImageBytes)
			if errors.Is(err, errTooLarge) || err == nil && imageBytes+len(data) > maxAttachmentImageTotal {
				skipped = append(skipped, name+" (image too large)")
				continue
			}
			if err != nil {
				return err
			}
			imageBytes += len(data)
			r.Images = append(r.Images, map[string]string{"type": "base64", "media_type": mediaType, "data": base64.StdEncoding.EncodeToString(data)})
			images = append(images, name)
		case isTextAttachment(a):
			data, err := b.downloadAttachment(ctx, taskAPI, a, maxAttachmentFileBytes)
			if errors.Is(err, errTooLarge) {
				skipped = append(skipped, name+" (file too large)")
				continue
			}
			if err != nil {
				return err
			}
			var file struct {
				ID string `json:"id"`
			}
			if err := b.qoder.upload(ctx, "/files", name, data, &file); err != nil {
				var rejected *apiError
				if errors.As(err, &rejected) && (rejected.Status == 400 || rejected.Status == 413 || rejected.Status == 415) {
					skipped = append(skipped, name+" (rejected by Qoder Files)")
					continue
				}
				return fmt.Errorf("upload attachment %s to Qoder: %w", name, err)
			}
			if file.ID == "" {
				return fmt.Errorf("Qoder file upload returned no file ID")
			}
			mountPath := attachmentMountDir + name
			r.Files = append(r.Files, map[string]any{"type": "file", "file_id": file.ID, "mount_path": mountPath})
			mounted = append(mounted, mountPath)
		default:
			skipped = append(skipped, name+" (unsupported file type)")
		}
	}
	p.WriteString("\nIssue attachments:\n")
	for _, m := range mounted {
		fmt.Fprintf(p, "- %s (mounted file)\n", m)
	}
	for _, name := range images {
		fmt.Fprintf(p, "- %s (image attached to this message)\n", name)
	}
	for _, s := range skipped {
		fmt.Fprintf(p, "- %s: not available in this session\n", s)
	}
	return nil
}

func (b *Bridge) downloadAttachment(ctx context.Context, taskAPI *api, a issueAttachment, limit int64) ([]byte, error) {
	if a.SizeBytes > limit {
		return nil, errTooLarge
	}
	if a.DownloadURL == "" {
		return nil, fmt.Errorf("attachment %s has no download URL", a.Filename)
	}
	data, err := taskAPI.download(ctx, a.DownloadURL, limit)
	if err != nil && !errors.Is(err, errTooLarge) {
		return nil, fmt.Errorf("download attachment %s: %w", a.Filename, err)
	}
	return data, err
}

// attachmentName returns a unique, path-free file name for the mount path.
func attachmentName(a issueAttachment, used map[string]bool) string {
	name := strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r < 0x20 {
			return '_'
		}
		return r
	}, strings.TrimSpace(a.Filename))
	if name == "" || name == "." || name == ".." {
		name = "attachment"
	}
	if len(name) > 200 {
		name = name[len(name)-200:]
	}
	unique := name
	for i := 2; used[unique]; i++ {
		ext := path.Ext(name)
		unique = fmt.Sprintf("%s-%d%s", strings.TrimSuffix(name, ext), i, ext)
	}
	used[unique] = true
	return unique
}
