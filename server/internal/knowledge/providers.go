package knowledge

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/llm"
)

type providerRecord struct {
	Provider
	EncryptedAPIKey []byte
}

func scanProvider(row pgx.Row) (providerRecord, error) {
	var result providerRecord
	var hasKey bool
	err := row.Scan(&result.ID, &result.WorkspaceID, &result.Name, &result.Preset, &result.Protocol, &result.BaseURL, &hasKey, &result.SecretRevision, &result.IsEnabled, &result.Revision, &result.CreatedBy)
	if err != nil {
		return providerRecord{}, err
	}
	result.HasAPIKey = hasKey
	return result, nil
}

func scanProviderWithSecret(row pgx.Row) (providerRecord, error) {
	var result providerRecord
	err := row.Scan(&result.ID, &result.WorkspaceID, &result.Name, &result.Preset, &result.Protocol, &result.BaseURL, &result.EncryptedAPIKey, &result.SecretRevision, &result.IsEnabled, &result.Revision, &result.CreatedBy)
	if err != nil {
		return providerRecord{}, err
	}
	result.HasAPIKey = len(result.EncryptedAPIKey) > 0
	return result, nil
}

func providerSelect(includeSecret bool) string {
	secret := "(length(encrypted_api_key) > 0)"
	if includeSecret {
		secret = "encrypted_api_key"
	}
	return `id::text, workspace_id::text, name, preset, protocol, base_url, ` + secret + `, secret_revision, is_enabled, revision, created_by::text`
}

func validateProviderBaseURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", badRequest("invalid_provider_url", "provider base_url must be an absolute http or https URL without credentials or query parameters")
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.Fragment = ""
	if (parsed.Scheme == "http" && strings.HasSuffix(parsed.Host, ":80")) || (parsed.Scheme == "https" && strings.HasSuffix(parsed.Host, ":443")) {
		parsed.Host = parsed.Host[:len(parsed.Host)-3]
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}

// normalizePrivateModelHosts turns deployment configuration into the same
// host vocabulary used by validateProviderURL. A bare host is accepted for
// convenience, while a URL lets an operator pin a host and its port in the
// configuration. The exact host:port entry and the hostname-only entry are
// both retained so an explicit host allowlist can cover a gateway that moves
// between a default and a non-default port without accepting a different host.
func normalizePrivateModelHosts(raw string) []string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return nil
	}
	if !strings.Contains(value, "://") {
		value = "//" + value
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || parsed.User != nil || strings.Trim(parsed.Path, "/") != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Host, "."))
	if host == "" {
		return nil
	}
	result := []string{host}
	if hostname := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), ".")); hostname != "" && hostname != host {
		result = append(result, hostname)
	}
	return result
}

func (s *Service) validateProviderURL(raw string) (string, error) {
	normalized, err := validateProviderBaseURL(raw)
	if err != nil {
		return "", err
	}
	parsed, parseErr := url.Parse(normalized)
	if parseErr != nil {
		return "", internal("failed to normalize provider URL", parseErr)
	}
	if parsed.Scheme == "http" {
		host := strings.ToLower(strings.TrimSuffix(parsed.Host, "."))
		hostname := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
		if _, ok := s.privateModelHosts[host]; !ok {
			if _, ok := s.privateModelHosts[hostname]; !ok {
				return "", badRequest("http_provider_requires_allowlist", "plain HTTP provider addresses must be listed in KNOWLEDGE_PRIVATE_MODEL_HOSTS")
			}
		}
	}
	return normalized, nil
}

func validateProtocol(protocol string) string {
	protocol = strings.ToLower(strings.TrimSpace(protocol))
	if protocol == "" {
		protocol = llm.ProtocolOpenAI
	}
	return protocol
}

func validateProviderFields(name, protocol, baseURL string) (string, string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 120 {
		return "", "", badRequest("invalid_provider", "provider name is required and must be at most 120 characters")
	}
	protocol = validateProtocol(protocol)
	if protocol != llm.ProtocolOpenAI && protocol != llm.ProtocolCohere {
		return "", "", badRequest("invalid_provider_protocol", "provider protocol is not supported")
	}
	baseURL, err := validateProviderBaseURL(baseURL)
	if err != nil {
		return "", "", err
	}
	return name, baseURL, nil
}

func (s *Service) ListProviders(ctx context.Context, workspaceID string) ([]Provider, error) {
	if err := s.checkEnabled(); err != nil {
		return nil, err
	}
	ws, err := parseID(workspaceID, "workspace_id")
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(ctx, `SELECT `+providerSelect(false)+` FROM knowledge_provider WHERE workspace_id=$1 ORDER BY created_at DESC, id DESC`, ws)
	if err != nil {
		return nil, internal("failed to list knowledge providers", err)
	}
	defer rows.Close()
	providers := []Provider{}
	for rows.Next() {
		provider, scanErr := scanProvider(rows)
		if scanErr != nil {
			return nil, internal("failed to read knowledge provider", scanErr)
		}
		providers = append(providers, provider.Provider)
	}
	if err := rows.Err(); err != nil {
		return nil, internal("failed to list knowledge providers", err)
	}
	return providers, nil
}

func scanProviderWithCreatedAt(row pgx.Row) (providerRecord, time.Time, error) {
	var result providerRecord
	var hasKey bool
	var createdAt time.Time
	err := row.Scan(&result.ID, &result.WorkspaceID, &result.Name, &result.Preset, &result.Protocol, &result.BaseURL, &hasKey, &result.SecretRevision, &result.IsEnabled, &result.Revision, &result.CreatedBy, &createdAt)
	if err != nil {
		return providerRecord{}, time.Time{}, err
	}
	result.HasAPIKey = hasKey
	return result, createdAt, nil
}

