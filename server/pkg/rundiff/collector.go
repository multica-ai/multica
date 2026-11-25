package rundiff

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

const scanTimeout = 10 * time.Second
const maxScanBytes int64 = 1 << 30

// errFileLimit is the sentinel returned by WalkDir when one root's own
// enumeration exceeds limits.Files, so the caller labels that root
// file_limit instead of enumeration_failed.
var errFileLimit = errors.New("file_limit")

type entry struct {
	hash, mode, oid, reason string
	size                    int64
	body                    []byte
	binary                  bool
}
type rootState struct {
	Root
	dir string
	// git fields (git roots only): the common dir anchors ref writes
	// and reads across linked worktrees; the orphan commits pin the before /
	// after worktree snapshots and the refs make them addressable.
	gitCommonDir              string
	beforeRef, afterRef       string
	beforeCommit, afterCommit string
	before                    map[string]entry
}
type Collector struct {
	refJournal func([]RootRef) error
	readBuffer []byte
	scanBytes  int64
	// filesSeen counts entries scanned for the CURRENT root only; scan()
	// resets it so one large root cannot starve the roots scanned after it.
	// The collection-wide cap on persisted files stays in the Finish diff
	// loop.
	filesSeen   int
	conflicted  atomic.Bool
	mu          sync.Mutex
	id, workdir string
	roots       []*rootState
	finished    bool
	bytes       int
	limits      Limits
}

// Limits can be lowered by tests, but never raises the protocol budgets.
type Limits struct {
	Files, SideBytes, BodyBytes int
	Timeout                     time.Duration
}

func DefaultLimits() Limits { return Limits{MaxFiles, MaxSideBytes, MaxBodyBytes, scanTimeout} }

var leases = struct {
	sync.Mutex
	owners map[string]*Collector
}{owners: map[string]*Collector{}}

func New(workdir string) *Collector { return NewWithLimits(workdir, DefaultLimits()) }
func NewWithLimits(workdir string, l Limits) *Collector {
	l.Files = min(max(l.Files, 1), MaxFiles)
	l.SideBytes = min(max(l.SideBytes, 1), MaxSideBytes)
	l.BodyBytes = min(max(l.BodyBytes, 1), MaxBodyBytes)
	if l.Timeout <= 0 || l.Timeout > scanTimeout {
		l.Timeout = scanTimeout
	}
	dir, err := filepath.EvalSymlinks(workdir)
	if err != nil {
		dir = workdir
	}
	dir, _ = filepath.Abs(dir)
	return &Collector{id: uuid.NewString(), workdir: dir, limits: l}
}
func (c *Collector) Start(ctx context.Context) Collection {
	c.mu.Lock()
	defer c.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, c.limits.Timeout)
	defer cancel()
	c.discover(ctx, true)
	for _, r := range c.roots {
		if r.Coverage == "unavailable" {
			continue
		}
		if r.Kind == "git" {
			// git roots snapshot the worktree into a pinned tree
			// instead of hashing files. Failure keeps the old baseline_*
			// reasons so downstream copies and the UI stay unchanged.
			if err := c.pinAndJournal(ctx, r, "before"); err != nil {
				r.Coverage = "unavailable"
				switch {
				case errors.Is(err, errJournalFailed):
					r.Reason = "baseline_journal_failed"
				case strings.Contains(err.Error(), "update-ref"):
					r.Reason = "baseline_pin_failed"
				default:
					r.Reason = "baseline_tree_failed"
				}
			}
			continue
		}
		r.before = c.scan(ctx, r, true)
	}
	return c.envelope("collecting")
}
func (c *Collector) envelope(status string) Collection {
	out := Empty(c.id, status, "")
	for _, r := range c.roots {
		out.Roots = append(out.Roots, r.Root)
		if r.Coverage != "complete" {
			out.TotalsComplete = false
		}
	}
	return out
}

