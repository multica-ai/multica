package handler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/knowledge"
)

const maxKnowledgeIdempotencyRequestBytes = 128 << 20

// KnowledgeIdempotency enforces replay semantics for knowledge mutations. It
// is deliberately attached only to the independent knowledge router, so the
// existing product APIs keep their historical contracts.
func (h *Handler) KnowledgeIdempotency(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !knowledgePostRequiresIdempotency(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		if key == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Idempotency-Key is required for this knowledge operation", "code": "idempotency_key_required"})
			return
		}
		workspaceID := h.resolveWorkspaceID(r)
		userID := requestUserID(r)
		if workspaceID == "" || userID == "" || h.Knowledge == nil {
			next.ServeHTTP(w, r)
			return
		}
		actorSource := r.Header.Get("X-Actor-Source")
		privateAccess := knowledgePrivateAccessAllowed(actorSource)
		taskToken := actorSource == "task_token"
		if taskToken {
			var subjectOK bool
			userID, privateAccess, subjectOK = h.resolveTaskTokenKnowledgeSubject(r, workspaceID, userID)
			if !subjectOK {
				writeJSON(w, http.StatusForbidden, map[string]any{
					"error":     "knowledge access denied",
					"code":      "knowledge_forbidden",
					"retryable": false,
				})
				return
			}
		}
		*r = *r.WithContext(knowledge.WithSubject(r.Context(), knowledge.Subject{TaskToken: taskToken, PrivateAccess: privateAccess}))
		body, err := readKnowledgeRequestBody(r)
		if err != nil {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{"error": err.Error(), "code": "request_too_large"})
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		hash := hashKnowledgeRequest(r.Header.Get("Content-Type"), body)
		actorKey := "human:" + userID
		if r.Header.Get("X-Actor-Source") == "task_token" {
			actorKey = "task:" + strings.TrimSpace(r.Header.Get("X-Task-ID"))
		}
		operation := r.Method + " " + r.URL.Path
		decision, err := h.Knowledge.BeginIdempotency(r.Context(), workspaceID, actorKey, operation, key, hash)
		if err != nil {
			h.writeKnowledgeError(w, r, err)
			return
		}
		switch decision.State {
		case "processing":
			writeJSON(w, http.StatusAccepted, map[string]any{"status": "processing"})
			return
		case "replay":
			if strings.HasSuffix(operation, "/answers") && decision.Status >= http.StatusOK && decision.Status < http.StatusMultipleChoices {
				if err := h.validateKnowledgeAnswerReplay(r.Context(), workspaceID, userID, chi.URLParam(r, "baseId"), decision.ResponseBody); err != nil {
					h.writeKnowledgeError(w, r, err)
					return
				}
			}
			writeKnowledgeReplay(w, decision)
			return
		case "execute":
		default:
			h.writeKnowledgeError(w, r, fmt.Errorf("invalid idempotency state"))
			return
		}

		recorder := newKnowledgeResponseRecorder()
		completed := false
		defer func() {
			if recovered := recover(); recovered != nil {
				_ = h.Knowledge.FinishIdempotency(r.Context(), decision.ReceiptID, http.StatusInternalServerError, "application/json", []byte(`{"error":"knowledge request failed"}`), false)
				panic(recovered)
			}
			if !completed {
				_ = h.Knowledge.FinishIdempotency(r.Context(), decision.ReceiptID, recorder.statusCode(), recorder.Header().Get("Content-Type"), recorder.body.Bytes(), recorder.statusCode() < 500)
			}
		}()
		next.ServeHTTP(recorder, r)
		completed = true
		_ = h.Knowledge.FinishIdempotency(r.Context(), decision.ReceiptID, recorder.statusCode(), recorder.Header().Get("Content-Type"), recorder.body.Bytes(), recorder.statusCode() < 500)
		copyKnowledgeResponse(w, recorder)
	})
}

