// Package knowledge is Multica's boundary to the company knowledge engine.
//
// The engine is WeKnora, run as a pinned sidecar. Multica owns the contract:
// callers see the Knowledge and Workspace interfaces below and the types in
// this file, never WeKnora's wire shapes, so replacing the engine means a new
// adapter and not new screens. The HTTP adapter is Client; Fake is an
// in-memory stand-in for other packages' tests.
//
// WeKnora stores and retrieves. Nothing here uses its agents, skills, IM
// channels or per-user memory.
package knowledge

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"
)

// Sentinel errors. Every error returned by this package satisfies errors.Is
// against at most one of them, so callers branch on the failure class and
// never on status codes. ErrUnavailable is the degradation signal: the
// sidecar could not be reached or answered with a gateway error, and the
// caller should carry on without knowledge.
var (
	// ErrUnavailable means the sidecar is unreachable, timed out, or answered
	// 502, 503 or 504.
	ErrUnavailable = errors.New("knowledge: service unavailable")
	// ErrAuth means WeKnora rejected the credentials: the service account's
	// login, or a workspace API key.
	ErrAuth = errors.New("knowledge: authentication failed")
	// ErrForbidden means the credentials are valid but lack the permission.
	ErrForbidden = errors.New("knowledge: forbidden")
	// ErrNotFound means the tenant, knowledge base, document or endpoint does
	// not exist.
	ErrNotFound = errors.New("knowledge: not found")
	// ErrDuplicate means the document is already in the knowledge base. The
	// error is a *DuplicateError carrying the existing document.
	ErrDuplicate = errors.New("knowledge: duplicate document")
	// ErrQuota means WeKnora refused for a cap it enforces, such as the number
	// of workspaces one user may own.
	ErrQuota = errors.New("knowledge: quota exceeded")
	// ErrInvalid means WeKnora rejected the request as malformed.
	ErrInvalid = errors.New("knowledge: invalid request")
	// ErrNotManual means the operation only applies to a manual (Markdown)
	// document, for example publishing a draft.
	ErrNotManual = errors.New("knowledge: not a manual document")
)

// APIError is a WeKnora failure that has no sentinel class of its own, or the
// underlying detail of one that does. It never contains request bodies, so it
// is safe to log.
type APIError struct {
	// Status is the HTTP status code.
	Status int
	// Code is WeKnora's machine-readable code, when it sent one.
	Code string
	// Message is WeKnora's human-readable message.
	Message string
	// Op names the operation, for example "create knowledge base".
	Op string

	class error
}

func (e *APIError) Error() string {
	msg := e.Message
	if msg == "" {
		msg = "no message"
	}
	return fmt.Sprintf("knowledge: %s: HTTP %d: %s", e.Op, e.Status, msg)
}

// Is reports whether the error belongs to a sentinel class.
func (e *APIError) Is(target error) bool { return e.class != nil && e.class == target }

// DuplicateError is returned with ErrDuplicate. Existing is the document
// WeKnora already holds.
type DuplicateError struct {
	APIError
	Existing Document
}

// Is reports ErrDuplicate.
func (e *DuplicateError) Is(target error) bool { return target == ErrDuplicate }

// Unwrap lets errors.As reach the embedded APIError.
func (e *DuplicateError) Unwrap() error { return &e.APIError }

// Knowledge is the platform scope: operations that need the service account,
// or that need nothing but reachability. Workspace operations hang off
// Workspace.
type Knowledge interface {
	// Ping reports whether the sidecar answers its health check.
	Ping(ctx context.Context) error

	// EnsureTenant returns the tenant the service account owns with this name,
	// creating it when it does not exist. The name is how a retried
	// provisioning finds the tenant a crashed attempt created.
	EnsureTenant(ctx context.Context, name, description string) (Tenant, error)
	// CreateWorkspaceKey mints a full-access API key for the tenant and returns
	// its usable token. WeKnora shows the token once.
	CreateWorkspaceKey(ctx context.Context, tenantID, name string) (string, error)
	// DeleteTenant removes a tenant and everything in it.
	DeleteTenant(ctx context.Context, tenantID string) error

	// EnsureUser creates a WeKnora login for a person, or finds the one with
	// this email. WeKnora returns no password for an existing login, so the
	// password is meaningful only on the call that creates it. The service
	// account must be a system administrator.
	EnsureUser(ctx context.Context, spec UserSpec) (User, error)
	// AddTenantMember gives an existing WeKnora login a role in a tenant.
	AddTenantMember(ctx context.Context, tenantID, email, role string) error

	// Workspace returns the tenant-scoped API for a workspace API key.
	Workspace(apiKey string) Workspace

	// MCPURL is the address agents use for an MCP endpoint, built from the
	// public base URL. It is not necessarily the address the backend uses.
	MCPURL(endpointID string) string
}

