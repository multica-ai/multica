package handler

import (
	"context"
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
	ID               string   `json:"id"`
	WorkspaceID      string   `json:"workspace_id"`
	AccountID        *string  `json:"account_id"`
	ContactID        *string  `json:"contact_id"`
	ProjectID        *string  `json:"project_id"`
	IssueID          *string  `json:"issue_id"`
	IssueIDs         []string `json:"issue_ids"`
	Subject          string   `json:"subject"`
	ExternalThreadID *string  `json:"external_thread_id"`
	Mailbox          *string  `json:"mailbox"`
	Direction        string   `json:"direction"`
	Status           string   `json:"status"`
	LastMessageAt    *string  `json:"last_message_at"`
	MessageCount     int64    `json:"message_count"`
	CreatedAt        string   `json:"created_at"`
	UpdatedAt        string   `json:"updated_at"`
}

type crmEmailThreadRow struct {
	ID               pgtype.UUID
	WorkspaceID      pgtype.UUID
	AccountID        pgtype.UUID
	ContactID        pgtype.UUID
	ProjectID        pgtype.UUID
	IssueID          pgtype.UUID
	IssueIDs         []pgtype.UUID
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

type UpdateCRMEmailThreadAssociationRequest struct {
	AccountID *string  `json:"account_id"`
	ContactID *string  `json:"contact_id"`
	ProjectID *string  `json:"project_id"`
	IssueID   *string  `json:"issue_id"`
	IssueIDs  []string `json:"issue_ids"`
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

func uuidSliceToStrings(values []pgtype.UUID) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value.Valid {
			out = append(out, uuidToString(value))
		}
	}
	return out
}

