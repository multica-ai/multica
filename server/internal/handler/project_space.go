package handler

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/projectspace"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type createProjectSpaceImportRequest struct {
	BatchName string                                `json:"batch_name"`
	Files     []createProjectSpaceImportFileRequest `json:"files"`
}

type createProjectSpaceImportFileRequest struct {
	RelativePath string `json:"relative_path"`
	ContentType  string `json:"content_type"`
	SizeBytes    int64  `json:"size_bytes"`
}

type projectSpaceImportFileResponse struct {
	ID                 string  `json:"id"`
	RelativePath       string  `json:"relative_path"`
	StoredRelativePath *string `json:"stored_relative_path,omitempty"`
	ContentType        string  `json:"content_type"`
	SizeBytes          int64   `json:"size_bytes"`
	Sha256             *string `json:"sha256,omitempty"`
	Status             string  `json:"status"`
	ErrorCode          *string `json:"error_code,omitempty"`
}

type projectSpaceImportResponse struct {
	ID             string                           `json:"id"`
	ProjectID      string                           `json:"project_id"`
	BatchName      string                           `json:"batch_name"`
	Status         string                           `json:"status"`
	TotalFiles     int32                            `json:"total_files"`
	TotalBytes     int64                            `json:"total_bytes"`
	CompletedFiles int32                            `json:"completed_files"`
	FailedFiles    int32                            `json:"failed_files"`
	Files          []projectSpaceImportFileResponse `json:"files"`
}

func projectSpaceImportFileToResponse(row db.ProjectSpaceImportFile) projectSpaceImportFileResponse {
	return projectSpaceImportFileResponse{
		ID:                 uuidToString(row.ID),
		RelativePath:       row.RelativePath,
		StoredRelativePath: textToPtr(row.StoredRelativePath),
		ContentType:        row.ContentType,
		SizeBytes:          row.SizeBytes,
		Sha256:             textToPtr(row.Sha256),
		Status:             row.Status,
		ErrorCode:          textToPtr(row.ErrorCode),
	}
}

func projectSpaceImportToResponse(row db.ProjectSpaceImport, files []db.ProjectSpaceImportFile) projectSpaceImportResponse {
	fileResponses := make([]projectSpaceImportFileResponse, len(files))
	for i, file := range files {
		fileResponses[i] = projectSpaceImportFileToResponse(file)
	}
	return projectSpaceImportResponse{
		ID:             uuidToString(row.ID),
		ProjectID:      uuidToString(row.ProjectID),
		BatchName:      row.BatchName,
		Status:         row.Status,
		TotalFiles:     row.TotalFiles,
		TotalBytes:     row.TotalBytes,
		CompletedFiles: row.CompletedFiles,
		FailedFiles:    row.FailedFiles,
		Files:          fileResponses,
	}
}

func (h *Handler) requireProjectSpace(w http.ResponseWriter) bool {
	if h.ProjectSpace == nil || !h.ProjectSpace.Status().Available {
		writeError(w, http.StatusServiceUnavailable, "project space is unavailable")
		return false
	}
	return true
}

func (h *Handler) ListProjectSpaceFiles(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForResource(w, r, chi.URLParam(r, "id"))
	if !ok || !h.requireProjectSpace(w) {
		return
	}
	entries, err := h.ProjectSpace.List(
		uuidToString(project.WorkspaceID),
		uuidToString(project.ID),
		r.URL.Query().Get("path"),
	)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			writeError(w, http.StatusNotFound, "project space path not found")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid project space path")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"entries": entries,
		"total":   len(entries),
	})
}