// Workspace is the tenant scope, authenticated by the workspace's API key.
type Workspace interface {
	// EnsureEmbeddingModel returns the tenant's embedding model with this name,
	// creating it when absent, and waits until it is usable. A knowledge base
	// fixes its vector store at creation, so this runs before any of them.
	EnsureEmbeddingModel(ctx context.Context, spec EmbeddingModelSpec) (Model, error)

	// EnsureModel returns the tenant's model with this name and type, creating
	// it when absent, and waits until it is usable. Chat, rerank and vision
	// models use it; embedding models go through EnsureEmbeddingModel.
	EnsureModel(ctx context.Context, spec ModelSpec) (Model, error)
	// TestModel runs the model once with the probe. A failure is an *APIError
	// carrying WeKnora's message, so the caller can say which model failed and
	// why.
	TestModel(ctx context.Context, modelID string, probe ModelProbe) error

	CreateKnowledgeBase(ctx context.Context, spec KnowledgeBaseSpec) (KnowledgeBase, error)
	ListKnowledgeBases(ctx context.Context) ([]KnowledgeBase, error)
	// UpdateKnowledgeBase changes the settings the patch names. Fields left nil
	// keep their values.
	UpdateKnowledgeBase(ctx context.Context, kbID string, patch KnowledgeBasePatch) (KnowledgeBase, error)
	DeleteKnowledgeBase(ctx context.Context, kbID string) error
	// ListDataSources returns the sync connections (Notion, Confluence, RSS and
	// others) that feed one knowledge base. Creating one happens in WeKnora.
	ListDataSources(ctx context.Context, kbID string) ([]DataSource, error)

	// CreateManual stores Markdown. With Publish false it is a draft: stored,
	// not indexed, not searchable. With Publish true it is indexed.
	CreateManual(ctx context.Context, kbID string, doc ManualDocument) (Document, error)
	// CreateURL imports a web page. WeKnora fetches and indexes it at once;
	// there is no draft state, so callers hold the URL until publish time.
	CreateURL(ctx context.Context, kbID string, doc URLDocument) (Document, error)
	// CreateFile uploads a file. WeKnora parses and indexes it at once.
	CreateFile(ctx context.Context, kbID string, doc FileDocument) (Document, error)
	// Publish indexes a manual draft. It is idempotent on a published
	// document and returns ErrNotManual for any other kind.
	Publish(ctx context.Context, docID string) (Document, error)
	// Delete removes a document. WeKnora completes the removal asynchronously.
	Delete(ctx context.Context, docID string) error
	ListDocuments(ctx context.Context, kbID string, opts ListOptions) (DocumentPage, error)
	// Read returns a document with its text.
	Read(ctx context.Context, docID string) (DocumentContent, error)
	// EnsureTag returns the id of the tag with this name in the knowledge base,
	// creating the tag when it is absent. Tags carry a document's kind and its
	// initiative folder into WeKnora.
	EnsureTag(ctx context.Context, kbID, name string) (string, error)
	// Search is retrieval only: no LLM is called. Drafts are never returned.
	Search(ctx context.Context, req SearchRequest) ([]SearchHit, error)

	ListMCPEndpoints(ctx context.Context) ([]MCPEndpoint, error)
	// CreateMCPEndpoint creates an endpoint. The returned Token is shown once.
	CreateMCPEndpoint(ctx context.Context, spec MCPEndpointSpec) (MCPEndpoint, error)
	// RotateMCPEndpointToken replaces the endpoint's token and returns the new
	// one; the old token stops working at once.
	RotateMCPEndpointToken(ctx context.Context, endpointID string) (MCPEndpoint, error)
	// SetMCPEndpointKnowledgeBases replaces the knowledge bases an endpoint
	// can read. An empty list would mean every base in the tenant, so
	// implementations reject it.
	SetMCPEndpointKnowledgeBases(ctx context.Context, endpointID string, kbIDs []string) (MCPEndpoint, error)
}

