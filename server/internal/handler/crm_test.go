package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCRMAccountLifecycle(t *testing.T) {
	// Create an account.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/crm/accounts", map[string]any{
		"name":     "  Acme   Trading Ltd  ",
		"website":  "https://example.com",
		"country":  "China",
		"industry": "Import Export",
	})
	testHandler.CreateCRMAccount(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateCRMAccount: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var account CRMAccountResponse
	if err := json.NewDecoder(w.Body).Decode(&account); err != nil {
		t.Fatalf("decode account: %v", err)
	}
	if account.Name != "Acme Trading Ltd" {
		t.Fatalf("normalized account name = %q, want %q", account.Name, "Acme Trading Ltd")
	}
	if account.Status != "active" {
		t.Fatalf("account status = %q, want active", account.Status)
	}

	// Duplicate normalized name in the same workspace must conflict.
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/crm/accounts", map[string]any{
		"name": "acme trading ltd",
	})
	testHandler.CreateCRMAccount(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("duplicate CreateCRMAccount: expected 409, got %d: %s", w.Code, w.Body.String())
	}

	// Add a contact.
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/crm/accounts/"+account.ID+"/contacts", map[string]any{
		"name":        "Alice Chen",
		"email":       "alice@example.com",
		"whatsapp_id": "+8613800000000",
		"role_title":  "Buyer",
	})
	req = withURLParam(req, "accountId", account.ID)
	testHandler.CreateCRMContact(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateCRMContact: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var contact CRMContactResponse
	if err := json.NewDecoder(w.Body).Decode(&contact); err != nil {
		t.Fatalf("decode contact: %v", err)
	}
	if contact.AccountID == nil || *contact.AccountID != account.ID {
		t.Fatalf("contact.AccountID = %v, want %s", contact.AccountID, account.ID)
	}

	// Save profile summary.
	w = httptest.NewRecorder()
	req = newRequest("PUT", "/api/crm/accounts/"+account.ID+"/profile", map[string]any{
		"summary": "Prefers WhatsApp follow-up and fast samples.",
		"profile_json": map[string]any{
			"product_interests": []string{"LED lights"},
		},
	})
	req = withURLParam(req, "accountId", account.ID)
	testHandler.UpsertCRMAccountProfile(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpsertCRMAccountProfile: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// List should include account with contact count.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/crm/accounts", nil)
	testHandler.ListCRMAccounts(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListCRMAccounts: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var list struct {
		Accounts []CRMAccountResponse `json:"accounts"`
		Total    int                  `json:"total"`
	}
	if err := json.NewDecoder(w.Body).Decode(&list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	found := false
	for _, item := range list.Accounts {
		if item.ID == account.ID {
			found = true
			if item.ContactCount != 1 {
				t.Fatalf("ContactCount = %d, want 1", item.ContactCount)
			}
		}
	}
	if !found {
		t.Fatalf("created account %s not found in list", account.ID)
	}
}
