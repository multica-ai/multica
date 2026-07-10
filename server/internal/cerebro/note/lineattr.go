package note

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

// FIR-2810: per-line attribution. Every Notes-feature note keeps a
// cerebro_note_line_attr row holding one LineAttr per body line: who created
// the line and who last edited it (Apple Notes-style "show who wrote what").
//
// The attribution advances by diffing the previous body against the new body
// on every save that goes through the note handler (edit, suggestion accept,
// version restore). Writes that bypass the note handler (agents using the
// artifact API, the note-types sweeper prepending a section) are healed lazily
// on the next read: the stored base_body no longer matches the artifact body,
// so the reader diffs base_body → current body with an unknown actor — kept
// lines keep their attribution, changed lines show no author.

// LineAttr is the attribution of a single body line. Empty strings mean
// "unknown" (line predates tracking or was written outside the note handler).
type LineAttr struct {
	CreatedBy string `json:"created_by"`
	CreatedAt string `json:"created_at"`
	UpdatedBy string `json:"updated_by"`
	UpdatedAt string `json:"updated_at"`
}

// splitLines splits a body into lines. An empty body has zero lines (not one
// empty line), so a brand-new note carries an empty attrs array.
func splitLines(body string) []string {
	if body == "" {
		return nil
	}
	return strings.Split(body, "\n")
}

// maxLCSCells caps the O(n·m) LCS table. Notes are small; a body pair beyond
// this (≈ 2000×2000 lines) falls back to prefix/suffix matching only, which
// attributes the whole changed middle to the actor — approximate but safe.
const maxLCSCells = 4_000_000