func (h *Handler) loadCRMEmailThreadIssueIDs(ctx context.Context, threadID pgtype.UUID) []pgtype.UUID {
	rows, err := h.DB.Query(ctx, `SELECT issue_id FROM crm_email_thread_issue_link WHERE thread_id = $1 ORDER BY created_at ASC`, threadID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var ids []pgtype.UUID
	for rows.Next() {
		var id pgtype.UUID
		if err := rows.Scan(&id); err == nil {
			ids = append(ids, id)
		}
	}
	return ids
}

func crmEmailThreadToResponse(row crmEmailThreadRow) CRMEmailThreadResponse {
	return CRMEmailThreadResponse{
		ID:               uuidToString(row.ID),
		WorkspaceID:      uuidToString(row.WorkspaceID),
		AccountID:        uuidToPtr(row.AccountID),
		ContactID:        uuidToPtr(row.ContactID),
		ProjectID:        uuidToPtr(row.ProjectID),
		IssueID:          uuidToPtr(row.IssueID),
		IssueIDs:         uuidSliceToStrings(row.IssueIDs),
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

type CRMIMAPSettingResponse struct {
	ID              string  `json:"id"`
	WorkspaceID     string  `json:"workspace_id"`
	Label           string  `json:"label"`
	Email           string  `json:"email"`
	Host            string  `json:"host"`
	Port            int32   `json:"port"`
	TLSMode         string  `json:"tls_mode"`
	Username        string  `json:"username"`
	SecretRef       *string `json:"secret_ref"`
	SyncEnabled     bool    `json:"sync_enabled"`
	LastTestStatus  *string `json:"last_test_status"`
	LastTestMessage *string `json:"last_test_message"`
	LastTestedAt    *string `json:"last_tested_at"`
	OwnerType       *string `json:"owner_type"`
	OwnerID         *string `json:"owner_id"`
	SMTPHost        *string `json:"smtp_host"`
	SMTPPort        *int32  `json:"smtp_port"`
	SMTPTLSMode     *string `json:"smtp_tls_mode"`
	SMTPUsername    *string `json:"smtp_username"`
	SMTPSecretRef   *string `json:"smtp_secret_ref"`
	CreatedAt       string  `json:"created_at"`
	UpdatedAt       string  `json:"updated_at"`
}

type UpsertCRMIMAPSettingRequest struct {
	ID            *string `json:"id"`
	Label         string  `json:"label"`
	Email         string  `json:"email"`
	Host          string  `json:"host"`
	Port          int32   `json:"port"`
	TLSMode       string  `json:"tls_mode"`
	Username      string  `json:"username"`
	SecretRef     *string `json:"secret_ref"`
	Secret        *string `json:"secret"`
	SyncEnabled   bool    `json:"sync_enabled"`
	OwnerType     *string `json:"owner_type"`
	OwnerID       *string `json:"owner_id"`
	SMTPHost      *string `json:"smtp_host"`
	SMTPPort      *int32  `json:"smtp_port"`
	SMTPTLSMode   *string `json:"smtp_tls_mode"`
	SMTPUsername  *string `json:"smtp_username"`
	SMTPSecretRef *string `json:"smtp_secret_ref"`
	SMTPSecret    *string `json:"smtp_secret"`
}

type CRMIMAPPreviewRequest struct {
	MailboxID *string `json:"mailbox_id"`
	Folder    *string `json:"folder"`
	Limit     int     `json:"limit"`
	RangeDays int     `json:"range_days"`
}

type CRMIMAPImportRequest struct {
	MailboxID *string  `json:"mailbox_id"`
	Folder    *string  `json:"folder"`
	UIDs      []string `json:"uids"`
	Limit     int      `json:"limit"`
	RangeDays int      `json:"range_days"`
}

type CRMIMAPPreviewMessageResponse struct {
	UID               string   `json:"uid"`
	ExternalMessageID string   `json:"external_message_id"`
	Subject           string   `json:"subject"`
	FromEmail         string   `json:"from_email"`
	FromName          string   `json:"from_name"`
	ToEmails          []string `json:"to_emails"`
	CcEmails          []string `json:"cc_emails"`
	ReceivedAt        *string  `json:"received_at"`
	Snippet           string   `json:"snippet"`
	RawSize           int      `json:"raw_size"`
}

type CRMEmailDraftResponse struct {
	ID          string   `json:"id"`
	MailboxID   *string  `json:"mailbox_id"`
	ThreadID    *string  `json:"thread_id"`
	ToEmails    []string `json:"to_emails"`
	CcEmails    []string `json:"cc_emails"`
	BccEmails   []string `json:"bcc_emails"`
	Subject     string   `json:"subject"`
	BodyText    string   `json:"body_text"`
	Status      string   `json:"status"`
	AIGenerated bool     `json:"ai_generated"`
	CreatedAt   string   `json:"created_at"`
	UpdatedAt   string   `json:"updated_at"`
}

type CreateCRMEmailDraftRequest struct {
	MailboxID   *string  `json:"mailbox_id"`
	ThreadID    *string  `json:"thread_id"`
	ToEmails    []string `json:"to_emails"`
	CcEmails    []string `json:"cc_emails"`
	BccEmails   []string `json:"bcc_emails"`
	Subject     string   `json:"subject"`
	BodyText    string   `json:"body_text"`
	AIGenerated bool     `json:"ai_generated"`
}
type CRMProfileSuggestionResponse struct {
	ID          string          `json:"id"`
	WorkspaceID string          `json:"workspace_id"`
	AccountID   string          `json:"account_id"`
	Summary     *string         `json:"summary"`
	ProfileJSON json.RawMessage `json:"profile_json"`
	SourceCount int32           `json:"source_count"`
	Status      string          `json:"status"`
	CreatedAt   string          `json:"created_at"`
	AppliedAt   *string         `json:"applied_at"`
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

func validCRMAccountRating(value string) bool {
	switch value {
	case "hot", "warm", "cold", "unknown":
		return true
	default:
		return false
	}
}

func validCRMAccountPriority(value string) bool {
	switch value {
	case "high", "medium", "low":
		return true
	default:
		return false
	}
}

func validCRMAccountSource(value string) bool {
	switch value {
	case "manual", "email", "whatsapp", "website", "referral", "trade_show", "linkedin", "other":
		return true
	default:
		return false
	}
}

func validCRMFollowUpBucket(value string) bool {
	switch value {
	case "today", "next_7_days", "overdue", "none":
		return true
	default:
		return false
	}
}

func validCRMAccountSort(value string) bool {
	switch value {
	case "updated", "name", "next_follow_up", "priority_rating":
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
	query := r.URL.Query()
	search := strings.TrimSpace(query.Get("search"))
	status := strings.TrimSpace(query.Get("status"))
	rating := strings.TrimSpace(query.Get("rating"))
	priority := strings.TrimSpace(query.Get("priority"))
	countryCode := strings.TrimSpace(query.Get("country_code"))
	industry := strings.TrimSpace(query.Get("industry"))
	source := strings.TrimSpace(query.Get("source"))
	followUpBucket := strings.TrimSpace(query.Get("follow_up_bucket"))
	sort := strings.TrimSpace(query.Get("sort"))
	if sort == "" {
		sort = "updated"
	}
	if status != "" && !validCRMAccountStatus(status) {
		writeError(w, http.StatusBadRequest, "invalid account status")
		return
	}
	if rating != "" && !validCRMAccountRating(rating) {
		writeError(w, http.StatusBadRequest, "invalid account rating")
		return
	}
	if priority != "" && !validCRMAccountPriority(priority) {
		writeError(w, http.StatusBadRequest, "invalid account priority")
		return
	}
	if source != "" && !validCRMAccountSource(source) {
		writeError(w, http.StatusBadRequest, "invalid account source")
		return
	}
	if followUpBucket != "" && !validCRMFollowUpBucket(followUpBucket) {
		writeError(w, http.StatusBadRequest, "invalid follow up bucket")
		return
	}
	if !validCRMAccountSort(sort) {
		writeError(w, http.StatusBadRequest, "invalid account sort")
		return
	}
	var searchArg pgtype.Text
	if search != "" {
		searchArg = pgtype.Text{String: normalizedCRMKey(search), Valid: true}
	}
	textArg := func(value string) pgtype.Text {
		if value == "" {
			return pgtype.Text{}
		}
		return pgtype.Text{String: value, Valid: true}
	}
	orderBy := "a.updated_at DESC, a.created_at DESC"
	switch sort {
	case "name":
		orderBy = "a.normalized_name ASC, a.name ASC"
	case "next_follow_up":
		orderBy = "a.next_follow_up_at ASC NULLS LAST, a.updated_at DESC"
	case "priority_rating":
		orderBy = "CASE a.priority WHEN 'high' THEN 1 WHEN 'medium' THEN 2 ELSE 3 END ASC, CASE a.rating WHEN 'hot' THEN 1 WHEN 'warm' THEN 2 WHEN 'cold' THEN 3 ELSE 4 END ASC, a.updated_at DESC"
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
		  AND ($4::text IS NULL OR a.rating = $4::text)
		  AND ($5::text IS NULL OR a.priority = $5::text)
		  AND ($6::text IS NULL OR a.country_code = $6::text OR a.country = $6::text)
		  AND ($7::text IS NULL OR a.industry = $7::text)
		  AND ($8::text IS NULL OR a.source = $8::text)
		  AND (
		    $9::text IS NULL
		    OR ($9::text = 'today' AND a.next_follow_up_at >= date_trunc('day', now()) AND a.next_follow_up_at < date_trunc('day', now()) + interval '1 day')
		    OR ($9::text = 'next_7_days' AND a.next_follow_up_at >= now() AND a.next_follow_up_at < now() + interval '7 days')
		    OR ($9::text = 'overdue' AND a.next_follow_up_at < now())
		    OR ($9::text = 'none' AND a.next_follow_up_at IS NULL)
		  )
		GROUP BY a.id
		ORDER BY `+orderBy+`
		LIMIT 100
	`, workspaceID, textArg(status), searchArg, textArg(rating), textArg(priority), textArg(countryCode), textArg(industry), textArg(source), textArg(followUpBucket))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list CRM accounts")
		return
	}
	defer rows.Close()
	accounts := []CRMAccountResponse{}
	for rows.Next() {
		account, err := h.scanCRMAccount(rows)
		if err != nil {
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
	_, _ = h.regenerateCRMAccountProfile(r.Context(), workspaceID, accountID)
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
		&thread.ID, &thread.WorkspaceID, &thread.AccountID, &thread.ContactID, &thread.ProjectID, &thread.IssueID,
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
		SELECT t.id, t.workspace_id, t.account_id, t.contact_id, t.project_id, t.issue_id, t.subject,
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
		SELECT t.id, t.workspace_id, t.account_id, t.contact_id, t.project_id, t.issue_id, t.subject,
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
		thread.IssueIDs = h.loadCRMEmailThreadIssueIDs(r.Context(), thread.ID)
		threads = append(threads, crmEmailThreadToResponse(thread))
	}
	writeJSON(w, http.StatusOK, map[string]any{"threads": threads, "total": len(threads)})
}

func (h *Handler) GetCRMEmailThread(w http.ResponseWriter, r *http.Request) {
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
	writeJSON(w, http.StatusOK, crmEmailThreadToResponse(thread))
}

func (h *Handler) UpdateCRMEmailThreadAssociation(w http.ResponseWriter, r *http.Request) {
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
	var req UpdateCRMEmailThreadAssociationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
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
	projectID, ok := optionalUUID(w, req.ProjectID, "project_id")
	if !ok {
		return
	}
	if projectID.Valid {
		if _, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{ID: projectID, WorkspaceID: workspaceID}); err != nil {
			writeError(w, http.StatusBadRequest, "project not found in this workspace")
			return
		}
	}
	issueID, ok := optionalUUID(w, req.IssueID, "issue_id")
	if !ok {
		return
	}
	issueIDs := make([]pgtype.UUID, 0, len(req.IssueIDs))
	if len(req.IssueIDs) == 0 && issueID.Valid {
		issueIDs = append(issueIDs, issueID)
	}
	for _, rawIssueID := range req.IssueIDs {
		parsed, ok := parseUUIDOrBadRequest(w, rawIssueID, "issue_id")
		if !ok {
			return
		}
		issueIDs = append(issueIDs, parsed)
	}
	for _, linkedIssueID := range issueIDs {
		issue, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{ID: linkedIssueID, WorkspaceID: workspaceID})
		if err != nil {
			writeError(w, http.StatusBadRequest, "issue not found in this workspace")
			return
		}
		if projectID.Valid && issue.ProjectID.Valid && issue.ProjectID.Bytes != projectID.Bytes {
			writeError(w, http.StatusBadRequest, "issue does not belong to selected project")
			return
		}
	}
	primaryIssueID := issueID
	if len(issueIDs) > 0 {
		primaryIssueID = issueIDs[0]
	}
	thread, err := h.scanCRMEmailThread(h.DB.QueryRow(r.Context(), `
		UPDATE crm_email_thread
		SET account_id = $3, contact_id = $4, project_id = $5, issue_id = $6, updated_at = now()
		WHERE id = $1 AND workspace_id = $2
		RETURNING id, workspace_id, account_id, contact_id, project_id, issue_id, subject, external_thread_id, mailbox, direction, status, last_message_at, created_at, updated_at,
		          (SELECT COUNT(*)::bigint FROM crm_email_message m WHERE m.thread_id = crm_email_thread.id AND m.workspace_id = crm_email_thread.workspace_id)
	`, threadID, workspaceID, accountID, contactID, projectID, primaryIssueID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update CRM email thread association")
		return
	}
	thread.IssueIDs = issueIDs
	if _, err := h.DB.Exec(r.Context(), `DELETE FROM crm_email_thread_issue_link WHERE thread_id = $1`, threadID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update CRM email issue links")
		return
	}
	for _, linkedIssueID := range issueIDs {
		if _, err := h.DB.Exec(r.Context(), `INSERT INTO crm_email_thread_issue_link (thread_id, issue_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, threadID, linkedIssueID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update CRM email issue links")
			return
		}
	}
	writeJSON(w, http.StatusOK, crmEmailThreadToResponse(thread))
}

func scanCRMIMAPSetting(row pgx.Row) (CRMIMAPSettingResponse, error) {
	var r CRMIMAPSettingResponse
	var id, ws pgtype.UUID
	var secretRef, status, msg, ownerType, smtpHost, smtpTLSMode, smtpUsername, smtpSecretRef pgtype.Text
	var ownerID pgtype.UUID
	var smtpPort pgtype.Int4
	var tested, created, updated pgtype.Timestamptz
	err := row.Scan(&id, &ws, &r.Label, &r.Email, &r.Host, &r.Port, &r.TLSMode, &r.Username, &secretRef, &r.SyncEnabled, &status, &msg, &tested, &ownerType, &ownerID, &smtpHost, &smtpPort, &smtpTLSMode, &smtpUsername, &smtpSecretRef, &created, &updated)
	r.ID = uuidToString(id)
	r.WorkspaceID = uuidToString(ws)
	r.SecretRef = textToPtr(secretRef)
	r.LastTestStatus = textToPtr(status)
	r.LastTestMessage = textToPtr(msg)
	r.LastTestedAt = timestampToPtr(tested)
	r.OwnerType = textToPtr(ownerType)
	r.OwnerID = uuidToPtr(ownerID)
	r.SMTPHost = textToPtr(smtpHost)
	if smtpPort.Valid {
		v := int32(smtpPort.Int32)
		r.SMTPPort = &v
	}
	r.SMTPTLSMode = textToPtr(smtpTLSMode)
	r.SMTPUsername = textToPtr(smtpUsername)
	r.SMTPSecretRef = textToPtr(smtpSecretRef)
	r.CreatedAt = timestampToString(created)
	r.UpdatedAt = timestampToString(updated)
	return r, err
}

func (h *Handler) ListCRMIMAPSettings(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := h.crmWorkspaceUUID(w, r)
	if !ok {
		return
	}
	rows, err := h.DB.Query(r.Context(), `SELECT id, workspace_id, label, email, host, port, tls_mode, username, secret_ref, sync_enabled, last_test_status, last_test_message, last_tested_at, owner_type, owner_id, smtp_host, smtp_port, smtp_tls_mode, smtp_username, smtp_secret_ref, created_at, updated_at FROM crm_imap_setting WHERE workspace_id=$1 ORDER BY updated_at DESC`, workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list CRM IMAP settings")
		return
	}
	defer rows.Close()
	settings := []CRMIMAPSettingResponse{}
	for rows.Next() {
		item, err := scanCRMIMAPSetting(rows)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to scan CRM IMAP setting")
			return
		}
		settings = append(settings, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"settings": settings, "total": len(settings)})
}

func (h *Handler) UpsertCRMIMAPSetting(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := h.crmWorkspaceUUID(w, r)
	if !ok {
		return
	}
	var req UpsertCRMIMAPSettingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	label := normalizeCRMName(req.Label)
	email := strings.TrimSpace(req.Email)
	host := strings.TrimSpace(req.Host)
	user := strings.TrimSpace(req.Username)
	tlsMode := cleanDefault(&req.TLSMode, "ssl")
	if label == "" || email == "" || host == "" || user == "" {
		writeError(w, http.StatusBadRequest, "label, email, host, and username are required")
		return
	}
	if req.Port <= 0 {
		req.Port = 993
	}
	secretRef := cleanOptionalText(req.SecretRef)
	if req.Secret != nil && strings.TrimSpace(*req.Secret) != "" {
		secretRef = pgtype.Text{String: encodeCRMIMAPInlineSecret(strings.TrimSpace(*req.Secret)), Valid: true}
	}
	if tlsMode != "ssl" && tlsMode != "starttls" && tlsMode != "none" {
		writeError(w, http.StatusBadRequest, "invalid tls_mode")
		return
	}
	ownerType := cleanOptionalText(req.OwnerType)
	ownerID, ok := optionalUUID(w, req.OwnerID, "owner_id")
	if !ok {
		return
	}
	smtpHost := cleanOptionalText(req.SMTPHost)
	smtpPort := pgtype.Int4{}
	if req.SMTPPort != nil && *req.SMTPPort > 0 {
		smtpPort = pgtype.Int4{Int32: *req.SMTPPort, Valid: true}
	}
	smtpTLSMode := cleanOptionalText(req.SMTPTLSMode)
	smtpUsername := cleanOptionalText(req.SMTPUsername)
	smtpSecretRef := cleanOptionalText(req.SMTPSecretRef)
	if req.SMTPSecret != nil && strings.TrimSpace(*req.SMTPSecret) != "" {
		smtpSecretRef = pgtype.Text{String: encodeCRMIMAPInlineSecret(strings.TrimSpace(*req.SMTPSecret)), Valid: true}
	}
	id, ok := optionalUUID(w, req.ID, "id")
	if !ok {
		return
	}
	var row pgx.Row
	if id.Valid {
		row = h.DB.QueryRow(r.Context(), `UPDATE crm_imap_setting SET label=$3,email=$4,host=$5,port=$6,tls_mode=$7,username=$8,secret_ref=$9,sync_enabled=$10,owner_type=$11,owner_id=$12,smtp_host=$13,smtp_port=$14,smtp_tls_mode=$15,smtp_username=$16,smtp_secret_ref=$17,updated_at=now() WHERE id=$1 AND workspace_id=$2 RETURNING id, workspace_id, label, email, host, port, tls_mode, username, secret_ref, sync_enabled, last_test_status, last_test_message, last_tested_at, owner_type, owner_id, smtp_host, smtp_port, smtp_tls_mode, smtp_username, smtp_secret_ref, created_at, updated_at`, id, workspaceID, label, email, host, req.Port, tlsMode, user, secretRef, req.SyncEnabled, ownerType, ownerID, smtpHost, smtpPort, smtpTLSMode, smtpUsername, smtpSecretRef)
	} else {
		row = h.DB.QueryRow(r.Context(), `INSERT INTO crm_imap_setting (workspace_id,label,email,host,port,tls_mode,username,secret_ref,sync_enabled,owner_type,owner_id,smtp_host,smtp_port,smtp_tls_mode,smtp_username,smtp_secret_ref) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16) RETURNING id, workspace_id, label, email, host, port, tls_mode, username, secret_ref, sync_enabled, last_test_status, last_test_message, last_tested_at, owner_type, owner_id, smtp_host, smtp_port, smtp_tls_mode, smtp_username, smtp_secret_ref, created_at, updated_at`, workspaceID, label, email, host, req.Port, tlsMode, user, secretRef, req.SyncEnabled, ownerType, ownerID, smtpHost, smtpPort, smtpTLSMode, smtpUsername, smtpSecretRef)
	}
	item, err := scanCRMIMAPSetting(row)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save CRM IMAP setting")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *Handler) TestCRMIMAPSetting(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := h.crmWorkspaceUUID(w, r)
	if !ok {
		return
	}
	mailboxID := chi.URLParam(r, "mailboxId")
	cfg, ok := h.loadCRMIMAPConfig(w, r, workspaceID, &mailboxID)
	if !ok {
		return
	}

	status := "ok"
	msg := "IMAP connection successful"
	if _, err := fetchCRMIMAPMessages(cfg, "INBOX", 1, 0, nil); err != nil {
		status = "failed"
		msg = "IMAP connection failed: " + err.Error()
	}
	_, _ = h.DB.Exec(r.Context(), `UPDATE crm_imap_setting SET last_test_status=$3,last_test_message=$4,last_tested_at=now(),updated_at=now() WHERE id=$1 AND workspace_id=$2`, cfg.UUID, workspaceID, status, msg)
	writeJSON(w, http.StatusOK, map[string]any{"ok": status == "ok", "status": status, "message": msg})
}

func (h *Handler) PreviewCRMIMAP(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := h.crmWorkspaceUUID(w, r)
	if !ok {
		return
	}
	var req CRMIMAPPreviewRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	limit := req.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	cfg, ok := h.loadCRMIMAPConfig(w, r, workspaceID, req.MailboxID)
	if !ok {
		return
	}
	messages, err := fetchCRMIMAPMessages(cfg, cleanCRMIMAPFolder(req.Folder), limit, req.RangeDays, nil)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to fetch IMAP messages: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": crmIMAPPreviewMessagesToResponse(messages), "total": len(messages), "limit": limit, "sync_enabled": false, "note": "Fetched live IMAP messages for manual preview; no messages imported yet."})
}

func (h *Handler) ImportCRMIMAP(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := h.crmWorkspaceUUID(w, r)
	if !ok {
		return
	}
	var req CRMIMAPImportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.UIDs) == 0 {
		writeError(w, http.StatusBadRequest, "uids are required")
		return
	}
	cfg, ok := h.loadCRMIMAPConfig(w, r, workspaceID, req.MailboxID)
	if !ok {
		return
	}
	messages, err := fetchCRMIMAPMessages(cfg, cleanCRMIMAPFolder(req.Folder), len(req.UIDs), 0, req.UIDs)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to fetch IMAP messages: "+err.Error())
		return
	}
	imported, skipped, err := h.importCRMIMAPMessages(r.Context(), workspaceID, cfg, messages)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to import IMAP messages")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "fetched": len(messages), "imported": imported, "skipped": skipped})
}

func (h *Handler) SyncCRMIMAP(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := h.crmWorkspaceUUID(w, r)
	if !ok {
		return
	}
	var req CRMIMAPImportRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	limit := req.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	cfg, ok := h.loadCRMIMAPConfig(w, r, workspaceID, req.MailboxID)
	if !ok {
		return
	}
	folder := cleanCRMIMAPFolder(req.Folder)
	var runID pgtype.UUID
	_ = h.DB.QueryRow(r.Context(), `INSERT INTO crm_imap_sync_run (workspace_id, mailbox_id, folder, requested_limit) VALUES ($1,$2,$3,$4) RETURNING id`, workspaceID, cfg.UUID, folder, limit).Scan(&runID)
	messages, err := fetchCRMIMAPMessages(cfg, folder, limit, req.RangeDays, nil)
	if err != nil {
		_, _ = h.DB.Exec(r.Context(), `UPDATE crm_imap_sync_run SET status='failed', error_message=$2, finished_at=now(), updated_at=now() WHERE id=$1`, runID, err.Error())
		writeError(w, http.StatusBadGateway, "failed to fetch IMAP messages: "+err.Error())
		return
	}
	imported, skipped, err := h.importCRMIMAPMessages(r.Context(), workspaceID, cfg, messages)
	if err != nil {
		_, _ = h.DB.Exec(r.Context(), `UPDATE crm_imap_sync_run SET status='failed', fetched_count=$2, error_message=$3, finished_at=now(), updated_at=now() WHERE id=$1`, runID, len(messages), err.Error())
		writeError(w, http.StatusInternalServerError, "failed to import IMAP messages")
		return
	}
	_, _ = h.DB.Exec(r.Context(), `UPDATE crm_imap_sync_run SET status='ok', fetched_count=$2, imported_count=$3, skipped_count=$4, finished_at=now(), updated_at=now() WHERE id=$1`, runID, len(messages), imported, skipped)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "run_id": uuidToString(runID), "fetched": len(messages), "imported": imported, "skipped": skipped})
}

