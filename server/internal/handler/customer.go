package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type CustomerResponse struct {
	ID           string  `json:"id"`
	WorkspaceID  string  `json:"workspace_id"`
	Name         string  `json:"name"`
	Description  *string `json:"description"`
	Website      *string `json:"website"`
	Email        *string `json:"email"`
	Phone        *string `json:"phone"`
	Status       string  `json:"status"`
	CreatedAt    string  `json:"created_at"`
	UpdatedAt    string  `json:"updated_at"`
	ProjectCount int64   `json:"project_count"`
}

type CreateCustomerRequest struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
	Website     *string `json:"website"`
	Email       *string `json:"email"`
	Phone       *string `json:"phone"`
	Status      string  `json:"status"`
}

type UpdateCustomerRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	Website     *string `json:"website"`
	Email       *string `json:"email"`
	Phone       *string `json:"phone"`
	Status      *string `json:"status"`
}

func customerToResponse(c db.Customer, projectCount int64) CustomerResponse {
	return CustomerResponse{
		ID:           uuidToString(c.ID),
		WorkspaceID:  uuidToString(c.WorkspaceID),
		Name:         c.Name,
		Description:  textToPtr(c.Description),
		Website:      textToPtr(c.Website),
		Email:        textToPtr(c.Email),
		Phone:        textToPtr(c.Phone),
		Status:       c.Status,
		CreatedAt:    timestampToString(c.CreatedAt),
		UpdatedAt:    timestampToString(c.UpdatedAt),
		ProjectCount: projectCount,
	}
}

func listCustomerRowToResponse(c db.ListCustomersRow) CustomerResponse {
	return CustomerResponse{
		ID:           uuidToString(c.ID),
		WorkspaceID:  uuidToString(c.WorkspaceID),
		Name:         c.Name,
		Description:  textToPtr(c.Description),
		Website:      textToPtr(c.Website),
		Email:        textToPtr(c.Email),
		Phone:        textToPtr(c.Phone),
		Status:       c.Status,
		CreatedAt:    timestampToString(c.CreatedAt),
		UpdatedAt:    timestampToString(c.UpdatedAt),
		ProjectCount: c.ProjectCount,
	}
}

func getCustomerRowToResponse(c db.GetCustomerInWorkspaceRow) CustomerResponse {
	return CustomerResponse{
		ID:           uuidToString(c.ID),
		WorkspaceID:  uuidToString(c.WorkspaceID),
		Name:         c.Name,
		Description:  textToPtr(c.Description),
		Website:      textToPtr(c.Website),
		Email:        textToPtr(c.Email),
		Phone:        textToPtr(c.Phone),
		Status:       c.Status,
		CreatedAt:    timestampToString(c.CreatedAt),
		UpdatedAt:    timestampToString(c.UpdatedAt),
		ProjectCount: c.ProjectCount,
	}
}

func normalizeOptionalString(v *string) *string {
	if v == nil {
		return nil
	}
	s := strings.TrimSpace(*v)
	if s == "" {
		return nil
	}
	return &s
}

func validateCustomerStatus(status string) bool {
	return status == "active" || status == "archived"
}

func (h *Handler) ListCustomers(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	var statusFilter pgtype.Text
	if s := r.URL.Query().Get("status"); s != "" {
		if !validateCustomerStatus(s) {
			writeError(w, http.StatusBadRequest, "invalid status")
			return
		}
		statusFilter = pgtype.Text{String: s, Valid: true}
	}
	customers, err := h.Queries.ListCustomers(r.Context(), db.ListCustomersParams{
		WorkspaceID: wsUUID,
		Status:      statusFilter,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list customers")
		return
	}
	resp := make([]CustomerResponse, len(customers))
	for i, c := range customers {
		resp[i] = listCustomerRowToResponse(c)
	}
	writeJSON(w, http.StatusOK, map[string]any{"customers": resp, "total": len(resp)})
}

func (h *Handler) GetCustomer(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)
	idUUID, ok := parseUUIDOrBadRequest(w, id, "customer id")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	customer, err := h.Queries.GetCustomerInWorkspace(r.Context(), db.GetCustomerInWorkspaceParams{
		ID: idUUID, WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "customer not found")
		return
	}
	writeJSON(w, http.StatusOK, getCustomerRowToResponse(customer))
}

