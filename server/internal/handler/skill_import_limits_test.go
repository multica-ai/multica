package handler

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/testutil"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestImportLimitsExactValues(t *testing.T) {
	if maxImportFileSize != 104857600 || maxImportTotalSize != 1099511627776 || maxImportFileCount != 100000 || maxImportArchiveUploadSize != 1099511627776 {
		t.Fatal("unexpected import ceilings")
	}
	for _, current := range []int64{maxImportTotalSize, math.MaxInt64} {
		s := &importedSkill{bundleSize: current}
		if err := s.addFile("ref.md", "x"); !isCapError(err) {
			t.Fatalf("size %d: expected cap error, got %v", current, err)
		}
		if s.bundleSize != current || len(s.files) != 0 {
			t.Fatal("rejected file changed bundle")
		}
	}
	s := &importedSkill{bundleSize: maxImportTotalSize - 1}
	if err := s.addFile("ref.md", "x"); err != nil || s.bundleSize != maxImportTotalSize {
		t.Fatalf("exact boundary: %v", err)
	}
}

type untouchedArchiveReader struct{ reads int }

func (r *untouchedArchiveReader) ReadAt(p []byte, off int64) (int, error) {
	r.reads++
	return 0, io.EOF
}

func TestArchiveSizeBoundaryWithoutLargeData(t *testing.T) {
	for _, size := range []int64{maxImportArchiveUploadSize - 1, maxImportArchiveUploadSize, maxImportArchiveUploadSize + 1} {
		r := &untouchedArchiveReader{}
		_, err := parseSkillArchiveReader(t.Context(), r, size, "test.zip", "")
		if size > maxImportArchiveUploadSize {
			if !isCapError(err) || r.reads != 0 {
				t.Fatalf("oversized archive read before rejection: %v", err)
			}
		} else if isCapError(err) || r.reads == 0 {
			t.Fatalf("size %d rejected by size guard: %v", size, err)
		}
	}
}

func TestArchiveRejectsOversizedHeaderBeforeDecompression(t *testing.T) {
	data := buildTestZip(t, map[string]string{"SKILL.md": "---\nname: limits\n---", "ref.md": "small"})
	for i := 0; i+46 < len(data); i++ {
		if binary.LittleEndian.Uint32(data[i:]) != 0x02014b50 {
			continue
		}
		n := int(binary.LittleEndian.Uint16(data[i+28:]))
		if string(data[i+46:i+46+n]) == "ref.md" {
			binary.LittleEndian.PutUint32(data[i+24:], maxImportFileSize+1)
		}
	}
	if _, err := parseSkillArchive(data, "test.zip"); !isCapError(err) {
		t.Fatalf("expected cap error, got %v", err)
	}
}

