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

	// Save IMAP settings and preview existing CRM email messages.
	w = httptest.NewRecorder()
	req = newRequest("PUT", "/api/crm/imap-settings", map[string]any{
		"host":     "imap.example.com",
		"username": "sales@example.com",
		"enabled":  true,
	})
	testHandler.UpsertCRMIMAPSettings(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpsertCRMIMAPSettings: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var imapSettings CRMIMAPSettingsResponse
	if err := json.NewDecoder(w.Body).Decode(&imapSettings); err != nil {
		t.Fatalf("decode imap settings: %v", err)
	}
	if imapSettings.Port != 993 || imapSettings.Mailbox != "INBOX" || !imapSettings.UseTLS || !imapSettings.Enabled {
		t.Fatalf("unexpected imap defaults: %+v", imapSettings)
	}

	if _, err := testPool.Exec(req.Context(), `
		INSERT INTO crm_email_thread (workspace_id, account_id, subject)
		VALUES ($1, $2, 'RFQ')
	`, parseUUID(testWorkspaceID), parseUUID(account.ID)); err != nil {
		t.Fatalf("insert email thread: %v", err)
	}
	if _, err := testPool.Exec(req.Context(), `
		INSERT INTO crm_email_message (workspace_id, thread_id, account_id, from_email, subject, snippet, direction)
		SELECT $1, id, $2, 'buyer@example.com', 'RFQ', 'Need quote', 'inbound'
		FROM crm_email_thread WHERE workspace_id = $1 AND account_id = $2 LIMIT 1
	`, parseUUID(testWorkspaceID), parseUUID(account.ID)); err != nil {
		t.Fatalf("insert email message: %v", err)
	}
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/crm/imap/preview", map[string]any{"limit": 1})
	testHandler.PreviewCRMIMAP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PreviewCRMIMAP: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if _, err := testPool.Exec(req.Context(), `
		INSERT INTO crm_profile_suggestion (workspace_id, account_id, field_path, suggested_value, confidence)
		VALUES ($1, $2, 'product_interests', '["LED lights"]', 0.9)
	`, parseUUID(testWorkspaceID), parseUUID(account.ID)); err != nil {
		t.Fatalf("insert profile suggestion: %v", err)
	}
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/crm/accounts/"+account.ID+"/profile/suggestions", nil)
	req = withURLParam(req, "accountId", account.ID)
	testHandler.ListCRMProfileSuggestions(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListCRMProfileSuggestions: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var suggestionList struct {
		Suggestions []CRMProfileSuggestionResponse `json:"suggestions"`
	}
	if err := json.NewDecoder(w.Body).Decode(&suggestionList); err != nil {
		t.Fatalf("decode suggestions: %v", err)
	}
	if len(suggestionList.Suggestions) == 0 || suggestionList.Suggestions[0].FieldPath != "product_interests" {
		t.Fatalf("unexpected suggestions: %+v", suggestionList.Suggestions)
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
