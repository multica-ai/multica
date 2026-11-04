package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultTimeout      = 15 * time.Second
	defaultUploadTime   = 120 * time.Second
	defaultModelTimeout = 30 * time.Second
	maxResponseBytes    = 16 << 20
	apiPrefix           = "/api/v1"
)

// Config configures the HTTP adapter.
type Config struct {
	// BaseURL is how the backend reaches WeKnora, without a path, for example
	// "http://knowledge-app:8080".
	BaseURL string
	// PublicURL is how agents reach WeKnora for MCP. Defaults to BaseURL.
	PublicURL string
	// Email and Password are the service account that provisions tenants.
	Email    string
	Password string

	// HTTPClient is optional. Its Timeout is ignored: each call carries its own
	// deadline so uploads can run longer than lookups.
	HTTPClient *http.Client
	// Timeout bounds one ordinary request. Default 15s.
	Timeout time.Duration
	// UploadTimeout bounds a file or URL import request. Default 120s.
	UploadTimeout time.Duration
	// ModelReadyTimeout bounds the wait for a new embedding model to become
	// usable. Default 30s.
	ModelReadyTimeout time.Duration
	// ModelPollInterval is the wait between model status checks. Default 500ms.
	ModelPollInterval time.Duration
}

// Client is the HTTP adapter for WeKnora.
type Client struct {
	cfg  Config
	base *url.URL
	pub  *url.URL
	http *http.Client

	mu  sync.Mutex
	jwt string
}

var _ Knowledge = (*Client)(nil)

// New validates the configuration and returns a Client. It makes no request.
func New(cfg Config) (*Client, error) {
	base, err := parseBase(cfg.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("knowledge: base url: %w", err)
	}
	pub := base
	if strings.TrimSpace(cfg.PublicURL) != "" {
		if pub, err = parseBase(cfg.PublicURL); err != nil {
			return nil, fmt.Errorf("knowledge: public url: %w", err)
		}
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultTimeout
	}
	if cfg.UploadTimeout <= 0 {
		cfg.UploadTimeout = defaultUploadTime
	}
	if cfg.ModelReadyTimeout <= 0 {
		cfg.ModelReadyTimeout = defaultModelTimeout
	}
	if cfg.ModelPollInterval <= 0 {
		cfg.ModelPollInterval = 500 * time.Millisecond
	}
	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{}
	} else {
		copied := *hc
		hc = &copied
	}
	hc.Timeout = 0
	return &Client{cfg: cfg, base: base, pub: pub, http: hc}, nil
}

func parseBase(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimRight(strings.TrimSpace(raw), "/"))
	if err != nil {
		return nil, err
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, errors.New("must be an absolute http or https URL")
	}
	u.RawQuery, u.Fragment = "", ""
	return u, nil
}

// MCPURL implements Knowledge.
func (c *Client) MCPURL(endpointID string) string {
	return strings.TrimRight(c.pub.String(), "/") + "/mcp/" + url.PathEscape(endpointID)
}

// ---- transport ------------------------------------------------------------

type authMode int

const (
	authNone authMode = iota
	authJWT
	authKey
)

type request struct {
	op      string
	method  string
	path    string // under /api/v1, may carry a query string
	root    bool   // path is relative to the base URL, not /api/v1
	mode    authMode
	apiKey  string
	tenant  string // X-Tenant-ID, JWT mode only
	body    any
	raw     io.Reader
	rawType string
	timeout time.Duration
}

// do sends the request and decodes a successful JSON body into out (which may
// be nil). A JWT-mode 401 triggers one fresh login and one retry.
func (c *Client) do(ctx context.Context, r request, out any) error {
	if r.timeout <= 0 {
		r.timeout = c.cfg.Timeout
	}
	var payload []byte
	if r.body != nil {
		b, err := json.Marshal(r.body)
		if err != nil {
			return fmt.Errorf("knowledge: %s: encode request: %w", r.op, err)
		}
		payload = b
	}
	if r.raw != nil {
		// A multipart body is single-use, so buffer it once for the retry.
		b, err := io.ReadAll(r.raw)
		if err != nil {
			return fmt.Errorf("knowledge: %s: read upload: %w", r.op, err)
		}
		payload = b
	}

	retried := false
	for {
		var token string
		if r.mode == authJWT {
			t, err := c.token(ctx, false)
			if err != nil {
				return err
			}
			token = t
		}
		status, body, err := c.send(ctx, r, payload, token)
		if err != nil {
			return err
		}
		if status == http.StatusUnauthorized && r.mode == authJWT && !retried {
			retried = true
			if _, err := c.token(ctx, true); err != nil {
				return err
			}
			continue
		}
		if status >= 200 && status < 300 {
			if out == nil || len(bytes.TrimSpace(body)) == 0 {
				return nil
			}
			if err := json.Unmarshal(body, out); err != nil {
				return fmt.Errorf("knowledge: %s: decode response: %w", r.op, err)
			}
			return nil
		}
		return decodeError(r.op, status, body)
	}
}