// Tenant is a WeKnora workspace. IDs are decimal strings.
type Tenant struct {
	ID          string
	Name        string
	Description string
}

// EmbeddingModelSpec describes the embedding model to create in a tenant.
type EmbeddingModelSpec struct {
	// Name is the model name, for example "bge-m3".
	Name string
	// Provider is "ollama" for a local Ollama, or any other WeKnora provider
	// such as "generic" for an OpenAI-compatible endpoint.
	Provider string
	// BaseURL is the endpoint as WeKnora, not Multica, reaches it. A private
	// address must be in WeKnora's SSRF allow-list.
	BaseURL string
	// APIKey is optional and write-only.
	APIKey string
	// Dimension is the vector size.
	Dimension int
}

// Model is a model registered in a tenant.
type Model struct {
	ID     string
	Name   string
	Status string
}

// KnowledgeBaseSpec describes a knowledge base to create.
type KnowledgeBaseSpec struct {
	Name             string
	Description      string
	EmbeddingModelID string
}

// KnowledgeBase is a WeKnora knowledge base.
type KnowledgeBase struct {
	ID               string
	Name             string
	Description      string
	EmbeddingModelID string
	DocumentCount    int
	// SummaryModelID is the model that writes document summaries; empty when
	// summaries are off.
	SummaryModelID string
	// WikiEnabled reports whether Wiki pages are generated for the base.
	WikiEnabled bool
}

// KnowledgeBasePatch changes a knowledge base. A nil field is left as it is.
type KnowledgeBasePatch struct {
	SummaryModelID *string
	// WikiEnabled turns Wiki generation on or off. Turning it on uses the
	// focused extraction granularity, which bounds the LLM calls per document.
	WikiEnabled *bool
}

// ModelSpec describes a model to register in a tenant.
type ModelSpec struct {
	// Name is the model name the endpoint expects, for example "gpt-4o-mini".
	Name string
	// Type is one of the ModelType constants.
	Type ModelType
	// Provider is "generic" for any OpenAI-compatible endpoint.
	Provider string
	// BaseURL is the endpoint as WeKnora, not Multica, reaches it.
	BaseURL string
	// APIKey is optional and write-only.
	APIKey string
	// ExtraBody is merged into every request WeKnora sends to this model. WeKnora
	// omits "stream" on a non-streaming call, and a gateway that streams by
	// default needs {"stream": false} to return JSON.
	ExtraBody map[string]any
}

// ModelType is the role a model plays in WeKnora.
type ModelType string

// The model roles WeKnora knows that Multica registers.
const (
	ModelTypeKnowledgeQA ModelType = "KnowledgeQA"
	ModelTypeRerank      ModelType = "Rerank"
	ModelTypeVLLM        ModelType = "VLLM"
)

// ModelProbe is the input for a connection test. Documents is set for a
// rerank model only.
type ModelProbe struct {
	Input     string
	Documents []string
}

// UserSpec describes a WeKnora login.
type UserSpec struct {
	Username string
	Email    string
	// Password is set when the login is created. It is never read back.
	Password string
}

// User is a WeKnora login.
type User struct {
	ID    string
	Email string
}

// DataSource is a sync connection that feeds a knowledge base.
type DataSource struct {
	ID              string
	KnowledgeBaseID string
	Name            string
	// Type is the connector, such as "notion", "confluence" or "rss".
	Type string
	// Status is "active", "paused" or "error", or a newer value.
	Status string
	// SyncSchedule is a cron expression; empty for manual sync.
	SyncSchedule string
	// LastSyncAt is nil before the first sync.
	LastSyncAt *time.Time
	// ErrorMessage is the last sync failure; empty when none.
	ErrorMessage     string
	TotalItemsSynced int64
}

