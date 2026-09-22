package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/knowledge"
	"github.com/multica-ai/multica/server/internal/storage"
)

func knowledgePrivateAccessAllowed(actorSource string) bool {
	return actorSource != "task_token" && actorSource != "cloud_pat"
}

func setKnowledgeContentHeaders(w http.ResponseWriter, filename string) {
	w.Header().Set("Content-Disposition", storage.AttachmentContentDisposition(filename))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, no-store")
}

func (h *Handler) writeKnowledgeError(w http.ResponseWriter, r *http.Request, err error) {
	status, code, message, retryable := http.StatusInternalServerError, "knowledge_internal_error", "knowledge request failed", false
	var typed *knowledge.Error
	if errors.As(err, &typed) {
		if typed.Status > 0 {
			status = typed.Status
		}
		if typed.Code != "" {
			code = typed.Code
		}
		if typed.Message != "" && status < http.StatusInternalServerError {
			message = typed.Message
		}
		retryable = typed.Retryable
	}
	requestID := strings.TrimSpace(r.Header.Get("X-Request-ID"))
	payload := map[string]any{"error": message, "code": code, "retryable": retryable}
	if requestID != "" {
		payload["request_id"] = requestID
	}
	writeJSON(w, status, payload)
}

func (h *Handler) knowledgeActor(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	workspaceID := h.resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return "", "", false
	}
	if h.Knowledge == nil {
		h.writeKnowledgeError(w, r, fmt.Errorf("knowledge service unavailable"))
		return "", "", false
	}
	actorSource := r.Header.Get("X-Actor-Source")
	privateAccess := knowledgePrivateAccessAllowed(actorSource)
	if actorSource == "task_token" {
		var subjectUserID string
		var subjectOK bool
		subjectUserID, privateAccess, subjectOK = h.resolveTaskTokenKnowledgeSubject(r, workspaceID, userID)
		if !subjectOK {
			writeJSON(w, http.StatusForbidden, map[string]any{
				"error":     "knowledge access denied",
				"code":      "knowledge_forbidden",
				"retryable": false,
			})
			return "", "", false
		}
		userID = subjectUserID
	}
	*r = *r.WithContext(knowledge.WithSubject(r.Context(), knowledge.Subject{TaskToken: actorSource == "task_token", PrivateAccess: privateAccess}))
	return workspaceID, userID, true
}

type knowledgeProviderRequest struct {
	Name     string `json:"name"`
	Preset   string `json:"preset"`
	Protocol string `json:"protocol"`
	BaseURL  string `json:"base_url"`
	APIKey   string `json:"api_key"`
}

func (h *Handler) ListKnowledgeProviders(w http.ResponseWriter, r *http.Request) {
	workspaceID, _, ok := h.knowledgeActor(w, r)
	if !ok {
		return
	}
	member, memberOK := h.workspaceMember(w, r, workspaceID)
	if !memberOK {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	providerPage, err := h.Knowledge.ListProvidersPage(r.Context(), workspaceID, r.URL.Query().Get("cursor"), limit)
	if err != nil {
		h.writeKnowledgeError(w, r, err)
		return
	}
	providers := providerPage.Providers
	if member.Role != "owner" && member.Role != "admin" {
		for index := range providers {
			providers[index].BaseURL = ""
		}
	}
	providerPage.Providers = providers
	writeJSON(w, http.StatusOK, providerPage)
}

func (h *Handler) CreateKnowledgeProvider(w http.ResponseWriter, r *http.Request) {
	workspaceID, userID, ok := h.knowledgeActor(w, r)
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return
	}
	var request knowledgeProviderRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		h.writeKnowledgeError(w, r, fmt.Errorf("invalid provider request: %w", err))
		return
	}
	provider, err := h.Knowledge.CreateProvider(r.Context(), workspaceID, knowledge.CreateProviderInput{Name: request.Name, Preset: request.Preset, Protocol: request.Protocol, BaseURL: request.BaseURL, APIKey: request.APIKey, CreatedBy: userID})
	if err != nil {
		h.writeKnowledgeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, provider)
}