func (c *Client) send(ctx context.Context, r request, payload []byte, token string) (int, []byte, error) {
	target := c.base.String()
	if r.root {
		target += r.path
	} else {
		target += apiPrefix + r.path
	}
	callCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	var rdr io.Reader
	if payload != nil {
		rdr = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(callCtx, r.method, target, rdr)
	if err != nil {
		return 0, nil, fmt.Errorf("knowledge: %s: %w", r.op, err)
	}
	req.Header.Set("Accept", "application/json")
	switch {
	case r.rawType != "":
		req.Header.Set("Content-Type", r.rawType)
	case payload != nil:
		req.Header.Set("Content-Type", "application/json")
	}
	switch r.mode {
	case authJWT:
		req.Header.Set("Authorization", "Bearer "+token)
		if r.tenant != "" {
			req.Header.Set("X-Tenant-ID", r.tenant)
		}
	case authKey:
		req.Header.Set("X-API-Key", r.apiKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return 0, nil, ctxErr
		}
		return 0, nil, fmt.Errorf("knowledge: %s: %w: %s", r.op, ErrUnavailable, scrubURL(err))
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return 0, nil, ctxErr
		}
		return 0, nil, fmt.Errorf("knowledge: %s: %w: read response: %v", r.op, ErrUnavailable, err)
	}
	return resp.StatusCode, body, nil
}

// scrubURL keeps the cause of a transport failure without the request URL,
// which can carry identifiers worth keeping out of logs.
func scrubURL(err error) string {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err.Error()
	}
	return err.Error()
}

// token returns the cached service-account JWT, logging in when there is none
// or when refresh is set.
func (c *Client) token(ctx context.Context, refresh bool) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.jwt != "" && !refresh {
		return c.jwt, nil
	}
	if c.cfg.Email == "" || c.cfg.Password == "" {
		return "", fmt.Errorf("knowledge: service account is not configured: %w", ErrAuth)
	}
	var resp struct {
		Success bool   `json:"success"`
		Token   string `json:"token"`
	}
	err := c.do(ctx, request{
		op: "login", method: http.MethodPost, path: "/auth/login", mode: authNone,
		body: map[string]string{"email": c.cfg.Email, "password": c.cfg.Password},
	}, &resp)
	if err != nil {
		c.jwt = ""
		if errors.Is(err, ErrAuth) || errors.Is(err, ErrForbidden) {
			return "", fmt.Errorf("knowledge: service account login rejected: %w", ErrAuth)
		}
		return "", err
	}
	if resp.Token == "" {
		return "", fmt.Errorf("knowledge: login response carried no token: %w", ErrAuth)
	}
	c.jwt = resp.Token
	return c.jwt, nil
}

// decodeError turns a non-2xx response into a typed error. WeKnora answers
// failures in three shapes: {"error":{"code","message"}}, {"error":"text"},
// and {"success":false,"message":"text"}; a duplicate carries its existing
// document in "data".
func decodeError(op string, status int, body []byte) error {
	var env struct {
		Error   json.RawMessage `json:"error"`
		Message string          `json:"message"`
		Code    json.RawMessage `json:"code"`
		Data    json.RawMessage `json:"data"`
	}
	_ = json.Unmarshal(body, &env)

	apiErr := &APIError{Status: status, Op: op, Message: env.Message}
	if len(env.Code) > 0 {
		apiErr.Code = strings.Trim(string(env.Code), `"`)
	}
	if len(env.Error) > 0 {
		var obj struct {
			Code    json.RawMessage `json:"code"`
			Message string          `json:"message"`
		}
		var text string
		switch {
		case json.Unmarshal(env.Error, &obj) == nil && (obj.Message != "" || len(obj.Code) > 0):
			apiErr.Message = obj.Message
			if len(obj.Code) > 0 {
				apiErr.Code = strings.Trim(string(obj.Code), `"`)
			}
		case json.Unmarshal(env.Error, &text) == nil:
			apiErr.Message = text
		}
	}

	switch {
	case status == http.StatusUnauthorized:
		apiErr.class = ErrAuth
	case status == http.StatusForbidden:
		apiErr.class = ErrForbidden
	case status == http.StatusNotFound:
		apiErr.class = ErrNotFound
	case status == http.StatusTooManyRequests:
		apiErr.class = ErrQuota
	case status == http.StatusBadRequest, status == http.StatusUnprocessableEntity:
		apiErr.class = ErrInvalid
	case status == http.StatusBadGateway, status == http.StatusServiceUnavailable, status == http.StatusGatewayTimeout:
		apiErr.class = ErrUnavailable
	case status == http.StatusConflict:
		dup := &DuplicateError{APIError: *apiErr}
		var d wireDocument
		if len(env.Data) > 0 && json.Unmarshal(env.Data, &d) == nil {
			dup.Existing = d.toDocument()
		}
		return dup
	}
	return apiErr
}

// ---- wire shapes ----------------------------------------------------------

// flexID reads an id that WeKnora sends as a number in some places and a
// string in others.
type flexID string

