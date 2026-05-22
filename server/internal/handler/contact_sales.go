package handler

import (
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"net/mail"
	"net/netip"
	"slices"
	"strings"

	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Public, unauthenticated endpoint for the landing-page "Contact Sales"
// form. Spam is mitigated by:
//   - business-email validation (free providers blocked at the handler).
//   - a per-email hourly cap so a successful submission can't be replayed
//     into a flood from the same address.
//   - per-IP rate limiting applied at the router (RATE_LIMIT_CONTACT_SALES).
//
// Free-text fields are length-capped before they reach the DB, and the
// request body itself is bounded so an attacker can't POST megabytes of
// junk into the JSONB-free TEXT columns.
const (
	contactSalesMaxFirstName    = 80
	contactSalesMaxLastName     = 80
	contactSalesMaxEmail        = 254
	contactSalesMaxCompanyName  = 200
	contactSalesMaxGoals        = 2000
	contactSalesHourlyEmailCap  = 3
	contactSalesBodyLimit       = 16 * 1024
)

// contactSalesAllowedCompanySize is the closed enum the frontend dropdown
// emits. Keep this in sync with `apps/web/features/landing/i18n/types.ts`.
var contactSalesAllowedCompanySize = []string{
	"1-10",
	"11-50",
	"51-200",
	"201-500",
	"501-1000",
	"1000+",
}

// contactSalesAllowedUseCase mirrors the "How do you plan to use or
// collaborate with Multica?" dropdown.
var contactSalesAllowedUseCase = []string{
	"evaluate",
	"adopt_team",
	"self_host",
	"integrate",
	"partner",
	"other",
}

// freeEmailDomains are the personal-mail domains we reject up front. The
// frontend shows the same warning copy, but server-side enforcement is the
// authority — a hand-rolled curl request mustn't be able to bypass it.
var freeEmailDomains = map[string]struct{}{
	"gmail.com":      {},
	"googlemail.com": {},
	"outlook.com":    {},
	"hotmail.com":    {},
	"live.com":       {},
	"msn.com":        {},
	"yahoo.com":      {},
	"yahoo.co.uk":    {},
	"yahoo.co.jp":    {},
	"ymail.com":      {},
	"icloud.com":     {},
	"me.com":         {},
	"mac.com":        {},
	"aol.com":        {},
	"protonmail.com": {},
	"proton.me":      {},
	"pm.me":          {},
	"gmx.com":        {},
	"gmx.de":         {},
	"mail.com":       {},
	"zoho.com":       {},
	"yandex.com":     {},
	"yandex.ru":      {},
	"qq.com":         {},
	"163.com":        {},
	"126.com":        {},
	"sina.com":       {},
	"foxmail.com":    {},
}

type CreateContactSalesRequest struct {
	FirstName       string `json:"first_name"`
	LastName        string `json:"last_name"`
	BusinessEmail   string `json:"business_email"`
	CompanyName     string `json:"company_name"`
	CompanySize     string `json:"company_size"`
	CountryRegion   string `json:"country_region"`
	UseCase         string `json:"use_case"`
	Goals           string `json:"goals"`
	ConsentOutreach bool   `json:"consent_outreach"`
	ConsentUpdates  bool   `json:"consent_updates"`
}

type ContactSalesResponse struct {
	ID        string `json:"id"`
	CreatedAt string `json:"created_at"`
}

// CreateContactSales is the public POST /api/contact-sales endpoint.
func (h *Handler) CreateContactSales(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, contactSalesBodyLimit)
	var req CreateContactSalesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	firstName, ok := requireTrimmedField(w, req.FirstName, "first_name", contactSalesMaxFirstName)
	if !ok {
		return
	}
	lastName, ok := requireTrimmedField(w, req.LastName, "last_name", contactSalesMaxLastName)
	if !ok {
		return
	}
	companyName, ok := requireTrimmedField(w, req.CompanyName, "company_name", contactSalesMaxCompanyName)
	if !ok {
		return
	}

	email, ok := canonicalBusinessEmail(req.BusinessEmail)
	if !ok {
		writeError(w, http.StatusBadRequest, "business_email is invalid")
		return
	}
	if len(email) > contactSalesMaxEmail {
		writeError(w, http.StatusBadRequest, "business_email is too long")
		return
	}
	if !isBusinessEmailDomain(email) {
		writeError(w, http.StatusBadRequest, "please use a business email address")
		return
	}

	companySize := strings.TrimSpace(req.CompanySize)
	if !slices.Contains(contactSalesAllowedCompanySize, companySize) {
		writeError(w, http.StatusBadRequest, "company_size is invalid")
		return
	}

	countryRegion := strings.TrimSpace(req.CountryRegion)
	if countryRegion == "" {
		writeError(w, http.StatusBadRequest, "country_region is required")
		return
	}
	// Bound the dropdown value at a generous length — we accept any string
	// the frontend submits (country list is large) but cap it so an
	// attacker can't stuff the column.
	if len(countryRegion) > 80 {
		writeError(w, http.StatusBadRequest, "country_region is too long")
		return
	}

	useCase := strings.TrimSpace(req.UseCase)
	if !slices.Contains(contactSalesAllowedUseCase, useCase) {
		writeError(w, http.StatusBadRequest, "use_case is invalid")
		return
	}

	goals := strings.TrimSpace(req.Goals)
	if len(goals) > contactSalesMaxGoals {
		writeError(w, http.StatusBadRequest, "goals is too long")
		return
	}

	// Per-email hourly cap: keeps a single business address from being
	// used as a re-submit channel after one valid pass. DB-backed so it
	// works across replicas and survives a restart.
	count, err := h.Queries.CountRecentContactSalesByEmail(r.Context(), email)
	if err != nil {
		slog.Warn("count recent contact sales failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to submit inquiry")
		return
	}
	if count >= contactSalesHourlyEmailCap {
		writeError(w, http.StatusTooManyRequests, "too many recent inquiries from this email")
		return
	}

	inquiry, err := h.Queries.CreateContactSalesInquiry(r.Context(), db.CreateContactSalesInquiryParams{
		FirstName:       firstName,
		LastName:        lastName,
		BusinessEmail:   email,
		CompanyName:     companyName,
		CompanySize:     companySize,
		CountryRegion:   countryRegion,
		UseCase:         useCase,
		Goals:           goals,
		ConsentOutreach: req.ConsentOutreach,
		ConsentUpdates:  req.ConsentUpdates,
		UserAgent:       truncateString(r.UserAgent(), 512),
		SubmitterIp:     submitterIP(r),
	})
	if err != nil {
		slog.Warn("create contact sales failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to submit inquiry")
		return
	}

	inquiryID := uuidToString(inquiry.ID)
	slog.Info("contact sales submitted",
		append(logger.RequestAttrs(r),
			"inquiry_id", inquiryID,
			"company_size", companySize,
			"country_region", countryRegion,
			"use_case", useCase,
		)...)

	h.Analytics.Capture(analytics.ContactSalesSubmitted(
		inquiryID,
		companySize,
		countryRegion,
		useCase,
		goals != "",
	))

	writeJSON(w, http.StatusCreated, ContactSalesResponse{
		ID:        inquiryID,
		CreatedAt: timestampToString(inquiry.CreatedAt),
	})
}

