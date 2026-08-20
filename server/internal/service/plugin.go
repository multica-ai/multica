package service

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/featureflag"
	"github.com/multica-ai/multica/server/pkg/plugincontract"
	"github.com/multica-ai/multica/server/pkg/remotemcp"
)

// LocalSourcePrefix installs from MULTICA_PLUGIN_DIR instead of the network.
// Self-hosted operators and plugin authors need a loop that does not require
// publishing to a public HTTPS host first.
const LocalSourcePrefix = "local:"

// PluginService owns plugin installation, configuration, and plugin-owned
// state. It never executes plugin code: a plugin runs only in the user's
// browser inside a sandboxed iframe, or on the author's own server.
type PluginService struct {
	Queries   *db.Queries
	TxStarter TxStarter
	// Secrets encrypts `secret` config values. Nil disables secret writes so a
	// misconfigured deployment fails closed instead of storing plaintext.
	Secrets *secretbox.Box
	// LocalDir is MULTICA_PLUGIN_DIR. Empty disables local sources.
	LocalDir string
	// DevOrigins is MULTICA_PLUGIN_DEV_ORIGINS: origins a plugin author may
	// serve a manifest from while building one, typically a localhost static
	// server. Empty in every deployment that has not opted in, which is the
	// only reason it is safe to skip the public-HTTPS guard for them.
	DevOrigins []string
	// Host gates which declared contributions this build can actually run.
	Host plugincontract.Capabilities
	// DeploymentKey is the raw MULTICA_PLUGIN_SECRET_KEY, used to derive each
	// installation's hook signing secret. Held separately from Secrets because
	// signing needs a key it can reproduce, not a sealed box.
	DeploymentKey []byte
	// Callbacks issues the short-lived tokens a hook handler uses to call back.
	// Nil means hooks go out without one.
	Callbacks *CallbackTokens
	// CallbackBaseURL is the Action API root a hook handler should call, sent
	// alongside the callback token so a plugin does not hardcode our hostname.
	CallbackBaseURL string
	// FeatureFlags gates the hook engine. Held here because the EVENT path has
	// no request to read the flag from: it runs on a worker, so the check has to
	// live with the service rather than in a handler. Nil reads as disabled,
	// which is the safe direction for the one path that reaches a third party.
	FeatureFlags *featureflag.Service
	// HookClient overrides the outbound client, and ONLY for an endpoint whose
	// origin the operator already named in DevOrigins. Nil everywhere else, so
	// a public endpoint always goes through the SSRF-guarded client and this
	// field cannot widen what a deployment can reach.
	HookClient *http.Client
}

func NewPluginService(queries *db.Queries, txStarter TxStarter) *PluginService {
	service := &PluginService{
		Queries:    queries,
		TxStarter:  txStarter,
		LocalDir:   strings.TrimSpace(os.Getenv("MULTICA_PLUGIN_DIR")),
		DevOrigins: parseDevOrigins(os.Getenv("MULTICA_PLUGIN_DEV_ORIGINS")),
		Host:       plugincontract.HostCapabilities(),
	}
	service.HookClient = devHookClient(service.DevOrigins, os.Getenv("MULTICA_PLUGIN_DEV_CA"))
	return service
}

// devHookClient trusts an additional CA for dev-origin hook endpoints only.
//
// A hook URL must be HTTPS — the manifest requires it — so an author testing
// against a local server needs its certificate trusted by something. This adds
// a named CA file rather than turning verification off: the failure mode of
// InsecureSkipVerify is that it survives into production behind a config flag
// somebody forgot, and there is no way to tell from the code whether it is
// active. A CA that has to be supplied by path cannot silently apply to
// anything else.
//
// Nil unless BOTH an origin allowlist and a CA path are set, and it is only
// ever consulted for an endpoint already inside that allowlist.
func devHookClient(devOrigins []string, caPath string) *http.Client {
	if len(devOrigins) == 0 || strings.TrimSpace(caPath) == "" {
		return nil
	}
	pem, err := os.ReadFile(caPath)
	if err != nil {
		slog.Warn("plugins: MULTICA_PLUGIN_DEV_CA could not be read; dev hook endpoints will not be trusted", "path", caPath, "error", err)
		return nil
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		slog.Warn("plugins: MULTICA_PLUGIN_DEV_CA contained no certificates", "path", caPath)
		return nil
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	slog.Info("plugins: dev hook endpoints will trust an additional CA", "path", caPath, "origins", devOrigins)
	return &http.Client{
		Timeout:   30 * time.Second,
		Transport: transport,
		// Refuse redirects, matching the secure client. A 302 from a dev
		// endpoint would replay the SIGNED body and the callback token to
		// wherever it pointed — the one destination check this path skipped is
		// exactly the one a redirect would need.
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("hook endpoints must not redirect")
		},
	}
}

