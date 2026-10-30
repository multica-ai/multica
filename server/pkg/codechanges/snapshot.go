// Package codechanges captures immutable, bounded commit diffs on the runtime.
package codechanges

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	MaxFiles     = 100
	MaxFileBytes = 200 * 1024
	MaxDiffBytes = 2 * 1024 * 1024
	MaxJSONBytes = 8 * 1024 * 1024
	SettingKey   = "code_changes_enabled"
)

type File struct {
	BaseRef   string `json:"base_ref,omitempty"`
	HeadRef   string `json:"head_ref,omitempty"`
	Path      string `json:"path"`
	Status    string `json:"status"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Diff      string `json:"diff"`
	Binary    bool   `json:"binary"`
	Truncated bool   `json:"truncated"`
}

type Snapshot struct {
	Version          int    `json:"version"`
	BaseRef          string `json:"base_ref"`
	HeadRef          string `json:"head_ref"`
	Files            []File `json:"files"`
	Truncated        bool   `json:"truncated"`
	WorkingTreeDirty bool   `json:"working_tree_dirty"`
	Reason           string `json:"reason,omitempty"`
}

func Enabled(settings []byte) bool {
	var values map[string]json.RawMessage
	if json.Unmarshal(settings, &values) != nil {
		return false
	}
	var enabled bool
	return json.Unmarshal(values[SettingKey], &enabled) == nil && enabled
}

var commitPattern = regexp.MustCompile(`^(?:[a-f0-9]{40}|[a-f0-9]{64})$`)

func (s *Snapshot) Validate() error {
	if s == nil || s.Version != 1 || len(s.Files) > MaxFiles {
		return errors.New("invalid code changes snapshot")
	}
	if s.Reason != "" && s.Reason != "no_repository" && s.Reason != "capture_failed" {
		return errors.New("invalid code changes reason")
	}
	if s.Reason == "" && (!commitPattern.MatchString(s.BaseRef) || !commitPattern.MatchString(s.HeadRef)) {
		return errors.New("invalid code changes commit")
	}
	total := 0
	seen := map[string]bool{}
	for _, f := range s.Files {
		if f.Path == "" || len(f.Path) > 4096 || path.IsAbs(f.Path) || strings.ContainsRune(f.Path, 0) || !utf8.ValidString(f.Path) || seen[f.Path] {
			return errors.New("invalid code changes path")
		}
		seen[f.Path] = true
		if (f.BaseRef != "" && !commitPattern.MatchString(f.BaseRef)) || (f.HeadRef != "" && !commitPattern.MatchString(f.HeadRef)) {
			return errors.New("invalid code changes file commit")
		}
		if f.Status != "A" && f.Status != "M" && f.Status != "D" && f.Status != "T" {
			return errors.New("invalid code changes status")
		}
		if len(f.Diff) > MaxFileBytes || f.Additions < 0 || f.Deletions < 0 || !utf8.ValidString(f.Diff) {
			return errors.New("invalid code changes file")
		}
		total += len(f.Diff)
	}
	if total > MaxDiffBytes {
		return errors.New("code changes snapshot too large")
	}

	raw, err := json.Marshal(s)
	if err != nil {
		return err
	}
	if len(raw) > MaxJSONBytes {
		return errors.New("code changes JSON too large")
	}
	return nil
}

func Encode(s *Snapshot) ([]byte, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	if len(raw) > MaxJSONBytes {
		return nil, errors.New("code changes JSON too large")
	}
	var buf bytes.Buffer
	writer := gzip.NewWriter(&buf)
	if _, err = writer.Write(raw); err != nil {
		return nil, err
	}
	if err = writer.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func Decode(raw []byte) (*Snapshot, error) {
	if len(raw) > MaxJSONBytes {
		return nil, errors.New("code changes storage too large")
	}
	reader, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, MaxJSONBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxJSONBytes {
		return nil, errors.New("code changes expanded JSON too large")
	}
	var s Snapshot
	if err = json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	if err = s.Validate(); err != nil {
		return nil, err
	}
	return &s, nil
}

type boundedBuffer struct {
	buffer    bytes.Buffer
	limit     int
	truncated bool
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	remain := b.limit - b.buffer.Len()
	if len(p) > remain {
		b.truncated = true
		p = p[:max(0, remain)]
	}
	_, _ = b.buffer.Write(p)
	return n, nil
}
func git(ctx context.Context, root string, limit int, args ...string) (string, bool, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"--no-optional-locks", "--literal-pathspecs", "-C", root}, args...)...)
	out := &boundedBuffer{limit: limit}
	cmd.Stdout = out
	cmd.Stderr = io.Discard
	err := cmd.Run()
	return out.buffer.String(), out.truncated, err
}

// Head resolves only a local commit. Caller captures it before launching tools.
func Head(ctx context.Context, root, ref string) (string, error) {
	out, _, err := git(ctx, root, 128, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
	if err != nil {
		return "", err
	}
	sha := strings.TrimSpace(out)
	if !commitPattern.MatchString(sha) {
		return "", errors.New("invalid git commit")
	}
	return sha, nil
}

// Capture uses one deadline for all files. It reads committed work only and
// reports a dirty working tree explicitly; it never writes the user's repo.
func Capture(ctx context.Context, root, base, head string, checkDirty bool) *Snapshot {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	s := &Snapshot{Version: 1, BaseRef: base, HeadRef: head, Files: []File{}}
	if !commitPattern.MatchString(base) || !commitPattern.MatchString(head) {
		s.Reason = "capture_failed"
		return s
	}
	if checkDirty {
		dirty, _, err := git(ctx, root, 1, "status", "--porcelain", "-z", "--untracked-files=normal")
		if err != nil {
			s.Reason = "capture_failed"
			return s
		}
		s.WorkingTreeDirty = dirty != ""
	}
	names, truncated, err := git(ctx, root, MaxFiles*4100, "diff", "--no-ext-diff", "--no-textconv", "--no-renames", "--name-status", "-z", base, head, "--")
	if err != nil {
		s.Reason = "capture_failed"
		return s
	}
	s.Truncated = truncated
	if truncated {
		names = names[:strings.LastIndex(names, "\x00")+1]
	}
	parts := strings.Split(names, "\x00")
	remaining := MaxDiffBytes
	for i := 0; i+1 < len(parts) && parts[i] != "" && parts[i+1] != ""; i += 2 {
		if len(s.Files) == MaxFiles {
			s.Truncated = true
			break
		}
		f := File{Path: parts[i+1], Status: parts[i]}
		stats, _, err := git(ctx, root, 8192, "diff", "--no-ext-diff", "--no-textconv", "--no-renames", "--numstat", "-z", base, head, "--", f.Path)
		if err != nil {
			s.Reason = "capture_failed"
			return s
		}
		fields := strings.SplitN(stats, "\t", 3)
		if len(fields) >= 2 {
			f.Binary = fields[0] == "-"
			if !f.Binary {
				f.Additions, _ = strconv.Atoi(fields[0])
				f.Deletions, _ = strconv.Atoi(fields[1])
			}
		}
		if !f.Binary {
			capBytes := min(MaxFileBytes, remaining)
			diff, cut, err := git(ctx, root, capBytes, "diff", "--no-ext-diff", "--no-textconv", "--no-renames", "--no-color", "--unified=3", base, head, "--", f.Path)
			if err != nil {
				s.Reason = "capture_failed"
				return s
			}
			// Git may contain non-UTF-8 text; keep transport valid within the byte cap.
			diff = strings.ToValidUTF8(diff, "?")
			f.Diff = diff
			f.Truncated = cut
			remaining -= len(diff)
			s.Truncated = s.Truncated || cut
		}
		s.Files = append(s.Files, f)
	}
	if err := s.Validate(); err != nil {
		return &Snapshot{Version: 1, Files: []File{}, Reason: "capture_failed"}
	}
	return s
}

const ReceiptHeader = "X-Insight-Code-Changes-Receipt"

func DigestEncoded(encoded []byte) string {
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}
