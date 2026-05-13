package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// CRMAccountResponse is the JSON shape returned by the CRM account API.
type CRMAccountResponse struct {
	ID              string   `json:"id"`
	WorkspaceID     string   `json:"workspace_id"`
	Name            string   `json:"name"`
	AccountCode     *string  `json:"account_code"`
	AccountType     string   `json:"account_type"`
	Website         *string  `json:"website"`
	Country         *string  `json:"country"`
	CountryCode     *string  `json:"country_code"`
	CountryName     *string  `json:"country_name"`
	Region          *string  `json:"region"`
	City            *string  `json:"city"`
	Industry        *string  `json:"industry"`
	SubIndustry     *string  `json:"sub_industry"`
	Status          string   `json:"status"`
	OwnerID         *string  `json:"owner_id"`
	OwnerMemberID   *string  `json:"owner_member_id"`
	Source          *string  `json:"source"`
	Rating          string   `json:"rating"`
	Priority        string   `json:"priority"`
	AnnualRevenue   *string  `json:"annual_revenue"`
	EmployeeCount   *string  `json:"employee_count"`
	Tags            []string `json:"tags"`
	Notes           *string  `json:"notes"`
	LastContactedAt *string  `json:"last_contacted_at"`
	NextFollowUpAt  *string  `json:"next_follow_up_at"`
	ContactCount    int64    `json:"contact_count"`
	CreatedAt       string   `json:"created_at"`
	UpdatedAt       string   `json:"updated_at"`
}

type crmAccountRow struct {
	ID              pgtype.UUID
	WorkspaceID     pgtype.UUID
	Name            string
	NormalizedName  string
	AccountCode     pgtype.Text
	AccountType     string
	Website         pgtype.Text
	Country         pgtype.Text
	CountryCode     pgtype.Text
	CountryName     pgtype.Text
	Region          pgtype.Text
	City            pgtype.Text
	Industry        pgtype.Text
	SubIndustry     pgtype.Text
	Status          string
	OwnerID         pgtype.UUID
	OwnerMemberID   pgtype.UUID
	Source          pgtype.Text
	Rating          string
	Priority        string
	AnnualRevenue   pgtype.Text
	EmployeeCount   pgtype.Text
	Tags            []string
	Notes           pgtype.Text
	LastContactedAt pgtype.Timestamptz
	NextFollowUpAt  pgtype.Timestamptz
	CreatedAt       pgtype.Timestamptz
	UpdatedAt       pgtype.Timestamptz
	ContactCount    int64
}

func crmAccountToResponse(row crmAccountRow) CRMAccountResponse {
	tags := row.Tags
	if tags == nil {
		tags = []string{}
	}
	return CRMAccountResponse{
		ID:              uuidToString(row.ID),
		WorkspaceID:     uuidToString(row.WorkspaceID),
		Name:            row.Name,
		AccountCode:     textToPtr(row.AccountCode),
		AccountType:     row.AccountType,
		Website:         textToPtr(row.Website),
		Country:         textToPtr(row.Country),
		CountryCode:     textToPtr(row.CountryCode),
		CountryName:     textToPtr(row.CountryName),
		Region:          textToPtr(row.Region),
		City:            textToPtr(row.City),
		Industry:        textToPtr(row.Industry),
		SubIndustry:     textToPtr(row.SubIndustry),
		Status:          row.Status,
		OwnerID:         uuidToPtr(row.OwnerID),
		OwnerMemberID:   uuidToPtr(row.OwnerMemberID),
		Source:          textToPtr(row.Source),
		Rating:          row.Rating,
		Priority:        row.Priority,
		AnnualRevenue:   textToPtr(row.AnnualRevenue),
		EmployeeCount:   textToPtr(row.EmployeeCount),
		Tags:            tags,
		Notes:           textToPtr(row.Notes),
		LastContactedAt: timestampToPtr(row.LastContactedAt),
		NextFollowUpAt:  timestampToPtr(row.NextFollowUpAt),
		ContactCount:    row.ContactCount,
		CreatedAt:       timestampToString(row.CreatedAt),
		UpdatedAt:       timestampToString(row.UpdatedAt),
	}
}

type CreateCRMAccountRequest struct {
	Name            string   `json:"name"`
	AccountCode     *string  `json:"account_code"`
	AccountType     *string  `json:"account_type"`
	Website         *string  `json:"website"`
	Country         *string  `json:"country"`
	CountryCode     *string  `json:"country_code"`
	CountryName     *string  `json:"country_name"`
	Region          *string  `json:"region"`
	City            *string  `json:"city"`
	Industry        *string  `json:"industry"`
	SubIndustry     *string  `json:"sub_industry"`
	Status          *string  `json:"status"`
	OwnerID         *string  `json:"owner_id"`
	OwnerMemberID   *string  `json:"owner_member_id"`
	Source          *string  `json:"source"`
	Rating          *string  `json:"rating"`
	Priority        *string  `json:"priority"`
	AnnualRevenue   *string  `json:"annual_revenue"`
	EmployeeCount   *string  `json:"employee_count"`
	Tags            []string `json:"tags"`
	Notes           *string  `json:"notes"`
	LastContactedAt *string  `json:"last_contacted_at"`
	NextFollowUpAt  *string  `json:"next_follow_up_at"`
}

type UpdateCRMAccountRequest = CreateCRMAccountRequest

// CRMContactResponse is the JSON shape returned by the CRM contact API.
type CRMContactResponse struct {
	ID                string  `json:"id"`
	WorkspaceID       string  `json:"workspace_id"`
	AccountID         *string `json:"account_id"`
	Name              string  `json:"name"`
	Salutation        *string `json:"salutation"`
	Email             *string `json:"email"`
	Phone             *string `json:"phone"`
	Mobile            *string `json:"mobile"`
	WhatsappID        *string `json:"whatsapp_id"`
	Whatsapp          *string `json:"whatsapp"`
	Wechat            *string `json:"wechat"`
	LinkedinURL       *string `json:"linkedin_url"`
	RoleTitle         *string `json:"role_title"`
	JobTitle          *string `json:"job_title"`
	Department        *string `json:"department"`
	Role              *string `json:"role"`
	Language          *string `json:"language"`
	PreferredLanguage *string `json:"preferred_language"`
	Timezone          *string `json:"timezone"`
	IsPrimary         bool    `json:"is_primary"`
	DecisionRole      *string `json:"decision_role"`
	Notes             *string `json:"notes"`
	LastContactedAt   *string `json:"last_contacted_at"`
	CreatedAt         string  `json:"created_at"`
	UpdatedAt         string  `json:"updated_at"`
}

type crmContactRow struct {
	ID                pgtype.UUID
	WorkspaceID       pgtype.UUID
	AccountID         pgtype.UUID
	Name              string
	Salutation        pgtype.Text
	Email             pgtype.Text
	Phone             pgtype.Text
	Mobile            pgtype.Text
	WhatsappID        pgtype.Text
	Whatsapp          pgtype.Text
	Wechat            pgtype.Text
	LinkedinURL       pgtype.Text
	RoleTitle         pgtype.Text
	JobTitle          pgtype.Text
	Department        pgtype.Text
	Role              pgtype.Text
	Language          pgtype.Text
	PreferredLanguage pgtype.Text
	Timezone          pgtype.Text
	IsPrimary         bool
	DecisionRole      pgtype.Text
	Notes             pgtype.Text
	LastContactedAt   pgtype.Timestamptz
	CreatedAt         pgtype.Timestamptz
	UpdatedAt         pgtype.Timestamptz
}

func crmContactToResponse(row crmContactRow) CRMContactResponse {
	return CRMContactResponse{
		ID:                uuidToString(row.ID),
		WorkspaceID:       uuidToString(row.WorkspaceID),
		AccountID:         uuidToPtr(row.AccountID),
		Name:              row.Name,
		Salutation:        textToPtr(row.Salutation),
		Email:             textToPtr(row.Email),
		Phone:             textToPtr(row.Phone),
		Mobile:            textToPtr(row.Mobile),
		WhatsappID:        textToPtr(row.WhatsappID),
		Whatsapp:          textToPtr(row.Whatsapp),
		Wechat:            textToPtr(row.Wechat),
		LinkedinURL:       textToPtr(row.LinkedinURL),
		RoleTitle:         textToPtr(row.RoleTitle),
		JobTitle:          textToPtr(row.JobTitle),
		Department:        textToPtr(row.Department),
		Role:              textToPtr(row.Role),
		Language:          textToPtr(row.Language),
		PreferredLanguage: textToPtr(row.PreferredLanguage),
		Timezone:          textToPtr(row.Timezone),
		IsPrimary:         row.IsPrimary,
		DecisionRole:      textToPtr(row.DecisionRole),
		Notes:             textToPtr(row.Notes),
		LastContactedAt:   timestampToPtr(row.LastContactedAt),
		CreatedAt:         timestampToString(row.CreatedAt),
		UpdatedAt:         timestampToString(row.UpdatedAt),
	}
}