type PluginErrorKind string

const (
	PluginErrorInvalid      PluginErrorKind = "invalid"
	PluginErrorNotFound     PluginErrorKind = "not_found"
	PluginErrorConflict     PluginErrorKind = "conflict"
	PluginErrorForbidden    PluginErrorKind = "forbidden"
	PluginErrorIncompatible PluginErrorKind = "incompatible"
	PluginErrorQuota        PluginErrorKind = "quota"
	PluginErrorUnavailable  PluginErrorKind = "unavailable"
)

type PluginError struct {
	Kind    PluginErrorKind
	Message string
	Err     error
}

func (e *PluginError) Error() string {
	if e.Err != nil {
		return e.Message + ": " + e.Err.Error()
	}
	return e.Message
}

func (e *PluginError) Unwrap() error { return e.Err }

func pluginErrf(kind PluginErrorKind, format string, args ...any) *PluginError {
	return &PluginError{Kind: kind, Message: fmt.Sprintf(format, args...)}
}

// PluginPreview is what the consent screen renders. It is deliberately the raw
// manifest text plus the scope list: there is no signature, no trust tier, and
// no publisher verification in this model, so the administrator reading the
// scopes IS the trust decision.
type PluginPreview struct {
	Manifest     plugincontract.Manifest `json:"manifest"`
	Scopes       []string                `json:"scopes"`
	ConfigSchema []PluginConfigField     `json:"config_schema"`
	// Upgrade describes what changes if this replaces an existing install.
	Installed        bool     `json:"installed"`
	InstalledVersion string   `json:"installed_version,omitempty"`
	AddedScopes      []string `json:"added_scopes,omitempty"`
}

// PluginConfigField is one host-rendered configuration input.
type PluginConfigField struct {
	Key         string   `json:"key"`
	Type        string   `json:"type"`
	Label       string   `json:"label"`
	Description string   `json:"description,omitempty"`
	Required    bool     `json:"required"`
	Options     []string `json:"options,omitempty"`
	Placeholder string   `json:"placeholder,omitempty"`
	Multiline   bool     `json:"multiline,omitempty"`
}

// ConfigFieldsForManifest flattens the manifest config schema into declaration
// order so the client renders the same form the server validates against.
func ConfigFieldsForManifest(manifest plugincontract.Manifest) []PluginConfigField {
	fields := make([]PluginConfigField, 0, manifest.Config.Len())
	for _, field := range manifest.Config.Fields {
		fields = append(fields, PluginConfigField{
			Key:         field.Key,
			Type:        field.Type,
			Label:       field.Label,
			Description: field.Description,
			Required:    field.Required,
			Options:     field.Options,
			Placeholder: field.Placeholder,
			Multiline:   field.Multiline,
		})
	}
	return fields
}

// FetchManifest resolves a source URL to a parsed manifest.
//
// Network sources reuse the Remote MCP endpoint guard: public HTTPS only, no
// userinfo/query/fragment, private and metadata address ranges refused, and the
// dialer re-checks the resolved address so a DNS rebind between validation and
// connection cannot reach an internal host.
func (s *PluginService) FetchManifest(ctx context.Context, sourceURL string) (plugincontract.Manifest, []byte, error) {
	sourceURL = strings.TrimSpace(sourceURL)
	if sourceURL == "" {
		return plugincontract.Manifest{}, nil, pluginErrf(PluginErrorInvalid, "source_url is required")
	}
	if len(sourceURL) > 2048 {
		return plugincontract.Manifest{}, nil, pluginErrf(PluginErrorInvalid, "source_url is too long")
	}

	var raw []byte
	var err error
	switch {
	case strings.HasPrefix(sourceURL, LocalSourcePrefix):
		raw, err = s.readLocalManifest(strings.TrimPrefix(sourceURL, LocalSourcePrefix))
	case s.isDevOrigin(sourceURL):
		// Explicitly opted in by the operator, so the public-HTTPS guard is
		// skipped rather than worked around. This is what makes iterating on a
		// surface possible: the author serves it from localhost and installs by
		// URL, exactly as a published plugin would be.
		raw, err = fetchDevManifest(ctx, sourceURL)
	default:
		raw, err = fetchRemoteManifest(ctx, sourceURL)
	}
	if err != nil {
		return plugincontract.Manifest{}, nil, err
	}

	manifest, canonical, parseErr := plugincontract.ParseManifest(raw)
	if parseErr != nil {
		return plugincontract.Manifest{}, nil, &PluginError{Kind: PluginErrorInvalid, Message: "plugin manifest is invalid", Err: parseErr}
	}
	return manifest, canonical, nil
}

