package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/featureflags"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/featureflag"
)

const (
	settingAllowSignup            = "allow_signup"
	settingSignupEmails           = "signup_allowed_emails"
	settingSignupDomains          = "signup_allowed_domains"
	settingAllowWorkspaceCreation = "allow_workspace_creation"
	settingWorkspaceCreatorEmails = "workspace_creation_allowed_emails"
	settingAutoCreateWorkspace    = "auto_create_workspace"
	settingVCSIntegration         = "vcs_integration"
)

type platformSettings map[string]json.RawMessage

func (h *Handler) loadPlatformSettings(ctx context.Context) (platformSettings, error) {
	if h.Queries == nil {
		return nil, nil
	}
	raw, err := h.Queries.GetPlatformSettings(ctx)
	if err != nil {
		return nil, err
	}
	var settings platformSettings
	if err := json.Unmarshal(raw, &settings); err != nil {
		return nil, err
	}
	return settings, nil
}

func settingValue[T any](settings platformSettings, key string, fallback T) T {
	var value T
	if raw, exists := settings[key]; exists && json.Unmarshal(raw, &value) == nil {
		return value
	}
	return fallback
}

func (h *Handler) canCreateWorkspace(settings platformSettings, email string) bool {
	allowedEmails := settingValue(settings, settingWorkspaceCreatorEmails, h.cfg.WorkspaceCreationAllowedEmails)
	return settingValue(settings, settingAllowWorkspaceCreation, !h.cfg.DisableWorkspaceCreation) &&
		(len(allowedEmails) == 0 || contains(allowedEmails, strings.TrimSpace(email)))
}

type platformFlag struct {
	key          string
	defaultValue bool
}

var managedPlatformFlags = []platformFlag{
	{featureflags.BillingWorkspaceSubscriptions, false},
	{featureflags.ComposioMCPApps, false},
	{featureflags.PluginsV1, false},
	{featureflags.TriageV1, false},
	{featureflags.LocalSearchIndex, true},
	{featureflags.DocsV1, false},
	{featureflags.IssueDescriptionLive, false},
	{featureflags.DashboardHistory365d, true},
}

func isManagedPlatformFlag(key string) bool {
	for _, flag := range managedPlatformFlags {
		if flag.key == key {
			return true
		}
	}
	return false
}

type platformFlagProvider struct {
	queries *db.Queries
}

type platformSettingsContextKey struct{}

func (platformFlagProvider) Name() string { return "platform" }

func (p platformFlagProvider) Lookup(ctx context.Context, key string) (featureflag.Decision, bool) {
	if !isManagedPlatformFlag(key) {
		return featureflag.Decision{}, false
	}

	var raw []byte
	if settings, ok := ctx.Value(platformSettingsContextKey{}).(platformSettings); ok {
		raw = settings[key]
	} else {
		var err error
		raw, err = p.queries.GetPlatformSetting(ctx, key)
		if err != nil {
			return featureflag.Decision{Source: p.Name(), Reason: featureflag.ReasonError}, true
		}
	}
	var enabled *bool
	if len(raw) == 0 || json.Unmarshal(raw, &enabled) != nil || enabled == nil {
		return featureflag.Decision{}, false
	}
	variant := "off"
	if *enabled {
		variant = "on"
	}
	return featureflag.Decision{
		Enabled: *enabled, Variant: variant, Source: p.Name(), Reason: featureflag.ReasonStatic,
	}, true
}

func WithPlatformFeatureFlags(queries *db.Queries, configured *featureflag.Service) *featureflag.Service {
	provider := featureflag.NewChainProvider(platformFlagProvider{queries}, configured.Provider())
	return featureflag.NewService(provider, featureflag.WithLogger(slog.Default()))
}

type platformSetting struct {
	Key       string `json:"key"`
	Value     any    `json:"value"`
	Source    string `json:"source"`
	Available bool   `json:"available"`
}

func platformSettingRow[T any](settings platformSettings, key string, fallback T, source string) platformSetting {
	value := settingValue(settings, key, fallback)
	if _, exists := settings[key]; exists {
		source = "platform"
	}
	return platformSetting{Key: key, Value: value, Source: source, Available: true}
}