func (f *flexID) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(string(b))
	if s == "null" {
		*f = ""
		return nil
	}
	if strings.HasPrefix(s, `"`) {
		var v string
		if err := json.Unmarshal(b, &v); err != nil {
			return err
		}
		*f = flexID(v)
		return nil
	}
	*f = flexID(s)
	return nil
}

type wireTenant struct {
	ID          flexID `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type wireModel struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Type   string `json:"type"`
	Status string `json:"status"`
}

type wireKB struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Description      string `json:"description"`
	EmbeddingModelID string `json:"embedding_model_id"`
	KnowledgeCount   int    `json:"knowledge_count"`
	SummaryModelID   string `json:"summary_model_id"`
	// IndexingStrategy is sent back whole on update, so the flags Multica does
	// not own are read first and returned unchanged.
	IndexingStrategy wireStrategy `json:"indexing_strategy"`
}

type wireStrategy struct {
	VectorEnabled  bool `json:"vector_enabled"`
	KeywordEnabled bool `json:"keyword_enabled"`
	WikiEnabled    bool `json:"wiki_enabled"`
	GraphEnabled   bool `json:"graph_enabled"`
}

type wireDataSource struct {
	ID               string     `json:"id"`
	KnowledgeBaseID  string     `json:"knowledge_base_id"`
	Name             string     `json:"name"`
	Type             string     `json:"type"`
	Status           string     `json:"status"`
	SyncSchedule     string     `json:"sync_schedule"`
	LastSyncAt       *time.Time `json:"last_sync_at"`
	ErrorMessage     string     `json:"error_message"`
	TotalItemsSynced int64      `json:"total_items_synced"`
}

type wireDocument struct {
	ID              string `json:"id"`
	KnowledgeBaseID string `json:"knowledge_base_id"`
	Type            string `json:"type"`
	Title           string `json:"title"`
	Source          string `json:"source"`
	ParseStatus     string `json:"parse_status"`
	FileName        string `json:"file_name"`
	FolderPath      string `json:"folder_path"`
	ErrorMessage    string `json:"error_message"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
	Metadata        struct {
		Content string `json:"content"`
		Status  string `json:"status"`
	} `json:"metadata"`
}

func (w wireDocument) toDocument() Document {
	return Document{
		ID: w.ID, KnowledgeBaseID: w.KnowledgeBaseID, Title: w.Title,
		FileName: w.FileName, FolderPath: w.FolderPath, Type: w.Type,
		Source: w.Source, ParseStatus: w.ParseStatus, Error: w.ErrorMessage,
		CreatedAt: parseTime(w.CreatedAt), UpdatedAt: parseTime(w.UpdatedAt),
	}
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

type wireEndpoint struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Path             string   `json:"path"`
	Enabled          bool     `json:"enabled"`
	KnowledgeBaseIDs []string `json:"knowledge_base_ids"`
	Tools            []string `json:"tools"`
	Token            string   `json:"token"`
}

func (w wireEndpoint) toEndpoint() MCPEndpoint {
	return MCPEndpoint{
		ID: w.ID, Name: w.Name, Path: w.Path, Enabled: w.Enabled,
		KnowledgeBaseIDs: w.KnowledgeBaseIDs, Tools: w.Tools, Token: w.Token,
	}
}

// ---- Knowledge: platform scope --------------------------------------------

// Ping implements Knowledge.
func (c *Client) Ping(ctx context.Context) error {
	return c.do(ctx, request{op: "health", method: http.MethodGet, path: "/health", root: true, mode: authNone}, nil)
}

// EnsureTenant implements Knowledge.
func (c *Client) EnsureTenant(ctx context.Context, name, description string) (Tenant, error) {
	var me struct {
		Data struct {
			Memberships []struct {
				TenantID   flexID `json:"tenant_id"`
				TenantName string `json:"tenant_name"`
				Role       string `json:"role"`
			} `json:"memberships"`
		} `json:"data"`
	}
	if err := c.do(ctx, request{op: "list tenants", method: http.MethodGet, path: "/auth/me", mode: authJWT}, &me); err != nil {
		return Tenant{}, err
	}
	for _, m := range me.Data.Memberships {
		if m.TenantName == name && m.Role == "owner" {
			return Tenant{ID: string(m.TenantID), Name: name, Description: description}, nil
		}
	}

	var created struct {
		Data wireTenant `json:"data"`
	}
	err := c.do(ctx, request{
		op: "create tenant", method: http.MethodPost, path: "/tenants", mode: authJWT,
		body: map[string]string{"name": name, "description": description},
	}, &created)
	if err != nil {
		return Tenant{}, err
	}
	if created.Data.ID == "" {
		return Tenant{}, &APIError{Op: "create tenant", Status: http.StatusOK, Message: "response carried no tenant id"}
	}
	return Tenant{ID: string(created.Data.ID), Name: created.Data.Name, Description: created.Data.Description}, nil
}

