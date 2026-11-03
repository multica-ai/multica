package qoderruntime

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
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
	// Qoder echoes the submitted message in the send response and in the event
	// list, and the bridge reads at most 8 MiB per response. Keep the encoded
	// images of one message well below that so the echo stays readable.
	maxAttachmentImageEncoded = 6 << 20
	maxAttachmentImageBytes   = maxAttachmentImageEncoded / 4 * 3
	// Qoder rejects base64 images with a side longer than this.
	maxAttachmentImageSide = 8000
	attachmentMountDir     = "/data/attachments/"
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
	imageEncoded := 0
	for _, a := range attachments {
		name := attachmentName(a, used)
		mediaType := strings.ToLower(strings.TrimSpace(strings.Split(a.ContentType, ";")[0]))
		switch {
		case imageMediaTypes[mediaType]:
			data, err := b.downloadAttachment(ctx, taskAPI, a, maxAttachmentImageBytes)
			if errors.Is(err, errTooLarge) || err == nil && imageEncoded+base64.StdEncoding.EncodedLen(len(data)) > maxAttachmentImageEncoded {
				skipped = append(skipped, name+" (image too large)")
				continue
			}
			if err != nil {
				return err
			}
			width, height, ok := imageSize(mediaType, data)
			if !ok {
				skipped = append(skipped, name+" (unreadable image)")
				continue
			}
			if width > maxAttachmentImageSide || height > maxAttachmentImageSide {
				skipped = append(skipped, fmt.Sprintf("%s (image larger than %d px per side)", name, maxAttachmentImageSide))
				continue
			}
			encoded := base64.StdEncoding.EncodeToString(data)
			imageEncoded += len(encoded)
			r.Images = append(r.Images, map[string]string{"type": "base64", "media_type": mediaType, "data": encoded})
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
			if err := b.qoder.upload(ctx, "/files", name, data, nil, &file); err != nil {
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

// imageSize reads the pixel dimensions from the image header. The standard
// library has no WebP decoder, so WebP headers are parsed directly.
func imageSize(mediaType string, data []byte) (int, int, bool) {
	if mediaType != "image/webp" {
		cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
		return cfg.Width, cfg.Height, err == nil
	}
	if len(data) < 30 || string(data[0:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
		return 0, 0, false
	}
	switch string(data[12:16]) {
	case "VP8 ":
		if data[23] != 0x9d || data[24] != 0x01 || data[25] != 0x2a {
			return 0, 0, false
		}
		return int(binary.LittleEndian.Uint16(data[26:28]) & 0x3fff), int(binary.LittleEndian.Uint16(data[28:30]) & 0x3fff), true
	case "VP8L":
		if data[20] != 0x2f {
			return 0, 0, false
		}
		bits := binary.LittleEndian.Uint32(data[21:25])
		return int(bits&0x3fff) + 1, int(bits>>14&0x3fff) + 1, true
	case "VP8X":
		width := int(data[24]) | int(data[25])<<8 | int(data[26])<<16
		height := int(data[27]) | int(data[28])<<8 | int(data[29])<<16
		return width + 1, height + 1, true
	}
	return 0, 0, false
}
