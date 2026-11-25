package rundiff

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Git-tree capture for "git" roots (). Instead of hashing and reading
// every file, a root's worktree is snapshotted as a git tree written through a
// task-private temporary index and pinned as an orphan commit under
// refs/multica/run-diff/<collection>/before|after. The manifest comes from
// git diff-tree between the two pinned trees, and file bodies are re-read on
// demand from the pinned refs — they never travel in the upload.
//
// The refs live in the repository's own object database (via the shared bare
// cache for linked worktrees), so committed work costs zero extra objects and
// cleanup is a pair of ref deletions.

// RootRef is the local anchor of one root's pinned trees. It is persisted in
// the daemon's per-task index so ref deletion (task GC) and on-demand body
// reads survive daemon restarts and worktree removal.
type RootRef struct {
	ID           string `json:"id"`
	Dir          string `json:"dir"`
	GitCommonDir string `json:"git_common_dir,omitempty"`
	BeforeRef    string `json:"before_ref,omitempty"`
	AfterRef     string `json:"after_ref,omitempty"`
}

func runDiffRef(collectionID, side string) string {
	return "refs/multica/run-diff/" + collectionID + "/" + side
}

// gitEnvRun runs git in dir with extra environment variables and optional
// stdin, returning bounded stdout. It is the tree-building counterpart of the
// package's plain git() helper: tree construction needs GIT_INDEX_FILE and
// stdin plumbing the shared helper does not carry.
func gitEnvRun(ctx context.Context, dir string, extraEnv []string, limit int, stdin []byte, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir, "--no-optional-locks"}, args...)...)
	cmd.WaitDelay = time.Second
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_NO_LAZY_FETCH=1")
	cmd.Env = append(cmd.Env, extraEnv...)
	out := &boundedBuffer{limit: limit}
	cmd.Stdout = out
	cmd.Stderr = io.Discard
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	err := cmd.Run()
	return out.Bytes(), err
}

// resolveGitDirs returns the root's git common dir and index path, both
// absolute. The common dir is where refs must be written and read: linked
// worktrees share the main repository (or bare cache), so a ref written from a
// short-lived task worktree must outlive it.
func resolveGitDirs(ctx context.Context, dir string) (commonDir, indexPath string, err error) {
	out, err := git(ctx, dir, 4096, "rev-parse", "--git-common-dir")
	if err != nil {
		return "", "", fmt.Errorf("git-common-dir: %w", err)
	}
	commonDir = strings.TrimSpace(string(out))
	if !filepath.IsAbs(commonDir) {
		commonDir = filepath.Join(dir, commonDir)
	}
	if commonDir, err = filepath.Abs(commonDir); err != nil {
		return "", "", err
	}
	out, err = git(ctx, dir, 4096, "rev-parse", "--git-path", "index")
	if err != nil {
		return "", "", fmt.Errorf("git-path index: %w", err)
	}
	indexPath = strings.TrimSpace(string(out))
	if !filepath.IsAbs(indexPath) {
		indexPath = filepath.Join(dir, indexPath)
	}
	return commonDir, filepath.Clean(indexPath), nil
}