func (s *Service) ListProvidersPage(ctx context.Context, workspaceID, cursor string, limit int) (ProviderPage, error) {
	if err := s.checkEnabled(); err != nil {
		return ProviderPage{}, err
	}
	ws, err := parseID(workspaceID, "workspace_id")
	if err != nil {
		return ProviderPage{}, err
	}
	limit = knowledgePageLimit(limit)
	query := `SELECT ` + providerSelect(false) + `,created_at FROM knowledge_provider WHERE workspace_id=$1`
	args := []any{ws}
	if strings.TrimSpace(cursor) != "" {
		createdAt, id, cursorErr := decodeKnowledgeCreatedCursor(cursor)
		if cursorErr != nil {
			return ProviderPage{}, cursorErr
		}
		query += ` AND (created_at<$2 OR (created_at=$2 AND id<$3))`
		args = append(args, createdAt, id)
	}
	query += fmt.Sprintf(" ORDER BY created_at DESC,id DESC LIMIT $%d", len(args)+1)
	args = append(args, limit+1)
	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return ProviderPage{}, internal("failed to list knowledge providers", err)
	}
	defer rows.Close()
	providers := make([]Provider, 0, limit)
	createdAt := make([]time.Time, 0, limit)
	for rows.Next() {
		provider, providerCreatedAt, scanErr := scanProviderWithCreatedAt(rows)
		if scanErr != nil {
			return ProviderPage{}, internal("failed to read knowledge provider", scanErr)
		}
		providers = append(providers, provider.Provider)
		createdAt = append(createdAt, providerCreatedAt)
	}
	if err := rows.Err(); err != nil {
		return ProviderPage{}, internal("failed to list knowledge providers", err)
	}
	page := ProviderPage{Providers: providers}
	if len(providers) > limit {
		page.Providers = providers[:limit]
		page.NextCursor = encodeKnowledgeCreatedCursor(createdAt[limit-1], page.Providers[limit-1].ID)
	}
	return page, nil
}

type CreateProviderInput struct {
	Name      string
	Preset    string
	Protocol  string
	BaseURL   string
	APIKey    string
	CreatedBy string
}

