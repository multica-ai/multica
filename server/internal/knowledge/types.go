// Package knowledge implements the independent, workspace-scoped knowledge
// base. It deliberately has no dependency on projects, workflows, or
// autopilots: a knowledge base is a source-and-retrieval product in its own
// right.
package knowledge

import "time"

const (
	VisibilityPrivate   = "private"
	VisibilityWorkspace = "workspace"

	SourceKindFile = "file"
	SourceKindURL  = "url"

	VersionProcessing  = "processing"
	VersionReady       = "ready"
	VersionUnsupported = "unsupported"
	VersionFailed      = "failed"
	VersionCancelled   = "cancelled"

	JobQueued        = "queued"
	JobRunning       = "running"
	JobWaitingConfig = "waiting_config"
	JobSucceeded     = "succeeded"
	JobFailed        = "failed"
	JobCancelled     = "cancelled"

	IndexBuilding  = "building"
	IndexActive    = "active"
	IndexRetired   = "retired"
	IndexFailed    = "failed"
	IndexCancelled = "cancelled"

	CapabilityReady       = "ready"
	CapabilityUnsupported = "unsupported"
	CapabilityFailed      = "failed"
	CapabilityNotTested   = "not_tested"
)

const (
	PurposeMain      = "main"
	PurposeExtract   = "extract"
	PurposeAnswer    = "answer"
	PurposeParse     = "parse"
	PurposeEmbedding = "embedding"
	PurposeRerank    = "rerank"
)

var SupportedPredicates = []string{
	"is_a", "part_of", "belongs_to", "creates", "uses", "depends_on",
	"causes", "supports", "contradicts", "related_to",
}

var SupportedEntityTypes = []string{
	"person", "organization", "product", "concept", "event",
}

type Base struct {
	ID             string    `json:"id"`
	WorkspaceID    string    `json:"workspace_id"`
	CreatorID      string    `json:"creator_id"`
	Name           string    `json:"name"`
	Description    string    `json:"description"`
	Visibility     string    `json:"visibility"`
	Revision       int64     `json:"revision"`
	ACLRevision    int64     `json:"acl_revision"`
	CorpusRevision int64     `json:"corpus_revision"`
	ActiveIndexID  *string   `json:"active_index_id,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type BasePage struct {
	Bases      []Base `json:"bases"`
	NextCursor string `json:"next_cursor,omitempty"`
}

type Document struct {
	ID               string     `json:"id"`
	WorkspaceID      string     `json:"workspace_id"`
	KnowledgeBaseID  string     `json:"knowledge_base_id"`
	Title            string     `json:"title"`
	SourceKind       string     `json:"source_kind"`
	SourceURL        *string    `json:"source_url,omitempty"`
	Tags             []string   `json:"tags"`
	CurrentVersionID *string    `json:"current_version_id,omitempty"`
	Revision         int64      `json:"revision"`
	Status           string     `json:"status"`
	DeletedAt        *time.Time `json:"deleted_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type DocumentPage struct {
	Documents  []Document `json:"documents"`
	NextCursor string     `json:"next_cursor,omitempty"`
}

type DocumentVersion struct {
	ID              string         `json:"id"`
	DocumentID      string         `json:"document_id"`
	VersionNumber   int64          `json:"version_number"`
	SourceObjectKey string         `json:"source_object_key"`
	SourceHash      string         `json:"source_hash"`
	ByteSize        int64          `json:"byte_size"`
	MIMEType        string         `json:"mime_type"`
	SourceMetadata  map[string]any `json:"source_metadata"`
	ParsedObjectKey *string        `json:"parsed_object_key,omitempty"`
	ParserVersion   string         `json:"parser_version"`
	ChunkerVersion  string         `json:"chunker_version"`
	ConfigSnapshot  map[string]any `json:"config_snapshot"`
	Status          string         `json:"status"`
	ErrorCode       *string        `json:"error_code,omitempty"`
	CreatedAt       time.Time      `json:"created_at"`
}

type VersionPage struct {
	Versions   []DocumentVersion `json:"versions"`
	NextCursor string            `json:"next_cursor,omitempty"`
}

type DocumentBlock struct {
	BlockID      string         `json:"block_id"`
	Kind         string         `json:"kind"`
	Text         string         `json:"text"`
	HeadingPath  []string       `json:"heading_path,omitempty"`
	Locator      map[string]any `json:"locator"`
	ModelDerived bool           `json:"model_derived,omitempty"`
}