type CreateCRMContactRequest struct {
	AccountID         *string `json:"account_id"`
	Name              string  `json:"name"`
	Salutation        *string `json:"salutation"`
	Email             *string `json:"email"`
	Phone             *string `json:"phone"`
	Mobile            *string `json:"mobile"`
	WhatsappID        *string `json:"whatsapp_id"`
	Whatsapp          *string `json:"whatsapp"`
	Wechat            *string `json:"wechat"`
	LinkedinURL       *string `json:"linkedin_url"`
	RoleTitle         *string `json:"role_title"`
	JobTitle          *string `json:"job_title"`
	Department        *string `json:"department"`
	Role              *string `json:"role"`
	Language          *string `json:"language"`
	PreferredLanguage *string `json:"preferred_language"`
	Timezone          *string `json:"timezone"`
	IsPrimary         *bool   `json:"is_primary"`
	DecisionRole      *string `json:"decision_role"`
	Notes             *string `json:"notes"`
	LastContactedAt   *string `json:"last_contacted_at"`
}

type UpdateCRMContactRequest = CreateCRMContactRequest

type CRMEmailThreadResponse struct {
	ID               string  `json:"id"`
	WorkspaceID      string  `json:"workspace_id"`
	AccountID        *string `json:"account_id"`
	ContactID        *string `json:"contact_id"`
	Subject          string  `json:"subject"`
	ExternalThreadID *string `json:"external_thread_id"`
	Mailbox          *string `json:"mailbox"`
	Direction        string  `json:"direction"`
	Status           string  `json:"status"`
	LastMessageAt    *string `json:"last_message_at"`
	MessageCount     int64   `json:"message_count"`
	CreatedAt        string  `json:"created_at"`
	UpdatedAt        string  `json:"updated_at"`
}

type crmEmailThreadRow struct {
	ID               pgtype.UUID
	WorkspaceID      pgtype.UUID
	AccountID        pgtype.UUID
	ContactID        pgtype.UUID
	Subject          string
	ExternalThreadID pgtype.Text
	Mailbox          pgtype.Text
	Direction        string
	Status           string
	LastMessageAt    pgtype.Timestamptz
	CreatedAt        pgtype.Timestamptz
	UpdatedAt        pgtype.Timestamptz
	MessageCount     int64
}

type CRMEmailMessageResponse struct {
	ID                string   `json:"id"`
	WorkspaceID       string   `json:"workspace_id"`
	ThreadID          string   `json:"thread_id"`
	AccountID         *string  `json:"account_id"`
	ContactID         *string  `json:"contact_id"`
	ExternalMessageID *string  `json:"external_message_id"`
	FromEmail         *string  `json:"from_email"`
	FromName          *string  `json:"from_name"`
	ToEmails          []string `json:"to_emails"`
	CcEmails          []string `json:"cc_emails"`
	BccEmails         []string `json:"bcc_emails"`
	Subject           *string  `json:"subject"`
	SentAt            *string  `json:"sent_at"`
	ReceivedAt        *string  `json:"received_at"`
	BodyText          *string  `json:"body_text"`
	BodyHTML          *string  `json:"body_html"`
	Snippet           *string  `json:"snippet"`
	Direction         string   `json:"direction"`
	CreatedAt         string   `json:"created_at"`
	UpdatedAt         string   `json:"updated_at"`
}

type crmEmailMessageRow struct {
	ID                pgtype.UUID
	WorkspaceID       pgtype.UUID
	ThreadID          pgtype.UUID
	AccountID         pgtype.UUID
	ContactID         pgtype.UUID
	ExternalMessageID pgtype.Text
	FromEmail         pgtype.Text
	FromName          pgtype.Text
	ToEmails          []string
	CcEmails          []string
	BccEmails         []string
	Subject           pgtype.Text
	SentAt            pgtype.Timestamptz
	ReceivedAt        pgtype.Timestamptz
	BodyText          pgtype.Text
	BodyHTML          pgtype.Text
	Snippet           pgtype.Text
	Direction         string
	CreatedAt         pgtype.Timestamptz
	UpdatedAt         pgtype.Timestamptz
}

type CreateCRMEmailThreadRequest struct {
	AccountID        *string `json:"account_id"`
	ContactID        *string `json:"contact_id"`
	Subject          string  `json:"subject"`
	ExternalThreadID *string `json:"external_thread_id"`
	Mailbox          *string `json:"mailbox"`
	Direction        *string `json:"direction"`
	Status           *string `json:"status"`
	LastMessageAt    *string `json:"last_message_at"`
}

type CreateCRMEmailMessageRequest struct {
	AccountID         *string  `json:"account_id"`
	ContactID         *string  `json:"contact_id"`
	ExternalMessageID *string  `json:"external_message_id"`
	FromEmail         *string  `json:"from_email"`
	FromName          *string  `json:"from_name"`
	ToEmails          []string `json:"to_emails"`
	CcEmails          []string `json:"cc_emails"`
	BccEmails         []string `json:"bcc_emails"`
	Subject           *string  `json:"subject"`
	SentAt            *string  `json:"sent_at"`
	ReceivedAt        *string  `json:"received_at"`
	BodyText          *string  `json:"body_text"`
	BodyHTML          *string  `json:"body_html"`
	Snippet           *string  `json:"snippet"`
	Direction         string   `json:"direction"`
}

func crmEmailThreadToResponse(row crmEmailThreadRow) CRMEmailThreadResponse {
	return CRMEmailThreadResponse{
		ID:               uuidToString(row.ID),
		WorkspaceID:      uuidToString(row.WorkspaceID),
		AccountID:        uuidToPtr(row.AccountID),
		ContactID:        uuidToPtr(row.ContactID),
		Subject:          row.Subject,
		ExternalThreadID: textToPtr(row.ExternalThreadID),
		Mailbox:          textToPtr(row.Mailbox),
		Direction:        row.Direction,
		Status:           row.Status,
		LastMessageAt:    timestampToPtr(row.LastMessageAt),
		MessageCount:     row.MessageCount,
		CreatedAt:        timestampToString(row.CreatedAt),
		UpdatedAt:        timestampToString(row.UpdatedAt),
	}
}

func crmEmailMessageToResponse(row crmEmailMessageRow) CRMEmailMessageResponse {
	toEmails := row.ToEmails
	if toEmails == nil {
		toEmails = []string{}
	}
	ccEmails := row.CcEmails
	if ccEmails == nil {
		ccEmails = []string{}
	}
	bccEmails := row.BccEmails
	if bccEmails == nil {
		bccEmails = []string{}
	}
	return CRMEmailMessageResponse{
		ID:                uuidToString(row.ID),
		WorkspaceID:       uuidToString(row.WorkspaceID),
		ThreadID:          uuidToString(row.ThreadID),
		AccountID:         uuidToPtr(row.AccountID),
		ContactID:         uuidToPtr(row.ContactID),
		ExternalMessageID: textToPtr(row.ExternalMessageID),
		FromEmail:         textToPtr(row.FromEmail),
		FromName:          textToPtr(row.FromName),
		ToEmails:          toEmails,
		CcEmails:          ccEmails,
		BccEmails:         bccEmails,
		Subject:           textToPtr(row.Subject),
		SentAt:            timestampToPtr(row.SentAt),
		ReceivedAt:        timestampToPtr(row.ReceivedAt),
		BodyText:          textToPtr(row.BodyText),
		BodyHTML:          textToPtr(row.BodyHTML),
		Snippet:           textToPtr(row.Snippet),
		Direction:         row.Direction,
		CreatedAt:         timestampToString(row.CreatedAt),
		UpdatedAt:         timestampToString(row.UpdatedAt),
	}
}