func requireTrimmedField(w http.ResponseWriter, raw, field string, max int) (string, bool) {
	v := strings.TrimSpace(raw)
	if v == "" {
		writeError(w, http.StatusBadRequest, field+" is required")
		return "", false
	}
	if len(v) > max {
		writeError(w, http.StatusBadRequest, field+" is too long")
		return "", false
	}
	return v, true
}

func truncateString(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}

// canonicalBusinessEmail parses raw user input with net/mail and returns the
// canonical "local@domain" form (lower-cased, no display name, no surrounding
// whitespace). It is the only safe input to isBusinessEmailDomain — checking
// the raw string allows `Ada <ada@gmail.com>` to slip past the free-email
// block list because the parsed RFC 5322 address would have domain `gmail.com`
// while the raw "@" suffix would be `gmail.com>`.
func canonicalBusinessEmail(raw string) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", false
	}
	addr, err := mail.ParseAddress(trimmed)
	if err != nil {
		return "", false
	}
	email := strings.ToLower(strings.TrimSpace(addr.Address))
	// mail.ParseAddress also accepts forms like `<ada@example.com>` and
	// addresses with comments; addr.Address strips both. Require the
	// canonical local@domain shape with non-empty pieces on either side.
	at := strings.LastIndex(email, "@")
	if at <= 0 || at == len(email)-1 {
		return "", false
	}
	return email, true
}

func isBusinessEmailDomain(email string) bool {
	at := strings.LastIndex(email, "@")
	if at < 0 || at == len(email)-1 {
		return false
	}
	domain := strings.ToLower(email[at+1:])
	if _, blocked := freeEmailDomains[domain]; blocked {
		return false
	}
	return true
}

// submitterIP captures the raw connection IP so we can correlate abuse
// after the fact. We deliberately ignore X-Forwarded-For here — the
// router's rate-limit middleware already vets trusted-proxy headers; for
// the audit record we want the actual TCP peer, which is the conservative
// signal under deployments that don't enforce XFF stripping.
func submitterIP(r *http.Request) *netip.Addr {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return nil
	}
	return &addr
}