// validateKnowledgeAnswerReplay re-authorizes the durable citations before an
// answer response is replayed. Search results are intentionally copied into
// the idempotency receipt, so a retry must not resurrect a document that was
// deleted or made private after the original request completed.
func (h *Handler) validateKnowledgeAnswerReplay(ctx context.Context, workspaceID, actorID, baseID string, body []byte) error {
	var response knowledge.AnswerResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return &knowledge.Error{Status: http.StatusConflict, Code: "idempotency_dependency_changed", Message: "the stored answer response is no longer valid"}
	}
	if _, err := h.Knowledge.GetBase(ctx, workspaceID, actorID, baseID); err != nil {
		return &knowledge.Error{Status: http.StatusConflict, Code: "idempotency_dependency_changed", Message: "the knowledge base is no longer available for this answer"}
	}
	seen := make(map[string]struct{}, len(response.Search.Results))
	for _, result := range response.Search.Results {
		documentID := strings.TrimSpace(result.DocumentID)
		if documentID == "" {
			continue
		}
		if _, ok := seen[documentID]; ok {
			continue
		}
		seen[documentID] = struct{}{}
		if _, err := h.Knowledge.GetDocument(ctx, workspaceID, actorID, baseID, documentID); err != nil {
			return &knowledge.Error{Status: http.StatusConflict, Code: "idempotency_dependency_changed", Message: "a cited knowledge document is no longer available"}
		}
	}
	return nil
}

func knowledgePostRequiresIdempotency(path string) bool {
	// Search is a read-only query expressed as POST because its filter body can
	// be large. It does not create a durable resource and is therefore exempt.
	return !strings.HasSuffix(path, "/search")
}

func readKnowledgeRequestBody(r *http.Request) ([]byte, error) {
	limit := int64(maxKnowledgeIdempotencyRequestBytes)
	if r.ContentLength > limit {
		return nil, fmt.Errorf("request exceeds the knowledge request limit")
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("failed to read request body: %w", err)
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("request exceeds the knowledge request limit")
	}
	return body, nil
}

func hashKnowledgeRequest(contentType string, body []byte) string {
	if mediaType, params, err := mime.ParseMediaType(contentType); err == nil && strings.HasPrefix(mediaType, "multipart/") && params["boundary"] != "" {
		if canonical, ok := canonicalKnowledgeMultipart(body, params["boundary"]); ok {
			body = canonical
		}
	}
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:])
}

type knowledgeMultipartPart struct {
	Name        string `json:"name"`
	Filename    string `json:"filename,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	Digest      string `json:"digest"`
}

func canonicalKnowledgeMultipart(body []byte, boundary string) ([]byte, bool) {
	reader := multipart.NewReader(bytes.NewReader(body), boundary)
	parts := make([]knowledgeMultipartPart, 0)
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, false
		}
		value, err := io.ReadAll(part)
		if err != nil {
			return nil, false
		}
		digest := sha256.Sum256(value)
		parts = append(parts, knowledgeMultipartPart{
			Name:        part.FormName(),
			Filename:    part.FileName(),
			ContentType: part.Header.Get("Content-Type"),
			Digest:      hex.EncodeToString(digest[:]),
		})
	}
	sort.Slice(parts, func(i, j int) bool {
		left, _ := json.Marshal(parts[i])
		right, _ := json.Marshal(parts[j])
		return string(left) < string(right)
	})
	canonical, err := json.Marshal(parts)
	if err != nil {
		return nil, false
	}
	return canonical, true
}

type knowledgeResponseRecorder struct {
	header http.Header
	body   bytes.Buffer
	status int
	wrote  bool
}

func newKnowledgeResponseRecorder() *knowledgeResponseRecorder {
	return &knowledgeResponseRecorder{header: make(http.Header)}
}

func (r *knowledgeResponseRecorder) Header() http.Header { return r.header }

func (r *knowledgeResponseRecorder) WriteHeader(status int) {
	if r.wrote {
		return
	}
	r.status = status
	r.wrote = true
}

func (r *knowledgeResponseRecorder) Write(body []byte) (int, error) {
	if !r.wrote {
		r.WriteHeader(http.StatusOK)
	}
	return r.body.Write(body)
}

func (r *knowledgeResponseRecorder) statusCode() int {
	if !r.wrote {
		return http.StatusOK
	}
	return r.status
}

func writeKnowledgeReplay(w http.ResponseWriter, result knowledge.IdempotencyResult) {
	if result.ContentType != "" {
		w.Header().Set("Content-Type", result.ContentType)
	}
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(result.ResponseBody)))
	w.WriteHeader(result.Status)
	_, _ = w.Write(result.ResponseBody)
}

func copyKnowledgeResponse(w http.ResponseWriter, recorder *knowledgeResponseRecorder) {
	for key, values := range recorder.Header() {
		w.Header()[key] = append([]string(nil), values...)
	}
	w.WriteHeader(recorder.statusCode())
	_, _ = w.Write(recorder.body.Bytes())
}