func cleanCRMIMAPFolder(value *string) string {
	if value == nil || strings.TrimSpace(*value) == "" || strings.EqualFold(strings.TrimSpace(*value), "inbox") {
		return "INBOX"
	}
	return strings.TrimSpace(*value)
}

func (h *Handler) loadCRMIMAPConfig(w http.ResponseWriter, r *http.Request, workspaceID pgtype.UUID, mailboxIDValue *string) (crmIMAPMailboxConfig, bool) {
	mailboxID, ok := optionalUUID(w, mailboxIDValue, "mailbox_id")
	if !ok {
		return crmIMAPMailboxConfig{}, false
	}
	query := `SELECT id, label, email, host, port, tls_mode, username, secret_ref, owner_type, owner_id, smtp_host, smtp_port, smtp_tls_mode, smtp_username, smtp_secret_ref FROM crm_imap_setting WHERE workspace_id=$1`
	args := []any{workspaceID}
	if mailboxID.Valid {
		query += ` AND id=$2`
		args = append(args, mailboxID)
	}
	query += ` ORDER BY updated_at DESC LIMIT 1`
	var cfg crmIMAPMailboxConfig
	var id pgtype.UUID
	var secretRef, ownerType, smtpHost, smtpTLSMode, smtpUsername, smtpSecretRef pgtype.Text
	var ownerID pgtype.UUID
	var smtpPort pgtype.Int4
	if err := h.DB.QueryRow(r.Context(), query, args...).Scan(&id, &cfg.Label, &cfg.Email, &cfg.Host, &cfg.Port, &cfg.TLSMode, &cfg.Username, &secretRef, &ownerType, &ownerID, &smtpHost, &smtpPort, &smtpTLSMode, &smtpUsername, &smtpSecretRef); err != nil {
		writeError(w, http.StatusNotFound, "CRM IMAP setting not found")
		return crmIMAPMailboxConfig{}, false
	}
	cfg.UUID = id
	cfg.ID = uuidToString(id)
	cfg.SecretRef = crmTextValue(secretRef)
	cfg.OwnerType = crmTextValue(ownerType)
	cfg.OwnerID = uuidToString(ownerID)
	cfg.SMTPHost = crmTextValue(smtpHost)
	if smtpPort.Valid {
		cfg.SMTPPort = smtpPort.Int32
	}
	cfg.SMTPTLSMode = crmTextValue(smtpTLSMode)
	cfg.SMTPUsername = crmTextValue(smtpUsername)
	cfg.SMTPSecretRef = crmTextValue(smtpSecretRef)
	return cfg, true
}

