package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/daemon"
)

var attachmentSHA256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// attachmentCache stores attachment bytes at <root>/.attachment-cache/<aa>/<sha256>.
// The workspaces root may be shared storage, so bytes one agent uploaded or
// downloaded are readable by every agent and host mounting the same root.
// Every read re-hashes the file, so a copy that was modified, truncated or
// half-written is never served; the caller downloads instead.
type attachmentCache struct {
	dir string
}

// attachmentCacheFor returns the cache under the workspaces root this CLI's
// daemon uses (the task's root inside a task). It is nil when that root does
// not exist yet: a machine that never ran a daemon gets no hidden cache.
func attachmentCacheFor(cmd *cobra.Command) *attachmentCache {
	root, err := resolveDiskUsageRoot(inDaemonManagedExecutionContext(), resolveProfile(cmd), "")
	if err != nil {
		return nil
	}
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		return nil
	}
	return newAttachmentCache(root)
}

// cacheUploadedAttachment keeps bytes the CLI just uploaded, so a later
// download of that attachment, by this agent or by another one sharing the
// workspaces root, reads them locally. A failure only costs that download.
func cacheUploadedAttachment(cmd *cobra.Command, att cli.AttachmentResponse, data []byte) {
	if att.ContentSHA256 == "" {
		return
	}
	if err := attachmentCacheFor(cmd).write(att.ContentSHA256, data); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not cache attachment %s locally: %v\n", att.ID, err)
	}
}

func newAttachmentCache(workspacesRoot string) *attachmentCache {
	if workspacesRoot == "" {
		return nil
	}
	return &attachmentCache{dir: filepath.Join(workspacesRoot, daemon.AttachmentCacheDirName)}
}

func (c *attachmentCache) path(sum string) string {
	return filepath.Join(c.dir, sum[:2], sum)
}

// read returns the cached bytes for sum when a copy exists and still hashes to
// sum. A missing, unreadable or mismatched copy is a miss.
func (c *attachmentCache) read(sum string) ([]byte, bool) {
	if c == nil || !attachmentSHA256Pattern.MatchString(sum) {
		return nil, false
	}
	p := c.path(sum)
	data, err := os.ReadFile(p)
	if err != nil || contentSHA256(data) != sum {
		return nil, false
	}
	// Refresh the mtime the daemon GC expires entries by.
	now := time.Now()
	_ = os.Chtimes(p, now, now)
	return data, true
}

// write stores data under sum when it hashes to sum. The bytes go to a temp
// file in the same directory first and are renamed into place, so a reader on
// another host never sees a partial file under the final name.
func (c *attachmentCache) write(sum string, data []byte) error {
	if c == nil || !attachmentSHA256Pattern.MatchString(sum) {
		return nil
	}
	if contentSHA256(data) != sum {
		return errors.New("content does not match its hash")
	}
	p := c.path(sum)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), sum+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpName, p)
}

func contentSHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