func (s *Service) CreateProvider(ctx context.Context, workspaceID string, input CreateProviderInput) (Provider, error) {
	if err := s.checkEnabled(); err != nil {
		return Provider{}, err
	}
	if s.secretBox == nil {
		return Provider{}, knowledgeError(http.StatusServiceUnavailable, "secret_not_configured", "knowledge provider secrets are not configured", nil)
	}
	ws, err := parseID(workspaceID, "workspace_id")
	if err != nil {
		return Provider{}, err
	}
	creator, err := parseID(input.CreatedBy, "user_id")
	if err != nil {
		return Provider{}, err
	}
	name, baseURL, err := validateProviderFields(input.Name, input.Protocol, input.BaseURL)
	if err != nil {
		return Provider{}, err
	}
	baseURL, err = s.validateProviderURL(baseURL)
	if err != nil {
		return Provider{}, err
	}
	if strings.TrimSpace(input.APIKey) == "" {
		return Provider{}, badRequest("api_key_required", "api_key is required and is never returned after saving")
	}
	protocol := validateProtocol(input.Protocol)
	sealed, err := s.secretBox.Seal([]byte(input.APIKey))
	if err != nil {
		return Provider{}, internal("failed to protect provider key", err)
	}
	preset := strings.TrimSpace(input.Preset)
	if preset == "" {
		preset = "custom"
	}
	tx, err := s.txStarter.Begin(ctx)
	if err != nil {
		return Provider{}, internal("failed to start knowledge provider transaction", err)
	}
	defer tx.Rollback(ctx)
	if err := lockKnowledgeWorkspace(ctx, tx, ws); err != nil {
		return Provider{}, err
	}
	result, err := scanProvider(tx.QueryRow(ctx, `
		INSERT INTO knowledge_provider(workspace_id,name,preset,protocol,base_url,encrypted_api_key,created_by)
		VALUES($1,$2,$3,$4,$5,$6,$7)
		RETURNING `+providerSelect(false), ws, name, preset, protocol, baseURL, sealed, creator))
	if err != nil {
		return Provider{}, internal("failed to create knowledge provider", err)
	}
	if err := resumeWaitingConfigJobs(ctx, tx, ws, nil); err != nil {
		return Provider{}, internal("failed to resume waiting knowledge jobs", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Provider{}, internal("failed to commit knowledge provider", err)
	}
	s.notifyWorkspaceInvalidations(ctx, workspaceID, input.CreatedBy)
	return result.Provider, nil
}

type UpdateProviderInput struct {
	Name             *string
	APIKey           *string
	IsEnabled        *bool
	BaseURL          *string
	Protocol         *string
	ExpectedRevision int64
}

func (s *Service) providerRecord(ctx context.Context, workspaceID, providerID string) (providerRecord, error) {
	ws, err := parseID(workspaceID, "workspace_id")
	if err != nil {
		return providerRecord{}, err
	}
	id, err := parseID(providerID, "provider_id")
	if err != nil {
		return providerRecord{}, err
	}
	result, err := scanProviderWithSecret(s.db.QueryRow(ctx, `SELECT `+providerSelect(true)+` FROM knowledge_provider WHERE id=$1 AND workspace_id=$2`, id, ws))
	if errors.Is(err, pgx.ErrNoRows) {
		return providerRecord{}, notFound()
	}
	if err != nil {
		return providerRecord{}, internal("failed to load knowledge provider", err)
	}
	return result, nil
}

func (s *Service) UpdateProvider(ctx context.Context, workspaceID, providerID string, input UpdateProviderInput) (Provider, error) {
	if err := s.checkEnabled(); err != nil {
		return Provider{}, err
	}
	if input.ExpectedRevision <= 0 {
		return Provider{}, badRequest("expected_revision_required", "expected_revision is required")
	}
	ws, err := parseID(workspaceID, "workspace_id")
	if err != nil {
		return Provider{}, err
	}
	id, err := parseID(providerID, "provider_id")
	if err != nil {
		return Provider{}, err
	}
	var current providerRecord
	var requestedURL, requestedProtocol string
	addressChanged := false
	if input.BaseURL != nil || input.Protocol != nil {
		var loadErr error
		current, loadErr = s.providerRecord(ctx, workspaceID, providerID)
		if loadErr != nil {
			return Provider{}, loadErr
		}
		requestedURL = current.BaseURL
		if input.BaseURL != nil {
			requestedURL, err = s.validateProviderURL(*input.BaseURL)
			if err != nil {
				return Provider{}, err
			}
		}
		requestedProtocol = current.Protocol
		if input.Protocol != nil {
			requestedProtocol = validateProtocol(*input.Protocol)
			if requestedProtocol != llm.ProtocolOpenAI && requestedProtocol != llm.ProtocolCohere {
				return Provider{}, badRequest("invalid_provider_protocol", "provider protocol is not supported")
			}
		}
		addressChanged = requestedURL != current.BaseURL || requestedProtocol != current.Protocol
	}
	name := cleanOptional(input.Name)
	if input.Name != nil && (strings.TrimSpace(*input.Name) == "" || len([]rune(strings.TrimSpace(*input.Name))) > 120) {
		return Provider{}, badRequest("invalid_provider", "provider name must be at most 120 characters")
	}
	if addressChanged {
		// A new address is a new trust domain. Requiring a key in the same
		// request prevents the old secret from being silently copied to a new
		// host while keeping the old immutable connection usable by existing
		// bindings and indexes.
		if input.APIKey == nil || strings.TrimSpace(*input.APIKey) == "" {
			return Provider{}, badRequest("api_key_required_for_provider_replacement", "changing a provider address or protocol requires a new api_key")
		}
		if s.secretBox == nil {
			return Provider{}, knowledgeError(http.StatusServiceUnavailable, "secret_not_configured", "knowledge provider secrets are not configured", nil)
		}
		sealed, sealErr := s.secretBox.Seal([]byte(*input.APIKey))
		if sealErr != nil {
			return Provider{}, internal("failed to protect provider key", sealErr)
		}
		nameValue := current.Name
		if input.Name != nil {
			nameValue = strings.TrimSpace(*input.Name)
		}
		enabledValue := current.IsEnabled
		if input.IsEnabled != nil {
			enabledValue = *input.IsEnabled
		}
		tx, txErr := s.txStarter.Begin(ctx)
		if txErr != nil {
			return Provider{}, internal("failed to start provider replacement", txErr)
		}
		defer tx.Rollback(ctx)
		if txErr = lockKnowledgeWorkspace(ctx, tx, ws); txErr != nil {
			return Provider{}, txErr
		}
		var lockedRevision int64
		if txErr = tx.QueryRow(ctx, `SELECT revision FROM knowledge_provider WHERE id=$1 AND workspace_id=$2 FOR UPDATE`, id, ws).Scan(&lockedRevision); txErr != nil {
			if errors.Is(txErr, pgx.ErrNoRows) {
				return Provider{}, notFound()
			}
			return Provider{}, internal("failed to lock provider for replacement", txErr)
		}
		if lockedRevision != input.ExpectedRevision {
			return Provider{}, conflict("revision_conflict", "provider changed; refresh before editing")
		}
		replacement, insertErr := scanProvider(tx.QueryRow(ctx, `
			INSERT INTO knowledge_provider(workspace_id,name,preset,protocol,base_url,encrypted_api_key,is_enabled,created_by)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8)
			RETURNING `+providerSelect(false), ws, nameValue, current.Preset, requestedProtocol, requestedURL, sealed, enabledValue, mustID(current.CreatedBy)))
		if insertErr != nil {
			return Provider{}, internal("failed to create replacement provider", insertErr)
		}
		if txErr = tx.Commit(ctx); txErr != nil {
			return Provider{}, internal("failed to commit provider replacement", txErr)
		}
		s.notifyWorkspaceInvalidations(ctx, workspaceID, "")
		return replacement.Provider, nil
	}
	if input.APIKey != nil {
		if strings.TrimSpace(*input.APIKey) == "" {
			return Provider{}, badRequest("api_key_required", "api_key cannot be cleared; rotate it with a non-empty value")
		}
		if s.secretBox == nil {
			return Provider{}, knowledgeError(http.StatusServiceUnavailable, "secret_not_configured", "knowledge provider secrets are not configured", nil)
		}
		sealed, sealErr := s.secretBox.Seal([]byte(*input.APIKey))
		if sealErr != nil {
			return Provider{}, internal("failed to protect provider key", sealErr)
		}
		tx, txErr := s.txStarter.Begin(ctx)
		if txErr != nil {
			return Provider{}, internal("failed to start provider key update", txErr)
		}
		defer tx.Rollback(ctx)
		if txErr = lockKnowledgeWorkspace(ctx, tx, ws); txErr != nil {
			return Provider{}, txErr
		}
		result, updateErr := scanProvider(tx.QueryRow(ctx, `
			UPDATE knowledge_provider SET name=COALESCE($3,name), is_enabled=COALESCE($4,is_enabled),
			       encrypted_api_key=$5, secret_revision=secret_revision+1, revision=revision+1, updated_at=now()
			WHERE id=$1 AND workspace_id=$2 AND revision=$6
			RETURNING `+providerSelect(false), id, ws, name, input.IsEnabled, sealed, input.ExpectedRevision))
		if errors.Is(updateErr, pgx.ErrNoRows) {
			return Provider{}, conflict("revision_conflict", "provider changed; refresh before editing")
		}
		if updateErr != nil {
			return Provider{}, internal("failed to update knowledge provider", updateErr)
		}
		if txErr = resumeWaitingConfigJobs(ctx, tx, ws, nil); txErr != nil {
			return Provider{}, internal("failed to resume waiting knowledge jobs", txErr)
		}
		if txErr = tx.Commit(ctx); txErr != nil {
			return Provider{}, internal("failed to commit provider key update", txErr)
		}
		s.notifyWorkspaceInvalidations(ctx, workspaceID, "")
		return result.Provider, nil
	}
	tx, err := s.txStarter.Begin(ctx)
	if err != nil {
		return Provider{}, internal("failed to start provider update", err)
	}
	defer tx.Rollback(ctx)
	if err := lockKnowledgeWorkspace(ctx, tx, ws); err != nil {
		return Provider{}, err
	}
	result, err := scanProvider(tx.QueryRow(ctx, `
		UPDATE knowledge_provider SET name=COALESCE($3,name), is_enabled=COALESCE($4,is_enabled), revision=revision+1, updated_at=now()
		WHERE id=$1 AND workspace_id=$2 AND revision=$5
		RETURNING `+providerSelect(false), id, ws, name, input.IsEnabled, input.ExpectedRevision))
	if errors.Is(err, pgx.ErrNoRows) {
		return Provider{}, conflict("revision_conflict", "provider changed; refresh before editing")
	}
	if err != nil {
		return Provider{}, internal("failed to update knowledge provider", err)
	}
	if err := resumeWaitingConfigJobs(ctx, tx, ws, nil); err != nil {
		return Provider{}, internal("failed to resume waiting knowledge jobs", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Provider{}, internal("failed to commit provider update", err)
	}
	s.notifyWorkspaceInvalidations(ctx, workspaceID, "")
	return result.Provider, nil
}

func (s *Service) DeleteProvider(ctx context.Context, workspaceID, providerID string) error {
	if err := s.checkEnabled(); err != nil {
		return err
	}
	ws, err := parseID(workspaceID, "workspace_id")
	if err != nil {
		return err
	}
	id, err := parseID(providerID, "provider_id")
	if err != nil {
		return err
	}
	tx, err := s.txStarter.Begin(ctx)
	if err != nil {
		return internal("failed to start provider deletion transaction", err)
	}
	defer tx.Rollback(ctx)
	if err := lockKnowledgeWorkspace(ctx, tx, ws); err != nil {
		return err
	}
	var lockedID pgtype.UUID
	if err := tx.QueryRow(ctx, `SELECT id FROM knowledge_provider WHERE id=$1 AND workspace_id=$2 FOR UPDATE`, id, ws).Scan(&lockedID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return notFound()
		}
		return internal("failed to lock knowledge provider", err)
	}
	var references int
	if err := tx.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM knowledge_model_binding WHERE provider_id=$1)
		     + (SELECT count(*) FROM knowledge_index WHERE embedding_snapshot->>'provider_id'=$2::text)`, id, id).Scan(&references); err != nil {
		return internal("failed to check provider references", err)
	}
	if references > 0 {
		return conflict("provider_in_use", "provider is still used by a model binding or index")
	}
	command, err := tx.Exec(ctx, `DELETE FROM knowledge_provider WHERE id=$1 AND workspace_id=$2`, id, ws)
	if err != nil {
		return internal("failed to delete knowledge provider", err)
	}
	if command.RowsAffected() == 0 {
		return notFound()
	}
	if err := tx.Commit(ctx); err != nil {
		return internal("failed to commit knowledge provider deletion", err)
	}
	s.notifyWorkspaceInvalidations(ctx, workspaceID, "")
	return nil
}

type ProviderTestInput struct {
	ProviderID string
	BaseURL    string
	Protocol   string
	APIKey     string
	Model      string
	Capability string
}

type ProviderTestResult struct {
	Status     string         `json:"status"`
	Capability string         `json:"capability"`
	Model      string         `json:"model"`
	Dimension  int            `json:"dimension,omitempty"`
	Details    map[string]any `json:"details,omitempty"`
}

var visionCapabilityProbeImage = func() []byte {
	data, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	if err != nil {
		return nil
	}
	return data
}()

func (s *Service) TestProvider(ctx context.Context, workspaceID string, input ProviderTestInput) (ProviderTestResult, error) {
	if err := s.checkEnabled(); err != nil {
		return ProviderTestResult{}, err
	}
	workspace, err := parseID(workspaceID, "workspace_id")
	if err != nil {
		return ProviderTestResult{}, err
	}
	baseURL, protocol, apiKey, providerID, secretRevision := strings.TrimSpace(input.BaseURL), validateProtocol(input.Protocol), input.APIKey, input.ProviderID, int64(0)
	if providerID != "" {
		record, err := s.providerRecord(ctx, workspaceID, providerID)
		if err != nil {
			return ProviderTestResult{}, err
		}
		baseURL, protocol = record.BaseURL, record.Protocol
		secretRevision = record.SecretRevision
		if apiKey == "" {
			if s.secretBox == nil {
				return ProviderTestResult{}, knowledgeError(http.StatusServiceUnavailable, "secret_not_configured", "knowledge provider secrets are not configured", nil)
			}
			opened, openErr := s.secretBox.Open(record.EncryptedAPIKey)
			if openErr != nil {
				return ProviderTestResult{}, internal("failed to open provider key", openErr)
			}
			apiKey = string(opened)
		}
	} else if strings.TrimSpace(apiKey) == "" {
		return ProviderTestResult{}, badRequest("api_key_required", "api_key is required for a draft provider test")
	}
	if strings.TrimSpace(apiKey) == "" {
		return ProviderTestResult{}, badRequest("api_key_required", "provider has no configured API key")
	}
	_, baseURL, err = validateProviderFields("test", protocol, baseURL)
	if err != nil {
		return ProviderTestResult{}, err
	}
	baseURL, err = s.validateProviderURL(baseURL)
	if err != nil {
		return ProviderTestResult{}, err
	}
	model := strings.TrimSpace(input.Model)
	if model == "" {
		return ProviderTestResult{}, badRequest("model_required", "model is required")
	}
	capability := strings.TrimSpace(input.Capability)
	if capability == "" {
		capability = "text"
	}
	client := llm.NewCompatibleClient(llm.CompatibleConfig{BaseURL: baseURL, APIKey: apiKey, Protocol: protocol, Timeout: s.providerTO})
	result := ProviderTestResult{Status: CapabilityReady, Capability: capability, Model: model, Details: map[string]any{"secret_revision": secretRevision}}
	switch capability {
	case "text", "structured_output":
		_, err = client.GenerateJSON(ctx, model, "Return a JSON object with an ok boolean.", `Return JSON only: {"ok":true}.`, 64)
	case "embedding":
		var vectors [][]float64
		vectors, err = client.Embeddings(ctx, model, []string{"knowledge capability probe"})
		if err == nil && len(vectors) == 1 {
			result.Dimension = len(vectors[0])
		}
	case "rerank":
		_, err = client.Rerank(ctx, model, "probe", []string{"knowledge capability probe"}, 1)
	case "vision":
		_, err = client.GenerateVisionJSON(ctx, model, "Return a JSON object with an ok boolean.", "Inspect the supplied probe image and return JSON only: {\"ok\":true}.", []llm.VisionImage{{MIMEType: "image/png", Data: visionCapabilityProbeImage}}, 64)
	default:
		return ProviderTestResult{}, badRequest("invalid_capability", "unsupported capability")
	}
	if err != nil {
		result.Status = CapabilityFailed
		result.Details["error"] = classifyProviderError(err)
	}
	if providerID != "" {
		pid, parseErr := parseID(providerID, "provider_id")
		if parseErr == nil {
			statusDetails, _ := json.Marshal(result.Details)
			if tx, txErr := s.txStarter.Begin(ctx); txErr == nil {
				stored := false
				if lockErr := lockKnowledgeWorkspace(ctx, tx, workspace); lockErr == nil {
					_, execErr := tx.Exec(ctx, `
						INSERT INTO knowledge_model_capability(workspace_id,provider_id,secret_revision,model,capability,status,details,tested_at)
						VALUES($1,$2,$3,$4,$5,$6,$7,now())
						ON CONFLICT (provider_id,secret_revision,model,capability) DO UPDATE SET status=EXCLUDED.status, details=EXCLUDED.details, tested_at=EXCLUDED.tested_at`, workspace, pid, secretRevision, model, capability, result.Status, statusDetails)
					stored = execErr == nil
				}
				if stored {
					stored = tx.Commit(ctx) == nil
				}
				if !stored {
					_ = tx.Rollback(ctx)
				}
				if stored {
					s.notifyWorkspaceInvalidations(ctx, workspaceID, "")
				}
			}
		}
	}
	return result, nil
}

func classifyProviderError(err error) string {
	var httpErr *llm.HTTPError
	if errors.As(err, &httpErr) {
		switch httpErr.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return "provider_auth_failed"
		case http.StatusRequestTimeout, http.StatusGatewayTimeout:
			return "upstream_timeout"
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "upstream_timeout"
	}
	return "model_incompatible"
}

func mustID(value string) pgtype.UUID { id, _ := util.ParseUUID(value); return id }

func (s *Service) ListProviderModels(ctx context.Context, workspaceID, providerID string) ([]llm.CompatibleModel, error) {
	if err := s.checkEnabled(); err != nil {
		return nil, err
	}
	record, err := s.providerRecord(ctx, workspaceID, providerID)
	if err != nil {
		return nil, err
	}
	if s.secretBox == nil {
		return nil, knowledgeError(http.StatusServiceUnavailable, "secret_not_configured", "knowledge provider secrets are not configured", nil)
	}
	if _, err := s.validateProviderURL(record.BaseURL); err != nil {
		return nil, err
	}
	key, err := s.secretBox.Open(record.EncryptedAPIKey)
	if err != nil {
		return nil, internal("failed to open provider key", err)
	}
	client := llm.NewCompatibleClient(llm.CompatibleConfig{BaseURL: record.BaseURL, APIKey: string(key), Protocol: record.Protocol, Timeout: s.providerTO})
	models, err := client.Models(ctx)
	if err != nil {
		return nil, internal("provider model discovery failed", err)
	}
	return models, nil
}

type BindingInput struct {
	Mode       string
	ProviderID string
	Model      string
	Options    map[string]any
}

type ModelSettingsInput struct {
	ExpectedRevision int64
	Main             *BindingInput
	Purposes         map[string]BindingInput
}

func scanBinding(row pgx.Row) (ModelBinding, error) {
	var binding ModelBinding
	var baseID, providerID, model pgtype.Text
	var options []byte
	err := row.Scan(&binding.ID, &binding.WorkspaceID, &baseID, &binding.Purpose, &binding.Mode, &providerID, &model, &options, &binding.Revision)
	if err != nil {
		return ModelBinding{}, err
	}
	binding.KnowledgeBaseID = nullableText(baseID)
	binding.ProviderID = nullableText(providerID)
	binding.Model = nullableText(model)
	binding.Options = mapJSON(options)
	return binding, nil
}

func (s *Service) GetModelSettings(ctx context.Context, workspaceID string) (ModelSettings, error) {
	if err := s.checkEnabled(); err != nil {
		return ModelSettings{}, err
	}
	ws, err := parseID(workspaceID, "workspace_id")
	if err != nil {
		return ModelSettings{}, err
	}
	var settings ModelSettings
	settings.WorkspaceID = workspaceID
	settings.Purposes = map[string]*ModelBinding{}
	err = s.db.QueryRow(ctx, `SELECT revision FROM knowledge_model_settings WHERE workspace_id=$1`, ws).Scan(&settings.Revision)
	if errors.Is(err, pgx.ErrNoRows) {
		settings.Revision = 0
	} else if err != nil {
		return ModelSettings{}, internal("failed to load knowledge model settings", err)
	}
	rows, err := s.db.Query(ctx, `SELECT id::text, workspace_id::text, knowledge_base_id::text, purpose, mode, provider_id::text, model, options, revision FROM knowledge_model_binding WHERE workspace_id=$1 AND knowledge_base_id IS NULL ORDER BY purpose`, ws)
	if err != nil {
		return ModelSettings{}, internal("failed to load knowledge model bindings", err)
	}
	defer rows.Close()
	for rows.Next() {
		binding, scanErr := scanBinding(rows)
		if scanErr != nil {
			return ModelSettings{}, internal("failed to read knowledge model binding", scanErr)
		}
		copy := binding
		if binding.Purpose == PurposeMain {
			settings.Main = &copy
		} else {
			settings.Purposes[binding.Purpose] = &copy
		}
	}
	if err := rows.Err(); err != nil {
		return ModelSettings{}, internal("failed to load knowledge model bindings", err)
	}
	capRows, err := s.db.Query(ctx, `SELECT provider_id::text, secret_revision, model, capability, status, details, tested_at FROM knowledge_model_capability WHERE workspace_id=$1 ORDER BY tested_at DESC NULLS LAST`, ws)
	if err == nil {
		defer capRows.Close()
		for capRows.Next() {
			var capability Capability
			var details []byte
			var tested pgtype.Timestamptz
			if scanErr := capRows.Scan(&capability.ProviderID, &capability.SecretRevision, &capability.Model, &capability.Capability, &capability.Status, &details, &tested); scanErr != nil {
				return ModelSettings{}, internal("failed to read knowledge model capability", scanErr)
			}
			capability.Details = mapJSON(details)
			capability.TestedAt = nullableTime(tested)
			settings.Capabilities = append(settings.Capabilities, capability)
		}
		if err := capRows.Err(); err != nil {
			return ModelSettings{}, internal("failed to load knowledge model capabilities", err)
		}
	} else {
		return ModelSettings{}, internal("failed to load knowledge model capabilities", err)
	}
	if settings.Capabilities == nil {
		settings.Capabilities = []Capability{}
	}
	return settings, nil
}

func validateBindingInput(purpose string, input BindingInput) error {
	validPurpose := purpose == PurposeMain || purpose == PurposeExtract || purpose == PurposeAnswer || purpose == PurposeParse || purpose == PurposeEmbedding || purpose == PurposeRerank
	if !validPurpose {
		return badRequest("invalid_model_purpose", "unsupported model purpose")
	}
	mode := strings.TrimSpace(input.Mode)
	if mode == "" {
		mode = "inherit"
	}
	if mode != "inherit" && mode != "explicit" && mode != "auto" && mode != "off" {
		return badRequest("invalid_model_mode", "model mode must be inherit, explicit, auto, or off")
	}
	if mode == "explicit" || mode == "auto" {
		if strings.TrimSpace(input.ProviderID) == "" || strings.TrimSpace(input.Model) == "" {
			return badRequest("model_binding_incomplete", "explicit model bindings require provider_id and model")
		}
	}
	if purpose == PurposeMain && mode == "auto" {
		return badRequest("invalid_model_mode", "main model does not support auto mode")
	}
	if purpose == PurposeParse && (mode == "explicit" || mode == "auto") {
		if _, err := parseEnhancementOptionMode(input.Options); err != nil {
			return err
		}
	}
	return nil
}

func validateBaseModelPurpose(purpose string) error {
	if purpose == PurposeMain {
		return badRequest("base_main_not_allowed", "knowledge base settings cannot override the workspace main model")
	}
	return nil
}

func parseEnhancementOptionMode(options map[string]any) (string, error) {
	if options == nil {
		return "text", nil
	}
	raw, exists := options["mode"]
	if !exists || raw == nil {
		return "text", nil
	}
	value, ok := raw.(string)
	if !ok {
		return "", badRequest("invalid_parse_enhancement_mode", "parse enhancement mode must be text or vision")
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "text":
		return "text", nil
	case "vision":
		return "vision", nil
	default:
		return "", badRequest("invalid_parse_enhancement_mode", "parse enhancement mode must be text or vision")
	}
}

func (s *Service) saveBinding(ctx context.Context, q DBTX, workspaceID pgtype.UUID, baseID *pgtype.UUID, purpose string, input BindingInput) error {
	if err := validateBindingInput(purpose, input); err != nil {
		return err
	}
	mode := strings.TrimSpace(input.Mode)
	if mode == "" {
		mode = "inherit"
	}
	var providerParam, modelParam any
	if mode == "explicit" || mode == "auto" {
		provider, err := parseID(input.ProviderID, "provider_id")
		if err != nil {
			return err
		}
		var enabled bool
		if err := q.QueryRow(ctx, `SELECT is_enabled FROM knowledge_provider WHERE id=$1 AND workspace_id=$2 FOR SHARE`, provider, workspaceID).Scan(&enabled); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return notFound()
			}
			return internal("failed to validate provider binding", err)
		}
		if !enabled {
			return conflict("provider_disabled", "provider is disabled")
		}
		if mode == "auto" {
			if purpose != PurposeEmbedding {
				return badRequest("invalid_model_mode", "auto mode is only supported for embedding")
			}
			var status string
			if err := q.QueryRow(ctx, `SELECT status FROM knowledge_model_capability WHERE provider_id=$1 AND secret_revision=(SELECT secret_revision FROM knowledge_provider WHERE id=$1) AND model=$2 AND capability='embedding' ORDER BY tested_at DESC LIMIT 1`, provider, strings.TrimSpace(input.Model)).Scan(&status); errors.Is(err, pgx.ErrNoRows) {
				return conflict("embedding_probe_required", "auto embedding requires a successful embedding capability test")
			} else if err != nil {
				return internal("failed to validate embedding capability", err)
			} else if status != CapabilityReady {
				return conflict("embedding_probe_required", "auto embedding requires a successful embedding capability test")
			}
		}
		if purpose == PurposeParse && mode == "explicit" {
			enhancementMode, modeErr := parseEnhancementOptionMode(input.Options)
			if modeErr != nil {
				return modeErr
			}
			if enhancementMode == "vision" {
				var status string
				err := q.QueryRow(ctx, `SELECT status FROM knowledge_model_capability WHERE provider_id=$1 AND secret_revision=(SELECT secret_revision FROM knowledge_provider WHERE id=$1) AND model=$2 AND capability='vision' ORDER BY tested_at DESC LIMIT 1`, provider, strings.TrimSpace(input.Model)).Scan(&status)
				if err != nil {
					if errors.Is(err, pgx.ErrNoRows) {
						return conflict("vision_probe_required", "vision enhancement requires a successful vision capability test")
					}
					return internal("failed to validate vision capability", err)
				}
				if status != CapabilityReady {
					return conflict("vision_probe_required", "vision enhancement requires a successful vision capability test")
				}
			}
		}
		providerParam = provider
		modelParam = strings.TrimSpace(input.Model)
	}
	options, err := jsonBytes(input.Options)
	if err != nil {
		return internal("failed to encode model options", err)
	}
	var existing string
	if baseID == nil {
		err = q.QueryRow(ctx, `SELECT id::text FROM knowledge_model_binding WHERE workspace_id=$1 AND knowledge_base_id IS NULL AND purpose=$2`, workspaceID, purpose).Scan(&existing)
	} else {
		err = q.QueryRow(ctx, `SELECT id::text FROM knowledge_model_binding WHERE workspace_id=$1 AND knowledge_base_id=$2 AND purpose=$3`, workspaceID, *baseID, purpose).Scan(&existing)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		if baseID == nil {
			_, err = q.Exec(ctx, `INSERT INTO knowledge_model_binding(workspace_id,knowledge_base_id,purpose,mode,provider_id,model,options) VALUES($1,NULL,$2,$3,$4,$5,$6)`, workspaceID, purpose, mode, providerParam, modelParam, options)
		} else {
			_, err = q.Exec(ctx, `INSERT INTO knowledge_model_binding(workspace_id,knowledge_base_id,purpose,mode,provider_id,model,options) VALUES($1,$2,$3,$4,$5,$6,$7)`, workspaceID, *baseID, purpose, mode, providerParam, modelParam, options)
		}
		if err != nil {
			return internal("failed to create model binding", err)
		}
		return nil
	}
	if err != nil {
		return internal("failed to load model binding", err)
	}
	bindingID, err := parseID(existing, "binding_id")
	if err != nil {
		return err
	}
	_, err = q.Exec(ctx, `UPDATE knowledge_model_binding SET mode=$2, provider_id=$3, model=$4, options=$5, revision=revision+1, updated_at=now() WHERE id=$1`, bindingID, mode, providerParam, modelParam, options)
	if err != nil {
		return internal("failed to update model binding", err)
	}
	return nil
}

func (s *Service) PutModelSettings(ctx context.Context, workspaceID, actorID string, input ModelSettingsInput) (ModelSettings, error) {
	if err := s.checkEnabled(); err != nil {
		return ModelSettings{}, err
	}
	ws, err := parseID(workspaceID, "workspace_id")
	if err != nil {
		return ModelSettings{}, err
	}
	actor, err := parseID(actorID, "user_id")
	if err != nil {
		return ModelSettings{}, err
	}
	if input.ExpectedRevision < 0 {
		return ModelSettings{}, badRequest("invalid_revision", "expected_revision cannot be negative")
	}
	tx, err := s.txStarter.Begin(ctx)
	if err != nil {
		return ModelSettings{}, internal("failed to start model settings transaction", err)
	}
	defer tx.Rollback(ctx)
	if err := lockKnowledgeWorkspace(ctx, tx, ws); err != nil {
		return ModelSettings{}, err
	}
	var current int64
	err = tx.QueryRow(ctx, `SELECT revision FROM knowledge_model_settings WHERE workspace_id=$1 FOR UPDATE`, ws).Scan(&current)
	if errors.Is(err, pgx.ErrNoRows) {
		if input.ExpectedRevision != 0 {
			return ModelSettings{}, conflict("revision_conflict", "model settings changed; refresh before editing")
		}
		if _, err = tx.Exec(ctx, `INSERT INTO knowledge_model_settings(workspace_id,revision,updated_by) VALUES($1,1,$2)`, ws, actor); err != nil {
			return ModelSettings{}, internal("failed to create model settings", err)
		}
	} else if err != nil {
		return ModelSettings{}, internal("failed to lock model settings", err)
	} else if current != input.ExpectedRevision {
		return ModelSettings{}, conflict("revision_conflict", "model settings changed; refresh before editing")
	} else if _, err = tx.Exec(ctx, `UPDATE knowledge_model_settings SET revision=revision+1, updated_by=$2, updated_at=now() WHERE workspace_id=$1`, ws, actor); err != nil {
		return ModelSettings{}, internal("failed to update model settings", err)
	}
	if input.Main != nil {
		if err := s.saveBinding(ctx, tx, ws, nil, PurposeMain, *input.Main); err != nil {
			return ModelSettings{}, err
		}
	}
	for purpose, binding := range input.Purposes {
		if err := s.saveBinding(ctx, tx, ws, nil, purpose, binding); err != nil {
			return ModelSettings{}, err
		}
	}
	if err := resumeWaitingConfigJobs(ctx, tx, ws, nil); err != nil {
		return ModelSettings{}, internal("failed to resume waiting knowledge jobs", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ModelSettings{}, internal("failed to commit model settings", err)
	}
	s.notifyWorkspaceInvalidations(ctx, workspaceID, actorID)
	return s.GetModelSettings(ctx, workspaceID)
}

// GetBaseModelSettings returns the workspace defaults with non-inherit base
// overrides applied. The revision is the base revision, so the same
// expected_revision/If-Match contract protects metadata and model settings
// from overwriting each other.
func (s *Service) GetBaseModelSettings(ctx context.Context, workspaceID, actorID, baseID string) (ModelSettings, error) {
	if err := s.checkEnabled(); err != nil {
		return ModelSettings{}, err
	}
	base, err := s.GetBase(ctx, workspaceID, actorID, baseID)
	if err != nil {
		return ModelSettings{}, err
	}
	settings, err := s.GetModelSettings(ctx, workspaceID)
	if err != nil {
		return ModelSettings{}, err
	}
	settings.KnowledgeBaseID = &base.ID
	settings.Revision = base.Revision
	// The workspace rows above are effective defaults for this base, not base
	// overrides. Mark them as inherit before returning so the base editor can
	// round-trip the scope distinction instead of silently materializing a copy
	// of every workspace binding on its next save.
	for purpose, binding := range settings.Purposes {
		if binding == nil {
			continue
		}
		copy := *binding
		copy.Mode = "inherit"
		settings.Purposes[purpose] = &copy
	}
	baseUUID, err := parseID(base.ID, "knowledge_base_id")
	if err != nil {
		return ModelSettings{}, err
	}
	rows, err := s.db.Query(ctx, `SELECT id::text,workspace_id::text,knowledge_base_id::text,purpose,mode,provider_id::text,model,options,revision FROM knowledge_model_binding WHERE workspace_id=$1 AND knowledge_base_id=$2 ORDER BY purpose`, mustID(workspaceID), baseUUID)
	if err != nil {
		return ModelSettings{}, internal("failed to load base model bindings", err)
	}
	defer rows.Close()
	for rows.Next() {
		binding, scanErr := scanBinding(rows)
		if scanErr != nil {
			return ModelSettings{}, internal("failed to read base model binding", scanErr)
		}
		// `inherit` is represented by the already loaded workspace effective
		// value. Explicit, auto, and off remain visible to the editor.
		if binding.Mode == "inherit" {
			continue
		}
		copy := binding
		if binding.Purpose == PurposeMain {
			settings.Main = &copy
		} else {
			settings.Purposes[binding.Purpose] = &copy
		}
	}
	if err := rows.Err(); err != nil {
		return ModelSettings{}, internal("failed to load base model bindings", err)
	}
	return settings, nil
}

func (s *Service) PutBaseModelSettings(ctx context.Context, workspaceID, actorID, baseID string, input ModelSettingsInput) (ModelSettings, error) {
	if err := s.checkEnabled(); err != nil {
		return ModelSettings{}, err
	}
	workspace, err := parseID(workspaceID, "workspace_id")
	if err != nil {
		return ModelSettings{}, err
	}
	actor, err := parseID(actorID, "user_id")
	if err != nil {
		return ModelSettings{}, err
	}
	base, err := parseID(baseID, "knowledge_base_id")
	if err != nil {
		return ModelSettings{}, err
	}
	if input.ExpectedRevision <= 0 {
		return ModelSettings{}, badRequest("expected_revision_required", "expected_revision is required")
	}
	if input.Main != nil {
		return ModelSettings{}, badRequest("base_main_not_allowed", "knowledge base settings cannot override the workspace main model")
	}
	tx, err := s.txStarter.Begin(ctx)
	if err != nil {
		return ModelSettings{}, internal("failed to start base model settings transaction", err)
	}
	defer tx.Rollback(ctx)
	if err := lockKnowledgeWorkspace(ctx, tx, workspace); err != nil {
		return ModelSettings{}, err
	}
	var current int64
	err = tx.QueryRow(ctx, `SELECT revision FROM knowledge_base WHERE id=$1 AND workspace_id=$2 AND deleted_at IS NULL AND (creator_id=$3 OR (visibility='workspace' AND EXISTS (SELECT 1 FROM member m WHERE m.workspace_id=knowledge_base.workspace_id AND m.user_id=$3 AND m.role IN ('owner','admin')))) FOR UPDATE`, base, workspace, actor).Scan(&current)
	if errors.Is(err, pgx.ErrNoRows) {
		return ModelSettings{}, notFound()
	}
	if err != nil {
		return ModelSettings{}, internal("failed to lock knowledge base settings", err)
	}
	if current != input.ExpectedRevision {
		return ModelSettings{}, conflict("revision_conflict", "knowledge base changed; refresh before editing model settings")
	}
	for purpose, binding := range input.Purposes {
		if err := validateBaseModelPurpose(purpose); err != nil {
			return ModelSettings{}, err
		}
		if err := s.saveBinding(ctx, tx, workspace, &base, purpose, binding); err != nil {
			return ModelSettings{}, err
		}
	}
	if err := resumeWaitingConfigJobs(ctx, tx, workspace, &base); err != nil {
		return ModelSettings{}, internal("failed to resume waiting knowledge jobs", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE knowledge_base SET revision=revision+1, updated_at=now() WHERE id=$1`, base); err != nil {
		return ModelSettings{}, internal("failed to update base settings revision", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ModelSettings{}, internal("failed to commit base model settings", err)
	}
	s.notifyKnowledgeInvalidation(ctx, workspaceID, baseID, actorID)
	return s.GetBaseModelSettings(ctx, workspaceID, actorID, baseID)
}