// CreateWorkspaceKey implements Knowledge. The response carries two forms of
// the key: api_key is the stored, encrypted form and is useless to a caller;
// token is the usable "sk-" value. Only token is read.
func (c *Client) CreateWorkspaceKey(ctx context.Context, tenantID, name string) (string, error) {
	var resp struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	err := c.do(ctx, request{
		op: "create workspace key", method: http.MethodPost,
		path: "/tenants/" + url.PathEscape(tenantID) + "/api-keys", mode: authJWT, tenant: tenantID,
		body: map[string]any{"name": name, "full_access": true},
	}, &resp)
	if err != nil {
		return "", err
	}
	if resp.Data.Token == "" {
		return "", &APIError{Op: "create workspace key", Status: http.StatusOK, Message: "response carried no token"}
	}
	return resp.Data.Token, nil
}

// DeleteTenant implements Knowledge.
func (c *Client) DeleteTenant(ctx context.Context, tenantID string) error {
	return c.do(ctx, request{
		op: "delete tenant", method: http.MethodDelete,
		path: "/tenants/" + url.PathEscape(tenantID), mode: authJWT, tenant: tenantID,
	}, nil)
}

// EnsureUser implements Knowledge. WeKnora answers an existing login with the
// same user and no password, so a retry never resets one.
func (c *Client) EnsureUser(ctx context.Context, spec UserSpec) (User, error) {
	if spec.Username == "" || spec.Email == "" {
		return User{}, fmt.Errorf("knowledge: user needs a username and an email: %w", ErrInvalid)
	}
	body := map[string]any{"username": spec.Username, "email": spec.Email}
	if spec.Password != "" {
		body["password"] = spec.Password
	}
	var resp struct {
		User *struct {
			ID    string `json:"id"`
			Email string `json:"email"`
		} `json:"user"`
	}
	if err := c.do(ctx, request{
		op: "create user", method: http.MethodPost, path: "/system/admin/users/create", mode: authJWT, body: body,
	}, &resp); err != nil {
		return User{}, err
	}
	if resp.User == nil || resp.User.ID == "" {
		return User{}, &APIError{Op: "create user", Status: http.StatusOK, Message: "response carried no user"}
	}
	return User{ID: resp.User.ID, Email: resp.User.Email}, nil
}

// AddTenantMember implements Knowledge. A login already in the tenant is not an
// error, so a retried enable can run it again.
func (c *Client) AddTenantMember(ctx context.Context, tenantID, email, role string) error {
	err := c.do(ctx, request{
		op: "add tenant member", method: http.MethodPost, path: "/tenants/" + url.PathEscape(tenantID) + "/members",
		mode: authJWT, tenant: tenantID, body: map[string]string{"email": email, "role": role},
	}, nil)
	if errors.Is(err, ErrDuplicate) {
		return nil
	}
	return err
}

// Workspace implements Knowledge.
func (c *Client) Workspace(apiKey string) Workspace { return &workspace{c: c, key: apiKey} }

// ---- Workspace: tenant scope ----------------------------------------------

type workspace struct {
	c   *Client
	key string
}

func (w *workspace) req(op, method, path string) request {
	return request{op: op, method: method, path: path, mode: authKey, apiKey: w.key}
}

func (w *workspace) EnsureEmbeddingModel(ctx context.Context, spec EmbeddingModelSpec) (Model, error) {
	if spec.Name == "" || spec.Dimension <= 0 {
		return Model{}, fmt.Errorf("knowledge: embedding model needs a name and a dimension: %w", ErrInvalid)
	}
	params := map[string]any{
		"provider":             spec.Provider,
		"base_url":             spec.BaseURL,
		"embedding_parameters": map[string]any{"dimension": spec.Dimension},
	}
	if spec.APIKey != "" {
		params["api_key"] = spec.APIKey
	}
	source := "remote"
	if spec.Provider == "ollama" {
		source = "local"
	}
	return w.ensureModel(ctx, spec.Name, "Embedding", source, params)
}

// EnsureModel implements Workspace.
func (w *workspace) EnsureModel(ctx context.Context, spec ModelSpec) (Model, error) {
	if spec.Name == "" || spec.Type == "" || spec.BaseURL == "" {
		return Model{}, fmt.Errorf("knowledge: model needs a name, a type and an endpoint: %w", ErrInvalid)
	}
	params := map[string]any{"provider": spec.Provider, "base_url": spec.BaseURL}
	if spec.APIKey != "" {
		params["api_key"] = spec.APIKey
	}
	if len(spec.ExtraBody) > 0 {
		params["spec"] = map[string]any{"compat": map[string]any{"extra_body": spec.ExtraBody}}
	}
	return w.ensureModel(ctx, spec.Name, string(spec.Type), "remote", params)
}

