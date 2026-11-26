// Package rundiff captures provider-independent final workdir changes.
package rundiff

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/pkg/redact"
)

const (
	MaxSideBytes   = 2 << 20
	MaxBodyBytes   = 16 << 20
	MaxFiles       = 10000
	MaxUploadBytes = 40 << 20 // JSON escaping can expand text.
	// maxDiffStatCells bounds the line-diff dynamic program per file; beyond
	// it (two large dissimilar sides) no trustworthy line count exists.
	maxDiffStatCells = 64_000_000
)

type Root struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Kind     string `json:"kind"`
	Coverage string `json:"coverage"`
	Reason   string `json:"reason"`
}
type File struct {
	ID          string `json:"id"`
	RootID      string `json:"root_id"`
	Path        string `json:"path"`
	Change      string `json:"change"`
	BeforeBytes int64  `json:"before_bytes"`
	AfterBytes  int64  `json:"after_bytes"`
	BeforeMode  string `json:"before_mode"`
	AfterMode   string `json:"after_mode"`
	Binary      bool   `json:"binary"`
	BodyStatus  string `json:"body_status"`
	Reason      string `json:"reason"`
	// Insertions/Deletions are the +/- line counts of the attached diff, set
	// whenever both sides were countable. Absent means "not computed" (binary
	// by nature, or a side the budgets did not capture), never zero.
	Insertions *int64  `json:"insertions,omitempty"`
	Deletions  *int64  `json:"deletions,omitempty"`
	Before     *string `json:"before"`
	After      *string `json:"after"`
	// BeforeOID/AfterOID are the git blob OIDs of each side inside the pinned
	// run-diff trees. They exist only for the daemon's local index ():
	// bodies are no longer uploaded, and the daemon re-reads content on demand
	// through refs/multica/run-diff/...:<path>. Both fields are stripped before
	// upload and by server-side validation, so the stored manifest never
	// carries them.
	BeforeOID string `json:"before_oid,omitempty"`
	AfterOID  string `json:"after_oid,omitempty"`
	// BodySHA256 pins the served content at capture time: the hex sha256 of
	// the sanitized before side, a NUL byte, and the sanitized after side,
	// exactly as the body endpoints serve them. Set for every file whose
	// sides were captured (available/redacted); absent means "not verifiable"
	// (binary, budget-omitted, or an unreadable side), never "matches".
	// The server recomputes over whatever it is about to serve — stored
	// directory bodies, or what the runtime's daemon relays back — and
	// degrades to digest_mismatch when the check fails.
	BodySHA256 string `json:"body_sha256,omitempty"`
}
type Collection struct {
	SchemaVersion  int     `json:"schema_version"`
	CollectionID   string  `json:"collection_id"`
	Status         string  `json:"status"`
	Reason         string  `json:"reason"`
	Scope          string  `json:"scope"`
	Roots          []Root  `json:"roots"`
	Files          []File  `json:"files"`
	FilesTotal     int     `json:"files_total"`
	TotalsComplete bool    `json:"totals_complete"`
	NextCursor     *string `json:"next_cursor"`
	// InsertionsTotal/DeletionsTotal sum the per-file line counts that were
	// computed. StatsComplete reports whether every non-binary file
	// contributed; files whose sides the budgets did not capture (omitted,
	// unavailable) leave the sums partial without failing them.
	InsertionsTotal int64 `json:"insertions_total"`
	DeletionsTotal  int64 `json:"deletions_total"`
	StatsComplete   bool  `json:"stats_complete"`
	// Digest covers the collection's verifiable identity: the file list and
	// each file's BodySHA256, in the DigestVersion serialization. Absent
	// means the collection predates integrity digests and nothing is checked.
	Digest        string `json:"digest,omitempty"`
	DigestVersion int    `json:"digest_version,omitempty"`
}

func Empty(id, status, reason string) Collection {
	return Collection{SchemaVersion: 1, CollectionID: id, Status: status, Reason: reason, Scope: "registered_workdir_roots", Roots: []Root{}, Files: []File{}, TotalsComplete: true, StatsComplete: true}
}

// DigestVersion identifies the integrity-digest serialization below; a change
// to the inputs or the framing must bump it.
const DigestVersion = 1

// BodyDigest pins one file's served content: hex sha256 over the before side,
// a NUL separator, and the after side — the exact bytes the body endpoints
// return, sanitized presentation included. A missing side hashes as empty.
func BodyDigest(before, after string) string {
	h := sha256.New()
	h.Write([]byte(before))
	h.Write([]byte{0})
	h.Write([]byte(after))
	return hex.EncodeToString(h.Sum(nil))
}