type knowledgeProviderPatch struct {
	Name             *string `json:"name"`
	APIKey           *string `json:"api_key"`
	IsEnabled        *bool   `json:"is_enabled"`
	BaseURL          *string `json:"base_url"`
	Protocol         *string `json:"protocol"`
	ExpectedRevision int64   `json:"expected_revision"`
}

func (h *Handler) UpdateKnowledgeProvider(w http.ResponseWriter, r *http.Request) {
	workspaceID, _, ok := h.knowledgeActor(w, r)
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return
	}
	var request knowledgeProviderPatch
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		h.writeKnowledgeError(w, r, err)
		return
	}
	providerID := chi.URLParam(r, "providerId")
	provider, err := h.Knowledge.UpdateProvider(r.Context(), workspaceID, providerID, knowledge.UpdateProviderInput{Name: request.Name, APIKey: request.APIKey, IsEnabled: request.IsEnabled, BaseURL: request.BaseURL, Protocol: request.Protocol, ExpectedRevision: request.ExpectedRevision})
	if err != nil {
		h.writeKnowledgeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, provider)
}

func (h *Handler) DeleteKnowledgeProvider(w http.ResponseWriter, r *http.Request) {
	workspaceID, _, ok := h.knowledgeActor(w, r)
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return
	}
	if err := h.Knowledge.DeleteProvider(r.Context(), workspaceID, chi.URLParam(r, "providerId")); err != nil {
		h.writeKnowledgeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type knowledgeProviderTestRequest struct {
	ProviderID string `json:"provider_id"`
	BaseURL    string `json:"base_url"`
	Protocol   string `json:"protocol"`
	APIKey     string `json:"api_key"`
	Model      string `json:"model"`
	Capability string `json:"capability"`
}

func (h *Handler) TestKnowledgeProvider(w http.ResponseWriter, r *http.Request) {
	workspaceID, _, ok := h.knowledgeActor(w, r)
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return
	}
	var request knowledgeProviderTestRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		h.writeKnowledgeError(w, r, err)
		return
	}
	result, err := h.Knowledge.TestProvider(r.Context(), workspaceID, knowledge.ProviderTestInput{ProviderID: request.ProviderID, BaseURL: request.BaseURL, Protocol: request.Protocol, APIKey: request.APIKey, Model: request.Model, Capability: request.Capability})
	if err != nil {
		h.writeKnowledgeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) ListKnowledgeProviderModels(w http.ResponseWriter, r *http.Request) {
	workspaceID, _, ok := h.knowledgeActor(w, r)
	if !ok {
		return
	}
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	models, err := h.Knowledge.ListProviderModels(r.Context(), workspaceID, chi.URLParam(r, "providerId"))
	if err != nil {
		h.writeKnowledgeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"models": models})
}

type knowledgeBindingRequest struct {
	Mode       string         `json:"mode"`
	ProviderID string         `json:"provider_id"`
	Model      string         `json:"model"`
	Options    map[string]any `json:"options"`
}
type knowledgeModelSettingsRequest struct {
	ExpectedRevision int64                              `json:"expected_revision"`
	Main             *knowledgeBindingRequest           `json:"main"`
	Purposes         map[string]knowledgeBindingRequest `json:"purposes"`
}

func bindingInput(value knowledgeBindingRequest) knowledge.BindingInput {
	return knowledge.BindingInput{Mode: value.Mode, ProviderID: value.ProviderID, Model: value.Model, Options: value.Options}
}
func (h *Handler) modelSettingsInput(request knowledgeModelSettingsRequest) knowledge.ModelSettingsInput {
	result := knowledge.ModelSettingsInput{ExpectedRevision: request.ExpectedRevision, Purposes: map[string]knowledge.BindingInput{}}
	if request.Main != nil {
		value := bindingInput(*request.Main)
		result.Main = &value
	}
	for purpose, value := range request.Purposes {
		result.Purposes[purpose] = bindingInput(value)
	}
	return result
}

