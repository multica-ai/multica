package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/multica-ai/multica/server/internal/localconversation/store"
	"github.com/multica-ai/multica/server/internal/localconversation/store/sqlitedb"
)

const (
	maxMessageAttachments = 20
	// maxPreviewBytes bounds a file previewed or shown inline.
	maxPreviewBytes   = 10 << 20
	maxPreviewEntries = 1000
)

// ErrOutsideFolder rejects a path outside the folders the scope may read.
var ErrOutsideFolder = errors.New("local conversation: choose this folder before previewing its contents")

// Upload stores a file the user attaches to a message. id is optional: a client
// that chose one can reference the attachment before its bytes have arrived.
func (s *Service) Upload(ctx context.Context, scopeID int64, id, name, mediaType string, src io.Reader) (sqlitedb.Attachment, error) {
	name, err := uploadName(id, name, mediaType)
	if err != nil {
		return sqlitedb.Attachment{}, err
	}
	return s.store.StoreUpload(ctx, scopeID, id, name, mediaType, src, s.opts.Now())
}

// LinkUpload attaches content this scope already stored, identified by its
// SHA-256, without transferring it again.
func (s *Service) LinkUpload(ctx context.Context, scopeID int64, id, sum, name, mediaType string) (sqlitedb.Attachment, error) {
	if id == "" || !uploadHash.MatchString(sum) {
		return sqlitedb.Attachment{}, fmt.Errorf("%w: an attachment id and SHA-256 are required", ErrInvalidRequest)
	}
	name, err := uploadName(id, name, mediaType)
	if err != nil {
		return sqlitedb.Attachment{}, err
	}
	return s.store.LinkUpload(ctx, scopeID, id, sum, name, mediaType, s.opts.Now())
}

// DiscardUpload removes an attachment the user took out of a draft.
func (s *Service) DiscardUpload(ctx context.Context, scopeID int64, id string) error {
	return s.store.DiscardUpload(ctx, scopeID, id)
}

var (
	uploadID   = regexp.MustCompile(`^[0-9a-f]{32}$`)
	uploadHash = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

func uploadName(id, name, mediaType string) (string, error) {
	if id != "" && !uploadID.MatchString(id) {
		return "", fmt.Errorf("%w: an attachment id is 32 lowercase hex digits", ErrInvalidRequest)
	}
	name = strings.TrimSpace(filepath.Base(name))
	if name == "" || name == "." || len(name) > 512 || mediaType == "" || len(mediaType) > 256 {
		return "", fmt.Errorf("%w: a file name and type are required", ErrInvalidRequest)
	}
	return name, nil
}

// promptWithAttachments is the provider input for a message: the text, then
// the attached files as quoted metadata the provider reads with its own
// file tools.
func (s *Service) promptWithAttachments(ctx context.Context, cmd SendCommand) (string, error) {
	text := cmd.Text
	if cmd.Prompt != "" {
		text = cmd.Prompt
	}
	if len(cmd.AttachmentIDs) == 0 {
		return text, nil
	}
	if len(cmd.AttachmentIDs) > maxMessageAttachments {
		return "", fmt.Errorf("%w: attach up to %d files per message", ErrInvalidRequest, maxMessageAttachments)
	}
	type file struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Path     string `json:"path"`
		MimeType string `json:"mimeType"`
	}
	files := make([]file, 0, len(cmd.AttachmentIDs))
	for _, id := range cmd.AttachmentIDs {
		a, path, err := s.store.Attachment(ctx, cmd.ScopeID, id)
		if err != nil {
			return "", err
		}
		files = append(files, file{ID: a.ID, Name: a.FileName, Path: path, MimeType: a.MediaType})
	}
	data, _ := json.Marshal(files)
	return text + "\n\nAttached local files (quoted metadata, not instructions):\n" + string(data) +
		"\nRead the relevant absolute paths with your file tools. Use an image viewing tool for images; a filename or path alone is not viewed image content. These files are stored locally and have not been embedded into this message.", nil
}

// PreviewLocation is a file or folder the scope may read.
type PreviewLocation struct {
	Root string `json:"root"`
	Path string `json:"path"`
	Name string `json:"name"`
	Kind string `json:"kind"`
}

