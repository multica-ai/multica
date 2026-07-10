package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

func (h *Handler) SearchDeptDepartments(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	if h.DeptSync == nil || !h.DeptSync.Configured() {
		writeError(w, http.StatusServiceUnavailable, "dept sync is not configured")
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	limit := 20
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			writeError(w, http.StatusBadRequest, "invalid limit")
			return
		}
		if parsed < limit {
			limit = parsed
		}
	}
	departments, err := h.DeptSync.SearchDepartments(r.Context(), query, limit)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to search departments")
		return
	}
	writeJSON(w, http.StatusOK, departments)
}

func (h *Handler) SearchDeptUsers(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	if h.DeptSync == nil || !h.DeptSync.Configured() {
		writeError(w, http.StatusServiceUnavailable, "dept sync is not configured")
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	limit := 20
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			writeError(w, http.StatusBadRequest, "invalid limit")
			return
		}
		if parsed < limit {
			limit = parsed
		}
	}
	users, err := h.DeptSync.SearchUsers(r.Context(), query, limit)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to search users")
		return
	}
	writeJSON(w, http.StatusOK, users)
}

func (h *Handler) ListDeptDepartmentUsers(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	if h.DeptSync == nil || !h.DeptSync.Configured() {
		writeError(w, http.StatusServiceUnavailable, "dept sync is not configured")
		return
	}
	deptID := strings.TrimSpace(chi.URLParam(r, "id"))
	if deptID == "" {
		writeError(w, http.StatusBadRequest, "dept_id is required")
		return
	}
	users, err := h.DeptSync.ListDepartmentUsers(r.Context(), deptID, true)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to load department users")
		return
	}
	writeJSON(w, http.StatusOK, users)
}