// Parse states a document passes through, as WeKnora reports them.
const (
	ParseDraft      = "draft"
	ParsePending    = "pending"
	ParseProcessing = "processing"
	ParseCompleted  = "completed"
	ParseFailed     = "failed"
)

// Document is a document in a knowledge base.
type Document struct {
	ID              string
	KnowledgeBaseID string
	Title           string
	FileName        string
	FolderPath      string
	// Type is "manual", "file" or "url".
	Type string
	// Source is the URL for an imported page.
	Source string
	// ParseStatus is one of the Parse constants, or a newer value.
	ParseStatus string
	Error       string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// IsDraft reports a stored, unindexed manual document.
func (d Document) IsDraft() bool { return d.ParseStatus == ParseDraft }

// IsIndexed reports that the document is parsed and searchable.
func (d Document) IsIndexed() bool { return d.ParseStatus == ParseCompleted }

// ManualDocument is Markdown written directly.
type ManualDocument struct {
	Title   string
	Content string
	// Publish indexes the document at once. False stores a draft.
	Publish bool
	// TagIDs are WeKnora tag ids from EnsureTag, attached to the document.
	TagIDs []string
}

// URLDocument is a web page to import.
type URLDocument struct {
	URL   string
	Title string
	// TagIDs are WeKnora tag ids from EnsureTag, attached to the imported page.
	TagIDs []string
}

// FileDocument is a file to upload.
type FileDocument struct {
	// Name is the file name, with its extension.
	Name string
	// FolderPath groups the document, slash separated, relative to the base.
	FolderPath string
	Content    io.Reader
	// TagIDs are WeKnora tag ids from EnsureTag, attached to the document.
	TagIDs []string
}

// ListOptions pages a document listing.
type ListOptions struct {
	// Page starts at 1. Zero means the first page.
	Page int
	// PageSize defaults to 20.
	PageSize int
	Keyword  string
}

// DocumentPage is one page of documents.
type DocumentPage struct {
	Items    []Document
	Total    int
	Page     int
	PageSize int
}

// DocumentContent is a document with its text.
type DocumentContent struct {
	Document
	Content string
}

// SearchRequest is a retrieval query.
type SearchRequest struct {
	Query            string
	KnowledgeBaseIDs []string
	// Limit caps the hits. Zero uses WeKnora's default.
	Limit int
	// MinScore drops weaker hits. WeKnora always returns something, even for
	// a query that matches nothing, so relevance is the caller's call.
	MinScore float64
}

// SearchHit is one retrieved chunk.
type SearchHit struct {
	ChunkID         string
	DocumentID      string
	KnowledgeBaseID string
	Title           string
	FileName        string
	Source          string
	Content         string
	Score           float64
}

// MCPEndpointSpec describes a read-only MCP endpoint.
type MCPEndpointSpec struct {
	Name        string
	Description string
	// KnowledgeBaseIDs are the bases the endpoint can read. Must not be empty.
	KnowledgeBaseIDs []string
	// Tools are the exposed tools. Must not be empty.
	Tools []string
	// RateLimitPerMinute is zero for WeKnora's default of 60.
	RateLimitPerMinute int
}

// MCPEndpoint is a published MCP endpoint.
type MCPEndpoint struct {
	ID               string
	Name             string
	Path             string
	Enabled          bool
	KnowledgeBaseIDs []string
	Tools            []string
	// Token is set only by create and rotate; WeKnora never returns it again.
	Token string
}

// ReadOnlyMCPTools are the tools of the company-read endpoint. ask is left out
// because it calls an LLM, and the write tools are left out because agents
// write drafts through Multica, never straight into the index.
var ReadOnlyMCPTools = []string{
	"list_knowledge_bases",
	"search_knowledge",
	"grep_chunks",
	"list_documents",
	"read_document",
	"wiki_search",
	"wiki_read_page",
	"wiki_index",
}