// pinTree snapshots the root's worktree into a git tree and returns the orphan
// commit OID that will pin it. The tree is written through a temporary index
// seeded from the real one, so unchanged files keep their stat cache and are
// not re-hashed.
func (c *Collector) pinTree(ctx context.Context, r *rootState, side string) (string, error) {
	commonDir, indexPath, err := resolveGitDirs(ctx, r.dir)
	if err != nil {
		return "", err
	}
	r.gitCommonDir = commonDir
	tmp, err := os.CreateTemp("", "multica-rundiff-index-")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	indexEnv := []string{"GIT_INDEX_FILE=" + tmpName}
	// Seed from the real index so unchanged files are not re-hashed. A missing
	// index (fresh repository) initializes an empty one through read-tree: git
	// rejects a zero-byte GIT_INDEX_FILE and a valid empty index carries a
	// checksum we let git compute. Preserve the seed's mtime as well: Git uses
	// it to detect racily clean entries. A newer copy could hide same-size edits
	// whose stat data still matches the cached entry.
	seeded := false
	var seedModTime time.Time
	if source, seedErr := os.Open(indexPath); seedErr == nil {
		// Read bytes and timestamp from the same descriptor: Git may replace
		// the real index atomically while the snapshot is being prepared.
		info, statErr := source.Stat()
		seed, readErr := io.ReadAll(source)
		source.Close()
		if statErr == nil && readErr == nil && len(seed) > 12 {
			if _, werr := tmp.Write(seed); werr != nil {
				tmp.Close()
				return "", werr
			}
			seedModTime = info.ModTime()
			seeded = true
		}
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if seeded {
		if err := os.Chtimes(tmpName, seedModTime, seedModTime); err != nil {
			return "", fmt.Errorf("preserve index timestamp: %w", err)
		}
	}
	if !seeded {
		if _, err := gitEnvRun(ctx, r.dir, indexEnv, 256, nil, "read-tree", "--empty"); err != nil {
			return "", fmt.Errorf("read-tree empty: %w", err)
		}
	}

	// Seed listing with modes: identifies gitlinks to strip and every entry
	// that the filtered worktree listing will not cover (tracked-but-sensitive
	// files), which must be force-removed from the temporary index so they stay
	// out of the tree.
	seedOut, err := gitEnvRun(ctx, r.dir, indexEnv, 16<<20, nil, "ls-files", "-z", "-s", "--cached")
	if err != nil {
		return "", fmt.Errorf("ls-files seed: %w", err)
	}
	type seedEntry struct{ path, mode string }
	var seed []seedEntry
	for _, line := range bytes.Split(seedOut, []byte{0}) {
		tab := bytes.IndexByte(line, '\t')
		if tab < 0 {
			continue
		}
		meta := strings.Fields(string(line[:tab]))
		if len(meta) != 4 {
			continue
		}
		seed = append(seed, seedEntry{mode: meta[1], path: string(line[tab+1:])})
	}

	// Worktree listing: tracked plus untracked-not-ignored. This is the full
	// candidate set for the tree; deleted files stay listed through --cached
	// and drop out via --remove when update-index sees them missing. Embedded
	// repositories surface as trailing-slash directories and are excluded.
	raw, err := gitEnvRun(ctx, r.dir, indexEnv, 16<<20, nil, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return "", fmt.Errorf("ls-files: %w", err)
	}
	keep := make([]string, 0, 1024)
	keepSet := make(map[string]bool, 1024)
	filesSeen := 0
	limitHit := false
	for _, p := range bytes.Split(raw, []byte{0}) {
		if len(p) == 0 {
			continue
		}
		path := string(p)
		if strings.HasSuffix(path, "/") {
			// Embedded repository: owned by its own root (or ignored when it
			// never got one), never part of the parent tree.
			continue
		}
		if !validLocalPath(path) || sensitive(path) || c.nested(r, filepath.Join(r.dir, filepath.FromSlash(path))) {
			continue
		}
		if filesSeen >= c.limits.Files {
			limitHit = true
			break
		}
		filesSeen++
		keep = append(keep, path)
		keepSet[path] = true
	}

	// Strip seeded entries the keep list does not cover: sensitive or nested
	// tracked files, and gitlinks (mode 160000), which belong to their own
	// root. --force-remove is plumbing-only and needs no worktree access.
	var remove []byte
	for _, s := range seed {
		if keepSet[s.path] && s.mode != "160000" {
			continue
		}
		remove = append(remove, []byte(s.path)...)
		remove = append(remove, 0)
	}
	if len(remove) > 0 {
		if _, err := gitEnvRun(ctx, r.dir, indexEnv, 1<<20, remove, "update-index", "-z", "--force-remove", "--stdin"); err != nil {
			return "", fmt.Errorf("update-index remove: %w", err)
		}
	}

	var input []byte
	for _, p := range keep {
		input = append(input, []byte(p)...)
		input = append(input, 0)
	}
	// --add --remove syncs the temporary index to the worktree: missing files
	// drop out, untracked files enter, kind changes (file↔symlink) are picked
	// up by the re-stat.
	if _, err := gitEnvRun(ctx, r.dir, indexEnv, 1<<20, input, "update-index", "-z", "--add", "--remove", "--stdin"); err != nil {
		return "", fmt.Errorf("update-index: %w", err)
	}

	treeOut, err := gitEnvRun(ctx, r.dir, indexEnv, 256, nil, "write-tree")
	if err != nil {
		return "", fmt.Errorf("write-tree: %w", err)
	}
	tree := strings.TrimSpace(string(treeOut))

	// Fixed author/committer and dates make the orphan commits deterministic
	// and keep the daemon's real identity out of the object database.
	authorEnv := []string{
		"GIT_AUTHOR_NAME=multica",
		"GIT_AUTHOR_EMAIL=multica@localhost",
		"GIT_AUTHOR_DATE=1700000000 +0000",
		"GIT_COMMITTER_NAME=multica",
		"GIT_COMMITTER_EMAIL=multica@localhost",
		"GIT_COMMITTER_DATE=1700000000 +0000",
	}
	commitOut, err := gitEnvRun(ctx, r.dir, authorEnv, 256, nil, "commit-tree", tree, "-m", "multica run-diff "+c.id+" "+side)
	if err != nil {
		return "", fmt.Errorf("commit-tree: %w", err)
	}
	if limitHit {
		r.Coverage = "partial"
		r.Reason = "file_limit"
	}
	return strings.TrimSpace(string(commitOut)), nil
}

// pinAndJournal pins one side and records the ref in the journal and the root
// state only after the ref write succeeds.
func (c *Collector) pinAndJournal(ctx context.Context, r *rootState, side string) error {
	ref := runDiffRef(c.id, side)
	commit, err := c.pinTree(ctx, r, side)
	if err != nil {
		return err
	}
	pending := c.rootRefs()
	switch side {
	case "before":
		pending = append(pending, RootRef{ID: r.ID, Dir: r.dir, GitCommonDir: r.gitCommonDir, BeforeRef: ref})
	case "after":
		pending = append(pending, RootRef{ID: r.ID, Dir: r.dir, GitCommonDir: r.gitCommonDir, AfterRef: ref})
	}
	if c.refJournal != nil {
		if err := c.refJournal(pending); err != nil {
			return errJournalFailed
		}
	}
	if _, err := git(ctx, r.dir, 256, "update-ref", ref, commit); err != nil {
		return fmt.Errorf("update-ref: %w", err)
	}
	switch side {
	case "before":
		r.beforeRef = ref
		r.beforeCommit = commit
	case "after":
		r.afterRef = ref
		r.afterCommit = commit
	}
	return nil
}

var errJournalFailed = fmt.Errorf("ref journal failed")

// RootRefs snapshots every git root's pinned-ref anchors recorded so far.
func (c *Collector) RootRefs() []RootRef {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.rootRefs()
}

func (c *Collector) rootRefs() []RootRef {
	out := make([]RootRef, 0, len(c.roots))
	for _, r := range c.roots {
		if r.Kind != "git" || (r.beforeRef == "" && r.afterRef == "") {
			continue
		}
		out = append(out, RootRef{ID: r.ID, Dir: r.dir, GitCommonDir: r.gitCommonDir, BeforeRef: r.beforeRef, AfterRef: r.afterRef})
	}
	return out
}

// deleteRefs removes every pinned run-diff ref this collector created. Used on
// abort; normal completion keeps the refs resident until task GC.
func (c *Collector) deleteRefs() {
	for _, ref := range c.rootRefs() {
		dir := ref.Dir
		if _, err := os.Stat(dir); err != nil {
			dir = ref.GitCommonDir
		}
		if dir == "" {
			continue
		}
		for _, name := range []string{ref.BeforeRef, ref.AfterRef} {
			if name == "" {
				continue
			}
			delCtx, delCancel := context.WithTimeout(context.Background(), 2*time.Second)
			_, _ = gitEnvRun(delCtx, dir, nil, 256, nil, "update-ref", "-d", name)
			delCancel()
		}
	}
}

type diffEntry struct {
	path              string
	change            string
	beforeMode        string
	aftMode           string
	beforeOID, aftOID string
}

type numstatEntry struct {
	ins, del int64
	binary   bool
	// hasStats distinguishes a numstat row of "0 0" (counted, no change) from
	// a row the degraded numstat pass never produced.
	hasStats bool
}

// manifestRoot builds the root's file list from the two pinned trees: change,
// modes, per-side sizes, line counts and body_status. Bodies are never
// attached — the daemon serves them on demand from the pinned refs.
func (c *Collector) manifestRoot(ctx context.Context, r *rootState) ([]File, bool) {
	rawOut, err := gitEnvRun(ctx, r.dir, nil, 64<<20, nil, "diff-tree", "-r", "-z", "--no-renames", "--raw", r.beforeCommit, r.afterCommit)
	if err != nil {
		failRoot(r, "manifest_failed")
		return nil, true
	}
	entries := parseDiffTreeRaw(rawOut)
	// A missing before-side blob can make the shared numstat pass fail even
	// though the raw manifest is intact; degrade the line statistics instead
	// of hiding the rows (the missing blob dominates per row below).
	statsComplete := true
	var stats map[string]numstatEntry
	if numOut, numErr := gitEnvRun(ctx, r.dir, nil, 64<<20, nil, "diff-tree", "-r", "-z", "--no-renames", "--numstat", r.beforeCommit, r.afterCommit); numErr != nil {
		stats = map[string]numstatEntry{}
		statsComplete = false
	} else {
		stats = parseDiffTreeNumstat(numOut)
	}
	sizes := c.blobSizes(ctx, r, entries)

	files := make([]File, 0, len(entries))
	for i := range entries {
		e := &entries[i]
		if len(files) >= c.limits.Files {
			failRoot(r, "file_limit")
			break
		}
		f := File{
			ID:         identity(r.ID + "/" + e.path),
			RootID:     r.ID,
			Path:       e.path,
			Change:     e.change,
			BeforeMode: e.beforeMode,
			AfterMode:  e.aftMode,
			BodyStatus: "available",
		}
		if e.beforeOID != "" {
			f.BeforeBytes = sizes[e.beforeOID]
			f.BeforeOID = e.beforeOID
		}
		if e.aftOID != "" {
			f.AfterBytes = sizes[e.aftOID]
			f.AfterOID = e.aftOID
		}
		st := stats[e.path]
		f.Binary = st.binary
		if st.binary {
			f.BodyStatus = "binary"
		} else {
			status, reason, before, after := c.classifyRow(ctx, r, e, f.BeforeBytes, f.AfterBytes)
			f.BodyStatus = status
			f.Reason = reason
			// The same sanitized text the relay will serve is in hand right
			// here, so pin it: this is the capture-time anchor the server
			// checks every relay response against.
			if status == "available" || status == "redacted" {
				f.BodySHA256 = BodyDigest(before, after)
			}
		}
		if !st.binary {
			if st.hasStats {
				ins, del := st.ins, st.del
				f.Insertions, f.Deletions = &ins, &del
			} else {
				statsComplete = false
			}
		}
		files = append(files, f)
	}
	return files, statsComplete
}

// classifyBlobPair reads both sides through the presentation pipeline
// (normalizeRuntime + SafeText) to decide whether the content is available
// as-is or must be flagged redacted. A read failure reports ok=false: the blob
// is gone even though the manifest row survives.
// classifyRow orders the presentation decisions for one non-binary row the
// way the pre-tree collector did: baseline readability first (a missing or
// unreadable before blob dominates everything), then the per-side budget that
// turns oversize rows into metadata-only, then redaction. The sanitized text
// travels only as far as this function's caller, which reduces it to the
// capture-time body digest; content is never attached to the manifest.
func (c *Collector) classifyRow(ctx context.Context, r *rootState, e *diffEntry, beforeBytes, afterBytes int64) (bodyStatus, reason string, before, after string) {
	read := func(tree string) (string, bool) {
		out, err := gitEnvRun(ctx, r.dir, nil, c.limits.SideBytes+1, nil, "cat-file", "blob", tree+":"+e.path)
		if err != nil {
			return "", false
		}
		return string(normalizeRuntime(e.path, out)), true
	}
	if e.beforeOID != "" {
		var ok bool
		if before, ok = read(r.beforeCommit); !ok {
			return "unavailable", "baseline_object_missing", "", ""
		}
	}
	oversize := (e.beforeOID != "" && beforeBytes > int64(c.limits.SideBytes)) ||
		(e.aftOID != "" && afterBytes > int64(c.limits.SideBytes))
	if oversize {
		// Above the per-side budget the content stays unviewable; the row and
		// its line statistics still come from diff-tree.
		return "omitted", "side_budget", "", ""
	}
	if e.aftOID != "" {
		var ok bool
		if after, ok = read(r.afterCommit); !ok {
			return "unavailable", "object_missing", "", ""
		}
	}
	if redactsText(before) || redactsText(after) {
		return "redacted", "", before, after
	}
	return "available", "", before, after
}

func redactsText(s string) bool { return SafeText(s) != s }

// blobSizes resolves per-oid blob sizes in one batch-check round trip.
func (c *Collector) blobSizes(ctx context.Context, r *rootState, entries []diffEntry) map[string]int64 {
	seen := make(map[string]bool, 2*len(entries))
	var input []byte
	for i := range entries {
		for _, oid := range []string{entries[i].beforeOID, entries[i].aftOID} {
			if oid == "" || seen[oid] {
				continue
			}
			seen[oid] = true
			input = append(input, []byte(oid)...)
			input = append(input, '\n')
		}
	}
	sizes := make(map[string]int64, len(seen))
	if len(input) == 0 {
		return sizes
	}
	out, err := gitEnvRun(ctx, r.dir, nil, 4<<20, input, "cat-file", "--batch-check")
	if err != nil {
		return sizes
	}
	for _, line := range bytes.Split(out, []byte{'\n'}) {
		fields := strings.Fields(string(line))
		if len(fields) == 3 && fields[1] == "blob" {
			var size int64
			if _, err := fmt.Sscanf(fields[2], "%d", &size); err == nil {
				sizes[fields[0]] = size
			}
		}
	}
	return sizes
}

// parseDiffTreeRaw parses `diff-tree -r -z --no-renames --raw` output:
// ":<old_mode> <new_mode> <old_oid> <new_oid> <status>\0<path>\0" records. The
// leading colon glues onto the old mode, so a record splits into 5 fields.
func parseDiffTreeRaw(out []byte) []diffEntry {
	var entries []diffEntry
	parts := bytes.Split(out, []byte{0})
	for i := 0; i < len(parts)-1; i++ {
		line := parts[i]
		if len(line) == 0 || line[0] != ':' {
			continue
		}
		fields := strings.Fields(string(line))
		if len(fields) != 5 || i+1 >= len(parts) {
			continue
		}
		path := string(parts[i+1])
		i++
		e := diffEntry{
			path:       path,
			beforeMode: fields[0][1:],
			aftMode:    fields[1],
			beforeOID:  trimZeroOID(fields[2]),
			aftOID:     trimZeroOID(fields[3]),
		}
		switch fields[4] {
		case "A":
			e.change = "added"
		case "D":
			e.change = "deleted"
		case "T":
			e.change = "mode_changed"
		default: // "M"
			if e.beforeMode != e.aftMode && e.beforeOID == e.aftOID {
				e.change = "mode_changed"
			} else {
				e.change = "modified"
			}
		}
		entries = append(entries, e)
	}
	return entries
}

func trimZeroOID(oid string) string {
	if strings.Trim(oid, "0") == "" {
		return ""
	}
	return oid
}

// parseDiffTreeNumstat parses `diff-tree -r -z --no-renames --numstat` output:
// each NUL-terminated record is "<ins>\t<del>\t<path>" within one segment
// (unlike --raw, the path is not a separate NUL-delimited field), with "-"
// marking binary content.
func parseDiffTreeNumstat(out []byte) map[string]numstatEntry {
	stats := make(map[string]numstatEntry)
	for _, rec := range bytes.Split(out, []byte{0}) {
		line := string(rec)
		if line == "" {
			continue
		}
		tab1 := strings.IndexByte(line, '\t')
		if tab1 < 0 {
			continue
		}
		tab2 := strings.IndexByte(line[tab1+1:], '\t')
		if tab2 < 0 {
			continue
		}
		tab2 += tab1 + 1
		path := line[tab2+1:]
		entry := numstatEntry{hasStats: true}
		if line[:tab1] == "-" || line[tab1+1:tab2] == "-" {
			entry.binary = true
		} else {
			fmt.Sscanf(line[:tab1], "%d", &entry.ins)
			fmt.Sscanf(line[tab1+1:tab2], "%d", &entry.del)
		}
		stats[path] = entry
	}
	return stats
}

func failRoot(r *rootState, reason string) {
	if r.Coverage == "unavailable" {
		return
	}
	r.Coverage = "partial"
	r.Reason = reason
}

// ReadPinnedBlob reads one blob from a pinned run-diff ref in the repository
// at gitDir (the common dir, so the read survives worktree removal). Output is
// bounded at limit bytes; a larger blob errors.
func ReadPinnedBlob(ctx context.Context, gitDir, ref, path string, limit int) ([]byte, error) {
	return gitEnvRun(ctx, gitDir, nil, limit, nil, "cat-file", "blob", ref+":"+path)
}

// DeleteGitRef deletes one ref in the repository at gitDir. Best-effort
// callers treat an error as "sweep again tomorrow".
func DeleteGitRef(ctx context.Context, gitDir, ref string) error {
	_, err := gitEnvRun(ctx, gitDir, nil, 256, nil, "update-ref", "-d", ref)
	return err
}

// ListRunDiffRefs returns every refs/multica/run-diff/* refname in the
// repository at gitDir. Used by the daemon's orphan sweep.
func ListRunDiffRefs(ctx context.Context, gitDir string) ([]string, error) {
	out, err := gitEnvRun(ctx, gitDir, nil, 4<<20, nil, "for-each-ref", "--format=%(refname)", "refs/multica/run-diff/")
	if err != nil {
		return nil, err
	}
	var refs []string
	for _, line := range strings.Split(string(out), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			refs = append(refs, line)
		}
	}
	return refs, nil
}

// AssembleRelayFile attaches freshly fetched bodies to a manifest row for the
// relay response, applying the same presentation pipeline upload// upload path (normalizeRuntime + SafeText). The row's identity and diff
// metadata carry over untouched; sizes follow the normalized text when a side
// was provided.
func AssembleRelayFile(row File, before, after []byte) *File {
	f := row
	f.BeforeOID, f.AfterOID = "", ""
	if before != nil {
		b := normalizeRuntime(f.Path, before)
		s := SafeText(string(b))
		f.Before = &s
		f.BeforeBytes = int64(len(s))
		if s != string(b) {
			f.BodyStatus = "redacted"
		}
	}
	if after != nil {
		b := normalizeRuntime(f.Path, after)
		s := SafeText(string(b))
		f.After = &s
		f.AfterBytes = int64(len(s))
		if s != string(b) {
			f.BodyStatus = "redacted"
		}
	}
	return &f
}