// Register is called before a controlled checkout is handed to the provider.
func (c *Collector) Register(ctx context.Context, dir string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.finished {
		return fmt.Errorf("collection already finalized")
	}
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(c.workdir, real)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("checkout outside run scope")
	}
	for _, r := range c.roots {
		if r.dir == real {
			return nil
		}
	}
	ctx, cancel := context.WithTimeout(ctx, c.limits.Timeout)
	defer cancel()
	r := c.addRoot(real, "git", true)
	if r.Coverage != "unavailable" {
		// the new root gets its own before tree. Parent roots prune
		// the nested prefix from their manifests at Finish (pruneNestedFiles).
		if err := c.pinAndJournal(ctx, r, "before"); err != nil {
			r.Coverage = "unavailable"
			r.Reason = "baseline_tree_failed"
		}
	}
	// A newly registered nested repository is no longer part of its parent root.
	for _, parent := range c.roots {
		if parent == r {
			continue
		}
		prefix, e := filepath.Rel(parent.dir, real)
		if e == nil && !strings.HasPrefix(prefix, "..") {
			prefix = filepath.ToSlash(prefix) + "/"
			for p := range parent.before {
				if strings.HasPrefix(p, prefix) {
					delete(parent.before, p)
				}
			}
		}
	}
	if r.Coverage == "unavailable" {
		return fmt.Errorf("checkout baseline unavailable: %s", r.Reason)
	}
	return nil
}
func within(parent, child string) bool {
	rel, e := filepath.Rel(parent, child)
	return e == nil && (rel == "." || rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}
func (c *Collector) addRoot(dir, kind string, baseline bool) *rootState {
	for _, r := range c.roots {
		if r.dir == dir {
			return r
		}
	}
	label, _ := filepath.Rel(c.workdir, dir)
	if label == "." {
		label = "workdir"
	}
	sum := sha256.Sum256([]byte(label))
	id := hex.EncodeToString(sum[:12])
	r := &rootState{Root: Root{ID: id, Label: SafeText(filepath.ToSlash(label)), Kind: kind, Coverage: "complete"}, dir: dir}
	if !baseline {
		r.Coverage = "unavailable"
		r.Reason = "missing_baseline"
	}
	leases.Lock()
	for p, owner := range leases.owners {
		if owner != c && (within(p, dir) || within(dir, p)) {
			r.Coverage = "unavailable"
			r.Reason = "shared_root"
			c.conflicted.Store(true)
			owner.conflicted.Store(true)
		}
	}
	if r.Coverage != "unavailable" {
		leases.owners[dir] = c
	}
	leases.Unlock()
	c.roots = append(c.roots, r)
	return r
}
func (c *Collector) discover(ctx context.Context, baseline bool) {
	err := filepath.WalkDir(c.workdir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(c.workdir, p)
		if rel != "." && sensitive(filepath.ToSlash(rel)) {
			return filepath.SkipDir
		}
		if len(c.roots) >= 256 {
			return fmt.Errorf("root_limit")
		}
		if _, err := os.Lstat(filepath.Join(p, ".git")); err == nil {
			c.addRoot(p, "git", baseline)
		}
		return nil
	})
	found := false
	for _, r := range c.roots {
		if r.dir == c.workdir {
			found = true
		}
	}
	if !found {
		c.addRoot(c.workdir, "directory", baseline)
	}
	if err != nil {
		for _, r := range c.roots {
			r.Coverage = "partial"
			r.Reason = "enumeration_failed"
		}
	}
}

type boundedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.Len() {
		return 0, fmt.Errorf("command output limit")
	}
	return b.Buffer.Write(p)
}
func git(ctx context.Context, dir string, limit int, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir, "--no-optional-locks"}, args...)...)
	cmd.WaitDelay = time.Second
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_NO_LAZY_FETCH=1")
	out := &boundedBuffer{limit: limit}
	cmd.Stdout = out
	cmd.Stderr = io.Discard
	err := cmd.Run()
	return out.Bytes(), err
}
func (c *Collector) scan(ctx context.Context, r *rootState, baseline bool) map[string]entry {
	entries := map[string]entry{}
	// Per-root budget: each root may enumerate up to limits.Files of its own
	// entries regardless of what earlier roots consumed.
	c.filesSeen = 0
	rootHandle, openErr := os.OpenRoot(r.dir)
	if openErr != nil {
		r.Coverage = "unavailable"
		r.Reason = "root_unavailable"
		return entries
	}
	defer rootHandle.Close()
	paths := []string{}
	fail := func(reason string) { r.Coverage = "partial"; r.Reason = reason }
	// Directory roots have no object database, so enumeration and body reads
	// stay on the file walk. Git roots never reach scan(): they
	// snapshot into pinned trees (gitroot.go).
	{
		err := filepath.WalkDir(r.dir, func(p string, d fs.DirEntry, e error) error {
			if e != nil {
				return e
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			rel, _ := filepath.Rel(r.dir, p)
			rel = filepath.ToSlash(rel)
			if rel == "." {
				return nil
			}
			if sensitive(rel) || c.nested(r, p) {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if !d.IsDir() {
				paths = append(paths, rel)
				if len(paths) > c.limits.Files {
					return errFileLimit
				}
			}
			return nil
		})
		if err != nil {
			if errors.Is(err, errFileLimit) {
				fail("file_limit")
			} else {
				fail("enumeration_failed")
			}
		}
	}
	sort.Strings(paths)

	for _, p := range paths {
		if _, ok := entries[p]; ok {
			continue
		}
		if sensitive(p) || c.nested(r, filepath.Join(r.dir, filepath.FromSlash(p))) {
			continue
		}
		if !validLocalPath(p) {
			fail("unsupported_path")
			continue
		}
		if c.filesSeen >= c.limits.Files {
			fail("file_limit")
			break
		}
		if ctx.Err() != nil {
			fail("scan_timeout")
			break
		}
		full := filepath.Join(r.dir, filepath.FromSlash(p))
		info, err := os.Lstat(full)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			entries[p] = entry{reason: "read_failed"}
			fail("read_failed")
			continue
		}
		if !info.Mode().IsRegular() {
			entries[p] = entry{size: info.Size(), mode: info.Mode().String(), reason: "unsupported_file_type"}
			fail("unsupported_file_type")
			continue
		}
		if c.scanBytes+info.Size() > maxScanBytes {
			entries[p] = entry{size: info.Size(), mode: "100644", reason: "scan_byte_limit"}
			fail("scan_byte_limit")
			continue
		}
		c.scanBytes += info.Size()
		c.filesSeen++
		if !noSymlinkParents(r.dir, p) {
			entries[p] = entry{reason: "symlink_path"}
			fail("symlink_path")
			continue
		}
		e := c.read(ctx, rootHandle, r.dir, p, info)
		if e.reason != "" && e.reason != "file_budget" {
			fail(e.reason)
		}
		if old, ok := r.before[p]; !baseline && ok && e.hash != "" && e.hash == old.hash && e.mode == old.mode {
			e.body = nil
		} else {
			if len(e.body) > 0 {
				if c.bytes+len(e.body) > c.limits.BodyBytes {
					e.body = nil
					e.reason = "body_budget"
					if baseline {
						e.reason = "baseline_budget"
					}
				} else {
					c.bytes += len(e.body)
				}
			}
		}
		entries[p] = e
	}
	return entries
}
func validLocalPath(p string) bool {
	return utf8.ValidString(p) && !filepath.IsAbs(p) && filepath.ToSlash(filepath.Clean(p)) == p && p != ".." && !strings.HasPrefix(p, "../") && !strings.ContainsAny(p, "\\\x00")
}
func (c *Collector) nested(root *rootState, p string) bool {
	for _, r := range c.roots {
		if r != root && within(root.dir, r.dir) && within(r.dir, p) {
			return true
		}
	}
	return false
}
func normalizeRuntime(p string, b []byte) []byte {
	if name := strings.ToLower(filepath.Base(p)); name != "agents.md" && name != "claude.md" {
		return b
	}
	begin := []byte("<!-- BEGIN MULTICA-RUNTIME (auto-managed; do not edit) -->")
	end := []byte("<!-- END MULTICA-RUNTIME -->")
	for {
		i := bytes.Index(b, begin)
		if i < 0 {
			break
		}
		j := bytes.Index(b[i:], end)
		if j < 0 {
			break
		}
		b = append(append([]byte{}, b[:i]...), b[i+j+len(end):]...)
	}
	return b
}
func (c *Collector) read(ctx context.Context, root *os.Root, dir, rel string, info fs.FileInfo) entry {
	p := filepath.Join(dir, filepath.FromSlash(rel))
	e := entry{size: info.Size(), mode: "100644"}
	if info.Mode()&0111 != 0 {
		e.mode = "100755"
	}
	f, err := root.Open(rel)
	if err != nil {
		e.reason = "read_failed"
		return e
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		e.reason = "concurrent_write"
		return e
	}
	h := sha256.New()
	var buf bytes.Buffer
	if c.readBuffer == nil {
		c.readBuffer = make([]byte, 64<<10)
	}
	chunk := c.readBuffer
	if info.Size() <= int64(c.limits.SideBytes) {
		buf.Grow(int(info.Size()))
	}
	var nread int64
	for {
		if ctx.Err() != nil {
			e.reason = "scan_timeout"
			return e
		}
		n, err := f.Read(chunk)
		if n > 0 {
			nread += int64(n)
			if nread > info.Size() {
				e.reason = "concurrent_write"
				return e
			}
			h.Write(chunk[:n])
			if bytes.IndexByte(chunk[:n], 0) >= 0 {
				e.binary = true
			}
			if info.Size() <= int64(c.limits.SideBytes) {
				buf.Write(chunk[:n])
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			e.reason = "read_failed"
			return e
		}
	}
	after, err := os.Lstat(p)
	if err != nil || !os.SameFile(info, after) || after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) || after.Mode() != info.Mode() || nread != info.Size() {
		e.reason = "concurrent_write"
		return e
	}
	e.hash = hex.EncodeToString(h.Sum(nil))
	if info.Size() > int64(c.limits.SideBytes) {
		e.reason = "file_budget"
		return e
	}
	b := buf.Bytes()
	normal := normalizeRuntime(p, b)
	if !bytes.Equal(normal, b) {
		sum := sha256.Sum256(normal)
		e.hash = hex.EncodeToString(sum[:])
		e.size = int64(len(normal))
		b = normal
	}
	if !utf8.Valid(b) {
		e.binary = true
	}
	if !e.binary {
		e.body = b
	}
	return e
}
func (c *Collector) Finish(ctx context.Context) Collection {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.finished = true
	c.bytes = 0
	c.scanBytes = 0
	ctx, cancel := context.WithTimeout(ctx, c.limits.Timeout)
	defer cancel()
	// pinned refs survive Finish on purpose — they are the local
	// record the relay serves from. Only the in-memory lease bookkeeping ends.
	defer c.releaseLeases()
	c.discover(ctx, false)
	result := c.envelope("ready")
	total := 0
	for _, r := range c.roots {
		if r.Coverage == "unavailable" {
			continue
		}
		if r.Kind == "git" {
			files, statsComplete := c.finishGitRoot(ctx, r)
			if !statsComplete {
				result.StatsComplete = false
			}
			for _, f := range files {
				if len(result.Files) >= c.limits.Files {
					failRoot(r, "file_limit")
					break
				}
				if f.BodyStatus == "omitted" || f.BodyStatus == "unavailable" {
					result.Status = "partial"
				}
				if f.Insertions != nil {
					result.InsertionsTotal += *f.Insertions
					result.DeletionsTotal += *f.Deletions
				}
				result.Files = append(result.Files, f)
			}
			continue
		}
		after := c.scan(ctx, r, false)
		if r.Coverage == "unavailable" {
			continue
		}
		paths := map[string]bool{}
		for p := range r.before {
			// Ownership moves to a nested root, even when its baseline is unavailable.
			// Its absence from the parent scan is not a filesystem deletion.
			migrated := false
			for _, child := range c.roots {
				if child != r && within(r.dir, child.dir) && within(child.dir, filepath.Join(r.dir, p)) {
					migrated = true
					break
				}
			}
			if migrated {
				continue
			}
			paths[p] = true
		}
		for p := range after {
			paths[p] = true
		}
		keys := make([]string, 0, len(paths))
		for p := range paths {
			keys = append(keys, p)
		}
		sort.Strings(keys)
		for _, p := range keys {
			a, aok := r.before[p]
			b, bok := after[p]
			if aok && bok && a.hash != "" && a.hash == b.hash && a.mode == b.mode {
				continue
			}
			if len(result.Files) >= c.limits.Files {
				r.Coverage = "partial"
				r.Reason = "file_limit"
				break
			}
			f := File{ID: identity(r.ID + "/" + p), RootID: r.ID, Path: p, Change: "modified", BeforeBytes: a.size, AfterBytes: b.size, BeforeMode: a.mode, AfterMode: b.mode, Binary: a.binary || b.binary, BodyStatus: "available"}
			if !aok {
				f.Change = "added"
			} else if !bok {
				f.Change = "deleted"
			} else if a.hash != "" && a.hash == b.hash {
				f.Change = "mode_changed"
			}
			// Establish baseline readability before presentation decisions. A missing
			// blob must dominate redaction, binary classification and body budgets.
			if aok && a.oid != "" && a.reason == "" && !a.binary {
				body, err := git(ctx, r.dir, c.limits.SideBytes+1, "cat-file", "blob", a.oid)
				if err != nil {
					a.reason = "baseline_object_missing"
				} else {
					a.body = normalizeRuntime(p, body)
				}
			}
			budget := func(reason string) bool {
				return reason == "file_budget" || reason == "baseline_budget" || reason == "body_budget"
			}
			for _, reason := range []string{a.reason, b.reason} {
				if reason != "" && (f.Reason == "" || budget(f.Reason)) {
					f.Reason = reason
				}
			}
			if (!aok || !bok) && r.Coverage != "complete" {
				f.Reason = r.Reason
				f.BodyStatus = "unavailable"
			} else if f.Reason != "" && !budget(f.Reason) {
				f.BodyStatus = "unavailable"
			} else if f.Reason != "" {
				f.BodyStatus = "omitted"
			} else if f.Binary {
				f.BodyStatus = "binary"
			}
			if f.BodyStatus == "available" {
				if aok {
					s := SafeText(string(a.body))
					f.Before = &s
					if s != string(a.body) {
						f.BodyStatus = "redacted"
					}
				}
				if bok {
					s := SafeText(string(b.body))
					f.After = &s
					if s != string(b.body) {
						f.BodyStatus = "redacted"
					}
				}
				if (f.Before != nil && len(*f.Before) > c.limits.SideBytes) || (f.After != nil && len(*f.After) > c.limits.SideBytes) {
					f.BodyStatus = "omitted"
					f.Reason = "redacted_body_budget"
				}
				n := 0
				if f.Before != nil {
					n += len(*f.Before)
				}
				if f.After != nil {
					n += len(*f.After)
				}
				if total+n > c.limits.BodyBytes {
					f.BodyStatus = "omitted"
					f.Reason = "body_budget"
				} else {
					total += n
				}
			}
			if f.BodyStatus != "available" && f.BodyStatus != "redacted" {
				f.Before = nil
				f.After = nil
			}
			// Pin the sanitized text as the capture-time anchor the body
			// endpoints will verify on every read.
			if f.BodyStatus == "available" || f.BodyStatus == "redacted" {
				f.BodySHA256 = BodyDigest(deref(f.Before), deref(f.After))
			}
			if f.BodyStatus == "omitted" || f.BodyStatus == "unavailable" {
				result.Status = "partial"
			}
			// Line statistics ride on the same attached texts the diff view
			// renders, so redaction and budgets shape the counts exactly as
			// they shape the bodies. Binary files have no line counts by
			// nature; anything else that cannot be counted leaves the file's
			// stats unset and marks the collection sums partial.
			switch {
			case f.Binary:
			case f.Change == "mode_changed":
				zero := int64(0)
				f.Insertions, f.Deletions = &zero, &zero
			default:
				if ins, del, ok := diffLineCounts(f.Before, f.After); ok {
					f.Insertions, f.Deletions = &ins, &del
					result.InsertionsTotal += ins
					result.DeletionsTotal += del
				} else {
					result.StatsComplete = false
				}
			}
			result.Files = append(result.Files, f)
		}
	}
	result.Roots = []Root{}
	result.TotalsComplete = true
	available := false
	for _, r := range c.roots {
		result.Roots = append(result.Roots, r.Root)
		if r.Coverage != "complete" {
			result.Status = "partial"
			result.TotalsComplete = false
		}
		if r.Coverage != "unavailable" {
			available = true
		}
	}
	if !available {
		result.Status = "unavailable"
		result.Reason = "baseline_unavailable"
	}
	if c.conflicted.Load() {
		result.Files = []File{}
		result.Status = "unavailable"
		result.Reason = "shared_root"
		result.TotalsComplete = false
		for i := range result.Roots {
			result.Roots[i].Coverage = "unavailable"
			result.Roots[i].Reason = "shared_root"
		}
	}
	result.FilesTotal = len(result.Files)
	if result.Digest = result.ComputeDigest(); result.Digest != "" {
		result.DigestVersion = DigestVersion
	}
	return result
}
func identity(s string) string { sum := sha256.Sum256([]byte(s)); return hex.EncodeToString(sum[:16]) }

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// finishGitRoot pins the after tree and materializes the root's manifest from
// the two pinned commits. Coverage/Reason reflect pin or manifest failures.
func (c *Collector) finishGitRoot(ctx context.Context, r *rootState) ([]File, bool) {
	if err := c.pinAndJournal(ctx, r, "after"); err != nil {
		if errors.Is(err, errJournalFailed) {
			failRoot(r, "journal_failed")
		} else {
			failRoot(r, "tree_failed")
		}
		return nil, true
	}
	files, statsComplete := c.manifestRoot(ctx, r)
	return c.pruneNestedFiles(r, files), statsComplete
}

// pruneNestedFiles drops parent-root manifest entries that live under another
// root's directory. Trees already exclude nested prefixes at build time; this
// catches entries captured before a nested root was registered mid-run, whose
// absence from the after tree is ownership migration, not a deletion.
func (c *Collector) pruneNestedFiles(r *rootState, files []File) []File {
	var prefixes []string
	for _, child := range c.roots {
		if child == r {
			continue
		}
		if rel, err := filepath.Rel(r.dir, child.dir); err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			prefixes = append(prefixes, filepath.ToSlash(rel)+"/")
		}
	}
	if len(prefixes) == 0 {
		return files
	}
	out := make([]File, 0, len(files))
	for _, f := range files {
		drop := false
		for _, p := range prefixes {
			if strings.HasPrefix(f.Path, p) {
				drop = true
				break
			}
		}
		if !drop {
			out = append(out, f)
		}
	}
	return out
}