func TestSkillSpoolRoundTripAndCleanup(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	request, cleanup, err := prepareSkillSpool(httptest.NewRequest("POST", "/", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	dir := skillSpoolDir(request.Context())
	data := buildTestZip(t, map[string]string{"SKILL.md": "---\nname: spool\n---", "ref.md": "retained on disk"})
	result, err := parseSkillArchiveReader(t.Context(), bytes.NewReader(data), int64(len(data)), "test.zip", dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.files) != 1 || result.files[0].content != "" || result.files[0].contentFile == "" {
		t.Fatal("supporting body retained in memory")
	}
	files := importedSkillFileRequests(result)
	body, err := files[0].readContent()
	if err != nil || body != "retained on disk" {
		t.Fatalf("read persisted body: %q %v", body, err)
	}
	cleanup()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("spool survived cleanup: %v", err)
	}
}

func TestArchiveReadLimitChecksActualBytes(t *testing.T) {
	data := buildTestZip(t, map[string]string{"ref.md": "12345"})
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	for _, limit := range []int64{4, 5, 6} {
		body, err := readZipFile(zr.File[0], limit)
		if limit < 5 {
			if !isCapError(err) {
				t.Fatalf("actual bytes not capped: %v", err)
			}
		} else if err != nil || body != "12345" {
			t.Fatalf("boundary %d: %q %v", limit, body, err)
		}
	}
}

func TestLargeArchivePersistsWithoutInliningBody(t *testing.T) {
	name := "large-import-spool"
	skillID := dbfx.Insert(t, "skill", testutil.Cols{"workspace_id": testWorkspaceID, "name": name, "content": "old", "created_by": testUserID})
	dbfx.Cleanup(t, "DELETE FROM skill_file WHERE skill_id = $1", skillID)
	body := strings.Repeat("x", (8<<20)+1)
	archive := buildTestZip(t, map[string]string{"SKILL.md": skillMdWithName(name, "large"), "ref.md": body})
	temp := t.TempDir()
	t.Setenv("TMPDIR", temp)
	var result SkillImportResult
	response := testutil.Call(t, testHandler.ImportSkill, newSkillArchiveImportRequest(testUserID, archive, "large.skill", "overwrite")).Want(http.StatusOK).JSON(&result)
	if result.Skill == nil || len(result.Skill.Files) != 1 {
		t.Fatal("missing imported file")
	}
	file := result.Skill.Files[0]
	if !file.ContentOmitted || file.Content != "" || response.Body.Len() > 10000 {
		t.Fatal("response inlined large supporting body")
	}
	req := skillRequest(t, skillID, "/api/skills/"+skillID+"/files/"+file.ID)
	chi.RouteContext(req.Context()).URLParams.Add("fileId", file.ID)
	var stored SkillFileResponse
	testutil.Call(t, testHandler.GetSkillFile, req).Want(http.StatusOK).JSON(&stored)
	if stored.Content != body {
		t.Fatal("spooled body was lost during persistence")
	}
	otherID := dbfx.Insert(t, "skill", testutil.Cols{"workspace_id": testWorkspaceID, "name": "other-large-parent", "created_by": testUserID})
	wrongParent := skillRequest(t, otherID, "/api/skills/"+otherID+"/files/"+file.ID)
	chi.RouteContext(wrongParent.Context()).URLParams.Add("fileId", file.ID)
	testutil.Call(t, testHandler.GetSkillFile, wrongParent).Want(http.StatusNotFound)
	// A legacy client must not turn omitted import bodies into empty stored files.
	update := withURLParam(newRequest(http.MethodPatch, "/api/skills/"+skillID, map[string]any{"files": []any{}}), "id", skillID)
	testutil.Call(t, testHandler.UpdateSkill, update).Want(http.StatusRequestEntityTooLarge)
	testutil.Call(t, testHandler.GetSkillFile, req).Want(http.StatusOK).JSON(&stored)
	if stored.Content != body {
		t.Fatal("bulk editing protection lost stored content")
	}

	entries, err := os.ReadDir(temp)
	if err != nil || len(entries) != 0 {
		t.Fatalf("request leaked temporary files: %v %v", entries, err)
	}
	testutil.Call(t, testHandler.GetSkill, skillRequest(t, skillID, "/api/skills/"+skillID)).Want(http.StatusRequestEntityTooLarge)
	var metadata SkillWithFileMetadataResponse
	testutil.Call(t, testHandler.GetSkill, skillRequest(t, skillID, "/api/skills/"+skillID+"?include=metadata")).Want(http.StatusOK).JSON(&metadata)
	if len(metadata.Files) != 1 || metadata.Files[0].Size != int64(len(body)) {
		t.Fatal("large-file metadata unavailable")
	}
}

func TestInvalidArchiveUploadRemovesMultipartSpool(t *testing.T) {
	temp := t.TempDir()
	t.Setenv("TMPDIR", temp)
	request := newSkillArchiveImportRequest(testUserID, make([]byte, 2<<20), "invalid.zip", "fail")
	testutil.Call(t, testHandler.ImportSkill, request).Want(http.StatusBadRequest)
	entries, err := os.ReadDir(temp)
	if err != nil || len(entries) != 0 {
		t.Fatalf("failed archive leaked temporary files: %v %v", entries, err)
	}
}