func (h *Handler) PlatformSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := h.loadPlatformSettings(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "platform settings are unavailable")
		return
	}
	rows := []platformSetting{
		platformSettingRow(settings, settingAllowWorkspaceCreation, !h.cfg.DisableWorkspaceCreation, "deployment"),
		platformSettingRow(settings, settingWorkspaceCreatorEmails, nonNilStrings(h.cfg.WorkspaceCreationAllowedEmails), "deployment"),
		platformSettingRow(settings, settingAutoCreateWorkspace, false, "default"),
		platformSettingRow(settings, settingAllowSignup, h.cfg.AllowSignup, "deployment"),
		platformSettingRow(settings, settingSignupEmails, nonNilStrings(h.cfg.AllowedEmails), "deployment"),
		platformSettingRow(settings, settingSignupDomains, nonNilStrings(h.cfg.AllowedEmailDomains), "deployment"),
		platformSettingRow(settings, settingVCSIntegration, h.cfg.VCSIntegrationEnabled, "deployment"),
	}
	rows[len(rows)-1].Available = h.isVCSConfigured()
	ctx := context.WithValue(r.Context(), platformSettingsContextKey{}, settings)
	for _, flag := range managedPlatformFlags {
		decision := h.FeatureFlags.Decision(ctx, flag.key, flag.defaultValue)
		row := platformSetting{Key: flag.key, Value: decision.Enabled, Source: decision.Source, Available: true}
		switch flag.key {
		case featureflags.ComposioMCPApps:
			row.Available = h.Composio != nil
		case featureflags.BillingWorkspaceSubscriptions:
			row.Available = h.Features != nil
		case featureflags.IssueDescriptionLive:
			row.Available = h.Collab != nil
		}
		rows = append(rows, row)
	}
	writeJSON(w, http.StatusOK, rows)
}

func nonNilStrings(value []string) []string {
	if value == nil {
		return []string{}
	}
	return value
}

func (h *Handler) UpdatePlatformSetting(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "key")
	var req struct {
		Value json.RawMessage `json:"value"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil || len(req.Value) == 0 {
		writeError(w, http.StatusBadRequest, "a setting value is required")
		return
	}
	isList := key == settingSignupEmails || key == settingSignupDomains || key == settingWorkspaceCreatorEmails
	isBoolean := key == settingAllowSignup || key == settingAllowWorkspaceCreation ||
		key == settingAutoCreateWorkspace || key == settingVCSIntegration || isManagedPlatformFlag(key)
	switch {
	case !isList && !isBoolean:
		writeError(w, http.StatusNotFound, "unknown platform setting")
		return
	case string(req.Value) == "null":
		// Resetting a key re-exposes the deployment's existing configuration.
	case isList:
		var entries []string
		if err := json.Unmarshal(req.Value, &entries); err != nil {
			writeError(w, http.StatusBadRequest, "expected a list of email addresses or domains")
			return
		}
		normalized := make([]string, 0, len(entries))
		for _, entry := range entries {
			entry = strings.ToLower(strings.TrimSpace(entry))
			local, domain, hasAt := strings.Cut(entry, "@")
			valid := local != "" && !strings.ContainsAny(entry, " ,\t\r\n")
			if key == settingSignupDomains {
				valid = valid && !hasAt
			} else {
				valid = valid && hasAt && domain != "" && !strings.Contains(domain, "@")
			}
			if !valid {
				writeError(w, http.StatusBadRequest, "invalid email address or domain")
				return
			}
			if !contains(normalized, entry) {
				normalized = append(normalized, entry)
			}
		}
		req.Value, _ = json.Marshal(normalized)
	default:
		var value bool
		if err := json.Unmarshal(req.Value, &value); err != nil {
			writeError(w, http.StatusBadRequest, "expected a boolean")
			return
		}
	}

	if err := h.Queries.SetPlatformSetting(r.Context(), db.SetPlatformSettingParams{Key: key, Value: req.Value}); err != nil {
		writeError(w, http.StatusServiceUnavailable, "could not save the platform setting")
		return
	}
	slog.InfoContext(r.Context(), "platform setting updated", "user_id", r.Header.Get("X-User-ID"), "key", key)
	w.WriteHeader(http.StatusNoContent)
}