// ensureModel finds the tenant's model of this name and type, or creates it,
// and waits until it is usable.
func (w *workspace) ensureModel(ctx context.Context, name, typ, source string, params map[string]any) (Model, error) {
	var list struct {
		Data []wireModel `json:"data"`
	}
	if err := w.c.do(ctx, w.req("list models", http.MethodGet, "/models"), &list); err != nil {
		return Model{}, err
	}
	var model Model
	found := false
	for _, m := range list.Data {
		if m.Type == typ && m.Name == name {
			model, found = Model{ID: m.ID, Name: m.Name, Status: m.Status}, true
			break
		}
	}
	if !found {
		var created struct {
			Data wireModel `json:"data"`
		}
		r := w.req("create "+typ+" model", http.MethodPost, "/models")
		r.body = map[string]any{"name": name, "type": typ, "source": source, "parameters": params}
		if err := w.c.do(ctx, r, &created); err != nil {
			return Model{}, err
		}
		model = Model{ID: created.Data.ID, Name: created.Data.Name, Status: created.Data.Status}
	}
	return w.waitModelActive(ctx, model)
}

// modelTestTimeout bounds one connection test. A model that answers in a few
// seconds is healthy; a longer wait is reported as a failure.
const modelTestTimeout = 60 * time.Second

// TestModel implements Workspace. WeKnora's debug route runs the model once and
// answers with an error body when the endpoint, key or model name is wrong.
func (w *workspace) TestModel(ctx context.Context, modelID string, probe ModelProbe) error {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.WriteField("input", probe.Input); err != nil {
		return fmt.Errorf("knowledge: test model: %w", err)
	}
	if len(probe.Documents) > 0 {
		docs, err := json.Marshal(probe.Documents)
		if err != nil {
			return fmt.Errorf("knowledge: test model: %w", err)
		}
		if err := mw.WriteField("documents", string(docs)); err != nil {
			return fmt.Errorf("knowledge: test model: %w", err)
		}
	}
	if err := mw.Close(); err != nil {
		return fmt.Errorf("knowledge: test model: %w", err)
	}
	r := w.req("test model", http.MethodPost, "/models/"+url.PathEscape(modelID)+"/debug")
	r.raw, r.rawType, r.timeout = &buf, mw.FormDataContentType(), modelTestTimeout
	return w.c.do(ctx, r, nil)
}

// UpdateKnowledgeBase implements Workspace. The indexing strategy is one object,
// so it is read first and sent back with only the wiki flag changed.
func (w *workspace) UpdateKnowledgeBase(ctx context.Context, kbID string, patch KnowledgeBasePatch) (KnowledgeBase, error) {
	body := map[string]any{}
	if patch.SummaryModelID != nil {
		body["summary_model_id"] = *patch.SummaryModelID
	}
	if patch.WikiEnabled != nil {
		var cur struct {
			Data wireKB `json:"data"`
		}
		if err := w.c.do(ctx, w.req("get knowledge base", http.MethodGet, "/knowledge-bases/"+url.PathEscape(kbID)), &cur); err != nil {
			return KnowledgeBase{}, err
		}
		strategy := cur.Data.IndexingStrategy
		strategy.WikiEnabled = *patch.WikiEnabled
		body["indexing_strategy"] = strategy
		if *patch.WikiEnabled {
			body["wiki_config"] = map[string]any{"extraction_granularity": "focused"}
		}
	}
	var resp struct {
		Data wireKB `json:"data"`
	}
	r := w.req("update knowledge base", http.MethodPatch, "/knowledge-bases/"+url.PathEscape(kbID))
	r.body = body
	if err := w.c.do(ctx, r, &resp); err != nil {
		return KnowledgeBase{}, err
	}
	return resp.Data.toKB(), nil
}

// ListDataSources implements Workspace. WeKnora answers this route with the bare
// list, not an envelope.
func (w *workspace) ListDataSources(ctx context.Context, kbID string) ([]DataSource, error) {
	var resp []wireDataSource
	path := "/datasource?" + url.Values{"kb_id": {kbID}}.Encode()
	if err := w.c.do(ctx, w.req("list data sources", http.MethodGet, path), &resp); err != nil {
		return nil, err
	}
	out := make([]DataSource, 0, len(resp))
	for _, d := range resp {
		out = append(out, DataSource{
			ID: d.ID, KnowledgeBaseID: d.KnowledgeBaseID, Name: d.Name, Type: d.Type, Status: d.Status,
			SyncSchedule: d.SyncSchedule, LastSyncAt: d.LastSyncAt, ErrorMessage: d.ErrorMessage,
			TotalItemsSynced: d.TotalItemsSynced,
		})
	}
	return out, nil
}

// waitModelActive polls until a new model leaves its "downloading" state. A
// local model is checked against its Ollama once, so this normally takes under
// a second.
func (w *workspace) waitModelActive(ctx context.Context, m Model) (Model, error) {
	deadline := time.Now().Add(w.c.cfg.ModelReadyTimeout)
	for {
		switch m.Status {
		case "active", "":
			return m, nil
		case "downloading", "pending":
		default:
			return m, fmt.Errorf("knowledge: embedding model %q is %s: %w", m.Name, m.Status, ErrInvalid)
		}
		if time.Now().After(deadline) {
			return m, fmt.Errorf("knowledge: embedding model %q still %s after %s: %w", m.Name, m.Status, w.c.cfg.ModelReadyTimeout, ErrUnavailable)
		}
		select {
		case <-ctx.Done():
			return m, ctx.Err()
		case <-time.After(w.c.cfg.ModelPollInterval):
		}
		var got struct {
			Data wireModel `json:"data"`
		}
		if err := w.c.do(ctx, w.req("get model", http.MethodGet, "/models/"+url.PathEscape(m.ID)), &got); err != nil {
			return m, err
		}
		m.Status = got.Data.Status
	}
}