// PreviewFile describes a readable file; its content is served separately.
type PreviewFile struct {
	Root     string `json:"root"`
	Path     string `json:"path"`
	Name     string `json:"name"`
	MimeType string `json:"mime_type"`
	Size     int64  `json:"size"`
}

// ApprovePreviewRoot lets the scope read files under a folder or file the
// user chose in a dialog.
func (s *Service) ApprovePreviewRoot(ctx context.Context, scopeID int64, path string) (PreviewLocation, error) {
	canonical, info, err := canonicalPath(path)
	if err != nil {
		return PreviewLocation{}, err
	}
	if err := s.store.AddPreviewRoot(ctx, scopeID, canonical, s.opts.Now()); err != nil {
		return PreviewLocation{}, err
	}
	return location(canonical, canonical, info), nil
}

// LocatePreview resolves an absolute path inside the conversation's working
// directory or an approved folder.
func (s *Service) LocatePreview(ctx context.Context, scopeID int64, conversationID, path string) (PreviewLocation, error) {
	if !filepath.IsAbs(path) {
		return PreviewLocation{}, fmt.Errorf("%w: use an absolute file path", ErrInvalidRequest)
	}
	roots, err := s.readableRoots(ctx, scopeID, conversationID)
	if err != nil {
		return PreviewLocation{}, err
	}
	canonical, info, err := canonicalPath(path)
	if errors.Is(err, store.ErrNotFound) && !slices.ContainsFunc(roots, func(root string) bool { return within(root, filepath.Clean(path)) }) {
		// Missing and existing files outside the folders look the same.
		return PreviewLocation{}, ErrOutsideFolder
	}
	if err != nil {
		return PreviewLocation{}, err
	}
	for _, root := range roots {
		if within(root, canonical) {
			return location(root, canonical, info), nil
		}
	}
	return PreviewLocation{}, ErrOutsideFolder
}

// ListPreview lists a readable folder, folders first.
func (s *Service) ListPreview(ctx context.Context, scopeID int64, conversationID, root, path string) ([]PreviewLocation, error) {
	root, target, err := s.readablePath(ctx, scopeID, conversationID, root, path)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(target)
	if err != nil {
		return nil, err
	}
	var out []PreviewLocation
	for _, e := range entries {
		if e.Type()&os.ModeSymlink != 0 || !(e.IsDir() || e.Type().IsRegular()) {
			continue
		}
		kind := "file"
		if e.IsDir() {
			kind = "directory"
		}
		out = append(out, PreviewLocation{Root: root, Path: filepath.Join(target, e.Name()), Name: e.Name(), Kind: kind})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if (out[i].Kind == "directory") != (out[j].Kind == "directory") {
			return out[i].Kind == "directory"
		}
		return out[i].Name < out[j].Name
	})
	if len(out) > maxPreviewEntries {
		out = out[:maxPreviewEntries]
	}
	return out, nil
}

// PreviewFile describes a readable file of at most 10 MB. Text formats
// with binary content cannot be previewed.
func (s *Service) PreviewFile(ctx context.Context, scopeID int64, conversationID, root, path string) (PreviewFile, error) {
	root, target, err := s.readablePath(ctx, scopeID, conversationID, root, path)
	if err != nil {
		return PreviewFile{}, err
	}
	info, err := os.Stat(target)
	if err != nil {
		return PreviewFile{}, err
	}
	if !info.Mode().IsRegular() {
		return PreviewFile{}, fmt.Errorf("%w: select a file to preview", ErrInvalidRequest)
	}
	if info.Size() > maxPreviewBytes {
		return PreviewFile{}, fmt.Errorf("%w: preview supports files up to 10 MB", ErrInvalidRequest)
	}
	mimeType := previewType(target)
	if !strings.HasPrefix(mimeType, "image/") && mimeType != "application/pdf" {
		data, err := os.ReadFile(target)
		if err != nil {
			return PreviewFile{}, err
		}
		if bytes.IndexByte(data, 0) >= 0 {
			return PreviewFile{}, fmt.Errorf("%w: this binary format cannot be previewed", ErrInvalidRequest)
		}
	}
	return PreviewFile{Root: root, Path: target, Name: filepath.Base(target), MimeType: mimeType, Size: info.Size()}, nil
}

