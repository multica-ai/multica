//go:build verificationdev

package handler

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/verificationworkspace"
)

type verificationWorkspaceContent = verificationworkspace.Content

type verificationWorkspaceRequest struct {
	RestoreID string                       `json:"restoreId"`
	Content   verificationWorkspaceContent `json:"content"`
}
type verificationWorkspaceReceipt struct {
	Digest      string            `json:"digest"`
	WorkspaceID string            `json:"workspaceId"`
	Mapping     map[string]string `json:"mapping"`
}

func verificationText(row map[string]any, key string) string {
	value, _ := row[key].(string)
	return value
}

func (h *Handler) RegisterVerificationRoutes(r chi.Router) {
	if os.Getenv("MULTICA_VERIFICATION_BACKEND") == "" || len(os.Getenv("MULTICA_VERIFICATION_TOKEN")) != 64 || !filepath.IsAbs(os.Getenv("MULTICA_VERIFICATION_DIRECTORY")) {
		return
	}
	r.Group(func(r chi.Router) {
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				host, _, err := net.SplitHostPort(r.RemoteAddr)
				expected := os.Getenv("MULTICA_VERIFICATION_TOKEN")
				if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Multica-Verification-Token")), []byte(expected)) != 1 {
					writeError(w, http.StatusForbidden, "verification capability required")
					return
				}
				next.ServeHTTP(w, r)
			})
		})
		r.Get("/api/verification/capabilities", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, map[string]any{"schemaVersion": 1, "environmentId": os.Getenv("MULTICA_VERIFICATION_BACKEND"), "format": "multica.workspace/v1", "restorePhase": "onlineAfterServiceReady"})
		})
		r.Post("/api/verification/import", h.importVerificationWorkspace)
	})
}
func verificationReceiptPath(restoreID string) string {
	sum := sha256.Sum256([]byte(restoreID))
	return filepath.Join(os.Getenv("MULTICA_VERIFICATION_DIRECTORY"), "backend-imports", hex.EncodeToString(sum[:])+".json")
}
func writeVerificationReceipt(path string, receipt verificationWorkspaceReceipt) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	value, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	temporary := path + "." + uuid.NewString() + ".tmp"
	defer os.Remove(temporary)
	if err = os.WriteFile(temporary, value, 0600); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}