func (w *workspace) CreateKnowledgeBase(ctx context.Context, spec KnowledgeBaseSpec) (KnowledgeBase, error) {
	var resp struct {
		Data wireKB `json:"data"`
	}
	r := w.req("create knowledge base", http.MethodPost, "/knowledge-bases")
	r.body = map[string]any{
		"name": spec.Name, "description": spec.Description, "type": "document",
		"embedding_model_id": spec.EmbeddingModelID,
	}
	if err := w.c.do(ctx, r, &resp); err != nil {
		return KnowledgeBase{}, err
	}
	return resp.Data.toKB(), nil
}

func (k wireKB) toKB() KnowledgeBase {
	return KnowledgeBase{
		ID: k.ID, Name: k.Name, Description: k.Description, EmbeddingModelID: k.EmbeddingModelID,
		DocumentCount: k.KnowledgeCount, SummaryModelID: k.SummaryModelID, WikiEnabled: k.IndexingStrategy.WikiEnabled,
	}
}

func (w *workspace) ListKnowledgeBases(ctx context.Context) ([]KnowledgeBase, error) {
	var resp struct {
		Data []wireKB `json:"data"`
	}
	if err := w.c.do(ctx, w.req("list knowledge bases", http.MethodGet, "/knowledge-bases"), &resp); err != nil {
		return nil, err
	}
	out := make([]KnowledgeBase, 0, len(resp.Data))
	for _, k := range resp.Data {
		out = append(out, k.toKB())
	}
	return out, nil
}

func (w *workspace) DeleteKnowledgeBase(ctx context.Context, kbID string) error {
	return w.c.do(ctx, w.req("delete knowledge base", http.MethodDelete, "/knowledge-bases/"+url.PathEscape(kbID)), nil)
}

func (w *workspace) docResult(ctx context.Context, r request) (Document, error) {
	var resp struct {
		Data wireDocument `json:"data"`
	}
	if err := w.c.do(ctx, r, &resp); err != nil {
		return Document{}, err
	}
	return resp.Data.toDocument(), nil
}

func (w *workspace) CreateManual(ctx context.Context, kbID string, doc ManualDocument) (Document, error) {
	if strings.TrimSpace(doc.Content) == "" {
		return Document{}, fmt.Errorf("knowledge: manual document needs content: %w", ErrInvalid)
	}
	status := "draft"
	if doc.Publish {
		status = "publish"
	}
	body := map[string]any{"title": doc.Title, "content": doc.Content, "status": status}
	if len(doc.TagIDs) > 0 {
		body["tag_ids"] = doc.TagIDs
	}
	r := w.req("create manual document", http.MethodPost, "/knowledge-bases/"+url.PathEscape(kbID)+"/knowledge/manual")
	r.body = body
	return w.docResult(ctx, r)
}

func (w *workspace) CreateURL(ctx context.Context, kbID string, doc URLDocument) (Document, error) {
	r := w.req("import url", http.MethodPost, "/knowledge-bases/"+url.PathEscape(kbID)+"/knowledge/url")
	body := map[string]any{"url": doc.URL, "title": doc.Title}
	if len(doc.TagIDs) > 0 {
		body["tag_ids"] = doc.TagIDs
	}
	r.body = body
	r.timeout = w.c.cfg.UploadTimeout
	return w.docResult(ctx, r)
}

func (w *workspace) CreateFile(ctx context.Context, kbID string, doc FileDocument) (Document, error) {
	if doc.Name == "" || doc.Content == nil {
		return Document{}, fmt.Errorf("knowledge: file upload needs a name and content: %w", ErrInvalid)
	}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, err := mw.CreateFormFile("file", doc.Name)
	if err != nil {
		return Document{}, fmt.Errorf("knowledge: upload file: %w", err)
	}
	if _, err := io.Copy(part, doc.Content); err != nil {
		return Document{}, fmt.Errorf("knowledge: upload file: %w", err)
	}
	// fileName carries the folder: WeKnora files the document under the
	// directory part of the path.
	fileName := doc.Name
	if folder := strings.Trim(doc.FolderPath, "/"); folder != "" {
		fileName = folder + "/" + doc.Name
	}
	if err := mw.WriteField("fileName", fileName); err != nil {
		return Document{}, fmt.Errorf("knowledge: upload file: %w", err)
	}
	// WeKnora reads an upload's tags as one comma-separated field.
	if len(doc.TagIDs) > 0 {
		if err := mw.WriteField("tag_ids", strings.Join(doc.TagIDs, ",")); err != nil {
			return Document{}, fmt.Errorf("knowledge: upload file: %w", err)
		}
	}
	if err := mw.Close(); err != nil {
		return Document{}, fmt.Errorf("knowledge: upload file: %w", err)
	}
	r := w.req("upload file", http.MethodPost, "/knowledge-bases/"+url.PathEscape(kbID)+"/knowledge/file")
	r.raw, r.rawType, r.timeout = &buf, mw.FormDataContentType(), w.c.cfg.UploadTimeout
	return w.docResult(ctx, r)
}