func (h *Handler) CreateProjectSpaceImport(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForResource(w, r, chi.URLParam(r, "id"))
	if !ok || !h.requireProjectSpace(w) {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var req createProjectSpaceImportRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.Files) == 0 {
		writeError(w, http.StatusBadRequest, "files are required")
		return
	}
	if len(req.Files) > projectspace.MaxImportFiles {
		writeError(w, http.StatusBadRequest, "too many files in import")
		return
	}
	batchName, err := projectspace.NormalizeBatchName(req.BatchName)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid batch_name")
		return
	}

	seen := make(map[string]struct{}, len(req.Files))
	var totalBytes int64
	normalizedPaths := make([]string, len(req.Files))
	for i, file := range req.Files {
		normalized, err := projectspace.NormalizeRelativePath(file.RelativePath)
		if err != nil {
			writeError(w, http.StatusBadRequest, "files contain an invalid relative_path")
			return
		}
		if _, exists := seen[normalized]; exists {
			writeError(w, http.StatusBadRequest, "files contain duplicate relative_path values")
			return
		}
		seen[normalized] = struct{}{}
		if file.SizeBytes < 0 || file.SizeBytes > projectspace.MaxFileBytes {
			writeError(w, http.StatusBadRequest, "file exceeds the 100 MB limit")
			return
		}
		if totalBytes > projectspace.MaxImportBytes-file.SizeBytes {
			writeError(w, http.StatusBadRequest, "import exceeds the 5 GB limit")
			return
		}
		totalBytes += file.SizeBytes
		normalizedPaths[i] = normalized
	}

	if _, err := h.ProjectSpace.EnsureProject(uuidToString(project.WorkspaceID), uuidToString(project.ID)); err != nil {
		writeError(w, http.StatusServiceUnavailable, "project space is unavailable")
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start import")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	creator, ok := h.parseUserUUIDOrZero(userID)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid user identity")
		return
	}
	importRow, err := qtx.CreateProjectSpaceImport(r.Context(), db.CreateProjectSpaceImportParams{
		WorkspaceID: project.WorkspaceID,
		ProjectID:   project.ID,
		BatchName:   batchName,
		TotalFiles:  int32(len(req.Files)),
		TotalBytes:  totalBytes,
		CreatedBy:   creator,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create import")
		return
	}
	files := make([]db.ProjectSpaceImportFile, 0, len(req.Files))
	for i, file := range req.Files {
		contentType := strings.TrimSpace(file.ContentType)
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		row, err := qtx.CreateProjectSpaceImportFile(r.Context(), db.CreateProjectSpaceImportFileParams{
			ImportID:     importRow.ID,
			WorkspaceID:  project.WorkspaceID,
			ProjectID:    project.ID,
			RelativePath: normalizedPaths[i],
			ContentType:  contentType,
			SizeBytes:    file.SizeBytes,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create import manifest")
			return
		}
		files = append(files, row)
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit import")
		return
	}
	writeJSON(w, http.StatusCreated, projectSpaceImportToResponse(importRow, files))
}

func (h *Handler) loadProjectSpaceImport(w http.ResponseWriter, r *http.Request, project db.Project) (db.ProjectSpaceImport, pgtype.UUID, bool) {
	importID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "importId"), "import id")
	if !ok {
		return db.ProjectSpaceImport{}, pgtype.UUID{}, false
	}
	row, err := h.Queries.GetProjectSpaceImport(r.Context(), db.GetProjectSpaceImportParams{
		ID: importID, WorkspaceID: project.WorkspaceID, ProjectID: project.ID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "project space import not found")
		return db.ProjectSpaceImport{}, pgtype.UUID{}, false
	}
	return row, importID, true
}