type ResolvedBinding struct {
	Binding        ModelBinding
	Provider       Provider
	APIKey         string
	SecretRevision int64
}

func (s *Service) resolveBinding(ctx context.Context, workspaceID, baseID, purpose string) (*ResolvedBinding, error) {
	ws, err := parseID(workspaceID, "workspace_id")
	if err != nil {
		return nil, err
	}
	base, err := parseID(baseID, "knowledge_base_id")
	if err != nil {
		return nil, err
	}
	var binding ModelBinding
	queryBinding := func(sql string, args ...any) error {
		var queryErr error
		binding, queryErr = scanBinding(s.db.QueryRow(ctx, sql, args...))
		return queryErr
	}
	err = queryBinding(`SELECT id::text,workspace_id::text,knowledge_base_id::text,purpose,mode,provider_id::text,model,options,revision FROM knowledge_model_binding WHERE workspace_id=$1 AND knowledge_base_id=$2 AND purpose=$3`, ws, base, purpose)
	if err == nil {
		switch binding.Mode {
		case "off":
			return nil, nil
		case "explicit", "auto":
			return s.resolvedBinding(ctx, workspaceID, binding)
		case "inherit":
			// Continue to the workspace scope below.
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, internal("failed to resolve base model binding", err)
	}
	err = queryBinding(`SELECT id::text,workspace_id::text,knowledge_base_id::text,purpose,mode,provider_id::text,model,options,revision FROM knowledge_model_binding WHERE workspace_id=$1 AND knowledge_base_id IS NULL AND purpose=$2`, ws, purpose)
	if errors.Is(err, pgx.ErrNoRows) && (purpose == PurposeAnswer || purpose == PurposeExtract) {
		err = queryBinding(`SELECT id::text,workspace_id::text,knowledge_base_id::text,purpose,mode,provider_id::text,model,options,revision FROM knowledge_model_binding WHERE workspace_id=$1 AND knowledge_base_id IS NULL AND purpose=$2`, ws, PurposeMain)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, internal("failed to resolve model binding", err)
	}
	if binding.Mode == "off" {
		return nil, nil
	}
	if binding.Mode == "inherit" && (purpose == PurposeAnswer || purpose == PurposeExtract) {
		err = queryBinding(`SELECT id::text,workspace_id::text,knowledge_base_id::text,purpose,mode,provider_id::text,model,options,revision FROM knowledge_model_binding WHERE workspace_id=$1 AND knowledge_base_id IS NULL AND purpose=$2`, ws, PurposeMain)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		if err != nil {
			return nil, internal("failed to resolve main model binding", err)
		}
		if binding.Mode == "off" {
			return nil, nil
		}
	} else if binding.Mode == "inherit" {
		return nil, nil
	}
	return s.resolvedBinding(ctx, workspaceID, binding)
}

func (s *Service) resolvedBinding(ctx context.Context, workspaceID string, binding ModelBinding) (*ResolvedBinding, error) {
	if binding.ProviderID == nil || binding.Model == nil || strings.TrimSpace(*binding.ProviderID) == "" || strings.TrimSpace(*binding.Model) == "" {
		return nil, knowledgeError(http.StatusServiceUnavailable, "model_not_configured", "model binding is incomplete", nil)
	}
	record, err := s.providerRecord(ctx, workspaceID, *binding.ProviderID)
	if err != nil {
		return nil, err
	}
	if !record.IsEnabled {
		return nil, knowledgeError(http.StatusServiceUnavailable, "provider_disabled", "provider is disabled", nil)
	}
	if _, err := s.validateProviderURL(record.BaseURL); err != nil {
		return nil, err
	}
	if s.secretBox == nil {
		return nil, knowledgeError(http.StatusServiceUnavailable, "secret_not_configured", "knowledge provider secrets are not configured", nil)
	}
	key, err := s.secretBox.Open(record.EncryptedAPIKey)
	if err != nil {
		return nil, internal("failed to open provider key", err)
	}
	return &ResolvedBinding{Binding: binding, Provider: record.Provider, APIKey: string(key), SecretRevision: record.SecretRevision}, nil
}

func (s *Service) compatibleClient(binding *ResolvedBinding) *llm.CompatibleClient {
	if binding == nil {
		return nil
	}
	return llm.NewCompatibleClient(llm.CompatibleConfig{BaseURL: binding.Provider.BaseURL, APIKey: binding.APIKey, Protocol: binding.Provider.Protocol, Timeout: s.providerTO})
}
