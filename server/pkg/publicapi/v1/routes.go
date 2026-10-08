// Package publicapiv1 owns the stable Multica Public API v1 contract.
//
// The same resource contract is exposed through different trust surfaces. The
// Plugin API is the first consumer: it accepts installation/callback tokens and
// applies manifest scopes. Future user/PAT entrypoints should reuse these
// paths, DTOs, and operation semantics instead of creating parallel handlers.
package publicapiv1

import "net/http"

const BasePath = "/v1"

const (
	PathContext       = "/context"
	PathIssue         = "/issues/{issue_ref}"
	PathIssueComments = "/issues/{issue_ref}/comments"
	PathStorageScope  = "/storage/{scope}"
	PathStorageValue  = "/storage/{scope}/{key}"
)

type ContractKind string

const (
	// ContractSharedResource is a Multica resource contract that can be exposed
	// to multiple credential types through surface-specific authorization.
	ContractSharedResource ContractKind = "shared_resource"
	// ContractPluginExtension belongs to Plugin installations rather than the
	// general Multica resource API.
	ContractPluginExtension ContractKind = "plugin_extension"
)

// Operation is the capability ledger for the currently implemented v1 slice.
// OpenAPI tests pin this registry to openapi.yaml so routes, scopes, and docs
// cannot drift independently.
type Operation struct {
	Method   string
	Path     string
	Contract ContractKind
	Policy   OperationPolicy
}

var sharedCredentials = []CredentialKind{
	CredentialUserOAuth,
	CredentialPersonalAccess,
	CredentialPluginInstall,
	CredentialPluginInvocation,
}

var pluginCredentials = []CredentialKind{
	CredentialPluginInstall,
	CredentialPluginInvocation,
}

var sharedRateLimits = []RateLimitProfile{RateLimitUserDefault, RateLimitPluginStrict}
var pluginRateLimits = []RateLimitProfile{RateLimitPluginStrict}

var Operations = []Operation{
	{Method: http.MethodGet, Path: PathContext, Contract: ContractPluginExtension, Policy: OperationPolicy{Credentials: pluginCredentials, Risk: RiskRead, Audit: AuditNotRequired, RateLimits: pluginRateLimits}},
	{Method: http.MethodGet, Path: PathIssue, Contract: ContractSharedResource, Policy: OperationPolicy{Credentials: sharedCredentials, Scope: "issues:read", Risk: RiskRead, Audit: AuditPlanned, RateLimits: sharedRateLimits}},
	{Method: http.MethodPatch, Path: PathIssue, Contract: ContractSharedResource, Policy: OperationPolicy{Credentials: sharedCredentials, Scope: "issues:write", Risk: RiskContentWrite, Audit: AuditPlanned, RateLimits: sharedRateLimits}},
	{Method: http.MethodGet, Path: PathIssueComments, Contract: ContractSharedResource, Policy: OperationPolicy{Credentials: sharedCredentials, Scope: "comments:read", Risk: RiskRead, Audit: AuditPlanned, RateLimits: sharedRateLimits}},
	{Method: http.MethodPost, Path: PathIssueComments, Contract: ContractSharedResource, Policy: OperationPolicy{Credentials: sharedCredentials, Scope: "comments:write", Risk: RiskContentWrite, Audit: AuditPlanned, RateLimits: sharedRateLimits}},
	{Method: http.MethodGet, Path: PathStorageScope, Contract: ContractPluginExtension, Policy: OperationPolicy{Credentials: pluginCredentials, Risk: RiskRead, Audit: AuditNotRequired, RateLimits: pluginRateLimits}},
	{Method: http.MethodGet, Path: PathStorageValue, Contract: ContractPluginExtension, Policy: OperationPolicy{Credentials: pluginCredentials, Risk: RiskRead, Audit: AuditNotRequired, RateLimits: pluginRateLimits}},
	{Method: http.MethodPut, Path: PathStorageValue, Contract: ContractPluginExtension, Policy: OperationPolicy{Credentials: pluginCredentials, Risk: RiskContentWrite, Audit: AuditPlanned, RateLimits: pluginRateLimits}},
	{Method: http.MethodDelete, Path: PathStorageValue, Contract: ContractPluginExtension, Policy: OperationPolicy{Credentials: pluginCredentials, Risk: RiskContentWrite, Audit: AuditPlanned, RateLimits: pluginRateLimits}},
}
