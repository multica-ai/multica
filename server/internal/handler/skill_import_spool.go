package handler

import (
	"context"
	"net/http"
	"os"
)

type skillSpoolContextKey struct{}

// A request owns its spool until persistence finishes, including failed fetches,
// conflicts and cancellation. File names never derive from archive paths.
func prepareSkillSpool(r *http.Request) (*http.Request, func(), error) {
	dir, err := os.MkdirTemp("", "multica-skill-import-")
	if err != nil {
		return nil, nil, err
	}
	return r.WithContext(context.WithValue(r.Context(), skillSpoolContextKey{}, dir)), func() { _ = os.RemoveAll(dir) }, nil
}

func skillSpoolDir(ctx context.Context) string {
	dir, _ := ctx.Value(skillSpoolContextKey{}).(string)
	return dir
}

func spoolSkillContent(dir, content string) (string, error) {
	f, err := os.CreateTemp(dir, "content-")
	if err != nil {
		return "", err
	}
	_, writeErr := f.WriteString(content)
	closeErr := f.Close()
	if writeErr != nil {
		return "", writeErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	return f.Name(), nil
}

func (f CreateSkillFileRequest) readContent() (string, error) {
	if f.contentFile == "" {
		return f.Content, nil
	}
	data, err := os.ReadFile(f.contentFile)
	return string(data), err
}