// ComputeDigest derives the collection digest over the files that carry a
// BodySHA256, sorted by (root_id, path) so upload order cannot change it. It
// returns "" when no file is verifiable, which callers store as an absent
// digest rather than a checked-and-passed one.
func (c *Collection) ComputeDigest() string {
	rows := make([]string, 0, len(c.Files))
	for i := range c.Files {
		f := &c.Files[i]
		if f.BodySHA256 == "" {
			continue
		}
		rows = append(rows, f.RootID+"\x1f"+f.Path+"\x1f"+f.Change+"\x1f"+f.ID+"\x1f"+f.BodySHA256)
	}
	if len(rows) == 0 {
		return ""
	}
	sort.Strings(rows)
	h := sha256.New()
	fmt.Fprintf(h, "multica-run-file-changes-digest-v%d\n", DigestVersion)
	for _, row := range rows {
		h.Write([]byte(row))
		h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// VerifyDigest recomputes the collection digest and reports whether it
// matches. A collection with no verifiable files (no stored digest) passes:
// absence is "not checked", and per-file verification still applies.
func (c *Collection) VerifyDigest() bool {
	if c.Digest == "" {
		return true
	}
	computed := c.ComputeDigest()
	return computed != "" && subtle.ConstantTimeCompare([]byte(computed), []byte(c.Digest)) == 1
}

// VerifyBody reports whether the given sides match the file's captured
// digest. Files without BodySHA256 pass: absence is "not checked".
func (f *File) VerifyBody(before, after string) bool {
	if f.BodySHA256 == "" {
		return true
	}
	return subtle.ConstantTimeCompare([]byte(BodyDigest(before, after)), []byte(f.BodySHA256)) == 1
}

var quotedCredential = regexp.MustCompile(`(?i)(["'](?:token|password|api_key|api_secret|secret|access_token|private_key)["']\s*[:=]\s*)["'](?:\\.|[^"'])*["']`)

func SafeText(s string) string {
	s = quotedCredential.ReplaceAllString(s, `${1}"[REDACTED]"`)
	return strings.ReplaceAll(strings.ToValidUTF8(redact.Text(s), "�"), "\x00", "")
}
func sensitive(p string) bool {
	for _, v := range strings.Split(strings.ToLower(p), "/") {
		switch v {
		case ".git", ".ssh", ".aws", ".azure", ".gnupg", ".multica", ".agent_context", "node_modules", "vendor", "dist", "build", ".next", ".turbo", ".venv", "__pycache__", ".claude", ".codex", "credentials", "credentials.json", "secrets.json", "id_rsa", "id_ed25519":
			return true
		}
		if v == ".env" || strings.HasPrefix(v, ".env.") || strings.HasSuffix(v, ".pem") || strings.HasSuffix(v, ".key") || v == ".npmrc" || v == ".netrc" {
			return true
		}
	}
	return false
}

// ValidateAndSanitize applies the same limits on the receiving server.
func (c *Collection) ValidateAndSanitize() error {
	if _, err := uuid.Parse(c.CollectionID); err != nil {
		return fmt.Errorf("invalid collection_id")
	}
	if c.SchemaVersion != 1 || c.Scope != "registered_workdir_roots" {
		return fmt.Errorf("unsupported schema or scope")
	}
	switch c.Status {
	case "collecting", "ready", "partial", "unavailable":
	default:
		return fmt.Errorf("invalid status")
	}
	if len(c.Reason) > 256 {
		return fmt.Errorf("reason too long")
	}
	if len(c.Files) > MaxFiles || len(c.Roots) > 256 {
		return fmt.Errorf("too many files or roots")
	}
	roots := map[string]bool{}
	rootKinds := map[string]string{}
	for i := range c.Roots {
		r := &c.Roots[i]
		if r.ID == "" || roots[r.ID] || len(r.ID) > 128 {
			return fmt.Errorf("invalid root")
		}
		roots[r.ID] = true
		rootKinds[r.ID] = r.Kind
		r.Label = SafeText(r.Label)
		r.Reason = SafeText(r.Reason)
		if len(r.Reason) > 256 || len(r.Label) > 1024 || (r.Kind != "git" && r.Kind != "directory") {
			return fmt.Errorf("invalid root metadata")
		}
		switch r.Coverage {
		case "complete", "partial", "unavailable":
		default:
			return fmt.Errorf("invalid coverage")
		}
	}
	ids := map[string]bool{}
	paths := map[string]bool{}
	total := 0
	for i := range c.Files {
		f := &c.Files[i]
		if f.ID == "" || len(f.ID) > 128 || ids[f.ID] || !roots[f.RootID] || len(f.Path) > 4096 || f.Path == "" || path.IsAbs(f.Path) || path.Clean(f.Path) != f.Path || f.Path == ".." || strings.HasPrefix(f.Path, "../") || strings.ContainsAny(f.Path, "\\\x00") || !utf8.ValidString(f.Path) {
			return fmt.Errorf("invalid file identity")
		}
		key := f.RootID + "\x00" + f.Path
		if paths[key] {
			return fmt.Errorf("duplicate path")
		}
		paths[key] = true
		ids[f.ID] = true
		if sensitive(f.Path) {
			return fmt.Errorf("excluded file")
		}
		switch f.Change {
		case "added", "deleted", "modified", "mode_changed":
		default:
			return fmt.Errorf("invalid change")
		}
		switch f.BodyStatus {
		case "available", "redacted", "binary", "omitted", "unavailable":
		default:
			return fmt.Errorf("invalid body status")
		}
		if len(f.Reason) > 256 || len(f.BeforeMode) > 32 || len(f.AfterMode) > 32 {
			return fmt.Errorf("invalid metadata length")
		}
		if f.BodyStatus != "available" && f.BodyStatus != "redacted" && (f.Before != nil || f.After != nil) {
			return fmt.Errorf("unavailable body must be absent")
		}
		// git-root content is served on demand from the daemon's pinned
		// trees, so bodies are optional there (older daemons still upload them
		// and the server drops them below). Directory roots keep the strict
		// contract that an available/redacted file carries its sides.
		gitRoot := rootKinds[f.RootID] == "git"
		if !gitRoot && (f.BodyStatus == "available" || f.BodyStatus == "redacted") {
			if (f.Change != "added" && f.Before == nil) || (f.Change != "deleted" && f.After == nil) {
				return fmt.Errorf("available body side missing")
			}
		}
		if f.BeforeBytes < 0 || f.AfterBytes < 0 {
			return fmt.Errorf("invalid size")
		}
		for _, p := range []**string{&f.Before, &f.After} {
			if *p != nil {
				if len(**p) > MaxSideBytes {
					return fmt.Errorf("body too large")
				}
				s := SafeText(**p)
				if s != **p {
					f.BodyStatus = "redacted"
				}
				*p = &s
				if len(s) > MaxSideBytes {
					f.BodyStatus = "omitted"
					f.Reason = "redacted_body_budget"
					c.Status = "partial"
				}
			}
		}
		if gitRoot {
			// Never persist git-root content: the upload's bodies (if any, from
			// an older daemon) end here and only the manifest is stored.
			f.Before = nil
			f.After = nil
		} else {
			if f.BodyStatus != "available" && f.BodyStatus != "redacted" {
				f.Before = nil
				f.After = nil
			}
			if f.Before != nil {
				total += len(*f.Before)
			}
			if f.After != nil {
				total += len(*f.After)
			}
		}
		f.BeforeOID = ""
		f.AfterOID = ""
		if safe := SafeText(f.Path); safe != f.Path {
			f.Path = safe + "~" + f.ID
		}
		f.Reason = SafeText(f.Reason)
	}
	if total > MaxBodyBytes {
		return fmt.Errorf("total body budget exceeded")
	}
	if c.Status == "collecting" && len(c.Files) > 0 {
		return fmt.Errorf("collecting cannot contain files")
	}
	if c.Status == "ready" && !c.TotalsComplete {
		return fmt.Errorf("incomplete collection cannot be ready")
	}
	if c.Status == "ready" {
		for _, r := range c.Roots {
			if r.Coverage != "complete" {
				c.Status = "partial"
				c.TotalsComplete = false
			}
		}
		for _, f := range c.Files {
			if f.BodyStatus == "omitted" || f.BodyStatus == "unavailable" {
				c.Status = "partial"
			}
		}
	}
	c.Reason = SafeText(c.Reason)
	c.FilesTotal = len(c.Files)
	c.NextCursor = nil
	if c.Files == nil {
		c.Files = []File{}
	}
	if c.Roots == nil {
		c.Roots = []Root{}
	}
	// Integrity digests are optional (older daemons never send them). When a
	// digest travels, the manifest must carry it consistently: version 1
	// framing, hex hashes, and a digest that actually covers the files it
	// claims to. A mismatched digest means the upload was corrupted or
	// assembled from mixed sources — reject it rather than store an anchor
	// the read path would fail on forever.
	for i := range c.Files {
		f := &c.Files[i]
		if f.BodySHA256 != "" && !isHexHash(f.BodySHA256) {
			return fmt.Errorf("invalid body_sha256")
		}
	}
	if c.Digest != "" {
		if !isHexHash(c.Digest) || (c.DigestVersion != 0 && c.DigestVersion != DigestVersion) {
			return fmt.Errorf("invalid digest framing")
		}
		if !c.VerifyDigest() {
			return fmt.Errorf("digest does not cover the manifest")
		}
		c.DigestVersion = DigestVersion
	} else {
		c.DigestVersion = 0
	}
	return nil
}

func isHexHash(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