func (h *Handler) CreateCustomer(w http.ResponseWriter, r *http.Request) {
	var req CreateCustomerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	status := req.Status
	if status == "" {
		status = "active"
	}
	if !validateCustomerStatus(status) {
		writeError(w, http.StatusBadRequest, "invalid status")
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	customer, err := h.Queries.CreateCustomer(r.Context(), db.CreateCustomerParams{
		WorkspaceID: wsUUID,
		Name:        name,
		Description: ptrToText(normalizeOptionalString(req.Description)),
		Website:     ptrToText(normalizeOptionalString(req.Website)),
		Email:       ptrToText(normalizeOptionalString(req.Email)),
		Phone:       ptrToText(normalizeOptionalString(req.Phone)),
		Status:      status,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create customer")
		return
	}
	resp := customerToResponse(customer, 0)
	h.publish(protocol.EventCustomerCreated, workspaceID, "member", userID, map[string]any{"customer": resp})
	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handler) UpdateCustomer(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)
	idUUID, ok := parseUUIDOrBadRequest(w, id, "customer id")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	prev, err := h.Queries.GetCustomerInWorkspace(r.Context(), db.GetCustomerInWorkspaceParams{
		ID: idUUID, WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "customer not found")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read request body")
		return
	}
	var req UpdateCustomerRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var rawFields map[string]json.RawMessage
	json.Unmarshal(bodyBytes, &rawFields)

	params := db.UpdateCustomerParams{
		ID:          idUUID,
		Description: prev.Description,
		Website:     prev.Website,
		Email:       prev.Email,
		Phone:       prev.Phone,
	}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			writeError(w, http.StatusBadRequest, "name is required")
			return
		}
		params.Name = pgtype.Text{String: name, Valid: true}
	}
	if req.Status != nil {
		if !validateCustomerStatus(*req.Status) {
			writeError(w, http.StatusBadRequest, "invalid status")
			return
		}
		params.Status = pgtype.Text{String: *req.Status, Valid: true}
	}
	if _, ok := rawFields["description"]; ok {
		params.Description = ptrToText(normalizeOptionalString(req.Description))
	}
	if _, ok := rawFields["website"]; ok {
		params.Website = ptrToText(normalizeOptionalString(req.Website))
	}
	if _, ok := rawFields["email"]; ok {
		params.Email = ptrToText(normalizeOptionalString(req.Email))
	}
	if _, ok := rawFields["phone"]; ok {
		params.Phone = ptrToText(normalizeOptionalString(req.Phone))
	}
	customer, err := h.Queries.UpdateCustomer(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update customer")
		return
	}
	projectCount, _ := h.Queries.CountProjectsByCustomer(r.Context(), customer.ID)
	resp := customerToResponse(customer, projectCount)
	h.publish(protocol.EventCustomerUpdated, workspaceID, "member", userID, map[string]any{"customer": resp})
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) DeleteCustomer(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)
	idUUID, ok := parseUUIDOrBadRequest(w, id, "customer id")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	customer, err := h.Queries.GetCustomerInWorkspace(r.Context(), db.GetCustomerInWorkspaceParams{
		ID: idUUID, WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "customer not found")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	if err := h.Queries.DeleteCustomer(r.Context(), idUUID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete customer")
		return
	}
	h.publish(protocol.EventCustomerDeleted, workspaceID, "member", userID, map[string]any{"customer_id": uuidToString(customer.ID)})
	w.WriteHeader(http.StatusNoContent)
}
