package composio

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
)

// --- Create link --------------------------------------------------------

// CreateLinkRequest is the body of POST /connected_accounts/link.
//
// Spec: https://docs.composio.dev/reference/api-reference/connected-accounts/postConnectedAccountsLink
type CreateLinkRequest struct {
	// AuthConfigID is the `ac_…` id of an auth config registered in your
	// Composio project (one per toolkit / OAuth client variant).
	AuthConfigID string `json:"auth_config_id"`

	// UserID is your own user identifier — Composio scopes the resulting
	// connected account by it.
	UserID string `json:"user_id"`

	// CallbackURL is where Composio sends the user after they finish the
	// hosted auth flow. Optional; Composio has a default landing page.
	CallbackURL string `json:"callback_url,omitempty"`

	// Alias is a human-readable label for the connection. Optional but useful
	// when the same user connects multiple accounts of the same toolkit.
	Alias string `json:"alias,omitempty"`

	// ConnectionData lets the caller pre-fill connection fields with default
	// values (per the Composio docs). Free-form to avoid coupling to the
	// scheme-specific child schemas.
	ConnectionData map[string]any `json:"connection_data,omitempty"`
}

// CreateLinkResponse is the body returned by POST /connected_accounts/link.
type CreateLinkResponse struct {
	LinkToken          string `json:"link_token"`
	RedirectURL        string `json:"redirect_url"`
	ExpiresAt          string `json:"expires_at"`
	ConnectedAccountID string `json:"connected_account_id"`
}

// CreateLink starts a hosted Composio Connect Link session. The redirect URL
// is what the caller should send the user to (popup, redirect, or
// SFSafariViewController).
func (c *Client) CreateLink(ctx context.Context, req CreateLinkRequest) (*CreateLinkResponse, error) {
	if req.AuthConfigID == "" {
		return nil, errors.New("composio: CreateLink: AuthConfigID is required")
	}
	if req.UserID == "" {
		return nil, errors.New("composio: CreateLink: UserID is required")
	}
	var out CreateLinkResponse
	if err := c.do(c.newRequest(ctx).SetBody(req), http.MethodPost, "/connected_accounts/link", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// --- List ---------------------------------------------------------------

// ListConnectedAccountsRequest collects the optional filters supported by
// GET /connected_accounts. Zero values are omitted from the query string.
//
// Per the Composio v3.1 spec all filters are plural array params: the SDK
// sends one query entry per slice element (`user_ids=u1&user_ids=u2`).
// Pass a single-element slice for the common "list by one user" case.
//
// Spec: https://docs.composio.dev/reference/api-reference/connected-accounts/getConnectedAccounts
type ListConnectedAccountsRequest struct {
	UserIDs             []string
	ToolkitSlugs        []string
	AuthConfigIDs       []string
	ConnectedAccountIDs []string
	Statuses            []string // ACTIVE, EXPIRED, INACTIVE, …
	OrderBy             string   // "created_at" (default) | "updated_at"
	OrderDirection      string   // "asc" | "desc" (default)
	AccountType         string   // experimental: PRIVATE | SHARED | ALL
	Limit               int      // 0 = use upstream default
	Cursor              string
}

// ConnectedAccount mirrors a subset of the Composio response shape. Only the
// fields actually consumed by the MVP are typed; extras live in Extra so
// callers can read them without an SDK update.
type ConnectedAccount struct {
	ID           string         `json:"id"`
	UserID       string         `json:"user_id"`
	AuthConfigID string         `json:"auth_config_id"`
	Toolkit      Toolkit        `json:"toolkit"`
	Status       string         `json:"status"`
	StatusReason string         `json:"status_reason,omitempty"`
	CreatedAt    string         `json:"created_at,omitempty"`
	UpdatedAt    string         `json:"updated_at,omitempty"`
	LastUsedAt   string         `json:"last_used_at,omitempty"`
	Extra        map[string]any `json:"-"`
}

// ListConnectedAccountsResponse is the typed paginated response.
type ListConnectedAccountsResponse struct {
	Items      []ConnectedAccount `json:"items"`
	NextCursor string             `json:"next_cursor,omitempty"`
	TotalItems int                `json:"total_items,omitempty"`
}

// ListConnectedAccounts returns the connections matching the supplied filters.
func (c *Client) ListConnectedAccounts(ctx context.Context, req ListConnectedAccountsRequest) (*ListConnectedAccountsResponse, error) {
	q := url.Values{}
	for _, v := range req.UserIDs {
		if v != "" {
			q.Add("user_ids", v)
		}
	}
	for _, v := range req.ToolkitSlugs {
		if v != "" {
			q.Add("toolkit_slugs", v)
		}
	}
	for _, v := range req.AuthConfigIDs {
		if v != "" {
			q.Add("auth_config_ids", v)
		}
	}
	for _, v := range req.ConnectedAccountIDs {
		if v != "" {
			q.Add("connected_account_ids", v)
		}
	}
	for _, v := range req.Statuses {
		if v != "" {
			q.Add("statuses", v)
		}
	}
	if req.OrderBy != "" {
		q.Set("order_by", req.OrderBy)
	}
	if req.OrderDirection != "" {
		q.Set("order_direction", req.OrderDirection)
	}
	if req.AccountType != "" {
		q.Set("account_type", req.AccountType)
	}
	if req.Limit > 0 {
		q.Set("limit", strconv.Itoa(req.Limit))
	}
	if req.Cursor != "" {
		q.Set("cursor", req.Cursor)
	}

	path := "/connected_accounts"
	if encoded := q.Encode(); encoded != "" {
		path += "?" + encoded
	}

	var out ListConnectedAccountsResponse
	if err := c.do(c.newRequest(ctx), http.MethodGet, path, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// --- Revoke / Delete ----------------------------------------------------

// RevokeConnection revokes the OAuth grant at the upstream provider but
// keeps the Composio record. Use this when the user disconnects and you
// want the provider-side tokens invalidated immediately.
func (c *Client) RevokeConnection(ctx context.Context, connectedAccountID string) error {
	if connectedAccountID == "" {
		return errors.New("composio: RevokeConnection: connectedAccountID is required")
	}
	return c.do(c.newRequest(ctx),
		http.MethodPost, "/connected_accounts/"+url.PathEscape(connectedAccountID)+"/revoke", nil)
}

// DeleteConnectedAccount removes the connection record from Composio. The
// provider tokens are NOT revoked by this call — call [Client.RevokeConnection]
// first if you need them invalidated upstream.
//
// Returns nil for 404 so callers can treat the operation as idempotent.
func (c *Client) DeleteConnectedAccount(ctx context.Context, connectedAccountID string) error {
	if connectedAccountID == "" {
		return errors.New("composio: DeleteConnectedAccount: connectedAccountID is required")
	}
	err := c.do(c.newRequest(ctx),
		http.MethodDelete, "/connected_accounts/"+url.PathEscape(connectedAccountID), nil)
	if err == nil {
		return nil
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.IsNotFound() {
		return nil
	}
	return err
}