func (h *Handler) GetKnowledgeModelSettings(w http.ResponseWriter, r *http.Request) {
	workspaceID, _, ok := h.knowledgeActor(w, r)
	if !ok {
		return
	}
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	settings, err := h.Knowledge.GetModelSettings(r.Context(), workspaceID)
	if err != nil {
		h.writeKnowledgeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, settings)
}
func (h *Handler) PutKnowledgeModelSettings(w http.ResponseWriter, r *http.Request) {
	workspaceID, userID, ok := h.knowledgeActor(w, r)
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return
	}
	var request knowledgeModelSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		h.writeKnowledgeError(w, r, err)
		return
	}
	settings, err := h.Knowledge.PutModelSettings(r.Context(), workspaceID, userID, h.modelSettingsInput(request))
	if err != nil {
		h.writeKnowledgeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (h *Handler) GetKnowledgeBaseModelSettings(w http.ResponseWriter, r *http.Request) {
	workspaceID, userID, ok := h.knowledgeActor(w, r)
	if !ok {
		return
	}
	settings, err := h.Knowledge.GetBaseModelSettings(r.Context(), workspaceID, userID, chi.URLParam(r, "baseId"))
	if err != nil {
		h.writeKnowledgeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (h *Handler) PutKnowledgeBaseModelSettings(w http.ResponseWriter, r *http.Request) {
	workspaceID, userID, ok := h.knowledgeActor(w, r)
	if !ok {
		return
	}
	var request knowledgeModelSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		h.writeKnowledgeError(w, r, err)
		return
	}
	settings, err := h.Knowledge.PutBaseModelSettings(r.Context(), workspaceID, userID, chi.URLParam(r, "baseId"), h.modelSettingsInput(request))
	if err != nil {
		h.writeKnowledgeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

type knowledgeBaseRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Visibility  string `json:"visibility"`
}
type knowledgeBasePatch struct {
	Name             *string `json:"name"`
	Description      *string `json:"description"`
	Visibility       *string `json:"visibility"`
	ExpectedRevision int64   `json:"expected_revision"`
}

func (h *Handler) ListKnowledgeBases(w http.ResponseWriter, r *http.Request) {
	workspaceID, userID, ok := h.knowledgeActor(w, r)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	bases, err := h.Knowledge.ListBasesPage(r.Context(), workspaceID, userID, r.URL.Query().Get("cursor"), limit)
	if err != nil {
		h.writeKnowledgeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, bases)
}
func (h *Handler) CreateKnowledgeBase(w http.ResponseWriter, r *http.Request) {
	workspaceID, userID, ok := h.knowledgeActor(w, r)
	if !ok {
		return
	}
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	var request knowledgeBaseRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		h.writeKnowledgeError(w, r, err)
		return
	}
	base, err := h.Knowledge.CreateBase(r.Context(), workspaceID, userID, knowledge.CreateBaseInput{Name: request.Name, Description: request.Description, Visibility: request.Visibility})
	if err != nil {
		h.writeKnowledgeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, base)
}
func (h *Handler) GetKnowledgeBase(w http.ResponseWriter, r *http.Request) {
	workspaceID, userID, ok := h.knowledgeActor(w, r)
	if !ok {
		return
	}
	base, err := h.Knowledge.GetBase(r.Context(), workspaceID, userID, chi.URLParam(r, "baseId"))
	if err != nil {
		h.writeKnowledgeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, base)
}
func (h *Handler) UpdateKnowledgeBase(w http.ResponseWriter, r *http.Request) {
	workspaceID, userID, ok := h.knowledgeActor(w, r)
	if !ok {
		return
	}
	var request knowledgeBasePatch
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		h.writeKnowledgeError(w, r, err)
		return
	}
	base, err := h.Knowledge.UpdateBase(r.Context(), workspaceID, userID, chi.URLParam(r, "baseId"), knowledge.UpdateBaseInput{Name: request.Name, Description: request.Description, Visibility: request.Visibility, ExpectedRevision: request.ExpectedRevision})
	if err != nil {
		h.writeKnowledgeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, base)
}
func (h *Handler) DeleteKnowledgeBase(w http.ResponseWriter, r *http.Request) {
	workspaceID, userID, ok := h.knowledgeActor(w, r)
	if !ok {
		return
	}
	revision, _ := strconv.ParseInt(r.URL.Query().Get("expected_revision"), 10, 64)
	if err := h.Knowledge.DeleteBase(r.Context(), workspaceID, userID, chi.URLParam(r, "baseId"), revision); err != nil {
		h.writeKnowledgeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ListKnowledgeDocuments(w http.ResponseWriter, r *http.Request) {
	workspaceID, userID, ok := h.knowledgeActor(w, r)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	documents, err := h.Knowledge.ListDocumentsPage(r.Context(), workspaceID, userID, chi.URLParam(r, "baseId"), r.URL.Query().Get("cursor"), limit)
	if err != nil {
		h.writeKnowledgeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, documents)
}
func (h *Handler) GetKnowledgeDocument(w http.ResponseWriter, r *http.Request) {
	workspaceID, userID, ok := h.knowledgeActor(w, r)
	if !ok {
		return
	}
	document, err := h.Knowledge.GetDocument(r.Context(), workspaceID, userID, chi.URLParam(r, "baseId"), chi.URLParam(r, "documentId"))
	if err != nil {
		h.writeKnowledgeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, document)
}

func readKnowledgeUpload(w http.ResponseWriter, r *http.Request, maxBytes int64) (knowledge.CreateFileInput, error) {
	if maxBytes <= 0 {
		maxBytes = 100 << 20
	}
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		return knowledge.CreateFileInput{}, err
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		return knowledge.CreateFileInput{}, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return knowledge.CreateFileInput{}, err
	}
	if int64(len(data)) > maxBytes {
		return knowledge.CreateFileInput{}, fmt.Errorf("file exceeds upload limit")
	}
	return knowledge.CreateFileInput{Filename: header.Filename, ContentType: header.Header.Get("Content-Type"), Bytes: data, Title: r.FormValue("title"), Tags: splitKnowledgeTags(r.FormValue("tags"))}, nil
}
func splitKnowledgeTags(raw string) []string {
	parts := strings.Split(raw, ",")
	result := []string{}
	for _, part := range parts {
		if value := strings.TrimSpace(part); value != "" {
			result = append(result, value)
		}
	}
	return result
}

func (h *Handler) CreateKnowledgeFile(w http.ResponseWriter, r *http.Request) {
	workspaceID, userID, ok := h.knowledgeActor(w, r)
	if !ok {
		return
	}
	input, err := readKnowledgeUpload(w, r, h.cfg.KnowledgeMaxUploadBytes)
	if err != nil {
		h.writeKnowledgeError(w, r, err)
		return
	}
	result, err := h.Knowledge.CreateFile(r.Context(), workspaceID, userID, chi.URLParam(r, "baseId"), input)
	if err != nil {
		h.writeKnowledgeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, result)
}

type knowledgeURLRequest struct {
	URL              string         `json:"url"`
	Title            string         `json:"title"`
	Tags             []string       `json:"tags"`
	Metadata         map[string]any `json:"metadata"`
	ExpectedRevision int64          `json:"expected_revision"`
}

func (h *Handler) CreateKnowledgeURL(w http.ResponseWriter, r *http.Request) {
	workspaceID, userID, ok := h.knowledgeActor(w, r)
	if !ok {
		return
	}
	var request knowledgeURLRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		h.writeKnowledgeError(w, r, err)
		return
	}
	result, err := h.Knowledge.CreateURL(r.Context(), workspaceID, userID, chi.URLParam(r, "baseId"), knowledge.CreateURLInput{URL: request.URL, Title: request.Title, Tags: request.Tags, Metadata: request.Metadata})
	if err != nil {
		h.writeKnowledgeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, result)
}

type knowledgeDocumentPatch struct {
	Title            *string  `json:"title"`
	Tags             []string `json:"tags"`
	ExpectedRevision int64    `json:"expected_revision"`
}

func (h *Handler) UpdateKnowledgeDocument(w http.ResponseWriter, r *http.Request) {
	workspaceID, userID, ok := h.knowledgeActor(w, r)
	if !ok {
		return
	}
	var request knowledgeDocumentPatch
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		h.writeKnowledgeError(w, r, err)
		return
	}
	document, err := h.Knowledge.UpdateDocument(r.Context(), workspaceID, userID, chi.URLParam(r, "baseId"), chi.URLParam(r, "documentId"), request.Title, request.Tags, request.ExpectedRevision)
	if err != nil {
		h.writeKnowledgeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, document)
}
func (h *Handler) DeleteKnowledgeDocument(w http.ResponseWriter, r *http.Request) {
	workspaceID, userID, ok := h.knowledgeActor(w, r)
	if !ok {
		return
	}
	revision, _ := strconv.ParseInt(r.URL.Query().Get("expected_revision"), 10, 64)
	if err := h.Knowledge.DeleteDocument(r.Context(), workspaceID, userID, chi.URLParam(r, "baseId"), chi.URLParam(r, "documentId"), revision); err != nil {
		h.writeKnowledgeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (h *Handler) ListKnowledgeVersions(w http.ResponseWriter, r *http.Request) {
	workspaceID, userID, ok := h.knowledgeActor(w, r)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	versions, err := h.Knowledge.ListVersionsPage(r.Context(), workspaceID, userID, chi.URLParam(r, "baseId"), chi.URLParam(r, "documentId"), r.URL.Query().Get("cursor"), limit)
	if err != nil {
		h.writeKnowledgeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, versions)
}

func (h *Handler) ReplaceKnowledgeVersion(w http.ResponseWriter, r *http.Request) {
	workspaceID, userID, ok := h.knowledgeActor(w, r)
	if !ok {
		return
	}
	input := knowledge.ReplaceVersionInput{}
	if strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "multipart/") {
		file, err := readKnowledgeUpload(w, r, h.cfg.KnowledgeMaxUploadBytes)
		if err != nil {
			h.writeKnowledgeError(w, r, err)
			return
		}
		input.Filename = file.Filename
		input.ContentType = file.ContentType
		input.Bytes = file.Bytes
		input.ExpectedRevision, _ = strconv.ParseInt(r.FormValue("expected_revision"), 10, 64)
	} else {
		var request knowledgeURLRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			h.writeKnowledgeError(w, r, err)
			return
		}
		input.URL = request.URL
		input.Metadata = request.Metadata
		input.ExpectedRevision = request.ExpectedRevision
	}
	result, err := h.Knowledge.ReplaceVersion(r.Context(), workspaceID, userID, chi.URLParam(r, "baseId"), chi.URLParam(r, "documentId"), input)
	if err != nil {
		h.writeKnowledgeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, result)
}