// parseDevOrigins reads the opt-in list. Anything that is not a bare origin is
// dropped rather than half-honored: a path or query here would read as a broader
// grant than it is.
func parseDevOrigins(raw string) []string {
	origins := make([]string, 0)
	for _, candidate := range strings.Split(raw, ",") {
		candidate = strings.TrimRight(strings.TrimSpace(candidate), "/")
		if candidate == "" {
			continue
		}
		parsed, err := url.Parse(candidate)
		if err != nil || parsed.Host == "" || parsed.Path != "" || parsed.RawQuery != "" {
			continue
		}
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			continue
		}
		origins = append(origins, parsed.Scheme+"://"+parsed.Host)
	}
	return origins
}

func (s *PluginService) isDevOrigin(sourceURL string) bool {
	if len(s.DevOrigins) == 0 {
		return false
	}
	parsed, err := url.Parse(sourceURL)
	if err != nil || parsed.Host == "" {
		return false
	}
	origin := parsed.Scheme + "://" + parsed.Host
	for _, allowed := range s.DevOrigins {
		if origin == allowed {
			return true
		}
	}
	return false
}

// fetchDevManifest is the opted-in path: an ordinary bounded GET with no SSRF
// guard, because the operator named this exact origin.
func fetchDevManifest(ctx context.Context, sourceURL string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return nil, &PluginError{Kind: PluginErrorInvalid, Message: "build manifest request", Err: err}
	}
	response, err := (&http.Client{Timeout: 10 * time.Second}).Do(request)
	if err != nil {
		return nil, &PluginError{Kind: PluginErrorUnavailable, Message: "fetch plugin manifest", Err: err}
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, pluginErrf(PluginErrorUnavailable, "plugin manifest request returned HTTP %d", response.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, plugincontract.MaxManifestSize+1))
	if err != nil {
		return nil, &PluginError{Kind: PluginErrorUnavailable, Message: "read plugin manifest", Err: err}
	}
	if len(raw) > plugincontract.MaxManifestSize {
		return nil, pluginErrf(PluginErrorInvalid, "plugin manifest exceeds %d bytes", plugincontract.MaxManifestSize)
	}
	return raw, nil
}