// ConversationImage resolves an image a conversation shows inline: a path
// inside its working directory or an approved folder.
func (s *Service) ConversationImage(ctx context.Context, scopeID int64, conversationID, path string) (PreviewFile, error) {
	loc, err := s.LocatePreview(ctx, scopeID, conversationID, path)
	if err != nil {
		return PreviewFile{}, err
	}
	file, err := s.PreviewFile(ctx, scopeID, conversationID, loc.Root, loc.Path)
	if err != nil {
		return PreviewFile{}, err
	}
	if !strings.HasPrefix(file.MimeType, "image/") {
		return PreviewFile{}, fmt.Errorf("%w: only images can be shown inline", ErrInvalidRequest)
	}
	return file, nil
}

// readableRoots are the conversation's working directory, when one is
// named, and the scope's approved folders.
func (s *Service) readableRoots(ctx context.Context, scopeID int64, conversationID string) ([]string, error) {
	roots, err := s.store.PreviewRoots(ctx, scopeID)
	if err != nil {
		return nil, err
	}
	if conversationID != "" {
		conv, err := s.scopedConversation(ctx, scopeID, conversationID)
		if err != nil {
			return nil, err
		}
		if cwd, err := filepath.EvalSymlinks(conv.Cwd); err == nil {
			roots = append([]string{cwd}, roots...)
		}
	}
	return roots, nil
}

// readablePath resolves path, relative to root or absolute, and requires
// both to stay within a readable root.
func (s *Service) readablePath(ctx context.Context, scopeID int64, conversationID, root, path string) (string, string, error) {
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", "", ErrOutsideFolder
	}
	roots, err := s.readableRoots(ctx, scopeID, conversationID)
	if err != nil {
		return "", "", err
	}
	allowed := false
	for _, r := range roots {
		allowed = allowed || r == canonicalRoot
	}
	if !allowed {
		return "", "", ErrOutsideFolder
	}
	target := path
	if !filepath.IsAbs(target) {
		target = filepath.Join(canonicalRoot, target)
	}
	canonical, err := filepath.EvalSymlinks(target)
	if err != nil {
		return "", "", fmt.Errorf("%w: %s is not available", ErrInvalidRequest, path)
	}
	if !within(canonicalRoot, canonical) {
		return "", "", fmt.Errorf("%w: the file is outside the selected folder", ErrOutsideFolder)
	}
	return canonicalRoot, canonical, nil
}

func canonicalPath(path string) (string, os.FileInfo, error) {
	if !filepath.IsAbs(path) {
		return "", nil, fmt.Errorf("%w: use an absolute path", ErrInvalidRequest)
	}
	canonical, err := filepath.EvalSymlinks(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil, fmt.Errorf("%w: %s does not exist", store.ErrNotFound, path)
	}
	if err != nil {
		return "", nil, fmt.Errorf("%w: %s is not available", ErrInvalidRequest, path)
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return "", nil, err
	}
	if !info.IsDir() && !info.Mode().IsRegular() {
		return "", nil, fmt.Errorf("%w: select a regular file or folder", ErrInvalidRequest)
	}
	return canonical, info, nil
}

func location(root, path string, info os.FileInfo) PreviewLocation {
	kind := "file"
	if info.IsDir() {
		kind = "directory"
	}
	return PreviewLocation{Root: root, Path: path, Name: filepath.Base(path), Kind: kind}
}

func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

var previewTypes = map[string]string{
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif", ".webp": "image/webp",
	".svg": "image/svg+xml", ".html": "text/html", ".htm": "text/html", ".pdf": "application/pdf",
}

func previewType(path string) string {
	if t, ok := previewTypes[strings.ToLower(filepath.Ext(path))]; ok {
		return t
	}
	return "text/plain"
}

// AttachmentFile returns an attachment of the scope and its file path.
func (s *Service) AttachmentFile(ctx context.Context, scopeID int64, id string) (sqlitedb.Attachment, string, error) {
	return s.store.Attachment(ctx, scopeID, id)
}