func (w *workspace) getDocument(ctx context.Context, docID string) (wireDocument, error) {
	var resp struct {
		Data wireDocument `json:"data"`
	}
	if err := w.c.do(ctx, w.req("get document", http.MethodGet, "/knowledge/"+url.PathEscape(docID)), &resp); err != nil {
		return wireDocument{}, err
	}
	return resp.Data, nil
}

// Publish indexes a draft. WeKnora's update replaces the whole body and
// rejects empty content, so the stored Markdown is read first and sent back
// with the new status.
func (w *workspace) Publish(ctx context.Context, docID string) (Document, error) {
	cur, err := w.getDocument(ctx, docID)
	if err != nil {
		return Document{}, err
	}
	if cur.Type != "manual" {
		return Document{}, fmt.Errorf("knowledge: document %s is a %s: %w", docID, cur.Type, ErrNotManual)
	}
	if cur.Metadata.Status == "publish" {
		return cur.toDocument(), nil
	}
	r := w.req("publish document", http.MethodPut, "/knowledge/manual/"+url.PathEscape(docID))
	r.body = map[string]any{"title": cur.Title, "content": cur.Metadata.Content, "status": "publish"}
	return w.docResult(ctx, r)
}

func (w *workspace) Delete(ctx context.Context, docID string) error {
	return w.c.do(ctx, w.req("delete document", http.MethodDelete, "/knowledge/"+url.PathEscape(docID)), nil)
}