// AdvanceLineAttrs moves attribution from (oldBody, oldAttrs) to newBody.
// Unchanged lines keep their attribution. Within a replaced run, the i-th new
// line inherits created_by/created_at from the i-th removed line (a "modified"
// line) and gets updated_by = actor. Lines beyond the removed run are brand
// new: created and updated by the actor. An empty actor records "unknown".
// oldAttrs shorter/longer than oldBody's line count is tolerated (padded with
// unknown / truncated) so a corrupt row can never panic a save.
func AdvanceLineAttrs(oldBody string, oldAttrs []LineAttr, newBody, actor string, now time.Time) []LineAttr {
	oldLines := splitLines(oldBody)
	newLines := splitLines(newBody)
	ts := now.UTC().Format(time.RFC3339)

	// Normalize oldAttrs to exactly one entry per old line.
	attrs := make([]LineAttr, len(oldLines))
	copy(attrs, oldAttrs)

	newAttr := func() LineAttr {
		if actor == "" {
			return LineAttr{}
		}
		return LineAttr{CreatedBy: actor, CreatedAt: ts, UpdatedBy: actor, UpdatedAt: ts}
	}
	editOf := func(old LineAttr) LineAttr {
		out := old
		out.UpdatedBy = actor
		out.UpdatedAt = ts
		if actor == "" {
			out.UpdatedBy = ""
			out.UpdatedAt = ""
		}
		return out
	}

	// Trim the common prefix and suffix — the typical save touches one spot.
	prefix := 0
	for prefix < len(oldLines) && prefix < len(newLines) && oldLines[prefix] == newLines[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(oldLines)-prefix && suffix < len(newLines)-prefix &&
		oldLines[len(oldLines)-1-suffix] == newLines[len(newLines)-1-suffix] {
		suffix++
	}

	midOld := oldLines[prefix : len(oldLines)-suffix]
	midOldAttrs := attrs[prefix : len(attrs)-suffix]
	midNew := newLines[prefix : len(newLines)-suffix]

	out := make([]LineAttr, 0, len(newLines))
	out = append(out, attrs[:prefix]...)
	out = append(out, alignMiddle(midOld, midOldAttrs, midNew, newAttr, editOf)...)
	out = append(out, attrs[len(attrs)-suffix:]...)
	return out
}

// alignMiddle attributes the changed middle segment. When the segment is small
// enough it runs an LCS so lines that merely moved (or survived among edits)
// keep their attribution; replaced runs pair old and new lines positionally.
func alignMiddle(
	oldLines []string, oldAttrs []LineAttr, newLines []string,
	newAttr func() LineAttr, editOf func(LineAttr) LineAttr,
) []LineAttr {
	if len(newLines) == 0 {
		return nil
	}
	if len(oldLines) == 0 {
		out := make([]LineAttr, len(newLines))
		for i := range out {
			out[i] = newAttr()
		}
		return out
	}
	if len(oldLines)*len(newLines) > maxLCSCells {
		// Too big for LCS: pair positionally, everything is a modify/insert.
		out := make([]LineAttr, len(newLines))
		for i := range newLines {
			if i < len(oldLines) {
				out[i] = editOf(oldAttrs[i])
			} else {
				out[i] = newAttr()
			}
		}
		return out
	}

	// Standard LCS table over the middle lines.
	n, m := len(oldLines), len(newLines)
	lcs := make([][]int32, n+1)
	for i := range lcs {
		lcs[i] = make([]int32, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if oldLines[i] == newLines[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}

	// Classic diff walk. Deleted lines buffer up in pendingDel; each inserted
	// line consumes the oldest buffered delete as its "previous version" (a
	// modify: creator kept, editor = actor). Inserts with no buffered delete
	// are brand-new lines. Leftover deletes at the end are simply gone.
	out := make([]LineAttr, 0, m)
	var pendingDel []LineAttr
	i, j := 0, 0
	for j < m {
		switch {
		case i < n && oldLines[i] == newLines[j]:
			pendingDel = nil
			out = append(out, oldAttrs[i])
			i++
			j++
		case i < n && lcs[i+1][j] >= lcs[i][j+1]:
			pendingDel = append(pendingDel, oldAttrs[i])
			i++
		default:
			if len(pendingDel) > 0 {
				out = append(out, editOf(pendingDel[0]))
				pendingDel = pendingDel[1:]
			} else {
				out = append(out, newAttr())
			}
			j++
		}
	}
	return out
}

// parseLineAttrs decodes a stored attrs JSONB payload; a corrupt payload
// degrades to unknown attribution instead of failing the request.
func parseLineAttrs(raw []byte) []LineAttr {
	if len(raw) == 0 {
		return nil
	}
	var out []LineAttr
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return out
}

// loadCurrentLineAttrs returns the attribution for body as stored, healing it
// first when the stored base no longer matches (an out-of-band write happened:
// agent via the artifact API, note-types sweeper, …). A missing row yields
// all-unknown attribution for the current body. Never returns an error — the
// note read/save must not fail because attribution is unavailable.
func (h *Handler) loadCurrentLineAttrs(ctx context.Context, noteID pgtype.UUID, body string, now time.Time) []LineAttr {
	row, err := h.Cerebro.GetNoteLineAttr(ctx, noteID)
	if err != nil {
		// No row yet: every existing line has unknown attribution.
		return make([]LineAttr, len(splitLines(body)))
	}
	attrs := parseLineAttrs(row.Attrs)
	if row.BaseBody == body {
		// Normalize a length drift defensively.
		lines := splitLines(body)
		if len(attrs) != len(lines) {
			norm := make([]LineAttr, len(lines))
			copy(norm, attrs)
			return norm
		}
		return attrs
	}
	// Out-of-band edit: heal with an unknown actor.
	return AdvanceLineAttrs(row.BaseBody, attrs, body, "", now)
}

// saveLineAttrs persists attribution for body; failures are logged, never
// surfaced (attribution is best-effort metadata).
func (h *Handler) saveLineAttrs(ctx context.Context, noteID pgtype.UUID, body string, attrs []LineAttr) {
	payload, err := json.Marshal(attrs)
	if err != nil {
		slog.Warn("note line attrs marshal failed", "note_id", uuidStr(noteID), "err", err)
		return
	}
	if err := h.Cerebro.UpsertNoteLineAttr(ctx, cerebrodb.UpsertNoteLineAttrParams{
		ArtifactID: noteID,
		BaseBody:   body,
		Attrs:      payload,
	}); err != nil {
		slog.Warn("note line attrs save failed", "note_id", uuidStr(noteID), "err", err)
	}
}

// advanceAndSaveLineAttrs is the write-path helper: heal to oldBody if needed,
// advance to newBody crediting actor, persist, and return the new attribution.
func (h *Handler) advanceAndSaveLineAttrs(ctx context.Context, noteID pgtype.UUID, oldBody, newBody, actor string) []LineAttr {
	now := time.Now()
	cur := h.loadCurrentLineAttrs(ctx, noteID, oldBody, now)
	next := AdvanceLineAttrs(oldBody, cur, newBody, actor, now)
	h.saveLineAttrs(ctx, noteID, newBody, next)
	return next
}

// ensureLineAttrs is the read-path helper: return attribution aligned with
// body, persisting a heal when the stored base is missing or stale so the next
// read is cheap again.
func (h *Handler) ensureLineAttrs(ctx context.Context, noteID pgtype.UUID, body string) []LineAttr {
	now := time.Now()
	row, err := h.Cerebro.GetNoteLineAttr(ctx, noteID)
	if err == nil && row.BaseBody == body {
		attrs := parseLineAttrs(row.Attrs)
		if lines := splitLines(body); len(attrs) != len(lines) {
			norm := make([]LineAttr, len(lines))
			copy(norm, attrs)
			return norm
		}
		return attrs
	}
	attrs := h.loadCurrentLineAttrs(ctx, noteID, body, now)
	h.saveLineAttrs(ctx, noteID, body, attrs)
	return attrs
}