func (c *Collector) releaseLeases() {
	leases.Lock()
	for p, owner := range leases.owners {
		if owner == c {
			delete(leases.owners, p)
		}
	}
	leases.Unlock()
}

// Abort releases resources when the provider could not be started: nothing is
// ever shown for this collection, so its pinned refs go with it.
func (c *Collector) Abort() {
	c.mu.Lock()
	c.deleteRefs()
	c.releaseLeases()
	c.mu.Unlock()
}

// Page returns metadata only. Bodies are independently addressed by file ID.
func (c Collection) Page(offset, limit int) Collection {
	out := c
	out.Files = []File{}
	for i := offset; i < len(c.Files) && i < offset+limit; i++ {
		f := c.Files[i]
		f.Before = nil
		f.After = nil
		out.Files = append(out.Files, f)
	}
	if offset+limit < len(c.Files) {
		s := strconv.Itoa(offset + limit)
		out.NextCursor = &s
	}
	return out
}

func (c *Collector) Pending() Collection { return Empty(c.id, "collecting", "") }

// SetRefJournal persists cleanup intent before creating each Git reference.
// The journal receives the ref anchors that are about to exist so the daemon's
// index can drive deletion even if the process dies mid-write.
func (c *Collector) SetRefJournal(fn func([]RootRef) error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.refJournal = fn
}

// ReleasePins returns obligations that must survive upload and process restart.
func ReleasePins(ctx context.Context, pins map[string]string, id string) map[string]string {
	remaining := map[string]string{}
	if _, err := uuid.Parse(id); err != nil {
		return pins
	}
	for dir, ref := range pins {
		if ref == "refs/multica/run-diff/"+id {
			if _, err := git(ctx, dir, 256, "update-ref", "-d", ref); err != nil {
				remaining[dir] = ref
			}
		}
	}
	return remaining
}

func noSymlinkParents(root, p string) bool {
	parts := strings.Split(filepath.ToSlash(p), "/")
	for _, part := range parts[:len(parts)-1] {
		root = filepath.Join(root, part)
		info, err := os.Lstat(root)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return false
		}
	}
	return true
}