// EnsureTag implements Workspace. WeKnora refuses a second tag with a name
// that exists (409), so the existing tag is looked up by name when the create
// is refused. The lookup is one page of keyword matches, which is ample for
// the handful of kinds and initiatives a knowledge base carries.
func (w *workspace) EnsureTag(ctx context.Context, kbID, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("knowledge: tag needs a name: %w", ErrInvalid)
	}
	base := "/knowledge-bases/" + url.PathEscape(kbID) + "/tags"
	find := func() (string, error) {
		var resp struct {
			Data struct {
				Data []struct {
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"data"`
			} `json:"data"`
		}
		path := base + "?" + url.Values{"page": {"1"}, "page_size": {"100"}, "keyword": {name}}.Encode()
		if err := w.c.do(ctx, w.req("list tags", http.MethodGet, path), &resp); err != nil {
			return "", err
		}
		for _, t := range resp.Data.Data {
			if t.Name == name {
				return t.ID, nil
			}
		}
		return "", fmt.Errorf("knowledge: tag %q: %w", name, ErrNotFound)
	}
	id, err := find()
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return "", err
	}
	var created struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	r := w.req("create tag", http.MethodPost, base)
	r.body = map[string]any{"name": name}
	err = w.c.do(ctx, r, &created)
	var dup *DuplicateError
	if errors.As(err, &dup) {
		return find()
	}
	if err != nil {
		return "", err
	}
	return created.Data.ID, nil
}

func (w *workspace) ListDocuments(ctx context.Context, kbID string, opts ListOptions) (DocumentPage, error) {
	q := url.Values{}
	page, size := opts.Page, opts.PageSize
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 20
	}
	q.Set("page", strconv.Itoa(page))
	q.Set("page_size", strconv.Itoa(size))
	if opts.Keyword != "" {
		q.Set("keyword", opts.Keyword)
	}
	var resp struct {
		Data     []wireDocument `json:"data"`
		Total    int            `json:"total"`
		Page     int            `json:"page"`
		PageSize int            `json:"page_size"`
	}
	path := "/knowledge-bases/" + url.PathEscape(kbID) + "/knowledge?" + q.Encode()
	if err := w.c.do(ctx, w.req("list documents", http.MethodGet, path), &resp); err != nil {
		return DocumentPage{}, err
	}
	out := DocumentPage{Total: resp.Total, Page: resp.Page, PageSize: resp.PageSize, Items: make([]Document, 0, len(resp.Data))}
	for _, d := range resp.Data {
		out.Items = append(out.Items, d.toDocument())
	}
	return out, nil
}

// Read returns the Markdown a manual document stores, or the joined text
// chunks of a parsed file or page.
func (w *workspace) Read(ctx context.Context, docID string) (DocumentContent, error) {
	cur, err := w.getDocument(ctx, docID)
	if err != nil {
		return DocumentContent{}, err
	}
	out := DocumentContent{Document: cur.toDocument()}
	if cur.Metadata.Content != "" {
		out.Content = cur.Metadata.Content
		return out, nil
	}
	var sb strings.Builder
	for page := 1; ; page++ {
		var resp struct {
			Data []struct {
				Content    string `json:"content"`
				ChunkType  string `json:"chunk_type"`
				ChunkIndex int    `json:"chunk_index"`
			} `json:"data"`
			Total int `json:"total"`
		}
		path := fmt.Sprintf("/chunks/%s?page=%d&page_size=100", url.PathEscape(docID), page)
		if err := w.c.do(ctx, w.req("read chunks", http.MethodGet, path), &resp); err != nil {
			return DocumentContent{}, err
		}
		for _, ch := range resp.Data {
			if ch.ChunkType != "" && ch.ChunkType != "text" {
				continue
			}
			if sb.Len() > 0 {
				sb.WriteString("\n\n")
			}
			sb.WriteString(ch.Content)
		}
		if len(resp.Data) == 0 || page*100 >= resp.Total {
			break
		}
	}
	out.Content = sb.String()
	return out, nil
}

func (w *workspace) Search(ctx context.Context, req SearchRequest) ([]SearchHit, error) {
	if strings.TrimSpace(req.Query) == "" {
		return nil, fmt.Errorf("knowledge: search needs a query: %w", ErrInvalid)
	}
	body := map[string]any{"query": req.Query}
	if len(req.KnowledgeBaseIDs) > 0 {
		body["knowledge_base_ids"] = req.KnowledgeBaseIDs
	}
	if req.Limit > 0 {
		body["match_count"] = req.Limit
	}
	var resp struct {
		Data []struct {
			ID                string  `json:"id"`
			Content           string  `json:"content"`
			KnowledgeID       string  `json:"knowledge_id"`
			KnowledgeBaseID   string  `json:"knowledge_base_id"`
			KnowledgeTitle    string  `json:"knowledge_title"`
			KnowledgeFilename string  `json:"knowledge_filename"`
			KnowledgeSource   string  `json:"knowledge_source"`
			Score             float64 `json:"score"`
		} `json:"data"`
	}
	r := w.req("search", http.MethodPost, "/knowledge-search")
	r.body = body
	if err := w.c.do(ctx, r, &resp); err != nil {
		return nil, err
	}
	hits := make([]SearchHit, 0, len(resp.Data))
	for _, h := range resp.Data {
		if h.Score < req.MinScore {
			continue
		}
		hits = append(hits, SearchHit{
			ChunkID: h.ID, DocumentID: h.KnowledgeID, KnowledgeBaseID: h.KnowledgeBaseID,
			Title: h.KnowledgeTitle, FileName: h.KnowledgeFilename, Source: h.KnowledgeSource,
			Content: h.Content, Score: h.Score,
		})
	}
	return hits, nil
}

func (w *workspace) ListMCPEndpoints(ctx context.Context) ([]MCPEndpoint, error) {
	var resp struct {
		Data []wireEndpoint `json:"data"`
	}
	if err := w.c.do(ctx, w.req("list mcp endpoints", http.MethodGet, "/mcp-endpoints"), &resp); err != nil {
		return nil, err
	}
	out := make([]MCPEndpoint, 0, len(resp.Data))
	for _, e := range resp.Data {
		out = append(out, e.toEndpoint())
	}
	return out, nil
}

func (w *workspace) endpointResult(ctx context.Context, r request) (MCPEndpoint, error) {
	var resp struct {
		Data wireEndpoint `json:"data"`
	}
	if err := w.c.do(ctx, r, &resp); err != nil {
		return MCPEndpoint{}, err
	}
	return resp.Data.toEndpoint(), nil
}

func (w *workspace) CreateMCPEndpoint(ctx context.Context, spec MCPEndpointSpec) (MCPEndpoint, error) {
	if len(spec.KnowledgeBaseIDs) == 0 || len(spec.Tools) == 0 {
		return MCPEndpoint{}, fmt.Errorf("knowledge: mcp endpoint needs knowledge bases and tools: %w", ErrInvalid)
	}
	body := map[string]any{
		"name": spec.Name, "description": spec.Description,
		"knowledge_base_ids": spec.KnowledgeBaseIDs, "tools": spec.Tools,
	}
	if spec.RateLimitPerMinute > 0 {
		body["rate_limit_per_minute"] = spec.RateLimitPerMinute
	}
	r := w.req("create mcp endpoint", http.MethodPost, "/mcp-endpoints")
	r.body = body
	return w.endpointResult(ctx, r)
}

func (w *workspace) RotateMCPEndpointToken(ctx context.Context, endpointID string) (MCPEndpoint, error) {
	return w.endpointResult(ctx, w.req("rotate mcp token", http.MethodPost, "/mcp-endpoints/"+url.PathEscape(endpointID)+"/rotate-token"))
}

func (w *workspace) SetMCPEndpointKnowledgeBases(ctx context.Context, endpointID string, kbIDs []string) (MCPEndpoint, error) {
	if len(kbIDs) == 0 {
		return MCPEndpoint{}, fmt.Errorf("knowledge: an empty list would expose every knowledge base: %w", ErrInvalid)
	}
	r := w.req("update mcp endpoint", http.MethodPut, "/mcp-endpoints/"+url.PathEscape(endpointID))
	r.body = map[string]any{"knowledge_base_ids": kbIDs}
	return w.endpointResult(ctx, r)
}