func crmIMAPPreviewMessagesToResponse(messages []crmIMAPFetchedMessage) []CRMIMAPPreviewMessageResponse {
	out := make([]CRMIMAPPreviewMessageResponse, 0, len(messages))
	for _, message := range messages {
		var receivedAt *string
		if !message.Date.IsZero() {
			value := message.Date.UTC().Format(time.RFC3339)
			receivedAt = &value
		}
		out = append(out, CRMIMAPPreviewMessageResponse{
			UID: message.UID, ExternalMessageID: message.MessageID, Subject: message.Subject,
			FromEmail: message.FromEmail, FromName: message.FromName, ToEmails: message.ToEmails,
			CcEmails: message.CcEmails, ReceivedAt: receivedAt, Snippet: message.Snippet, RawSize: message.RawSize,
		})
	}
	return out
}

func (h *Handler) importCRMIMAPMessages(ctx context.Context, workspaceID pgtype.UUID, cfg crmIMAPMailboxConfig, messages []crmIMAPFetchedMessage) (int, int, error) {
	imported := 0
	skipped := 0
	for _, message := range messages {
		externalID := message.MessageID
		if externalID == "" {
			externalID = cfg.ID + ":" + message.UID
		}
		var exists bool
		if err := h.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM crm_email_message WHERE workspace_id=$1 AND external_message_id=$2)`, workspaceID, externalID).Scan(&exists); err != nil {
			return imported, skipped, err
		}
		if exists {
			skipped++
			continue
		}
		subject := strings.TrimSpace(message.Subject)
		if subject == "" {
			subject = "(no subject)"
		}
		var threadID pgtype.UUID
		threadExternalID := cfg.ID + ":" + subject
		if err := h.DB.QueryRow(ctx, `SELECT id FROM crm_email_thread WHERE workspace_id=$1 AND external_thread_id=$2 LIMIT 1`, workspaceID, threadExternalID).Scan(&threadID); err != nil {
			if !errors.Is(err, pgx.ErrNoRows) {
				return imported, skipped, err
			}
			lastAt := pgtype.Timestamptz{}
			if !message.Date.IsZero() {
				lastAt = pgtype.Timestamptz{Time: message.Date, Valid: true}
			}
			if err := h.DB.QueryRow(ctx, `INSERT INTO crm_email_thread (workspace_id, subject, external_thread_id, mailbox, direction, status, last_message_at) VALUES ($1,$2,$3,$4,'inbound','open',$5) RETURNING id`, workspaceID, subject, threadExternalID, cfg.Email, lastAt).Scan(&threadID); err != nil {
				return imported, skipped, err
			}
		}
		receivedAt := pgtype.Timestamptz{}
		if !message.Date.IsZero() {
			receivedAt = pgtype.Timestamptz{Time: message.Date, Valid: true}
		}
		_, err := h.DB.Exec(ctx, `INSERT INTO crm_email_message (workspace_id, thread_id, external_message_id, from_email, from_name, to_emails, cc_emails, subject, received_at, body_text, body_html, snippet, direction) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,'inbound')`, workspaceID, threadID, externalID, cleanOptionalText(&message.FromEmail), cleanOptionalText(&message.FromName), message.ToEmails, message.CcEmails, cleanOptionalText(&subject), receivedAt, cleanOptionalText(&message.BodyText), cleanOptionalText(&message.BodyHTML), cleanOptionalText(&message.Snippet))
		if err != nil {
			return imported, skipped, err
		}
		_, _ = h.DB.Exec(ctx, `UPDATE crm_email_thread SET last_message_at=COALESCE($3,last_message_at,now()), updated_at=now() WHERE id=$1 AND workspace_id=$2`, threadID, workspaceID, receivedAt)
		imported++
	}
	return imported, skipped, nil
}

func (h *Handler) ListCRMEmailDrafts(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := h.crmWorkspaceUUID(w, r)
	if !ok {
		return
	}
	rows, err := h.DB.Query(r.Context(), `SELECT id, mailbox_id, thread_id, to_emails, cc_emails, bcc_emails, subject, body_text, status, ai_generated, created_at, updated_at FROM crm_email_draft WHERE workspace_id=$1 ORDER BY updated_at DESC LIMIT 100`, workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list CRM email drafts")
		return
	}
	defer rows.Close()
	items := []CRMEmailDraftResponse{}
	for rows.Next() {
		var id, mailboxID, threadID pgtype.UUID
		var subject, body, status string
		var toEmails, ccEmails, bccEmails []string
		var ai bool
		var created, updated pgtype.Timestamptz
		if err := rows.Scan(&id, &mailboxID, &threadID, &toEmails, &ccEmails, &bccEmails, &subject, &body, &status, &ai, &created, &updated); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to scan CRM email draft")
			return
		}
		items = append(items, CRMEmailDraftResponse{ID: uuidToString(id), MailboxID: uuidToPtr(mailboxID), ThreadID: uuidToPtr(threadID), ToEmails: toEmails, CcEmails: ccEmails, BccEmails: bccEmails, Subject: subject, BodyText: body, Status: status, AIGenerated: ai, CreatedAt: timestampToString(created), UpdatedAt: timestampToString(updated)})
	}
	writeJSON(w, http.StatusOK, map[string]any{"drafts": items, "total": len(items)})
}

func (h *Handler) CreateCRMEmailDraft(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := h.crmWorkspaceUUID(w, r)
	if !ok {
		return
	}
	var req CreateCRMEmailDraftRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	mailboxID, ok := optionalUUID(w, req.MailboxID, "mailbox_id")
	if !ok {
		return
	}
	threadID, ok := optionalUUID(w, req.ThreadID, "thread_id")
	if !ok {
		return
	}
	var id pgtype.UUID
	if err := h.DB.QueryRow(r.Context(), `INSERT INTO crm_email_draft (workspace_id, mailbox_id, thread_id, to_emails, cc_emails, bcc_emails, subject, body_text, ai_generated) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id`, workspaceID, mailboxID, threadID, req.ToEmails, req.CcEmails, req.BccEmails, req.Subject, req.BodyText, req.AIGenerated).Scan(&id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create CRM email draft")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": uuidToString(id)})
}

func (h *Handler) SendCRMEmailDraft(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := h.crmWorkspaceUUID(w, r)
	if !ok {
		return
	}
	draftID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "draftId"), "draft_id")
	if !ok {
		return
	}
	var mailboxID, threadID pgtype.UUID
	var toEmails, ccEmails, bccEmails []string
	var subject, body string
	if err := h.DB.QueryRow(r.Context(), `SELECT mailbox_id, thread_id, to_emails, cc_emails, bcc_emails, subject, body_text FROM crm_email_draft WHERE id=$1 AND workspace_id=$2`, draftID, workspaceID).Scan(&mailboxID, &threadID, &toEmails, &ccEmails, &bccEmails, &subject, &body); err != nil {
		writeError(w, http.StatusNotFound, "CRM email draft not found")
		return
	}
	mailboxIDString := uuidToString(mailboxID)
	cfg, ok := h.loadCRMIMAPConfig(w, r, workspaceID, &mailboxIDString)
	if !ok {
		return
	}
	if err := sendCRMSMTP(cfg, toEmails, ccEmails, bccEmails, subject, body); err != nil {
		_, _ = h.DB.Exec(r.Context(), `UPDATE crm_email_draft SET status='failed', error_message=$3, updated_at=now() WHERE id=$1 AND workspace_id=$2`, draftID, workspaceID, err.Error())
		writeError(w, http.StatusBadGateway, "failed to send CRM email draft: "+err.Error())
		return
	}
	if threadID.Valid {
		_, _ = h.DB.Exec(r.Context(), `INSERT INTO crm_email_message (workspace_id, thread_id, direction, from_email, to_emails, cc_emails, bcc_emails, subject, body_text, sent_at) VALUES ($1,$2,'outbound',$3,$4,$5,$6,$7,$8,now()); UPDATE crm_email_thread SET direction='outbound', status='open', last_message_at=now(), message_count=message_count+1, updated_at=now() WHERE id=$2 AND workspace_id=$1`, workspaceID, threadID, cfg.Email, toEmails, ccEmails, bccEmails, subject, body)
	}
	_, _ = h.DB.Exec(r.Context(), `UPDATE crm_email_draft SET status='sent', sent_at=now(), updated_at=now() WHERE id=$1 AND workspace_id=$2`, draftID, workspaceID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": "sent"})
}

func crmTextValue(t pgtype.Text) string {
	if !t.Valid {
		return ""
	}
	return strings.TrimSpace(t.String)
}

func (h *Handler) regenerateCRMAccountProfile(ctx context.Context, workspaceID, accountID pgtype.UUID) (CRMAccountProfileResponse, error) {
	var name, status, rating, priority string
	var website, countryName, industry, notes pgtype.Text
	if err := h.DB.QueryRow(ctx, `SELECT name, status, rating, priority, website, country_name, industry, notes FROM crm_account WHERE id=$1 AND workspace_id=$2`, accountID, workspaceID).Scan(&name, &status, &rating, &priority, &website, &countryName, &industry, &notes); err != nil {
		return CRMAccountProfileResponse{}, err
	}
	rows, err := h.DB.Query(ctx, `SELECT channel, direction, COALESCE(subject,''), body FROM crm_communication_note WHERE account_id=$1 AND workspace_id=$2 ORDER BY occurred_at DESC, created_at DESC LIMIT 5`, accountID, workspaceID)
	if err != nil {
		return CRMAccountProfileResponse{}, err
	}
	defer rows.Close()
	communications := make([]string, 0, 5)
	for rows.Next() {
		var channel, direction, subject, body string
		if err := rows.Scan(&channel, &direction, &subject, &body); err == nil {
			line := strings.TrimSpace(strings.Join([]string{channel, direction, subject, body}, " "))
			if len(line) > 220 {
				line = line[:220]
			}
			communications = append(communications, line)
		}
	}
	country := crmTextValue(countryName)
	industryValue := crmTextValue(industry)
	baseParts := []string{name}
	if industryValue != "" {
		baseParts = append(baseParts, industryValue)
	}
	if country != "" {
		baseParts = append(baseParts, country)
	}
	summary := strings.TrimSpace(strings.Join(baseParts, " · "))
	if summary == "" {
		summary = "CRM customer profile"
	}
	if len(communications) > 0 {
		summary += "。最近往来：" + communications[0]
	}
	profile := map[string]any{
		"business_model":           strings.TrimSpace(strings.Join([]string{industryValue, crmTextValue(website)}, " ")),
		"main_products":            "根据客户基础信息和往来记录持续更新；请在后续沟通中补充具体产品。",
		"procurement_needs":        "结合最近往来跟进需求、数量、交期、目标价格和决策人。",
		"pain_points":              strings.Join(communications, "\n"),
		"decision_process":         "根据联系人、项目和历史往来持续归纳；优先确认决策链路和采购周期。",
		"communication_preference": "参考最近往来渠道和回复习惯安排跟进。",
		"risk_notes":               strings.TrimSpace(strings.Join([]string{crmTextValue(notes), "自动画像由客户信息和历史往来生成；新增往来或修改客户信息会自动刷新。"}, "\n")),
		"cooperation_history":      strings.Join(communications, "\n"),
		"rating_hint":              rating,
		"priority_hint":            priority,
		"status_hint":              status,
		"auto_generated":           true,
	}
	profileJSON, _ := json.Marshal(profile)
	var rawProfile []byte
	var updatedAt pgtype.Timestamptz
	if err := h.DB.QueryRow(ctx, `INSERT INTO crm_account_profile (workspace_id, account_id, summary, profile_json, updated_at) VALUES ($1,$2,$3,$4,now()) ON CONFLICT (account_id) DO UPDATE SET summary=EXCLUDED.summary, profile_json=EXCLUDED.profile_json, updated_at=now() RETURNING profile_json, updated_at`, workspaceID, accountID, summary, profileJSON).Scan(&rawProfile, &updatedAt); err != nil {
		return CRMAccountProfileResponse{}, err
	}
	return CRMAccountProfileResponse{WorkspaceID: uuidToString(workspaceID), AccountID: uuidToString(accountID), Summary: &summary, ProfileJSON: rawProfile, UpdatedAt: timestampToString(updatedAt)}, nil
}

func (h *Handler) SuggestCRMAccountProfile(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := h.crmWorkspaceUUID(w, r)
	if !ok {
		return
	}
	accountID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "accountId"), "account id")
	if !ok {
		return
	}
	profile, err := h.regenerateCRMAccountProfile(r.Context(), workspaceID, accountID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "CRM account not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to generate CRM profile")
		return
	}
	writeJSON(w, http.StatusOK, profile)
}

func (h *Handler) ApplyCRMAccountProfileSuggestion(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := h.crmWorkspaceUUID(w, r)
	if !ok {
		return
	}
	accountID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "accountId"), "account id")
	if !ok {
		return
	}
	suggestionID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "suggestionId"), "suggestion id")
	if !ok {
		return
	}
	var summary pgtype.Text
	var profile json.RawMessage
	if err := h.DB.QueryRow(r.Context(), `SELECT summary, profile_json FROM crm_profile_suggestion WHERE id=$1 AND workspace_id=$2 AND account_id=$3 AND status='draft'`, suggestionID, workspaceID, accountID).Scan(&summary, &profile); err != nil {
		writeError(w, http.StatusNotFound, "CRM profile suggestion not found")
		return
	}
	_, err := h.DB.Exec(r.Context(), `INSERT INTO crm_account_profile (workspace_id, account_id, summary, profile_json, updated_at) VALUES ($1,$2,$3,$4,now()) ON CONFLICT (account_id) DO UPDATE SET summary=EXCLUDED.summary, profile_json=crm_account_profile.profile_json || EXCLUDED.profile_json, updated_at=now(); UPDATE crm_profile_suggestion SET status='applied', applied_at=now() WHERE id=$5 AND workspace_id=$1`, workspaceID, accountID, summary, profile, suggestionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to apply CRM profile suggestion")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
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
		RETURNING id, workspace_id, account_id, contact_id, project_id, issue_id, subject, external_thread_id, mailbox, direction, status, last_message_at, created_at, updated_at, 0::bigint
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
	_, _ = h.regenerateCRMAccountProfile(r.Context(), workspaceID, accountID)
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

type CRMAISettingResponse struct {
	WorkspaceID     string          `json:"workspace_id"`
	AutomationKey   string          `json:"automation_key"`
	Enabled         bool            `json:"enabled"`
	IntervalMinutes int32           `json:"interval_minutes"`
	AssigneeAgentID *string         `json:"assignee_agent_id"`
	MaxItemsPerRun  int32           `json:"max_items_per_run"`
	Config          json.RawMessage `json:"config"`
	LastResult      json.RawMessage `json:"last_result"`
	LastCheckedAt   *time.Time      `json:"last_checked_at"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