func (h *Handler) GetProjectSpaceImport(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForResource(w, r, chi.URLParam(r, "id"))
	if !ok || !h.requireProjectSpace(w) {
		return
	}
	row, importID, ok := h.loadProjectSpaceImport(w, r, project)
	if !ok {
		return
	}
	files, err := h.Queries.ListProjectSpaceImportFiles(r.Context(), db.ListProjectSpaceImportFilesParams{
		ImportID: importID, WorkspaceID: project.WorkspaceID, ProjectID: project.ID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load import")
		return
	}
	writeJSON(w, http.StatusOK, projectSpaceImportToResponse(row, files))
}

func (h *Handler) ListProjectSpaceImports(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForResource(w, r, chi.URLParam(r, "id"))
	if !ok || !h.requireProjectSpace(w) {
		return
	}
	rows, err := h.Queries.ListProjectSpaceImports(r.Context(), db.ListProjectSpaceImportsParams{
		WorkspaceID: project.WorkspaceID,
		ProjectID:   project.ID,
		Limit:       20,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list project space imports")
		return
	}
	imports := make([]projectSpaceImportResponse, 0, len(rows))
	for _, row := range rows {
		files, err := h.Queries.ListProjectSpaceImportFiles(r.Context(), db.ListProjectSpaceImportFilesParams{
			ImportID: row.ID, WorkspaceID: project.WorkspaceID, ProjectID: project.ID,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list project space imports")
			return
		}
		imports = append(imports, projectSpaceImportToResponse(row, files))
	}
	writeJSON(w, http.StatusOK, map[string]any{"imports": imports, "total": len(imports)})
}

func (h *Handler) UploadProjectSpaceImportFile(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForResource(w, r, chi.URLParam(r, "id"))
	if !ok || !h.requireProjectSpace(w) {
		return
	}
	importRow, importID, ok := h.loadProjectSpaceImport(w, r, project)
	if !ok {
		return
	}
	fileID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "fileId"), "file id")
	if !ok {
		return
	}
	fileRow, err := h.Queries.GetProjectSpaceImportFile(r.Context(), db.GetProjectSpaceImportFileParams{
		ID: fileID, ImportID: importID, WorkspaceID: project.WorkspaceID, ProjectID: project.ID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "project space import file not found")
		return
	}
	if fileRow.Status == "completed" || fileRow.Status == "skipped" {
		writeJSON(w, http.StatusOK, projectSpaceImportFileToResponse(fileRow))
		return
	}
	if err := h.Queries.MarkProjectSpaceImportUploading(r.Context(), db.MarkProjectSpaceImportUploadingParams{
		ID: importID, WorkspaceID: project.WorkspaceID, ProjectID: project.ID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update import")
		return
	}
	if err := h.Queries.MarkProjectSpaceImportFileUploading(r.Context(), db.MarkProjectSpaceImportFileUploadingParams{
		ID: fileID, ImportID: importID, WorkspaceID: project.WorkspaceID, ProjectID: project.ID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update import file")
		return
	}

	stagingPath, err := h.ProjectSpace.ImportStagingPath(uuidToString(importID), uuidToString(fileID))
	if err != nil {
		h.failProjectSpaceImportFile(r, project, importID, fileID, "staging_unavailable")
		writeError(w, http.StatusServiceUnavailable, "project import staging is unavailable")
		return
	}
	out, err := os.OpenFile(stagingPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		h.failProjectSpaceImportFile(r, project, importID, fileID, "staging_unavailable")
		writeError(w, http.StatusServiceUnavailable, "project import staging is unavailable")
		return
	}
	limit := fileRow.SizeBytes + 1
	n, copyErr := io.Copy(out, io.LimitReader(r.Body, limit))
	closeErr := out.Close()
	if copyErr != nil || closeErr != nil || n != fileRow.SizeBytes {
		_ = os.Remove(stagingPath)
		h.failProjectSpaceImportFile(r, project, importID, fileID, "size_mismatch")
		writeError(w, http.StatusBadRequest, "uploaded file size does not match manifest")
		return
	}
	hash, actualSize, err := projectspace.HashFile(stagingPath)
	if err != nil || actualSize != fileRow.SizeBytes {
		_ = os.Remove(stagingPath)
		h.failProjectSpaceImportFile(r, project, importID, fileID, "hash_failed")
		writeError(w, http.StatusInternalServerError, "failed to verify uploaded file")
		return
	}
	actualContentType, err := detectFileContentType(stagingPath)
	if err != nil {
		_ = os.Remove(stagingPath)
		h.failProjectSpaceImportFile(r, project, importID, fileID, "mime_detection_failed")
		writeError(w, http.StatusInternalServerError, "failed to inspect uploaded file")
		return
	}

	date := importRow.CreatedAt.Time.UTC().Format("2006-01-02")
	storedRel, target, err := h.ProjectSpace.ImportTarget(
		uuidToString(project.WorkspaceID),
		uuidToString(project.ID),
		date,
		importRow.BatchName,
		fileRow.RelativePath,
	)
	if err != nil {
		_ = os.Remove(stagingPath)
		h.failProjectSpaceImportFile(r, project, importID, fileID, "invalid_target")
		writeError(w, http.StatusBadRequest, "invalid project space target")
		return
	}
	status := "completed"
	createdTarget := false
	if existingHash, _, hashErr := projectspace.HashFile(target); hashErr == nil && existingHash == hash {
		status = "skipped"
	} else {
		target, _, err = projectspace.ConflictPath(target)
		if err != nil {
			_ = os.Remove(stagingPath)
			h.failProjectSpaceImportFile(r, project, importID, fileID, "target_unavailable")
			writeError(w, http.StatusInternalServerError, "failed to allocate project space target")
			return
		}
		if err := projectspace.CopyFile(stagingPath, target, 0o640); err != nil {
			_ = os.Remove(stagingPath)
			h.failProjectSpaceImportFile(r, project, importID, fileID, "finalize_failed")
			writeError(w, http.StatusInternalServerError, "failed to finalize uploaded file")
			return
		}
		createdTarget = true
		root, _ := h.ProjectSpace.ProjectDir(uuidToString(project.WorkspaceID), uuidToString(project.ID))
		if relative, relErr := filepath.Rel(root, target); relErr == nil {
			storedRel = filepath.ToSlash(relative)
		}
	}
	_ = os.Remove(stagingPath)

	if err := h.Queries.CompleteProjectSpaceImportFile(r.Context(), db.CompleteProjectSpaceImportFileParams{
		ID: fileID, ImportID: importID, WorkspaceID: project.WorkspaceID, ProjectID: project.ID,
		Status:             status,
		StoredRelativePath: pgtype.Text{String: storedRel, Valid: true},
		Sha256:             pgtype.Text{String: hash, Valid: true},
		ContentType:        actualContentType,
	}); err != nil {
		if createdTarget {
			_ = os.Remove(target)
		}
		writeError(w, http.StatusInternalServerError, "failed to record uploaded file")
		return
	}
	_, _ = h.Queries.RefreshProjectSpaceImportCounts(r.Context(), db.RefreshProjectSpaceImportCountsParams{
		ID: importID, WorkspaceID: project.WorkspaceID, ProjectID: project.ID,
	})
	updated, _ := h.Queries.GetProjectSpaceImportFile(r.Context(), db.GetProjectSpaceImportFileParams{
		ID: fileID, ImportID: importID, WorkspaceID: project.WorkspaceID, ProjectID: project.ID,
	})
	writeJSON(w, http.StatusOK, projectSpaceImportFileToResponse(updated))
}

func detectFileContentType(filename string) (string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return "", err
	}
	defer file.Close()
	reader := bufio.NewReader(file)
	header, err := reader.Peek(512)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, bufio.ErrBufferFull) {
		return "", err
	}
	return http.DetectContentType(header), nil
}

func (h *Handler) failProjectSpaceImportFile(r *http.Request, project db.Project, importID, fileID pgtype.UUID, code string) {
	if err := h.Queries.FailProjectSpaceImportFile(r.Context(), db.FailProjectSpaceImportFileParams{
		ID: fileID, ImportID: importID, WorkspaceID: project.WorkspaceID, ProjectID: project.ID,
		ErrorCode: pgtype.Text{String: code, Valid: true},
	}); err != nil {
		slog.Warn("failed to record project import error", "error", err)
	}
	_, _ = h.Queries.RefreshProjectSpaceImportCounts(r.Context(), db.RefreshProjectSpaceImportCountsParams{
		ID: importID, WorkspaceID: project.WorkspaceID, ProjectID: project.ID,
	})
}

func (h *Handler) CompleteProjectSpaceImport(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForResource(w, r, chi.URLParam(r, "id"))
	if !ok || !h.requireProjectSpace(w) {
		return
	}
	_, importID, ok := h.loadProjectSpaceImport(w, r, project)
	if !ok {
		return
	}
	row, err := h.Queries.RefreshProjectSpaceImportCounts(r.Context(), db.RefreshProjectSpaceImportCountsParams{
		ID: importID, WorkspaceID: project.WorkspaceID, ProjectID: project.ID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to finalize import")
		return
	}
	files, err := h.Queries.ListProjectSpaceImportFiles(r.Context(), db.ListProjectSpaceImportFilesParams{
		ImportID: importID, WorkspaceID: project.WorkspaceID, ProjectID: project.ID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load finalized import")
		return
	}
	writeJSON(w, http.StatusOK, projectSpaceImportToResponse(row, files))
}

type saveAttachmentsToProjectSpaceRequest struct {
	AttachmentIDs []string `json:"attachment_ids"`
	TargetPath    string   `json:"target_path"`
}

type organizeProjectSpaceRequest struct {
	AgentID string `json:"agent_id"`
}

// OrganizeProjectSpace adapts the existing durable agent quick-create flow to
// a project-space-specific task. Uploading files never starts a model by
// itself; the user explicitly invokes this endpoint after reviewing the batch.
func (h *Handler) OrganizeProjectSpace(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForResource(w, r, chi.URLParam(r, "id"))
	if !ok || !h.requireProjectSpace(w) {
		return
	}
	var req organizeProjectSpaceRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	agentID := strings.TrimSpace(req.AgentID)
	if agentID == "" && project.LeadType.Valid && project.LeadType.String == "agent" && project.LeadID.Valid {
		agentID = uuidToString(project.LeadID)
	}
	if agentID == "" {
		writeError(w, http.StatusBadRequest, "agent_id is required when the project lead is not an agent")
		return
	}
	body, err := json.Marshal(QuickCreateIssueRequest{
		AgentID:   agentID,
		ProjectID: uuidToString(project.ID),
		Prompt:    "Review the files in $MULTICA_PROJECT_SPACE. Preserve original uploads, organize durable knowledge under knowledge/, update index.md with relative source links, and record unsupported or ambiguous files without deleting them.",
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create organization task")
		return
	}
	r.Body = io.NopCloser(strings.NewReader(string(body)))
	h.QuickCreateIssue(w, r)
}

func (h *Handler) SaveAttachmentsToProjectSpace(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForResource(w, r, chi.URLParam(r, "id"))
	if !ok || !h.requireProjectSpace(w) {
		return
	}
	var req saveAttachmentsToProjectSpaceRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.AttachmentIDs) == 0 || len(req.AttachmentIDs) > 20 {
		writeError(w, http.StatusBadRequest, "attachment_ids must contain between 1 and 20 items")
		return
	}
	if h.Storage == nil {
		writeError(w, http.StatusServiceUnavailable, "attachment storage is unavailable")
		return
	}
	targetPrefix := strings.TrimSpace(req.TargetPath)
	if targetPrefix == "" {
		targetPrefix = path.Join("inbox/uploads", time.Now().UTC().Format("2006-01-02"), "chat")
	} else {
		var err error
		targetPrefix, err = projectspace.NormalizeRelativePath(targetPrefix)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid target_path")
			return
		}
	}

	saved := make([]string, 0, len(req.AttachmentIDs))
	for _, rawID := range req.AttachmentIDs {
		attachmentID, ok := parseUUIDOrBadRequest(w, rawID, "attachment id")
		if !ok {
			return
		}
		attachment, err := h.Queries.GetAttachment(r.Context(), db.GetAttachmentParams{
			ID: attachmentID, WorkspaceID: project.WorkspaceID,
		})
		if err != nil {
			writeError(w, http.StatusNotFound, "attachment not found")
			return
		}
		key := h.Storage.KeyFromURL(attachment.Url)
		if key == "" {
			writeError(w, http.StatusBadRequest, "attachment storage is unavailable")
			return
		}
		reader, err := h.Storage.GetReader(r.Context(), key)
		if err != nil {
			writeError(w, http.StatusBadGateway, "failed to read attachment")
			return
		}
		temp, err := os.CreateTemp(h.ProjectSpace.StagingRoot(), ".attachment-*")
		if err != nil {
			_ = reader.Close()
			writeError(w, http.StatusServiceUnavailable, "project import staging is unavailable")
			return
		}
		tempPath := temp.Name()
		n, copyErr := io.Copy(temp, io.LimitReader(reader, projectspace.MaxFileBytes+1))
		closeReaderErr := reader.Close()
		closeTempErr := temp.Close()
		if copyErr != nil || closeReaderErr != nil || closeTempErr != nil || n > projectspace.MaxFileBytes {
			_ = os.Remove(tempPath)
			writeError(w, http.StatusBadRequest, "attachment exceeds project space limits")
			return
		}
		filename := filepath.Base(strings.TrimSpace(attachment.Filename))
		if filename == "." || filename == "" {
			filename = uuidToString(attachment.ID)
		}
		relative, err := projectspace.NormalizeRelativePath(path.Join(targetPrefix, filename))
		if err != nil {
			_ = os.Remove(tempPath)
			writeError(w, http.StatusBadRequest, "attachment filename is invalid")
			return
		}
		target, err := h.ProjectSpace.ResolveProjectPath(
			uuidToString(project.WorkspaceID),
			uuidToString(project.ID),
			relative,
		)
		if err != nil {
			_ = os.Remove(tempPath)
			writeError(w, http.StatusBadRequest, "attachment target is invalid")
			return
		}
		hash, _, err := projectspace.HashFile(tempPath)
		if err != nil {
			_ = os.Remove(tempPath)
			writeError(w, http.StatusInternalServerError, "failed to verify attachment")
			return
		}
		if existingHash, _, hashErr := projectspace.HashFile(target); hashErr == nil && existingHash == hash {
			_ = os.Remove(tempPath)
			saved = append(saved, relative)
			continue
		}
		target, _, err = projectspace.ConflictPath(target)
		if err != nil || projectspace.CopyFile(tempPath, target, 0o640) != nil {
			_ = os.Remove(tempPath)
			writeError(w, http.StatusInternalServerError, "failed to save attachment")
			return
		}
		_ = os.Remove(tempPath)
		root, _ := h.ProjectSpace.ProjectDir(uuidToString(project.WorkspaceID), uuidToString(project.ID))
		storedRelative, err := filepath.Rel(root, target)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to record saved attachment")
			return
		}
		saved = append(saved, filepath.ToSlash(storedRelative))
	}
	writeJSON(w, http.StatusCreated, map[string]any{"saved_paths": saved})
}