type ParsedDocument struct {
	SchemaVersion string          `json:"schema_version"`
	ParserVersion string          `json:"parser_version"`
	Title         string          `json:"title"`
	Blocks        []DocumentBlock `json:"blocks"`
	Warnings      []string        `json:"warnings"`
	Stats         map[string]any  `json:"stats"`
}

type VersionBlocksResponse struct {
	VersionID  string          `json:"version_id"`
	Blocks     []DocumentBlock `json:"blocks"`
	NextCursor string          `json:"next_cursor,omitempty"`
}

type PreviewCapability struct {
	VersionID  string    `json:"version_id"`
	DocumentID string    `json:"document_id"`
	ExpiresAt  time.Time `json:"expires_at"`
	Token      string    `json:"token"`
	PreviewURL string    `json:"preview_url"`
}

type Chunk struct {
	ID            string         `json:"id"`
	DocumentID    string         `json:"document_id"`
	VersionID     string         `json:"version_id"`
	Ordinal       int            `json:"ordinal"`
	BlockRefs     []string       `json:"block_refs"`
	Text          string         `json:"text"`
	HeadingPath   []string       `json:"-"`
	SourceLocator map[string]any `json:"source_locator"`
	TokenEstimate int            `json:"token_estimate"`
	TextHash      string         `json:"text_hash"`
}

type Provider struct {
	ID             string `json:"id"`
	WorkspaceID    string `json:"workspace_id"`
	Name           string `json:"name"`
	Preset         string `json:"preset"`
	Protocol       string `json:"protocol"`
	BaseURL        string `json:"base_url"`
	HasAPIKey      bool   `json:"has_api_key"`
	SecretRevision int64  `json:"secret_revision"`
	IsEnabled      bool   `json:"is_enabled"`
	Revision       int64  `json:"revision"`
	CreatedBy      string `json:"created_by"`
}

type ProviderPage struct {
	Providers  []Provider `json:"providers"`
	NextCursor string     `json:"next_cursor,omitempty"`
}

type ModelBinding struct {
	ID              string         `json:"id,omitempty"`
	WorkspaceID     string         `json:"workspace_id"`
	KnowledgeBaseID *string        `json:"knowledge_base_id,omitempty"`
	Purpose         string         `json:"purpose"`
	Mode            string         `json:"mode"`
	ProviderID      *string        `json:"provider_id,omitempty"`
	Model           *string        `json:"model,omitempty"`
	Options         map[string]any `json:"options,omitempty"`
	Revision        int64          `json:"revision"`
}

type Capability struct {
	ProviderID     string         `json:"provider_id"`
	SecretRevision int64          `json:"secret_revision"`
	Model          string         `json:"model"`
	Capability     string         `json:"capability"`
	Status         string         `json:"status"`
	Dimension      int            `json:"dimension,omitempty"`
	Details        map[string]any `json:"details,omitempty"`
	TestedAt       *time.Time     `json:"tested_at,omitempty"`
}

type ModelSettings struct {
	WorkspaceID     string                   `json:"workspace_id"`
	KnowledgeBaseID *string                  `json:"knowledge_base_id,omitempty"`
	Revision        int64                    `json:"revision"`
	Main            *ModelBinding            `json:"main,omitempty"`
	Purposes        map[string]*ModelBinding `json:"purposes"`
	Capabilities    []Capability             `json:"capabilities"`
	AffectedBases   []string                 `json:"affected_bases,omitempty"`
}