type UpdateCRMAISettingRequest struct {
	Enabled         *bool           `json:"enabled"`
	IntervalMinutes *int32          `json:"interval_minutes"`
	AssigneeAgentID *string         `json:"assignee_agent_id"`
	MaxItemsPerRun  *int32          `json:"max_items_per_run"`
	Config          json.RawMessage `json:"config"`
}

func defaultCRMAISettings(workspaceID pgtype.UUID) []CRMAISettingResponse {
	now := time.Now().UTC()
	return []CRMAISettingResponse{
		{WorkspaceID: uuidToString(workspaceID), AutomationKey: "email_pending_reply", Enabled: true, IntervalMinutes: 5, MaxItemsPerRun: 5, Config: json.RawMessage(`{}`), LastResult: json.RawMessage(`{}`), CreatedAt: now, UpdatedAt: now},
		{WorkspaceID: uuidToString(workspaceID), AutomationKey: "due_followup", Enabled: true, IntervalMinutes: 15, MaxItemsPerRun: 10, Config: json.RawMessage(`{}`), LastResult: json.RawMessage(`{}`), CreatedAt: now, UpdatedAt: now},
	}
}

func (h *Handler) ListCRMAISettings(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := h.crmWorkspaceUUID(w, r)
	if !ok {
		return
	}

	rows, err := h.DB.Query(r.Context(), `
		WITH defaults AS (
			SELECT $1::uuid AS workspace_id, 'email_pending_reply'::text AS automation_key, true AS enabled, 5::int AS interval_minutes, NULL::uuid AS assignee_agent_id, 5::int AS max_items_per_run, '{}'::jsonb AS config
			UNION ALL
			SELECT $1::uuid, 'due_followup'::text, true, 15::int, NULL::uuid, 10::int, '{}'::jsonb
		)
		SELECT d.workspace_id, d.automation_key,
		       COALESCE(s.enabled, d.enabled) AS enabled,
		       COALESCE(s.interval_minutes, d.interval_minutes) AS interval_minutes,
		       COALESCE(s.assignee_agent_id, d.assignee_agent_id) AS assignee_agent_id,
		       COALESCE(s.max_items_per_run, d.max_items_per_run) AS max_items_per_run,
		       COALESCE(s.config, d.config) AS config,
		       COALESCE(s.last_result, '{}'::jsonb) AS last_result,
		       s.last_checked_at,
		       COALESCE(s.created_at, now()) AS created_at,
		       COALESCE(s.updated_at, now()) AS updated_at
		FROM defaults d
		LEFT JOIN crm_ai_setting s ON s.workspace_id = d.workspace_id AND s.automation_key = d.automation_key
		ORDER BY CASE d.automation_key WHEN 'email_pending_reply' THEN 1 ELSE 2 END`, workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load CRM AI settings")
		return
	}
	defer rows.Close()

	settings := make([]CRMAISettingResponse, 0, 2)
	for rows.Next() {
		var item CRMAISettingResponse
		var assignee pgtype.UUID
		var lastChecked pgtype.Timestamptz
		var config []byte
		var lastResult []byte
		if err := rows.Scan(&item.WorkspaceID, &item.AutomationKey, &item.Enabled, &item.IntervalMinutes, &assignee, &item.MaxItemsPerRun, &config, &lastResult, &lastChecked, &item.CreatedAt, &item.UpdatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to scan CRM AI settings")
			return
		}
		if assignee.Valid {
			v := uuidToString(assignee)
			item.AssigneeAgentID = &v
		}
		if lastChecked.Valid {
			t := lastChecked.Time
			item.LastCheckedAt = &t
		}
		item.Config = json.RawMessage(config)
		item.LastResult = json.RawMessage(lastResult)
		settings = append(settings, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"settings": settings})
}

