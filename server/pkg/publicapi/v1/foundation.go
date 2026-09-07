package publicapiv1

const (
	HeaderIdempotencyKey = "Idempotency-Key"
	HeaderIfMatch        = "If-Match"
	MaxIdempotencyBytes  = 255
	DefaultPageSize      = 50
	MaxPageSize          = 200
)

// CredentialKind identifies the trust boundary that authenticated a request.
// Resource services receive an authorized actor; they do not parse tokens.
type CredentialKind string

const (
	CredentialUserOAuth        CredentialKind = "user_oauth"
	CredentialPersonalAccess   CredentialKind = "personal_access_token"
	CredentialPluginInstall    CredentialKind = "plugin_installation"
	CredentialPluginInvocation CredentialKind = "plugin_invocation"
)

type ActorKind string

const (
	ActorMember ActorKind = "member"
	ActorPlugin ActorKind = "plugin"
)

// Actor is the transport-neutral identity supplied to shared authorization,
// audit, and service layers after a surface authenticates its credential.
type Actor struct {
	Kind        ActorKind
	SubjectID   string
	WorkspaceID string
	Credential  CredentialKind
}

type RiskLevel string

const (
	RiskRead         RiskLevel = "read"
	RiskContentWrite RiskLevel = "content_write"
	RiskHigh         RiskLevel = "high"
)

type RateLimitProfile string

const (
	RateLimitUserDefault  RateLimitProfile = "user_default"
	RateLimitPluginStrict RateLimitProfile = "plugin_strict"
)

// AuditStatus distinguishes a declared requirement from an implemented audit
// sink. A capability must not claim enforcement merely because it is listed in
// the contract ledger.
type AuditStatus string

const (
	AuditNotRequired AuditStatus = "not_required"
	AuditPlanned     AuditStatus = "planned"
	AuditEnforced    AuditStatus = "enforced"
)

// OperationPolicy records authorization and observability requirements next
// to the route contract. Surface-specific middleware enforces the credential
// kind and rate profile; resource authorization enforces workspace and scope.
type OperationPolicy struct {
	Credentials []CredentialKind
	Scope       string
	Risk        RiskLevel
	Audit       AuditStatus
	RateLimits  []RateLimitProfile
}

// PageInfo is the common response metadata for cursor-paginated collections.
// Cursors are opaque and scoped to the authenticated actor and query filters.
type PageInfo struct {
	NextCursor string `json:"next_cursor,omitempty"`
}