func (s *PluginService) readLocalManifest(name string) ([]byte, error) {
	if s.LocalDir == "" {
		return nil, pluginErrf(PluginErrorInvalid, "local plugin sources require MULTICA_PLUGIN_DIR")
	}
	// The name indexes a directory the operator already controls, but it still
	// arrives over the API, so it must not be able to escape the configured
	// root or name a dotfile path.
	if name == "" || strings.ContainsAny(name, `/\`) || strings.HasPrefix(name, ".") {
		return nil, pluginErrf(PluginErrorInvalid, "local plugin source must be a single directory name under MULTICA_PLUGIN_DIR")
	}
	path := filepath.Join(s.LocalDir, name, plugincontract.ManifestFilename)
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, pluginErrf(PluginErrorNotFound, "no %s found for local plugin %q", plugincontract.ManifestFilename, name)
	}
	if err != nil {
		return nil, &PluginError{Kind: PluginErrorInvalid, Message: "read local plugin manifest", Err: err}
	}
	return raw, nil
}

func fetchRemoteManifest(ctx context.Context, sourceURL string) ([]byte, error) {
	endpoint, err := remotemcp.ValidatePublicHTTPSEndpoint(ctx, sourceURL, nil, nil)
	if err != nil {
		return nil, &PluginError{Kind: PluginErrorInvalid, Message: "source_url must be a plain public HTTPS URL", Err: err}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, &PluginError{Kind: PluginErrorInvalid, Message: "build manifest request", Err: err}
	}
	request.Header.Set("Accept", "application/json")
	response, err := remotemcp.NewSecureHTTPClient(endpoint).Do(request)
	if err != nil {
		return nil, &PluginError{Kind: PluginErrorUnavailable, Message: "fetch plugin manifest", Err: err}
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, pluginErrf(PluginErrorUnavailable, "plugin manifest request returned HTTP %d", response.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, plugincontract.MaxManifestSize+1))
	if err != nil {
		return nil, &PluginError{Kind: PluginErrorUnavailable, Message: "read plugin manifest", Err: err}
	}
	if len(raw) > plugincontract.MaxManifestSize {
		return nil, pluginErrf(PluginErrorInvalid, "plugin manifest exceeds %d bytes", plugincontract.MaxManifestSize)
	}
	return raw, nil
}

// PreviewPlugin fetches and parses a manifest without writing anything. It is
// the first half of the two-step install: the administrator must see the scopes
// before an installation row exists.
func (s *PluginService) PreviewPlugin(ctx context.Context, workspaceID pgtype.UUID, sourceURL string) (*PluginPreview, error) {
	manifest, _, err := s.FetchManifest(ctx, sourceURL)
	if err != nil {
		return nil, err
	}
	if err := manifest.CheckCapabilities(s.Host); err != nil {
		return nil, &PluginError{Kind: PluginErrorIncompatible, Message: capabilityMessage(err), Err: err}
	}

	preview := &PluginPreview{
		Manifest:     manifest,
		Scopes:       manifest.Scopes,
		ConfigSchema: ConfigFieldsForManifest(manifest),
	}
	existing, err := s.Queries.GetWorkspacePluginInstallationByKey(ctx, db.GetWorkspacePluginInstallationByKeyParams{
		WorkspaceID: workspaceID,
		PluginKey:   manifest.Key,
	})
	if err == nil {
		preview.Installed = true
		preview.InstalledVersion = existing.Version
		preview.AddedScopes = addedScopes(decodeScopes(existing.GrantedScopes), manifest.Scopes)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, &PluginError{Kind: PluginErrorUnavailable, Message: "load existing installation", Err: err}
	}
	return preview, nil
}

func capabilityMessage(err error) string {
	var unavailable *plugincontract.ErrCapabilityUnavailable
	if errors.As(err, &unavailable) {
		return "This plugin declares capabilities that are not enabled yet: " + strings.Join(unavailable.Missing, ", ")
	}
	return "This plugin declares capabilities that are not enabled yet"
}

func addedScopes(granted []string, wanted []string) []string {
	have := make(map[string]bool, len(granted))
	for _, scope := range granted {
		have[scope] = true
	}
	added := make([]string, 0)
	for _, scope := range wanted {
		if !have[scope] {
			added = append(added, scope)
		}
	}
	return added
}

func decodeScopes(raw []byte) []string {
	var scopes []string
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, &scopes); err != nil {
		return nil
	}
	return scopes
}

// InstallPlugin is the second half of the install. grantedScopes must match the
// manifest exactly: partial consent would leave the plugin silently broken, and
// consenting to a scope the manifest does not request would grant access the
// administrator was never shown a reason for.
func (s *PluginService) InstallPlugin(ctx context.Context, workspaceID, userID pgtype.UUID, sourceURL string, grantedScopes []string) (db.PluginInstallation, error) {
	manifest, canonical, err := s.FetchManifest(ctx, sourceURL)
	if err != nil {
		return db.PluginInstallation{}, err
	}
	if err := manifest.CheckCapabilities(s.Host); err != nil {
		return db.PluginInstallation{}, &PluginError{Kind: PluginErrorIncompatible, Message: capabilityMessage(err), Err: err}
	}
	if err := requireExactScopes(manifest.Scopes, grantedScopes); err != nil {
		return db.PluginInstallation{}, err
	}
	scopesJSON, err := json.Marshal(manifest.Scopes)
	if err != nil {
		return db.PluginInstallation{}, &PluginError{Kind: PluginErrorInvalid, Message: "encode granted scopes", Err: err}
	}

	existing, err := s.Queries.GetWorkspacePluginInstallationByKey(ctx, db.GetWorkspacePluginInstallationByKeyParams{
		WorkspaceID: workspaceID,
		PluginKey:   manifest.Key,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// A transaction even though it is one row: the skill resources land in
		// the same commit. An installation whose skills half-arrived is worse
		// than one that failed, because the missing half is invisible.
		tx, txErr := s.TxStarter.Begin(ctx)
		if txErr != nil {
			return db.PluginInstallation{}, &PluginError{Kind: PluginErrorUnavailable, Message: "begin install", Err: txErr}
		}
		defer func() { _ = tx.Rollback(ctx) }()
		queries := s.Queries.WithTx(tx)

		installation, createErr := queries.CreatePluginInstallation(ctx, db.CreatePluginInstallationParams{
			WorkspaceID:   workspaceID,
			PluginKey:     manifest.Key,
			SourceUrl:     sourceURL,
			Version:       manifest.Version,
			Manifest:      canonical,
			GrantedScopes: scopesJSON,
			InstalledBy:   userID,
		})
		if createErr != nil {
			// Two admins installing the same plugin at once: one loses the
			// unique index. That is a conflict to retry, not a broken backend.
			if isUniqueViolation(createErr) {
				return db.PluginInstallation{}, pluginErrf(PluginErrorConflict, "this plugin is already installed in this workspace")
			}
			return db.PluginInstallation{}, &PluginError{Kind: PluginErrorUnavailable, Message: "install plugin", Err: createErr}
		}
		if skillErr := s.InstallSkillResources(ctx, queries, installation, manifest, sourceURL, userID); skillErr != nil {
			return db.PluginInstallation{}, skillErr
		}
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return db.PluginInstallation{}, &PluginError{Kind: PluginErrorUnavailable, Message: "commit install", Err: commitErr}
		}
		return installation, nil
	}
	if err != nil {
		return db.PluginInstallation{}, &PluginError{Kind: PluginErrorUnavailable, Message: "load existing installation", Err: err}
	}

	// Upgrade in place. Values whose fields the new manifest dropped are pruned
	// rather than left as state nothing can reach — and that applies to secrets
	// too, which is the case where unreachable residue is ciphertext. The whole
	// upgrade is one transaction so a snapshot can never land with the previous
	// version's secrets still attached.
	config := pruneConfig(existing.Config, manifest)
	orphanedSecrets, err := s.orphanedSecretKeys(ctx, existing.ID, manifest)
	if err != nil {
		return db.PluginInstallation{}, err
	}

	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return db.PluginInstallation{}, &PluginError{Kind: PluginErrorUnavailable, Message: "begin upgrade", Err: err}
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := s.Queries.WithTx(tx)

	for _, key := range orphanedSecrets {
		if _, err := queries.DeletePluginSecret(ctx, db.DeletePluginSecretParams{InstallationID: existing.ID, Key: key}); err != nil {
			return db.PluginInstallation{}, &PluginError{Kind: PluginErrorUnavailable, Message: "prune plugin secret", Err: err}
		}
	}
	updated, err := queries.UpdatePluginInstallationManifest(ctx, db.UpdatePluginInstallationManifestParams{
		ID:            existing.ID,
		SourceUrl:     sourceURL,
		Version:       manifest.Version,
		Manifest:      canonical,
		GrantedScopes: scopesJSON,
		Config:        config,
	})
	if err != nil {
		return db.PluginInstallation{}, &PluginError{Kind: PluginErrorUnavailable, Message: "upgrade plugin", Err: err}
	}
	// Re-run on upgrade so a changed SKILL.md takes effect and a dropped one is
	// pruned. Same transaction as the manifest snapshot: the two must never
	// disagree about what this version contributes.
	if err := s.InstallSkillResources(ctx, queries, updated, manifest, sourceURL, userID); err != nil {
		return db.PluginInstallation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return db.PluginInstallation{}, &PluginError{Kind: PluginErrorUnavailable, Message: "commit upgrade", Err: err}
	}
	return updated, nil
}

// orphanedSecretKeys returns stored secrets the new manifest no longer declares
// as a secret field — including a field that changed type away from secret.
func (s *PluginService) orphanedSecretKeys(ctx context.Context, installationID pgtype.UUID, manifest plugincontract.Manifest) ([]string, error) {
	stored, err := s.Queries.ListPluginSecretKeys(ctx, installationID)
	if err != nil {
		return nil, &PluginError{Kind: PluginErrorUnavailable, Message: "list plugin secrets", Err: err}
	}
	orphaned := make([]string, 0)
	for _, row := range stored {
		field, ok := manifest.Config.Field(row.Key)
		if !ok || field.Type != plugincontract.ConfigSecret {
			orphaned = append(orphaned, row.Key)
		}
	}
	return orphaned, nil
}

func requireExactScopes(manifestScopes, grantedScopes []string) error {
	if len(manifestScopes) != len(grantedScopes) {
		return pluginErrf(PluginErrorConflict, "granted_scopes must match the manifest scopes exactly")
	}
	wanted := make(map[string]bool, len(manifestScopes))
	for _, scope := range manifestScopes {
		wanted[scope] = true
	}
	for _, scope := range grantedScopes {
		if !wanted[scope] {
			return pluginErrf(PluginErrorConflict, "granted_scopes contains %q, which the manifest does not request", scope)
		}
	}
	return nil
}

func pruneConfig(raw []byte, manifest plugincontract.Manifest) []byte {
	var values map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &values) != nil {
		return []byte("{}")
	}
	for key := range values {
		if _, ok := manifest.Config.Field(key); !ok {
			delete(values, key)
		}
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return []byte("{}")
	}
	return encoded
}

func parseUUIDValue(value string) (pgtype.UUID, error) { return util.ParseUUID(value) }

func uuidString(value pgtype.UUID) string { return util.UUIDToString(value) }

// ParseInstallationManifest reads back the consented snapshot. Callers must use
// this, never a freshly fetched manifest: what the source URL serves today is
// not what the administrator approved.
func ParseInstallationManifest(installation db.PluginInstallation) (plugincontract.Manifest, error) {
	var manifest plugincontract.Manifest
	if err := json.Unmarshal(installation.Manifest, &manifest); err != nil {
		return plugincontract.Manifest{}, fmt.Errorf("decode stored plugin manifest: %w", err)
	}
	return manifest, nil
}

// SetConfig validates values against the stored manifest schema and splits them
// by destination: plain values go on the installation row, secrets go to the
// encrypted table. A secret value never lands in `config`.
func (s *PluginService) SetConfig(ctx context.Context, installation db.PluginInstallation, values map[string]any) (db.PluginInstallation, error) {
	manifest, err := ParseInstallationManifest(installation)
	if err != nil {
		return db.PluginInstallation{}, &PluginError{Kind: PluginErrorInvalid, Message: "stored plugin manifest is unreadable", Err: err}
	}

	plain := map[string]any{}
	secrets := map[string]string{}
	for key, value := range values {
		field, ok := manifest.Config.Field(key)
		if !ok {
			return db.PluginInstallation{}, pluginErrf(PluginErrorInvalid, "unknown config field %q", key)
		}
		if field.Type == plugincontract.ConfigSecret {
			text, ok := value.(string)
			if !ok {
				return db.PluginInstallation{}, pluginErrf(PluginErrorInvalid, "config field %q must be a string", key)
			}
			if len(text) > MaxPluginSecretBytes {
				return db.PluginInstallation{}, pluginErrf(PluginErrorInvalid, "config field %q exceeds %d bytes", key, MaxPluginSecretBytes)
			}
			secrets[key] = text
			continue
		}
		normalized, err := normalizeConfigValue(field, value)
		if err != nil {
			return db.PluginInstallation{}, err
		}
		plain[key] = normalized
	}

	// Merge over the stored values so a partial update does not silently clear
	// fields the form did not submit.
	merged := map[string]any{}
	if len(installation.Config) > 0 {
		_ = json.Unmarshal(installation.Config, &merged)
	}
	for key, value := range plain {
		merged[key] = value
	}
	encoded, err := json.Marshal(merged)
	if err != nil {
		return db.PluginInstallation{}, &PluginError{Kind: PluginErrorInvalid, Message: "encode plugin config", Err: err}
	}

	if len(secrets) > 0 && s.Secrets == nil {
		return db.PluginInstallation{}, pluginErrf(PluginErrorUnavailable, "plugin secrets are disabled: MULTICA_PLUGIN_SECRET_KEY is not configured")
	}

	// Secrets and plain values are two tables with no foreign key between them,
	// so one transaction is what keeps a saved form from landing half-applied.
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return db.PluginInstallation{}, &PluginError{Kind: PluginErrorUnavailable, Message: "begin configure", Err: err}
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := s.Queries.WithTx(tx)

	for key, text := range secrets {
		// An empty submission clears the secret rather than storing "".
		if text == "" {
			if _, err := queries.DeletePluginSecret(ctx, db.DeletePluginSecretParams{InstallationID: installation.ID, Key: key}); err != nil {
				return db.PluginInstallation{}, &PluginError{Kind: PluginErrorUnavailable, Message: "clear plugin secret", Err: err}
			}
			continue
		}
		ciphertext, sealErr := s.Secrets.Seal([]byte(text))
		if sealErr != nil {
			return db.PluginInstallation{}, &PluginError{Kind: PluginErrorUnavailable, Message: "encrypt plugin secret", Err: sealErr}
		}
		if err := queries.UpsertPluginSecret(ctx, db.UpsertPluginSecretParams{
			InstallationID: installation.ID,
			Key:            key,
			Ciphertext:     ciphertext,
		}); err != nil {
			return db.PluginInstallation{}, &PluginError{Kind: PluginErrorUnavailable, Message: "store plugin secret", Err: err}
		}
	}

	updated, err := queries.UpdatePluginInstallationConfig(ctx, db.UpdatePluginInstallationConfigParams{
		ID:     installation.ID,
		Config: encoded,
	})
	if err != nil {
		return db.PluginInstallation{}, &PluginError{Kind: PluginErrorUnavailable, Message: "store plugin config", Err: err}
	}
	if err := tx.Commit(ctx); err != nil {
		return db.PluginInstallation{}, &PluginError{Kind: PluginErrorUnavailable, Message: "commit configure", Err: err}
	}
	return updated, nil
}

// isUniqueViolation reports a Postgres 23505, the only class of write conflict
// the plugin surface can produce.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// MaxPluginSecretBytes bounds one secret value before encryption.
const MaxPluginSecretBytes = 8192

func normalizeConfigValue(field plugincontract.ConfigField, value any) (any, error) {
	label := fmt.Sprintf("config field %q", field.Key)
	switch field.Type {
	case plugincontract.ConfigString:
		text, ok := value.(string)
		if !ok {
			return nil, pluginErrf(PluginErrorInvalid, "%s must be a string", label)
		}
		if len(text) > 4096 {
			return nil, pluginErrf(PluginErrorInvalid, "%s exceeds 4096 bytes", label)
		}
		return text, nil
	case plugincontract.ConfigNumber:
		number, ok := value.(float64)
		if !ok {
			return nil, pluginErrf(PluginErrorInvalid, "%s must be a number", label)
		}
		return number, nil
	case plugincontract.ConfigBool:
		flag, ok := value.(bool)
		if !ok {
			return nil, pluginErrf(PluginErrorInvalid, "%s must be a boolean", label)
		}
		return flag, nil
	case plugincontract.ConfigEnum:
		text, ok := value.(string)
		if !ok {
			return nil, pluginErrf(PluginErrorInvalid, "%s must be a string", label)
		}
		for _, option := range field.Options {
			if option == text {
				return text, nil
			}
		}
		return nil, pluginErrf(PluginErrorInvalid, "%s must be one of the declared options", label)
	default:
		return nil, pluginErrf(PluginErrorInvalid, "%s has an unsupported type", label)
	}
}

// ConfiguredSecretKeys reports which secret fields hold a value. It returns
// names only — no endpoint ever returns a secret value, not even masked.
func (s *PluginService) ConfiguredSecretKeys(ctx context.Context, installationID pgtype.UUID) ([]string, error) {
	rows, err := s.Queries.ListPluginSecretKeys(ctx, installationID)
	if err != nil {
		return nil, &PluginError{Kind: PluginErrorUnavailable, Message: "list plugin secrets", Err: err}
	}
	keys := make([]string, 0, len(rows))
	for _, row := range rows {
		keys = append(keys, row.Key)
	}
	return keys, nil
}

// SetEnabled toggles an installation. Disabling hides every contribution
// immediately but keeps storage and secrets, so re-enabling is not a reinstall.
func (s *PluginService) SetEnabled(ctx context.Context, installation db.PluginInstallation, enabled bool) (db.PluginInstallation, error) {
	updated, err := s.Queries.SetPluginInstallationEnabled(ctx, db.SetPluginInstallationEnabledParams{
		ID:      installation.ID,
		Enabled: enabled,
	})
	if err != nil {
		return db.PluginInstallation{}, &PluginError{Kind: PluginErrorUnavailable, Message: "update plugin state", Err: err}
	}
	return updated, nil
}

// Uninstall removes the installation with its storage and secrets. There are no
// foreign keys or cascades by repository policy, so the three deletes share one
// transaction: a partial uninstall would strand plugin-owned rows nothing can
// reach or clean up later.
func (s *PluginService) Uninstall(ctx context.Context, installation db.PluginInstallation) error {
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return &PluginError{Kind: PluginErrorUnavailable, Message: "begin uninstall", Err: err}
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := s.Queries.WithTx(tx)
	if err := queries.DeletePluginStorageByInstallation(ctx, installation.ID); err != nil {
		return &PluginError{Kind: PluginErrorUnavailable, Message: "delete plugin storage", Err: err}
	}
	if err := queries.DeletePluginSecretsByInstallation(ctx, installation.ID); err != nil {
		return &PluginError{Kind: PluginErrorUnavailable, Message: "delete plugin secrets", Err: err}
	}
	// Hook records go too. They name an installation that is about to stop
	// existing, and keeping them would leave rows nothing can attribute — the
	// same reason storage and secrets are cleared here rather than swept later.
	if err := queries.DeletePluginInvocationsByInstallation(ctx, installation.ID); err != nil {
		return &PluginError{Kind: PluginErrorUnavailable, Message: "delete plugin invocations", Err: err}
	}
	// The skills this installation contributed go with it. Scoped by
	// plugin_installation_id, so a skill a person wrote is untouched even if it
	// happens to share a name with something the plugin once provided.
	if err := queries.DeletePluginSkillsByInstallation(ctx, installation.ID); err != nil {
		return &PluginError{Kind: PluginErrorUnavailable, Message: "delete plugin skills", Err: err}
	}
	if err := queries.DeletePluginInstallation(ctx, installation.ID); err != nil {
		return &PluginError{Kind: PluginErrorUnavailable, Message: "delete plugin installation", Err: err}
	}
	if err := tx.Commit(ctx); err != nil {
		return &PluginError{Kind: PluginErrorUnavailable, Message: "commit uninstall", Err: err}
	}
	return nil
}

// InstallationForWorkspace loads an installation and confirms it belongs to the
// workspace in the URL, so an id from another workspace cannot be operated on.
func (s *PluginService) InstallationForWorkspace(ctx context.Context, workspaceID pgtype.UUID, installationID string) (db.PluginInstallation, error) {
	parsed, err := util.ParseUUID(installationID)
	if err != nil {
		return db.PluginInstallation{}, pluginErrf(PluginErrorNotFound, "plugin installation not found")
	}
	installation, err := s.Queries.GetWorkspacePluginInstallation(ctx, db.GetWorkspacePluginInstallationParams{
		WorkspaceID: workspaceID,
		ID:          parsed,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.PluginInstallation{}, pluginErrf(PluginErrorNotFound, "plugin installation not found")
	}
	if err != nil {
		return db.PluginInstallation{}, &PluginError{Kind: PluginErrorUnavailable, Message: "load plugin installation", Err: err}
	}
	return installation, nil
}

// readLocalFile reads a file from inside a local plugin directory.
//
// Same containment as readLocalManifest, plus one more: the joined path is
// re-checked against the plugin's own directory after cleaning. `entry` is
// validated at manifest-parse time to be relative and traversal-free, but this
// is the layer that touches the filesystem and it does not get to assume that.
func (s *PluginService) readLocalFile(name, entry string) ([]byte, error) {
	if s.LocalDir == "" {
		return nil, pluginErrf(PluginErrorInvalid, "local plugin sources require MULTICA_PLUGIN_DIR")
	}
	if name == "" || strings.ContainsAny(name, `/\`) || strings.HasPrefix(name, ".") {
		return nil, pluginErrf(PluginErrorInvalid, "local plugin source must be a single directory name under MULTICA_PLUGIN_DIR")
	}
	root := filepath.Join(s.LocalDir, name)
	path := filepath.Clean(filepath.Join(root, entry))
	if path != root && !strings.HasPrefix(path, root+string(os.PathSeparator)) {
		return nil, pluginErrf(PluginErrorInvalid, "plugin resource %q escapes its directory", entry)
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, pluginErrf(PluginErrorNotFound, "plugin resource %q was not found", entry)
	}
	if err != nil {
		return nil, &PluginError{Kind: PluginErrorInvalid, Message: "read plugin resource", Err: err}
	}
	return raw, nil
}