func (h *Handler) UpdateCRMAISetting(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := h.crmWorkspaceUUID(w, r)
	if !ok {
		return
	}
	key := chi.URLParam(r, "automationKey")
	if key != "email_pending_reply" && key != "due_followup" {
		writeError(w, http.StatusBadRequest, "invalid CRM AI setting key")
		return
	}
	var req UpdateCRMAISettingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	enabled := true
	intervalMinutes := int32(5)
	maxItems := int32(5)
	if key == "due_followup" {
		intervalMinutes = 15
		maxItems = 10
	}
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	if req.IntervalMinutes != nil {
		intervalMinutes = *req.IntervalMinutes
	}
	if intervalMinutes < 1 || intervalMinutes > 1440 {
		writeError(w, http.StatusBadRequest, "interval_minutes must be between 1 and 1440")
		return
	}
	if req.MaxItemsPerRun != nil {
		maxItems = *req.MaxItemsPerRun
	}
	if maxItems < 1 || maxItems > 100 {
		writeError(w, http.StatusBadRequest, "max_items_per_run must be between 1 and 100")
		return
	}
	config := json.RawMessage(`{}`)
	if len(req.Config) > 0 {
		config = req.Config
	}
	var assignee pgtype.UUID
	if req.AssigneeAgentID != nil && strings.TrimSpace(*req.AssigneeAgentID) != "" {
		parsed, parseErr := parseUUIDLoose(strings.TrimSpace(*req.AssigneeAgentID))
		if parseErr != nil {
			writeError(w, http.StatusBadRequest, "invalid assignee_agent_id")
			return
		}
		assignee = parsed
	}

	var item CRMAISettingResponse
	var assigneeOut pgtype.UUID
	var lastChecked pgtype.Timestamptz
	var configOut []byte
	var lastResultOut []byte
	err := h.DB.QueryRow(r.Context(), `
		INSERT INTO crm_ai_setting (workspace_id, automation_key, enabled, interval_minutes, assignee_agent_id, max_items_per_run, config)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb)
		ON CONFLICT (workspace_id, automation_key) DO UPDATE SET
		  enabled = EXCLUDED.enabled,
		  interval_minutes = EXCLUDED.interval_minutes,
		  assignee_agent_id = EXCLUDED.assignee_agent_id,
		  max_items_per_run = EXCLUDED.max_items_per_run,
		  config = EXCLUDED.config,
		  updated_at = now()
		RETURNING workspace_id, automation_key, enabled, interval_minutes, assignee_agent_id, max_items_per_run, config, COALESCE(last_result, '{}'::jsonb), last_checked_at, created_at, updated_at`,
		workspaceID, key, enabled, intervalMinutes, assignee, maxItems, string(config)).Scan(&item.WorkspaceID, &item.AutomationKey, &item.Enabled, &item.IntervalMinutes, &assigneeOut, &item.MaxItemsPerRun, &configOut, &lastResultOut, &lastChecked, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save CRM AI setting")
		return
	}
	if assigneeOut.Valid {
		v := uuidToString(assigneeOut)
		item.AssigneeAgentID = &v
	}
	if lastChecked.Valid {
		t := lastChecked.Time
		item.LastCheckedAt = &t
	}
	item.Config = json.RawMessage(configOut)
	item.LastResult = json.RawMessage(lastResultOut)
	writeJSON(w, http.StatusOK, item)
}