type SearchFilter struct {
	DocumentIDs []string `json:"document_ids,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	SourceKind  string   `json:"source_kind,omitempty"`
}

type SearchInput struct {
	Query  string       `json:"query"`
	Limit  int          `json:"limit,omitempty"`
	Mode   string       `json:"mode,omitempty"`
	Filter SearchFilter `json:"filters,omitempty"`
}

type SearchResult struct {
	ChunkID           string         `json:"chunk_id"`
	DocumentID        string         `json:"document_id"`
	VersionID         string         `json:"version_id"`
	Title             string         `json:"title"`
	Text              string         `json:"text"`
	SourceLocator     map[string]any `json:"source_locator"`
	RetrievalChannels []string       `json:"retrieval_channels"`
	Rank              int            `json:"rank"`
	Citation          Citation       `json:"citation"`
}

type Citation struct {
	ID           string         `json:"id"`
	Locator      map[string]any `json:"locator"`
	SourceURL    *string        `json:"source_url,omitempty"`
	DocumentPath string         `json:"document_path"`
}

type SearchResponse struct {
	QueryID         string         `json:"query_id"`
	KnowledgeBaseID string         `json:"knowledge_base_id"`
	ModeRequested   string         `json:"mode_requested"`
	ModeEffective   string         `json:"mode_effective"`
	RerankApplied   bool           `json:"rerank_applied"`
	IndexID         *string        `json:"index_id,omitempty"`
	Warnings        []string       `json:"warnings"`
	Results         []SearchResult `json:"results"`
}

type AnswerCitation struct {
	ID              string `json:"id"`
	Paragraph       int    `json:"paragraph,omitempty"`
	AnswerParagraph int    `json:"answer_paragraph,omitempty"`
}

type AnswerResponse struct {
	QueryID              string           `json:"query_id"`
	Question             string           `json:"question"`
	Answer               string           `json:"answer"`
	Citations            []AnswerCitation `json:"citations"`
	InsufficientEvidence bool             `json:"insufficient_evidence"`
	ContextTruncated     bool             `json:"context_truncated"`
	GenerationError      *string          `json:"generation_error,omitempty"`
	Search               SearchResponse   `json:"search"`
}

type Entity struct {
	ID              string `json:"id"`
	KnowledgeBaseID string `json:"knowledge_base_id"`
	Type            string `json:"type"`
	CanonicalName   string `json:"canonical_name"`
	NormalizedName  string `json:"normalized_name"`
	ReviewStatus    string `json:"review_status"`
	Revision        int64  `json:"revision"`
	EvidenceCount   int    `json:"evidence_count"`
}

type EntityPage struct {
	Entities   []Entity `json:"entities"`
	NextCursor string   `json:"next_cursor,omitempty"`
}

type Evidence struct {
	ID              string         `json:"id"`
	SubjectType     string         `json:"subject_type"`
	SubjectID       string         `json:"subject_id"`
	VersionID       string         `json:"version_id"`
	ChunkID         string         `json:"chunk_id"`
	SourceLocator   map[string]any `json:"source_locator"`
	Quote           string         `json:"quote"`
	StartOffset     *int           `json:"start_offset,omitempty"`
	EndOffset       *int           `json:"end_offset,omitempty"`
	ExtractionRunID *string        `json:"extraction_run_id,omitempty"`
}

type EntityDetail struct {
	Entity   Entity     `json:"entity"`
	Evidence []Evidence `json:"evidence"`
}

type Relation struct {
	ID              string         `json:"id"`
	KnowledgeBaseID string         `json:"knowledge_base_id"`
	SourceEntityID  string         `json:"source_entity_id"`
	TargetEntityID  string         `json:"target_entity_id"`
	Predicate       string         `json:"predicate"`
	Qualifier       map[string]any `json:"qualifier"`
	ReviewStatus    string         `json:"review_status"`
	Revision        int64          `json:"revision"`
	EvidenceCount   int            `json:"evidence_count"`
}

type GraphResponse struct {
	Nodes     []Entity   `json:"nodes"`
	Relations []Relation `json:"relations"`
	Truncated bool       `json:"truncated"`
	NodeCount int        `json:"node_count"`
	EdgeCount int        `json:"edge_count"`
}

type GraphEdit struct {
	ID               string         `json:"id"`
	Operation        string         `json:"operation"`
	TargetID         string         `json:"target_id"`
	Payload          map[string]any `json:"payload"`
	PreviousState    map[string]any `json:"previous_state"`
	ActorID          string         `json:"actor_id"`
	ExpectedRevision int64          `json:"expected_revision"`
	CreatedAt        time.Time      `json:"created_at"`
	RevertedAt       *time.Time     `json:"reverted_at,omitempty"`
}

type GraphEditPage struct {
	Edits      []GraphEdit `json:"edits"`
	NextCursor string      `json:"next_cursor,omitempty"`
}

type Job struct {
	ID                string         `json:"id"`
	KnowledgeBaseID   string         `json:"knowledge_base_id"`
	DocumentVersionID *string        `json:"document_version_id,omitempty"`
	IndexID           *string        `json:"index_id,omitempty"`
	Stage             string         `json:"stage"`
	Status            string         `json:"status"`
	Attempt           int            `json:"attempt"`
	AvailableAt       time.Time      `json:"available_at"`
	Progress          map[string]any `json:"progress"`
	ErrorCode         *string        `json:"error_code,omitempty"`
	CreatedAt         time.Time      `json:"created_at"`
}

type JobPage struct {
	Jobs       []Job  `json:"jobs"`
	NextCursor string `json:"next_cursor,omitempty"`
}