type CRMAccountProfileResponse struct {
	ID          string          `json:"id"`
	WorkspaceID string          `json:"workspace_id"`
	AccountID   string          `json:"account_id"`
	Summary     *string         `json:"summary"`
	ProfileJSON json.RawMessage `json:"profile_json"`
	UpdatedBy   *string         `json:"updated_by"`
	CreatedAt   string          `json:"created_at"`
	UpdatedAt   string          `json:"updated_at"`
}

type UpsertCRMAccountProfileRequest struct {
	Summary     *string         `json:"summary"`
	ProfileJSON json.RawMessage `json:"profile_json"`
}

type CRMCommunicationNoteResponse struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	AccountID   *string `json:"account_id"`
	ContactID   *string `json:"contact_id"`
	Channel     string  `json:"channel"`
	Direction   string  `json:"direction"`
	OccurredAt  string  `json:"occurred_at"`
	Subject     *string `json:"subject"`
	Body        string  `json:"body"`
	CreatedBy   *string `json:"created_by"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

type CreateCRMCommunicationNoteRequest struct {
	ContactID  *string `json:"contact_id"`
	Channel    *string `json:"channel"`
	Direction  *string `json:"direction"`
	OccurredAt *string `json:"occurred_at"`
	Subject    *string `json:"subject"`
	Body       string  `json:"body"`
}

type LinkCRMAccountProjectRequest struct {
	ProjectID  *string  `json:"project_id"`
	ProjectIDs []string `json:"project_ids"`
	Label      *string  `json:"label"`
}

type CreateCRMFollowUpIssueRequest struct {
	ProjectID    *string `json:"project_id"`
	Title        string  `json:"title"`
	Description  *string `json:"description"`
	Priority     *string `json:"priority"`
	AssigneeType *string `json:"assignee_type"`
	AssigneeID   *string `json:"assignee_id"`
	DueDate      *string `json:"due_date"`
}

type CRMFollowUpIssueResponse struct {
	Issue IssueResponse `json:"issue"`
}

func normalizeCRMName(s string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
}

func normalizedCRMKey(s string) string {
	return strings.ToLowerSpecial(unicode.TurkishCase, normalizeCRMName(s))
}

func cleanOptionalText(s *string) pgtype.Text {
	if s == nil {
		return pgtype.Text{}
	}
	v := strings.TrimSpace(*s)
	if v == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: v, Valid: true}
}

func cleanStatus(s *string) string {
	if s == nil || strings.TrimSpace(*s) == "" {
		return "active"
	}
	return strings.TrimSpace(*s)
}

func cleanOptionalStringList(values []string) []string {
	cleaned := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		v := strings.TrimSpace(value)
		if v == "" {
			continue
		}
		key := strings.ToLower(v)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		cleaned = append(cleaned, v)
	}
	return cleaned
}

func cleanDefault(s *string, fallback string) string {
	if s == nil || strings.TrimSpace(*s) == "" {
		return fallback
	}
	return strings.TrimSpace(*s)
}

func cleanCountryCodeOrName(code, name *string) (pgtype.Text, pgtype.Text) {
	cleanCode := cleanOptionalText(code)
	cleanName := cleanOptionalText(name)
	if !cleanName.Valid && cleanCode.Valid {
		cleanName = cleanCode
	}
	if !cleanCode.Valid && cleanName.Valid {
		cleanCode = cleanName
	}
	return cleanCode, cleanName
}

func firstString(values ...*string) *string {
	for _, value := range values {
		if value != nil && strings.TrimSpace(*value) != "" {
			return value
		}
	}
	return nil
}

func validCRMAccountStatus(value string) bool {
	switch value {
	case "active", "inactive", "prospect", "archived":
		return true
	default:
		return false
	}
}

func optionalUUID(w http.ResponseWriter, value *string, fieldName string) (pgtype.UUID, bool) {
	var zero pgtype.UUID
	if value == nil || strings.TrimSpace(*value) == "" {
		return zero, true
	}
	parsed, ok := parseUUIDOrBadRequest(w, strings.TrimSpace(*value), fieldName)
	if !ok {
		return zero, false
	}
	return parsed, true
}

func cleanNoteChannel(value *string) string {
	if value == nil || strings.TrimSpace(*value) == "" {
		return "manual"
	}
	return strings.TrimSpace(*value)
}

func validCRMCommunicationChannel(value string) bool {
	switch value {
	case "manual", "email", "whatsapp", "phone", "meeting", "other":
		return true
	default:
		return false
	}
}

func cleanNoteDirection(value *string) string {
	if value == nil || strings.TrimSpace(*value) == "" {
		return "outbound"
	}
	return strings.TrimSpace(*value)
}

func validCRMCommunicationDirection(value string) bool {
	switch value {
	case "inbound", "outbound", "internal":
		return true
	default:
		return false
	}
}

func cleanOptionalTimestamp(w http.ResponseWriter, s *string, fieldName string) (pgtype.Timestamptz, bool) {
	if s == nil || strings.TrimSpace(*s) == "" {
		return pgtype.Timestamptz{}, true
	}
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(*s))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid "+fieldName+" format, expected RFC3339")
		return pgtype.Timestamptz{}, false
	}
	return pgtype.Timestamptz{Time: parsed, Valid: true}, true
}

func cleanOptionalBool(v *bool) bool {
	return v != nil && *v
}

func (h *Handler) crmWorkspaceUUID(w http.ResponseWriter, r *http.Request) (pgtype.UUID, bool) {
	return parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace id")
}

func (h *Handler) scanCRMAccount(row pgx.Row) (crmAccountRow, error) {
	var account crmAccountRow
	err := row.Scan(
		&account.ID, &account.WorkspaceID, &account.Name, &account.NormalizedName,
		&account.AccountCode, &account.AccountType, &account.Website, &account.Country,
		&account.CountryCode, &account.CountryName, &account.Region, &account.City,
		&account.Industry, &account.SubIndustry, &account.Status, &account.OwnerID,
		&account.OwnerMemberID, &account.Source, &account.Rating, &account.Priority,
		&account.AnnualRevenue, &account.EmployeeCount, &account.Tags, &account.Notes,
		&account.LastContactedAt, &account.NextFollowUpAt, &account.CreatedAt, &account.UpdatedAt,
		&account.ContactCount,
	)
	return account, err
}

func (h *Handler) getCRMAccount(w http.ResponseWriter, r *http.Request, accountID pgtype.UUID, workspaceID pgtype.UUID) (crmAccountRow, bool) {
	row, err := h.scanCRMAccount(h.DB.QueryRow(r.Context(), `
		SELECT a.id, a.workspace_id, a.name, a.normalized_name, a.account_code, a.account_type,
		       a.website, a.country, a.country_code, a.country_name, a.region, a.city,
		       a.industry, a.sub_industry, a.status, a.owner_id, a.owner_member_id,
		       a.source, a.rating, a.priority, a.annual_revenue, a.employee_count,
		       a.tags, a.notes, a.last_contacted_at, a.next_follow_up_at,
		       a.created_at, a.updated_at, COUNT(c.id)::bigint AS contact_count
		FROM crm_account a
		LEFT JOIN crm_contact c ON c.account_id = a.id AND c.workspace_id = a.workspace_id
		WHERE a.id = $1 AND a.workspace_id = $2
		GROUP BY a.id
	`, accountID, workspaceID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "CRM account not found")
			return crmAccountRow{}, false
		}
		writeError(w, http.StatusInternalServerError, "failed to load CRM account")
		return crmAccountRow{}, false
	}
	return row, true
}

func (h *Handler) CreateCRMAccount(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := h.crmWorkspaceUUID(w, r)
	if !ok {
		return
	}
	var req CreateCRMAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	name := normalizeCRMName(req.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	status := cleanStatus(req.Status)
	if !validCRMAccountStatus(status) {
		writeError(w, http.StatusBadRequest, "invalid account status")
		return
	}
	ownerID, ok := optionalUUID(w, req.OwnerID, "owner_id")
	if !ok {
		return
	}
	ownerMemberID, ok := optionalUUID(w, req.OwnerMemberID, "owner_member_id")
	if !ok {
		return
	}
	lastContactedAt, ok := cleanOptionalTimestamp(w, req.LastContactedAt, "last_contacted_at")
	if !ok {
		return
	}
	nextFollowUpAt, ok := cleanOptionalTimestamp(w, req.NextFollowUpAt, "next_follow_up_at")
	if !ok {
		return
	}
	countryCode, countryName := cleanCountryCodeOrName(req.CountryCode, firstString(req.CountryName, req.Country))
	row, err := h.scanCRMAccount(h.DB.QueryRow(r.Context(), `
		INSERT INTO crm_account (
			workspace_id, name, normalized_name, account_code, account_type, website, country,
			country_code, country_name, region, city, industry, sub_industry, status, owner_id,
			owner_member_id, source, rating, priority, annual_revenue, employee_count, tags,
			notes, last_contacted_at, next_follow_up_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
		        $11, $12, $13, $14, $15, $16, $17, $18, $19, $20,
		        $21, $22, $23, $24, $25)
		RETURNING id, workspace_id, name, normalized_name, account_code, account_type,
		          website, country, country_code, country_name, region, city,
		          industry, sub_industry, status, owner_id, owner_member_id,
		          source, rating, priority, annual_revenue, employee_count,
		          tags, notes, last_contacted_at, next_follow_up_at,
		          created_at, updated_at, 0::bigint
	`, workspaceID, name, normalizedCRMKey(name), cleanOptionalText(req.AccountCode), cleanDefault(req.AccountType, "prospect"),
		cleanOptionalText(req.Website), countryName, countryCode, countryName,
		cleanOptionalText(req.Region), cleanOptionalText(req.City), cleanOptionalText(req.Industry), cleanOptionalText(req.SubIndustry), status,
		ownerID, ownerMemberID, cleanOptionalText(req.Source), cleanDefault(req.Rating, "unknown"), cleanDefault(req.Priority, "medium"),
		cleanOptionalText(req.AnnualRevenue), cleanOptionalText(req.EmployeeCount), cleanOptionalStringList(req.Tags), cleanOptionalText(req.Notes),
		lastContactedAt, nextFollowUpAt))
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "CRM account name already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create CRM account")
		return
	}
	writeJSON(w, http.StatusCreated, crmAccountToResponse(row))
}

func (h *Handler) ListCRMAccounts(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := h.crmWorkspaceUUID(w, r)
	if !ok {
		return
	}
	search := strings.TrimSpace(r.URL.Query().Get("search"))
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	var searchArg pgtype.Text
	if search != "" {
		searchArg = pgtype.Text{String: normalizedCRMKey(search), Valid: true}
	}
	var statusArg pgtype.Text
	if status != "" {
		statusArg = pgtype.Text{String: status, Valid: true}
	}
	rows, err := h.DB.Query(r.Context(), `
		SELECT a.id, a.workspace_id, a.name, a.normalized_name, a.account_code, a.account_type,
		       a.website, a.country, a.country_code, a.country_name, a.region, a.city,
		       a.industry, a.sub_industry, a.status, a.owner_id, a.owner_member_id,
		       a.source, a.rating, a.priority, a.annual_revenue, a.employee_count,
		       a.tags, a.notes, a.last_contacted_at, a.next_follow_up_at,
		       a.created_at, a.updated_at, COUNT(c.id)::bigint AS contact_count
		FROM crm_account a
		LEFT JOIN crm_contact c ON c.account_id = a.id AND c.workspace_id = a.workspace_id
		WHERE a.workspace_id = $1
		  AND ($2::text IS NULL OR a.status = $2::text)
		  AND ($3::text IS NULL OR a.normalized_name LIKE '%' || $3::text || '%')
		GROUP BY a.id
		ORDER BY a.updated_at DESC, a.created_at DESC
		LIMIT 100
	`, workspaceID, statusArg, searchArg)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list CRM accounts")
		return
	}
	defer rows.Close()
	accounts := []CRMAccountResponse{}
	for rows.Next() {
		var account crmAccountRow
		if err := rows.Scan(
			&account.ID, &account.WorkspaceID, &account.Name, &account.NormalizedName,
			&account.AccountCode, &account.AccountType, &account.Website, &account.Country,
			&account.CountryCode, &account.CountryName, &account.Region, &account.City,
			&account.Industry, &account.SubIndustry, &account.Status, &account.OwnerID,
			&account.OwnerMemberID, &account.Source, &account.Rating, &account.Priority,
			&account.AnnualRevenue, &account.EmployeeCount, &account.Tags, &account.Notes,
			&account.LastContactedAt, &account.NextFollowUpAt, &account.CreatedAt, &account.UpdatedAt,
			&account.ContactCount,
		); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to scan CRM account")
			return
		}
		accounts = append(accounts, crmAccountToResponse(account))
	}
	writeJSON(w, http.StatusOK, map[string]any{"accounts": accounts, "total": len(accounts)})
}

func (h *Handler) GetCRMAccount(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := h.crmWorkspaceUUID(w, r)
	if !ok {
		return
	}
	accountID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "accountId"), "account id")
	if !ok {
		return
	}
	account, ok := h.getCRMAccount(w, r, accountID, workspaceID)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, crmAccountToResponse(account))
}

func (h *Handler) UpdateCRMAccount(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := h.crmWorkspaceUUID(w, r)
	if !ok {
		return
	}
	accountID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "accountId"), "account id")
	if !ok {
		return
	}
	if _, ok := h.getCRMAccount(w, r, accountID, workspaceID); !ok {
		return
	}
	var req UpdateCRMAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	name := normalizeCRMName(req.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	status := cleanStatus(req.Status)
	if !validCRMAccountStatus(status) {
		writeError(w, http.StatusBadRequest, "invalid account status")
		return
	}
	ownerID, ok := optionalUUID(w, req.OwnerID, "owner_id")
	if !ok {
		return
	}
	ownerMemberID, ok := optionalUUID(w, req.OwnerMemberID, "owner_member_id")
	if !ok {
		return
	}
	lastContactedAt, ok := cleanOptionalTimestamp(w, req.LastContactedAt, "last_contacted_at")
	if !ok {
		return
	}
	nextFollowUpAt, ok := cleanOptionalTimestamp(w, req.NextFollowUpAt, "next_follow_up_at")
	if !ok {
		return
	}
	countryCode, countryName := cleanCountryCodeOrName(req.CountryCode, firstString(req.CountryName, req.Country))
	row, err := h.scanCRMAccount(h.DB.QueryRow(r.Context(), `
		UPDATE crm_account SET
			name = $3,
			normalized_name = $4,
			account_code = $5,
			account_type = $6,
			website = $7,
			country = $8,
			country_code = $9,
			country_name = $10,
			region = $11,
			city = $12,
			industry = $13,
			sub_industry = $14,
			status = $15,
			owner_id = $16,
			owner_member_id = $17,
			source = $18,
			rating = $19,
			priority = $20,
			annual_revenue = $21,
			employee_count = $22,
			tags = $23,
			notes = $24,
			last_contacted_at = $25,
			next_follow_up_at = $26,
			updated_at = now()
		WHERE id = $1 AND workspace_id = $2
		RETURNING id, workspace_id, name, normalized_name, account_code, account_type,
		          website, country, country_code, country_name, region, city,
		          industry, sub_industry, status, owner_id, owner_member_id,
		          source, rating, priority, annual_revenue, employee_count,
		          tags, notes, last_contacted_at, next_follow_up_at,
		          created_at, updated_at,
		          (SELECT COUNT(*)::bigint FROM crm_contact c WHERE c.account_id = crm_account.id AND c.workspace_id = crm_account.workspace_id)
	`, accountID, workspaceID, name, normalizedCRMKey(name), cleanOptionalText(req.AccountCode), cleanDefault(req.AccountType, "prospect"),
		cleanOptionalText(req.Website), countryName, countryCode, countryName, cleanOptionalText(req.Region), cleanOptionalText(req.City),
		cleanOptionalText(req.Industry), cleanOptionalText(req.SubIndustry), status, ownerID, ownerMemberID, cleanOptionalText(req.Source),
		cleanDefault(req.Rating, "unknown"), cleanDefault(req.Priority, "medium"), cleanOptionalText(req.AnnualRevenue), cleanOptionalText(req.EmployeeCount),
		cleanOptionalStringList(req.Tags), cleanOptionalText(req.Notes), lastContactedAt, nextFollowUpAt))
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "CRM account name or code already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to update CRM account")
		return
	}
	writeJSON(w, http.StatusOK, crmAccountToResponse(row))
}

func (h *Handler) DeleteCRMAccount(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := h.crmWorkspaceUUID(w, r)
	if !ok {
		return
	}
	accountID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "accountId"), "account id")
	if !ok {
		return
	}
	if _, ok := h.getCRMAccount(w, r, accountID, workspaceID); !ok {
		return
	}
	if _, err := h.DB.Exec(r.Context(), `DELETE FROM crm_account WHERE id = $1 AND workspace_id = $2`, accountID, workspaceID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete CRM account")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) CreateCRMContact(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := h.crmWorkspaceUUID(w, r)
	if !ok {
		return
	}
	accountID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "accountId"), "account id")
	if !ok {
		return
	}
	if _, ok := h.getCRMAccount(w, r, accountID, workspaceID); !ok {
		return
	}
	var req CreateCRMContactRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	name := normalizeCRMName(req.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	lastContactedAt, ok := cleanOptionalTimestamp(w, req.LastContactedAt, "last_contacted_at")
	if !ok {
		return
	}
	contact, err := h.scanCRMContact(h.DB.QueryRow(r.Context(), `
		INSERT INTO crm_contact (
			workspace_id, account_id, name, salutation, email, phone, mobile, whatsapp_id,
			whatsapp, wechat, linkedin_url, role_title, job_title, department, role,
			language, preferred_language, timezone, is_primary, decision_role, notes, last_contacted_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
		        $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22)
		RETURNING id, workspace_id, account_id, name, salutation, email, phone, mobile,
		          whatsapp_id, whatsapp, wechat, linkedin_url, role_title, job_title,
		          department, role, language, preferred_language, timezone, is_primary,
		          decision_role, notes, last_contacted_at, created_at, updated_at
	`, workspaceID, accountID, name, cleanOptionalText(req.Salutation), cleanOptionalText(req.Email), cleanOptionalText(req.Phone),
		cleanOptionalText(req.Mobile), cleanOptionalText(req.WhatsappID), cleanOptionalText(req.Whatsapp), cleanOptionalText(req.Wechat),
		cleanOptionalText(req.LinkedinURL), cleanOptionalText(req.RoleTitle), cleanOptionalText(req.JobTitle), cleanOptionalText(req.Department),
		cleanOptionalText(req.Role), cleanOptionalText(req.Language), cleanOptionalText(req.PreferredLanguage), cleanOptionalText(req.Timezone),
		cleanOptionalBool(req.IsPrimary), cleanOptionalText(req.DecisionRole), cleanOptionalText(req.Notes), lastContactedAt))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create CRM contact")
		return
	}
	writeJSON(w, http.StatusCreated, crmContactToResponse(contact))
}

func (h *Handler) ListCRMContacts(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := h.crmWorkspaceUUID(w, r)
	if !ok {
		return
	}
	accountID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "accountId"), "account id")
	if !ok {
		return
	}
	rows, err := h.DB.Query(r.Context(), `
		SELECT id, workspace_id, account_id, name, salutation, email, phone, mobile,
		       whatsapp_id, whatsapp, wechat, linkedin_url, role_title, job_title,
		       department, role, language, preferred_language, timezone, is_primary,
		       decision_role, notes, last_contacted_at, created_at, updated_at
		FROM crm_contact WHERE workspace_id = $1 AND account_id = $2 ORDER BY is_primary DESC, created_at ASC
	`, workspaceID, accountID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list CRM contacts")
		return
	}
	defer rows.Close()
	contacts := []CRMContactResponse{}
	for rows.Next() {
		contact, err := h.scanCRMContact(rows)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to scan CRM contact")
			return
		}
		contacts = append(contacts, crmContactToResponse(contact))
	}
	writeJSON(w, http.StatusOK, map[string]any{"contacts": contacts, "total": len(contacts)})
}

func (h *Handler) UpdateCRMContact(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := h.crmWorkspaceUUID(w, r)
	if !ok {
		return
	}
	accountID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "accountId"), "account id")
	if !ok {
		return
	}
	if _, ok := h.getCRMAccount(w, r, accountID, workspaceID); !ok {
		return
	}
	contactID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "contactId"), "contact id")
	if !ok {
		return
	}
	var req UpdateCRMContactRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	name := normalizeCRMName(req.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	lastContactedAt, ok := cleanOptionalTimestamp(w, req.LastContactedAt, "last_contacted_at")
	if !ok {
		return
	}
	contact, err := h.scanCRMContact(h.DB.QueryRow(r.Context(), `
		UPDATE crm_contact SET
			account_id = $3,
			name = $4,
			salutation = $5,
			email = $6,
			phone = $7,
			mobile = $8,
			whatsapp_id = $9,
			whatsapp = $10,
			wechat = $11,
			linkedin_url = $12,
			role_title = $13,
			job_title = $14,
			department = $15,
			role = $16,
			language = $17,
			preferred_language = $18,
			timezone = $19,
			is_primary = $20,
			decision_role = $21,
			notes = $22,
			last_contacted_at = $23,
			updated_at = now()
		WHERE id = $1 AND workspace_id = $2 AND account_id = $3
		RETURNING id, workspace_id, account_id, name, salutation, email, phone, mobile,
		          whatsapp_id, whatsapp, wechat, linkedin_url, role_title, job_title,
		          department, role, language, preferred_language, timezone, is_primary,
		          decision_role, notes, last_contacted_at, created_at, updated_at
	`, contactID, workspaceID, accountID, name, cleanOptionalText(req.Salutation), cleanOptionalText(req.Email), cleanOptionalText(req.Phone),
		cleanOptionalText(req.Mobile), cleanOptionalText(req.WhatsappID), cleanOptionalText(req.Whatsapp), cleanOptionalText(req.Wechat),
		cleanOptionalText(req.LinkedinURL), cleanOptionalText(req.RoleTitle), cleanOptionalText(req.JobTitle), cleanOptionalText(req.Department),
		cleanOptionalText(req.Role), cleanOptionalText(req.Language), cleanOptionalText(req.PreferredLanguage), cleanOptionalText(req.Timezone),
		cleanOptionalBool(req.IsPrimary), cleanOptionalText(req.DecisionRole), cleanOptionalText(req.Notes), lastContactedAt))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "CRM contact not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to update CRM contact")
		return
	}
	writeJSON(w, http.StatusOK, crmContactToResponse(contact))
}

func (h *Handler) DeleteCRMContact(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := h.crmWorkspaceUUID(w, r)
	if !ok {
		return
	}
	accountID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "accountId"), "account id")
	if !ok {
		return
	}
	contactID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "contactId"), "contact id")
	if !ok {
		return
	}
	commandTag, err := h.DB.Exec(r.Context(), `DELETE FROM crm_contact WHERE id = $1 AND workspace_id = $2 AND account_id = $3`, contactID, workspaceID, accountID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete CRM contact")
		return
	}
	if commandTag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "CRM contact not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) scanCRMContact(row pgx.Row) (crmContactRow, error) {
	var contact crmContactRow
	err := row.Scan(
		&contact.ID, &contact.WorkspaceID, &contact.AccountID, &contact.Name,
		&contact.Salutation, &contact.Email, &contact.Phone, &contact.Mobile,
		&contact.WhatsappID, &contact.Whatsapp, &contact.Wechat, &contact.LinkedinURL,
		&contact.RoleTitle, &contact.JobTitle, &contact.Department, &contact.Role,
		&contact.Language, &contact.PreferredLanguage, &contact.Timezone, &contact.IsPrimary,
		&contact.DecisionRole, &contact.Notes, &contact.LastContactedAt,
		&contact.CreatedAt, &contact.UpdatedAt,
	)
	return contact, err
}

func (h *Handler) scanCRMEmailThread(row pgx.Row) (crmEmailThreadRow, error) {
	var thread crmEmailThreadRow
	err := row.Scan(
		&thread.ID, &thread.WorkspaceID, &thread.AccountID, &thread.ContactID,
		&thread.Subject, &thread.ExternalThreadID, &thread.Mailbox, &thread.Direction,
		&thread.Status, &thread.LastMessageAt, &thread.CreatedAt, &thread.UpdatedAt,
		&thread.MessageCount,
	)
	return thread, err
}

func (h *Handler) scanCRMEmailMessage(row pgx.Row) (crmEmailMessageRow, error) {
	var message crmEmailMessageRow
	err := row.Scan(
		&message.ID, &message.WorkspaceID, &message.ThreadID, &message.AccountID,
		&message.ContactID, &message.ExternalMessageID, &message.FromEmail, &message.FromName,
		&message.ToEmails, &message.CcEmails, &message.BccEmails, &message.Subject,
		&message.SentAt, &message.ReceivedAt, &message.BodyText, &message.BodyHTML,
		&message.Snippet, &message.Direction, &message.CreatedAt, &message.UpdatedAt,
	)
	return message, err
}

func (h *Handler) getCRMEmailThread(w http.ResponseWriter, r *http.Request, threadID pgtype.UUID, workspaceID pgtype.UUID) (crmEmailThreadRow, bool) {
	thread, err := h.scanCRMEmailThread(h.DB.QueryRow(r.Context(), `
		SELECT t.id, t.workspace_id, t.account_id, t.contact_id, t.subject,
		       t.external_thread_id, t.mailbox, t.direction, t.status, t.last_message_at,
		       t.created_at, t.updated_at, COUNT(m.id)::bigint AS message_count
		FROM crm_email_thread t
		LEFT JOIN crm_email_message m ON m.thread_id = t.id AND m.workspace_id = t.workspace_id
		WHERE t.id = $1 AND t.workspace_id = $2
		GROUP BY t.id
	`, threadID, workspaceID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "CRM email thread not found")
			return crmEmailThreadRow{}, false
		}
		writeError(w, http.StatusInternalServerError, "failed to load CRM email thread")
		return crmEmailThreadRow{}, false
	}
	return thread, true
}

func (h *Handler) ListCRMEmailThreads(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := h.crmWorkspaceUUID(w, r)
	if !ok {
		return
	}
	accountID, ok := optionalUUID(w, optionalStringFromQuery(r, "account_id"), "account_id")
	if !ok {
		return
	}
	rows, err := h.DB.Query(r.Context(), `
		SELECT t.id, t.workspace_id, t.account_id, t.contact_id, t.subject,
		       t.external_thread_id, t.mailbox, t.direction, t.status, t.last_message_at,
		       t.created_at, t.updated_at, COUNT(m.id)::bigint AS message_count
		FROM crm_email_thread t
		LEFT JOIN crm_email_message m ON m.thread_id = t.id AND m.workspace_id = t.workspace_id
		WHERE t.workspace_id = $1 AND ($2::uuid IS NULL OR t.account_id = $2)
		GROUP BY t.id
		ORDER BY COALESCE(t.last_message_at, t.updated_at) DESC
		LIMIT 100
	`, workspaceID, accountID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list CRM email threads")
		return
	}
	defer rows.Close()
	threads := []CRMEmailThreadResponse{}
	for rows.Next() {
		thread, err := h.scanCRMEmailThread(rows)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to scan CRM email thread")
			return
		}
		threads = append(threads, crmEmailThreadToResponse(thread))
	}
	writeJSON(w, http.StatusOK, map[string]any{"threads": threads, "total": len(threads)})
}

func optionalStringFromQuery(r *http.Request, key string) *string {
	value := strings.TrimSpace(r.URL.Query().Get(key))
	if value == "" {
		return nil
	}
	return &value
}

func (h *Handler) CreateCRMEmailThread(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := h.crmWorkspaceUUID(w, r)
	if !ok {
		return
	}
	var req CreateCRMEmailThreadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	subject := normalizeCRMName(req.Subject)
	if subject == "" {
		writeError(w, http.StatusBadRequest, "subject is required")
		return
	}
	accountID, ok := optionalUUID(w, req.AccountID, "account_id")
	if !ok {
		return
	}
	contactID, ok := optionalUUID(w, req.ContactID, "contact_id")
	if !ok {
		return
	}
	lastMessageAt, ok := cleanOptionalTimestamp(w, req.LastMessageAt, "last_message_at")
	if !ok {
		return
	}
	thread, err := h.scanCRMEmailThread(h.DB.QueryRow(r.Context(), `
		INSERT INTO crm_email_thread (workspace_id, account_id, contact_id, subject, external_thread_id, mailbox, direction, status, last_message_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, workspace_id, account_id, contact_id, subject, external_thread_id, mailbox, direction, status, last_message_at, created_at, updated_at, 0::bigint
	`, workspaceID, accountID, contactID, subject, cleanOptionalText(req.ExternalThreadID), cleanOptionalText(req.Mailbox), cleanDefault(req.Direction, "inbound"), cleanDefault(req.Status, "open"), lastMessageAt))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create CRM email thread")
		return
	}
	writeJSON(w, http.StatusCreated, crmEmailThreadToResponse(thread))
}

func (h *Handler) ListCRMEmailMessages(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := h.crmWorkspaceUUID(w, r)
	if !ok {
		return
	}
	threadID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "threadId"), "thread id")
	if !ok {
		return
	}
	if _, ok := h.getCRMEmailThread(w, r, threadID, workspaceID); !ok {
		return
	}
	rows, err := h.DB.Query(r.Context(), `
		SELECT id, workspace_id, thread_id, account_id, contact_id, external_message_id,
		       from_email, from_name, to_emails, cc_emails, bcc_emails, subject,
		       sent_at, received_at, body_text, body_html, snippet, direction,
		       created_at, updated_at
		FROM crm_email_message
		WHERE workspace_id = $1 AND thread_id = $2
		ORDER BY COALESCE(sent_at, received_at, created_at) ASC
	`, workspaceID, threadID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list CRM email messages")
		return
	}
	defer rows.Close()
	messages := []CRMEmailMessageResponse{}
	for rows.Next() {
		message, err := h.scanCRMEmailMessage(rows)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to scan CRM email message")
			return
		}
		messages = append(messages, crmEmailMessageToResponse(message))
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": messages, "total": len(messages)})
}

func (h *Handler) CreateCRMEmailMessage(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := h.crmWorkspaceUUID(w, r)
	if !ok {
		return
	}
	threadID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "threadId"), "thread id")
	if !ok {
		return
	}
	thread, ok := h.getCRMEmailThread(w, r, threadID, workspaceID)
	if !ok {
		return
	}
	var req CreateCRMEmailMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	direction := strings.TrimSpace(req.Direction)
	if direction == "" {
		writeError(w, http.StatusBadRequest, "direction is required")
		return
	}
	accountID, ok := optionalUUID(w, req.AccountID, "account_id")
	if !ok {
		return
	}
	if !accountID.Valid {
		accountID = thread.AccountID
	}
	contactID, ok := optionalUUID(w, req.ContactID, "contact_id")
	if !ok {
		return
	}
	if !contactID.Valid {
		contactID = thread.ContactID
	}
	sentAt, ok := cleanOptionalTimestamp(w, req.SentAt, "sent_at")
	if !ok {
		return
	}
	receivedAt, ok := cleanOptionalTimestamp(w, req.ReceivedAt, "received_at")
	if !ok {
		return
	}
	message, err := h.scanCRMEmailMessage(h.DB.QueryRow(r.Context(), `
		INSERT INTO crm_email_message (
			workspace_id, thread_id, account_id, contact_id, external_message_id,
			from_email, from_name, to_emails, cc_emails, bcc_emails, subject,
			sent_at, received_at, body_text, body_html, snippet, direction
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
		        $11, $12, $13, $14, $15, $16, $17)
		RETURNING id, workspace_id, thread_id, account_id, contact_id, external_message_id,
		          from_email, from_name, to_emails, cc_emails, bcc_emails, subject,
		          sent_at, received_at, body_text, body_html, snippet, direction,
		          created_at, updated_at
	`, workspaceID, threadID, accountID, contactID, cleanOptionalText(req.ExternalMessageID),
		cleanOptionalText(req.FromEmail), cleanOptionalText(req.FromName), cleanOptionalStringList(req.ToEmails),
		cleanOptionalStringList(req.CcEmails), cleanOptionalStringList(req.BccEmails), cleanOptionalText(req.Subject),
		sentAt, receivedAt, cleanOptionalText(req.BodyText), cleanOptionalText(req.BodyHTML), cleanOptionalText(req.Snippet), direction))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create CRM email message")
		return
	}
	_, _ = h.DB.Exec(r.Context(), `
		UPDATE crm_email_thread
		SET last_message_at = COALESCE($3, $4, last_message_at, now()), updated_at = now()
		WHERE id = $1 AND workspace_id = $2
	`, threadID, workspaceID, sentAt, receivedAt)
	writeJSON(w, http.StatusCreated, crmEmailMessageToResponse(message))
}

func (h *Handler) GetCRMAccountProfile(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := h.crmWorkspaceUUID(w, r)
	if !ok {
		return
	}
	accountID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "accountId"), "account id")
	if !ok {
		return
	}
	if _, ok := h.getCRMAccount(w, r, accountID, workspaceID); !ok {
		return
	}
	var id pgtype.UUID
	var summary pgtype.Text
	var updatedBy pgtype.UUID
	var createdAt, updatedAt pgtype.Timestamptz
	var rawProfile []byte
	err := h.DB.QueryRow(r.Context(), `
		SELECT id, summary, profile_json, updated_by, created_at, updated_at
		FROM crm_account_profile
		WHERE workspace_id = $1 AND account_id = $2
	`, workspaceID, accountID).Scan(&id, &summary, &rawProfile, &updatedBy, &createdAt, &updatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusOK, nil)
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get CRM account profile")
		return
	}
	writeJSON(w, http.StatusOK, CRMAccountProfileResponse{
		ID: uuidToString(id), WorkspaceID: uuidToString(workspaceID), AccountID: uuidToString(accountID),
		Summary: textToPtr(summary), ProfileJSON: json.RawMessage(rawProfile), UpdatedBy: uuidToPtr(updatedBy),
		CreatedAt: timestampToString(createdAt), UpdatedAt: timestampToString(updatedAt),
	})
}

func (h *Handler) UpsertCRMAccountProfile(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := h.crmWorkspaceUUID(w, r)
	if !ok {
		return
	}
	accountID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "accountId"), "account id")
	if !ok {
		return
	}
	if _, ok := h.getCRMAccount(w, r, accountID, workspaceID); !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	updatedBy, _ := parseUUIDLoose(userID)
	var req UpsertCRMAccountProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	profileJSON := req.ProfileJSON
	if len(profileJSON) == 0 {
		profileJSON = json.RawMessage("{}")
	}
	if !json.Valid(profileJSON) {
		writeError(w, http.StatusBadRequest, "profile_json must be valid JSON")
		return
	}
	var id pgtype.UUID
	var summary pgtype.Text
	var updatedByOut pgtype.UUID
	var createdAt, updatedAt pgtype.Timestamptz
	var rawProfile []byte
	err := h.DB.QueryRow(r.Context(), `
		INSERT INTO crm_account_profile (workspace_id, account_id, summary, profile_json, updated_by)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (account_id) DO UPDATE SET summary = EXCLUDED.summary, profile_json = EXCLUDED.profile_json, updated_by = EXCLUDED.updated_by, updated_at = now()
		RETURNING id, summary, profile_json, updated_by, created_at, updated_at
	`, workspaceID, accountID, cleanOptionalText(req.Summary), profileJSON, updatedBy).Scan(&id, &summary, &rawProfile, &updatedByOut, &createdAt, &updatedAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save CRM account profile")
		return
	}
	writeJSON(w, http.StatusOK, CRMAccountProfileResponse{ID: uuidToString(id), WorkspaceID: uuidToString(workspaceID), AccountID: uuidToString(accountID), Summary: textToPtr(summary), ProfileJSON: json.RawMessage(rawProfile), UpdatedBy: uuidToPtr(updatedByOut), CreatedAt: timestampToString(createdAt), UpdatedAt: timestampToString(updatedAt)})
}

func (h *Handler) CreateCRMCommunicationNote(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := h.crmWorkspaceUUID(w, r)
	if !ok {
		return
	}
	accountID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "accountId"), "account id")
	if !ok {
		return
	}
	if _, ok := h.getCRMAccount(w, r, accountID, workspaceID); !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	createdBy, _ := parseUUIDLoose(userID)
	var req CreateCRMCommunicationNoteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	body := strings.TrimSpace(req.Body)
	if body == "" {
		writeError(w, http.StatusBadRequest, "body is required")
		return
	}
	channel := cleanNoteChannel(req.Channel)
	if !validCRMCommunicationChannel(channel) {
		writeError(w, http.StatusBadRequest, "invalid communication channel")
		return
	}
	direction := cleanNoteDirection(req.Direction)
	if !validCRMCommunicationDirection(direction) {
		writeError(w, http.StatusBadRequest, "invalid communication direction")
		return
	}
	contactID, ok := optionalUUID(w, req.ContactID, "contact_id")
	if !ok {
		return
	}
	if contactID.Valid {
		var exists bool
		if err := h.DB.QueryRow(r.Context(), `SELECT EXISTS (SELECT 1 FROM crm_contact WHERE id = $1 AND workspace_id = $2 AND account_id = $3)`, contactID, workspaceID, accountID).Scan(&exists); err != nil || !exists {
			writeError(w, http.StatusBadRequest, "contact not found in this account")
			return
		}
	}
	var occurredAt pgtype.Timestamptz
	if req.OccurredAt != nil && strings.TrimSpace(*req.OccurredAt) != "" {
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(*req.OccurredAt))
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid occurred_at format, expected RFC3339")
			return
		}
		occurredAt = pgtype.Timestamptz{Time: parsed, Valid: true}
	}
	var id, outWorkspaceID, outAccountID, outContactID, outCreatedBy pgtype.UUID
	var outChannel, outDirection, outBody string
	var outOccurredAt, outCreatedAt, outUpdatedAt pgtype.Timestamptz
	var outSubject pgtype.Text
	err := h.DB.QueryRow(r.Context(), `
		INSERT INTO crm_communication_note (workspace_id, account_id, contact_id, channel, direction, occurred_at, subject, body, created_by)
		VALUES ($1, $2, $3, $4, $5, COALESCE($6, now()), $7, $8, $9)
		RETURNING id, workspace_id, account_id, contact_id, channel, direction, occurred_at, subject, body, created_by, created_at, updated_at
	`, workspaceID, accountID, contactID, channel, direction, occurredAt, cleanOptionalText(req.Subject), body, createdBy).Scan(
		&id, &outWorkspaceID, &outAccountID, &outContactID, &outChannel, &outDirection, &outOccurredAt, &outSubject, &outBody, &outCreatedBy, &outCreatedAt, &outUpdatedAt,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create CRM communication note")
		return
	}
	writeJSON(w, http.StatusCreated, CRMCommunicationNoteResponse{
		ID: uuidToString(id), WorkspaceID: uuidToString(outWorkspaceID), AccountID: uuidToPtr(outAccountID), ContactID: uuidToPtr(outContactID),
		Channel: outChannel, Direction: outDirection, OccurredAt: timestampToString(outOccurredAt), Subject: textToPtr(outSubject), Body: outBody,
		CreatedBy: uuidToPtr(outCreatedBy), CreatedAt: timestampToString(outCreatedAt), UpdatedAt: timestampToString(outUpdatedAt),
	})
}

func (h *Handler) ListCRMCommunicationNotes(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := h.crmWorkspaceUUID(w, r)
	if !ok {
		return
	}
	accountID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "accountId"), "account id")
	if !ok {
		return
	}
	if _, ok := h.getCRMAccount(w, r, accountID, workspaceID); !ok {
		return
	}
	rows, err := h.DB.Query(r.Context(), `
		SELECT id, workspace_id, account_id, contact_id, channel, direction, occurred_at, subject, body, created_by, created_at, updated_at
		FROM crm_communication_note
		WHERE workspace_id = $1 AND account_id = $2
		ORDER BY occurred_at DESC, created_at DESC
		LIMIT 100
	`, workspaceID, accountID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list CRM communication notes")
		return
	}
	defer rows.Close()
	notes := []CRMCommunicationNoteResponse{}
	for rows.Next() {
		var id, outWorkspaceID, outAccountID, outContactID, outCreatedBy pgtype.UUID
		var outChannel, outDirection, outBody string
		var outOccurredAt, outCreatedAt, outUpdatedAt pgtype.Timestamptz
		var outSubject pgtype.Text
		if err := rows.Scan(&id, &outWorkspaceID, &outAccountID, &outContactID, &outChannel, &outDirection, &outOccurredAt, &outSubject, &outBody, &outCreatedBy, &outCreatedAt, &outUpdatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to scan CRM communication note")
			return
		}
		notes = append(notes, CRMCommunicationNoteResponse{
			ID: uuidToString(id), WorkspaceID: uuidToString(outWorkspaceID), AccountID: uuidToPtr(outAccountID), ContactID: uuidToPtr(outContactID),
			Channel: outChannel, Direction: outDirection, OccurredAt: timestampToString(outOccurredAt), Subject: textToPtr(outSubject), Body: outBody,
			CreatedBy: uuidToPtr(outCreatedBy), CreatedAt: timestampToString(outCreatedAt), UpdatedAt: timestampToString(outUpdatedAt),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"notes": notes, "total": len(notes)})
}

func (h *Handler) LinkCRMAccountProject(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := h.crmWorkspaceUUID(w, r)
	if !ok {
		return
	}
	accountID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "accountId"), "account id")
	if !ok {
		return
	}
	account, ok := h.getCRMAccount(w, r, accountID, workspaceID)
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var req LinkCRMAccountProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	projectIDs := append([]string{}, req.ProjectIDs...)
	if req.ProjectID != nil {
		projectIDs = append(projectIDs, *req.ProjectID)
	}
	projectIDs = cleanOptionalStringList(projectIDs)
	if len(projectIDs) == 0 {
		writeError(w, http.StatusBadRequest, "project_id or project_ids is required")
		return
	}
	parsedProjectIDs := make([]pgtype.UUID, 0, len(projectIDs))
	for i, rawProjectID := range projectIDs {
		projectID, ok := parseUUIDOrBadRequest(w, rawProjectID, "project_ids["+strconv.Itoa(i)+"]")
		if !ok {
			return
		}
		parsedProjectIDs = append(parsedProjectIDs, projectID)
	}
	labelText := account.Name
	if req.Label != nil && strings.TrimSpace(*req.Label) != "" {
		labelText = strings.TrimSpace(*req.Label)
	}
	ref, err := json.Marshal(map[string]any{"account_id": uuidToString(account.ID), "name": account.Name})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to link CRM account to project")
		return
	}
	creator, _ := h.parseUserUUIDOrZero(userID)
	created := make([]ProjectResourceResponse, 0, len(parsedProjectIDs))
	skipped := []string{}
	for i, projectID := range parsedProjectIDs {
		project, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{ID: projectID, WorkspaceID: workspaceID})
		if err != nil {
			writeError(w, http.StatusNotFound, "project_ids["+strconv.Itoa(i)+"] not found")
			return
		}
		count, _ := h.Queries.CountProjectResources(r.Context(), project.ID)
		resource, err := h.Queries.CreateProjectResource(r.Context(), db.CreateProjectResourceParams{
			ProjectID:    project.ID,
			WorkspaceID:  project.WorkspaceID,
			ResourceType: "crm_account",
			ResourceRef:  ref,
			Label:        pgtype.Text{String: labelText, Valid: true},
			Position:     int32(count),
			CreatedBy:    creator,
		})
		if err != nil {
			if isUniqueViolation(err) {
				skipped = append(skipped, uuidToString(project.ID))
				continue
			}
			writeError(w, http.StatusInternalServerError, "failed to link CRM account to project")
			return
		}
		if _, err := h.DB.Exec(r.Context(), `
			INSERT INTO crm_entity_link (workspace_id, crm_entity_type, crm_entity_id, target_type, target_id, relation_type, created_by)
			VALUES ($1, 'account', $2, 'project', $3, 'customer_for', $4)
			ON CONFLICT DO NOTHING
		`, workspaceID, accountID, project.ID, creator); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to link CRM account to project")
			return
		}
		created = append(created, projectResourceToResponse(resource))
	}
	if len(created) == 0 && len(skipped) > 0 {
		writeError(w, http.StatusConflict, "CRM account is already attached to selected projects")
		return
	}
	if req.ProjectID != nil && len(req.ProjectIDs) == 0 && len(created) == 1 {
		writeJSON(w, http.StatusCreated, created[0])
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"resources": created, "total": len(created), "skipped_project_ids": skipped})
}

func (h *Handler) CreateCRMFollowUpIssue(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := h.crmWorkspaceUUID(w, r)
	if !ok {
		return
	}
	accountID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "accountId"), "account id")
	if !ok {
		return
	}
	account, ok := h.getCRMAccount(w, r, accountID, workspaceID)
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var req CreateCRMFollowUpIssueRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = "Follow up: " + account.Name
	}
	priority := "none"
	if req.Priority != nil && strings.TrimSpace(*req.Priority) != "" {
		priority = strings.TrimSpace(*req.Priority)
	}
	if priority != "none" && priority != "low" && priority != "medium" && priority != "high" && priority != "urgent" {
		writeError(w, http.StatusBadRequest, "invalid priority")
		return
	}
	var assigneeType pgtype.Text
	var assigneeID pgtype.UUID
	if req.AssigneeType != nil && strings.TrimSpace(*req.AssigneeType) != "" {
		assigneeType = pgtype.Text{String: strings.TrimSpace(*req.AssigneeType), Valid: true}
	}
	if req.AssigneeID != nil && strings.TrimSpace(*req.AssigneeID) != "" {
		assigneeID, ok = parseUUIDOrBadRequest(w, strings.TrimSpace(*req.AssigneeID), "assignee_id")
		if !ok {
			return
		}
	}
	if status, msg := h.validateAssigneePair(r.Context(), r, h.resolveWorkspaceID(r), assigneeType, assigneeID); status != 0 {
		writeError(w, status, msg)
		return
	}
	var projectID pgtype.UUID
	if req.ProjectID != nil && strings.TrimSpace(*req.ProjectID) != "" {
		projectID, ok = parseUUIDOrBadRequest(w, strings.TrimSpace(*req.ProjectID), "project_id")
		if !ok {
			return
		}
		if _, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{ID: projectID, WorkspaceID: workspaceID}); err != nil {
			writeError(w, http.StatusBadRequest, "project not found in this workspace")
			return
		}
	}
	var dueDate pgtype.Timestamptz
	if req.DueDate != nil && strings.TrimSpace(*req.DueDate) != "" {
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(*req.DueDate))
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid due_date format, expected RFC3339")
			return
		}
		dueDate = pgtype.Timestamptz{Time: parsed, Valid: true}
	}
	description := strings.TrimSpace("CRM follow-up for " + account.Name)
	if req.Description != nil && strings.TrimSpace(*req.Description) != "" {
		description = strings.TrimSpace(*req.Description)
	}
	creatorType, actualCreatorID := h.resolveActor(r, userID, uuidToString(workspaceID))
	creatorUUID := parseUUID(actualCreatorID)
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create follow-up issue")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	issueNumber, err := qtx.IncrementIssueCounter(r.Context(), workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create follow-up issue")
		return
	}
	issue, err := qtx.CreateIssueWithOrigin(r.Context(), db.CreateIssueWithOriginParams{
		WorkspaceID:  workspaceID,
		Title:        title,
		Description:  pgtype.Text{String: description, Valid: description != ""},
		Status:       "todo",
		Priority:     priority,
		AssigneeType: assigneeType,
		AssigneeID:   assigneeID,
		CreatorType:  creatorType,
		CreatorID:    creatorUUID,
		Position:     0,
		DueDate:      dueDate,
		Number:       issueNumber,
		ProjectID:    projectID,
		OriginType:   pgtype.Text{String: "crm_account", Valid: true},
		OriginID:     account.ID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create follow-up issue")
		return
	}
	if _, err := tx.Exec(r.Context(), `
		INSERT INTO crm_entity_link (workspace_id, crm_entity_type, crm_entity_id, target_type, target_id, relation_type, created_by)
		VALUES ($1, 'account', $2, 'issue', $3, 'follow_up_for', $4)
		ON CONFLICT DO NOTHING
	`, workspaceID, accountID, issue.ID, creatorUUID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to link follow-up issue")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create follow-up issue")
		return
	}
	prefix := h.getIssuePrefix(r.Context(), workspaceID)
	writeJSON(w, http.StatusCreated, CRMFollowUpIssueResponse{Issue: issueToResponse(issue, prefix)})
}