type knowledgeReprocessRequest struct {
	Stage string `json:"stage"`
}

func (h *Handler) ReprocessKnowledgeDocument(w http.ResponseWriter, r *http.Request) {
	workspaceID, userID, ok := h.knowledgeActor(w, r)
	if !ok {
		return
	}
	var request knowledgeReprocessRequest
	_ = json.NewDecoder(r.Body).Decode(&request)
	job, err := h.Knowledge.Reprocess(r.Context(), workspaceID, userID, chi.URLParam(r, "baseId"), chi.URLParam(r, "documentId"), request.Stage)
	if err != nil {
		h.writeKnowledgeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, job)
}

func (h *Handler) ConfirmKnowledgeBlocks(w http.ResponseWriter, r *http.Request) {
	workspaceID, userID, ok := h.knowledgeActor(w, r)
	if !ok {
		return
	}
	var request knowledge.ConfirmBlocksInput
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		h.writeKnowledgeError(w, r, err)
		return
	}
	result, err := h.Knowledge.ConfirmBlocks(r.Context(), workspaceID, userID, chi.URLParam(r, "baseId"), chi.URLParam(r, "documentId"), request)
	if err != nil {
		h.writeKnowledgeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, result)
}

func (h *Handler) GetKnowledgeVersionContent(w http.ResponseWriter, r *http.Request) {
	workspaceID, userID, ok := h.knowledgeActor(w, r)
	if !ok {
		return
	}
	reader, mimeType, filename, err := h.Knowledge.OpenVersion(r.Context(), workspaceID, userID, chi.URLParam(r, "baseId"), chi.URLParam(r, "documentId"), chi.URLParam(r, "versionId"))
	if err != nil {
		h.writeKnowledgeError(w, r, err)
		return
	}
	defer reader.Close()
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", mimeType)
	setKnowledgeContentHeaders(w, filename)
	_, _ = io.Copy(w, reader)
}