func (h *Handler) importVerificationWorkspace(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id"); !ok {
		return
	}
	var input verificationWorkspaceRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<20))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || input.Content.SchemaVersion != 1 || len(input.RestoreID) == 0 || len(input.RestoreID) > 200 || len(input.Content.Issues) > 1000 || len(input.Content.Projects) > 100 {
		writeError(w, http.StatusBadRequest, "invalid workspace snapshot")
		return
	}
	if err := input.Content.Validate(); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	encoded, _ := json.Marshal(input.Content)
	sum := sha256.Sum256(encoded)
	hash := hex.EncodeToString(sum[:])
	namespace := uuid.NewSHA1(uuid.NameSpaceOID, []byte(os.Getenv("MULTICA_VERIFICATION_BACKEND")+":"+input.RestoreID))
	mapping := map[string]string{}
	for _, rows := range [][]map[string]any{input.Content.Statuses, input.Content.Workflows, input.Content.Projects, input.Content.Issues, input.Content.Comments, input.Content.Labels} {
		for _, row := range rows {
			id := verificationText(row, "id")
			if _, err := uuid.Parse(id); err != nil {
				writeError(w, 400, "invalid source id")
				return
			}
			if _, exists := mapping[id]; exists {
				writeError(w, 400, "duplicate source id")
				return
			}
			mapping[id] = uuid.NewSHA1(namespace, []byte(id)).String()
		}
	}
	mapping[verificationText(input.Content.Workspace, "id")] = workspaceID
	for _, rows := range [][]map[string]any{input.Content.Projects, input.Content.Issues, input.Content.Comments} {
		for _, row := range rows {
			for _, key := range []string{"lead_id", "creator_id", "assignee_id", "author_id"} {
				if id := verificationText(row, key); id != "" {
					if _, exists := mapping[id]; !exists {
						mapping[id] = userID
					}
				}
			}
		}
	}
	for _, attachment := range input.Content.Attachments {
		if _, err := uuid.Parse(attachment.ID); err != nil || attachment.SizeBytes > 25<<20 {
			writeError(w, 400, "invalid attachment")
			return
		}
		mapping[attachment.ID] = uuid.NewSHA1(namespace, []byte(attachment.ID)).String()
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "begin import failed")
		return
	}
	defer tx.Rollback(r.Context())
	if _, err = tx.Exec(r.Context(), "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", namespace.String()); err != nil {
		writeError(w, 500, "import lock failed")
		return
	}
	path := verificationReceiptPath(input.RestoreID)
	receipt := verificationWorkspaceReceipt{Digest: hash, WorkspaceID: workspaceID, Mapping: mapping}
	if raw, err := os.ReadFile(path); err == nil {
		var prior verificationWorkspaceReceipt
		if json.Unmarshal(raw, &prior) != nil || prior.Digest != hash || prior.WorkspaceID != workspaceID {
			writeError(w, 409, "restore identity conflicts with prior content")
			return
		}
	}
	if err = writeVerificationReceipt(path, receipt); err != nil {
		writeError(w, 500, "persist import identity failed")
		return
	}
	var targetSlug string
	if err = tx.QueryRow(r.Context(), "SELECT slug FROM workspace WHERE id=$1", workspaceID).Scan(&targetSlug); err != nil {
		writeError(w, 500, "target workspace unavailable")
		return
	}
	targetOrigin := strings.TrimRight(r.Header.Get("X-Multica-API-Origin"), "/")
	if targetOrigin == "" {
		targetOrigin = "http://" + r.Host
	}
	rewrite := func(value string) string {
		for old, target := range mapping {
			value = strings.ReplaceAll(value, old, target)
		}
		if input.Content.SourceOrigin != "" {
			value = strings.ReplaceAll(value, strings.TrimRight(input.Content.SourceOrigin, "/"), targetOrigin)
		}
		if sourceSlug := verificationText(input.Content.Workspace, "slug"); sourceSlug != "" {
			value = strings.ReplaceAll(value, "/"+sourceSlug+"/", "/"+targetSlug+"/")
		}
		return value
	}
	mapped := func(value any) any {
		id, _ := value.(string)
		if id == "" {
			return nil
		}
		return mapping[id]
	}
	exec := func(query string, args ...any) bool {
		if err != nil {
			return false
		}
		_, err = tx.Exec(r.Context(), query, args...)
		return err == nil
	}
	for _, row := range input.Content.Statuses {
		key := verificationText(row, "key")
		sourceCategory, validCategory := issuestatus.ParseCategory(verificationText(row, "category"))
		if !validCategory {
			writeError(w, 400, "unsupported source status category")
			return
		}
		var id, category string
		lookup := tx.QueryRow(r.Context(), "SELECT id::text,category FROM issue_status WHERE workspace_id=$1 AND key=$2", workspaceID, key).Scan(&id, &category)
		if lookup == nil {
			if category != sourceCategory {
				writeError(w, 409, "status category conflict")
				return
			}
			mapping[verificationText(row, "id")] = id
			continue
		}
		if lookup != pgx.ErrNoRows {
			writeError(w, 500, "status lookup failed")
			return
		}
		exec("INSERT INTO issue_status(id,workspace_id,key,name,description,category,color,position,is_system,icon,archived_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,false,$9,$10) ON CONFLICT(id) DO NOTHING", mapping[verificationText(row, "id")], workspaceID, key, row["name"], verificationText(row, "description"), sourceCategory, row["color"], row["position"], verificationText(row, "icon"), row["archived_at"])
	}
	for _, row := range input.Content.Workflows {
		steps, _ := row["steps"].([]any)
		if len(steps) == 0 {
			writeError(w, 400, "workflow has no steps")
			return
		}
		copied := make([]map[string]any, 0, len(steps))
		for _, item := range steps {
			step, ok := item.(map[string]any)
			if !ok {
				writeError(w, 400, "invalid workflow step")
				return
			}
			copied = append(copied, map[string]any{"status_key": step["status_key"], "handler": map[string]string{"type": "none"}, "instructions": verificationText(step, "instructions"), "next_status_key": verificationText(step, "next_status_key"), "back_status_key": verificationText(step, "back_status_key")})
		}
		data, _ := json.Marshal(copied)
		name := verificationText(row, "name")
		if len([]rune(name)) > 48 {
			name = string([]rune(name)[:48])
		}
		name += " " + namespace.String()[:8]
		exec("INSERT INTO issue_workflow(id,workspace_id,name,description,initial_status_key,steps,is_default) VALUES($1,$2,$3,$4,$5,$6,false) ON CONFLICT(id) DO NOTHING", mapping[verificationText(row, "id")], workspaceID, name, verificationText(row, "description"), copied[0]["status_key"], data)
	}
	for _, row := range input.Content.Labels {
		name := verificationText(row, "name")
		if len([]rune(name)) > maxLabelNameLen-9 {
			name = string([]rune(name)[:maxLabelNameLen-9])
		}
		name += " " + namespace.String()[:8]
		exec("INSERT INTO issue_label(id,workspace_id,resource_type,name,description,color) VALUES($1,$2,'issue',$3,$4,$5) ON CONFLICT(id) DO NOTHING", mapping[verificationText(row, "id")], workspaceID, name, verificationText(row, "description"), row["color"])
	}
	for _, row := range input.Content.Projects {
		exec("INSERT INTO project(id,workspace_id,title,description,icon,status,priority,start_date,due_date,workflow_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT(id) DO NOTHING", mapping[verificationText(row, "id")], workspaceID, row["title"], rewrite(verificationText(row, "description")), row["icon"], row["status"], row["priority"], row["start_date"], row["due_date"], mapped(row["workflow_id"]))
	}
	for _, row := range input.Content.Issues {
		id := mapping[verificationText(row, "id")]
		var exists bool
		if err == nil {
			err = tx.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM issue WHERE id=$1 AND workspace_id=$2)", id, workspaceID).Scan(&exists)
		}
		if exists {
			continue
		}
		var number int
		if err == nil {
			err = tx.QueryRow(r.Context(), "UPDATE workspace SET issue_counter=issue_counter+1 WHERE id=$1 RETURNING issue_counter", workspaceID).Scan(&number)
		}
		exec("INSERT INTO issue(id,workspace_id,title,description,status,priority,creator_type,creator_id,project_id,parent_issue_id,number,start_date,due_date) VALUES($1,$2,$3,$4,$5,$6,'member',$7,$8,$9,$10,$11,$12)", id, workspaceID, row["title"], rewrite(verificationText(row, "description")), row["status"], row["priority"], userID, mapped(row["project_id"]), mapped(row["parent_issue_id"]), number, row["start_date"], row["due_date"])
		if labels, ok := row["labels"].([]any); ok {
			for _, item := range labels {
				label, _ := item.(map[string]any)
				exec("INSERT INTO issue_to_label(issue_id,label_id) VALUES($1,$2) ON CONFLICT DO NOTHING", id, mapped(label["id"]))
			}
		}
	}
	for _, row := range input.Content.Comments {
		exec("INSERT INTO comment(id,workspace_id,issue_id,author_type,author_id,content,type,parent_id,created_at) VALUES($1,$2,$3,'member',$4,$5,$6,$7,$8) ON CONFLICT(id) DO NOTHING", mapping[verificationText(row, "id")], workspaceID, mapped(row["issue_id"]), userID, rewrite(verificationText(row, "content")), row["type"], mapped(row["parent_id"]), row["created_at"])
	}
	for _, attachment := range input.Content.Attachments {
		data, decodeErr := base64.StdEncoding.DecodeString(attachment.Data)
		sum := sha256.Sum256(data)
		if decodeErr != nil || len(data) != attachment.SizeBytes || hex.EncodeToString(sum[:]) != attachment.SHA256 || h.Storage == nil {
			writeError(w, 400, "attachment integrity or storage unavailable")
			return
		}
		id := mapping[attachment.ID]
		url, uploadErr := h.Storage.Upload(r.Context(), "verification/"+namespace.String()+"/"+id, data, attachment.ContentType, attachment.Filename)
		if uploadErr != nil {
			writeError(w, 500, "attachment upload failed; retry the same restore identity")
			return
		}
		var issueID, commentID any
		if attachment.IssueID != nil {
			issueID = mapped(*attachment.IssueID)
		}
		if attachment.CommentID != nil {
			commentID = mapped(*attachment.CommentID)
		}
		exec("INSERT INTO attachment(id,workspace_id,issue_id,comment_id,uploader_type,uploader_id,filename,url,content_type,size_bytes) VALUES($1,$2,$3,$4,'member',$5,$6,$7,$8,$9) ON CONFLICT(id) DO NOTHING", id, workspaceID, issueID, commentID, userID, attachment.Filename, url, attachment.ContentType, len(data))
	}
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "snapshot content or target schema is unsupported; transaction rolled back")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, 500, "import outcome unknown; retry the same restore identity")
		return
	}
	receipt.Mapping = mapping
	_ = writeVerificationReceipt(path, receipt)
	writeJSON(w, 200, map[string]any{"schemaVersion": 1, "mapping": mapping, "policy": "isolatedWorkspaceCopy", "transformations": []string{"authors mapped to the test identity; assignees and leads cleared", "workflow handoffs disabled; imported workflows are not workspace defaults", "workflow and label names namespaced; entity and attachment references remapped", "external URLs remain reference content; page write operations must target the local test API"}})
}