func (h *Handler) ListKnowledgeVersionBlocks(w http.ResponseWriter, r *http.Request) {
	workspaceID, userID, ok := h.knowledgeActor(w, r)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	blocks, err := h.Knowledge.ListVersionBlocks(r.Context(), workspaceID, userID, chi.URLParam(r, "baseId"), chi.URLParam(r, "documentId"), chi.URLParam(r, "versionId"), r.URL.Query().Get("cursor"), limit)
	if err != nil {
		h.writeKnowledgeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, blocks)
}

func (h *Handler) IssueKnowledgePreviewCapability(w http.ResponseWriter, r *http.Request) {
	workspaceID, userID, ok := h.knowledgeActor(w, r)
	if !ok {
		return
	}
	capability, err := h.Knowledge.IssuePreviewCapability(r.Context(), workspaceID, userID, chi.URLParam(r, "baseId"), chi.URLParam(r, "documentId"), chi.URLParam(r, "versionId"))
	if err != nil {
		h.writeKnowledgeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, capability)
}

func (h *Handler) RedeemKnowledgePreview(w http.ResponseWriter, r *http.Request) {
	if h.Knowledge == nil {
		h.writeKnowledgeError(w, r, fmt.Errorf("knowledge service unavailable"))
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	reader, mimeType, filename, err := h.Knowledge.RedeemPreview(r.Context(), userID, chi.URLParam(r, "versionId"), r.URL.Query().Get("capability"))
	if err != nil {
		h.writeKnowledgeError(w, r, err)
		return
	}
	defer reader.Close()
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", mimeType)
	setKnowledgeContentHeaders(w, filename)
	_, _ = io.Copy(w, reader)
}

func (h *Handler) GetKnowledgeChunk(w http.ResponseWriter, r *http.Request) {
	workspaceID, userID, ok := h.knowledgeActor(w, r)
	if !ok {
		return
	}
	chunk, err := h.Knowledge.GetChunk(r.Context(), workspaceID, userID, chi.URLParam(r, "baseId"), chi.URLParam(r, "chunkId"))
	if err != nil {
		h.writeKnowledgeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, chunk)
}
func (h *Handler) ListKnowledgeJobs(w http.ResponseWriter, r *http.Request) {
	workspaceID, userID, ok := h.knowledgeActor(w, r)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	jobs, err := h.Knowledge.ListJobsPage(r.Context(), workspaceID, userID, chi.URLParam(r, "baseId"), r.URL.Query().Get("cursor"), limit)
	if err != nil {
		h.writeKnowledgeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, jobs)
}
func (h *Handler) CancelKnowledgeJob(w http.ResponseWriter, r *http.Request) {
	workspaceID, userID, ok := h.knowledgeActor(w, r)
	if !ok {
		return
	}
	if err := h.Knowledge.CancelJob(r.Context(), workspaceID, userID, chi.URLParam(r, "baseId"), chi.URLParam(r, "jobId")); err != nil {
		h.writeKnowledgeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (h *Handler) CreateKnowledgeIndex(w http.ResponseWriter, r *http.Request) {
	workspaceID, userID, ok := h.knowledgeActor(w, r)
	if !ok {
		return
	}
	result, err := h.Knowledge.CreateIndex(r.Context(), workspaceID, userID, chi.URLParam(r, "baseId"))
	if err != nil {
		h.writeKnowledgeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, result)
}

func (h *Handler) SearchKnowledge(w http.ResponseWriter, r *http.Request) {
	workspaceID, userID, ok := h.knowledgeActor(w, r)
	if !ok {
		return
	}
	var input knowledge.SearchInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		h.writeKnowledgeError(w, r, err)
		return
	}
	response, err := h.Knowledge.Search(r.Context(), workspaceID, userID, chi.URLParam(r, "baseId"), input)
	if err != nil {
		h.writeKnowledgeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

type knowledgeAnswerRequest struct {
	Question string                 `json:"question"`
	Query    string                 `json:"query"`
	Limit    int                    `json:"limit"`
	Mode     string                 `json:"mode"`
	Filters  knowledge.SearchFilter `json:"filters"`
}

func (h *Handler) AnswerKnowledge(w http.ResponseWriter, r *http.Request) {
	workspaceID, userID, ok := h.knowledgeActor(w, r)
	if !ok {
		return
	}
	var request knowledgeAnswerRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		h.writeKnowledgeError(w, r, err)
		return
	}
	question := request.Question
	if question == "" {
		question = request.Query
	}
	response, err := h.Knowledge.Answer(r.Context(), workspaceID, userID, chi.URLParam(r, "baseId"), knowledge.AnswerInput{Question: question, Search: knowledge.SearchInput{Query: request.Query, Limit: request.Limit, Mode: request.Mode, Filter: request.Filters}})
	if err != nil {
		h.writeKnowledgeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) ListKnowledgeEntities(w http.ResponseWriter, r *http.Request) {
	workspaceID, userID, ok := h.knowledgeActor(w, r)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	entities, err := h.Knowledge.ListEntitiesPage(r.Context(), workspaceID, userID, chi.URLParam(r, "baseId"), r.URL.Query().Get("query"), r.URL.Query().Get("type"), r.URL.Query().Get("cursor"), limit)
	if err != nil {
		h.writeKnowledgeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, entities)
}
func (h *Handler) GetKnowledgeEntity(w http.ResponseWriter, r *http.Request) {
	workspaceID, userID, ok := h.knowledgeActor(w, r)
	if !ok {
		return
	}
	entity, err := h.Knowledge.GetEntity(r.Context(), workspaceID, userID, chi.URLParam(r, "baseId"), chi.URLParam(r, "entityId"))
	if err != nil {
		h.writeKnowledgeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, entity)
}
func (h *Handler) GetKnowledgeGraph(w http.ResponseWriter, r *http.Request) {
	workspaceID, userID, ok := h.knowledgeActor(w, r)
	if !ok {
		return
	}
	depth, _ := strconv.Atoi(r.URL.Query().Get("depth"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("node_limit"))
	graph, err := h.Knowledge.Graph(r.Context(), workspaceID, userID, chi.URLParam(r, "baseId"), knowledge.GraphQuery{EntityID: r.URL.Query().Get("entity_id"), Depth: depth, Predicate: r.URL.Query().Get("predicate"), DocumentID: r.URL.Query().Get("document_id"), NodeLimit: limit})
	if err != nil {
		h.writeKnowledgeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, graph)
}
func (h *Handler) GetKnowledgeRelationEvidence(w http.ResponseWriter, r *http.Request) {
	workspaceID, userID, ok := h.knowledgeActor(w, r)
	if !ok {
		return
	}
	evidence, err := h.Knowledge.RelationEvidence(r.Context(), workspaceID, userID, chi.URLParam(r, "baseId"), chi.URLParam(r, "relationId"))
	if err != nil {
		h.writeKnowledgeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"evidence": evidence})
}

type knowledgeGraphEditRequest struct {
	Operation        string         `json:"operation"`
	TargetID         string         `json:"target_id"`
	Payload          map[string]any `json:"payload"`
	ExpectedRevision int64          `json:"expected_revision"`
}

func (h *Handler) ApplyKnowledgeGraphEdit(w http.ResponseWriter, r *http.Request) {
	workspaceID, userID, ok := h.knowledgeActor(w, r)
	if !ok {
		return
	}
	var request knowledgeGraphEditRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		h.writeKnowledgeError(w, r, err)
		return
	}
	edit, err := h.Knowledge.ApplyGraphEdit(r.Context(), workspaceID, userID, chi.URLParam(r, "baseId"), knowledge.GraphEditInput{Operation: request.Operation, TargetID: request.TargetID, Payload: request.Payload, ExpectedRevision: request.ExpectedRevision})
	if err != nil {
		h.writeKnowledgeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, edit)
}
func (h *Handler) ListKnowledgeGraphEdits(w http.ResponseWriter, r *http.Request) {
	workspaceID, userID, ok := h.knowledgeActor(w, r)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	edits, err := h.Knowledge.ListGraphEditsPage(r.Context(), workspaceID, userID, chi.URLParam(r, "baseId"), r.URL.Query().Get("cursor"), limit)
	if err != nil {
		h.writeKnowledgeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, edits)
}
func (h *Handler) RevertKnowledgeGraphEdit(w http.ResponseWriter, r *http.Request) {
	workspaceID, userID, ok := h.knowledgeActor(w, r)
	if !ok {
		return
	}
	edit, err := h.Knowledge.RevertGraphEdit(r.Context(), workspaceID, userID, chi.URLParam(r, "baseId"), chi.URLParam(r, "editId"))
	if err != nil {
		h.writeKnowledgeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, edit)
}
