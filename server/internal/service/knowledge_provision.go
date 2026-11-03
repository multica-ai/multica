package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/knowledge"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The models step of provisioning (plan 10.11) and the per-owner WeKnora login
// (plan 10.14). Both are degradable: nothing in the product depends on them, so
// a failure is reported and logged, and knowledge still becomes ready.

// KnowledgeLLM is the OpenAI-compatible endpoint WeKnora calls for the features
// that need a model: summaries, Wiki, reranking and vision. Multica registers
// each configured model in the tenant. An empty BaseURL means no LLM.
type KnowledgeLLM struct {
	BaseURL     string
	APIKey      string
	ChatModel   string
	RerankModel string
	VisionModel string
}

// Roles of the models Multica registers, as a connection test names them.
const (
	KnowledgeModelChat   = "chat"
	KnowledgeModelRerank = "rerank"
	KnowledgeModelVision = "vision"
)

// knowledgeWikiKeys are the bases Wiki is enabled on when a chat model exists,
// in priority order. Wiki is limited to maxKnowledgeWikiBases bases (spec 3.3).
var knowledgeWikiKeys = []string{"company", "ventures", "decisions"}

const maxKnowledgeWikiBases = 3

var (
	// ErrKnowledgeLLMNotConfigured means the requested feature needs a chat model
	// and the deployment has none.
	ErrKnowledgeLLMNotConfigured = errors.New("no chat model is configured for knowledge")
	// ErrKnowledgeWikiLimit means three bases already have Wiki.
	ErrKnowledgeWikiLimit = errors.New("wiki is limited to three knowledge bases")
	// ErrKnowledgeOwnerLoginMissing means the owner has no WeKnora login to show.
	ErrKnowledgeOwnerLoginMissing = errors.New("no WeKnora login exists for this owner")
	// ErrKnowledgeOwnerPasswordShown means the password was already revealed.
	ErrKnowledgeOwnerPasswordShown = errors.New("the WeKnora password was already shown")
)

// llmConfigured reports whether any model is configured.
func (s *KnowledgeService) llmConfigured() bool {
	return s.LLM != nil && s.LLM.BaseURL != "" && (s.LLM.ChatModel != "" || s.LLM.RerankModel != "" || s.LLM.VisionModel != "")
}

// chatConfigured reports whether summaries and Wiki can run: both need a chat
// model.
func (s *KnowledgeService) chatConfigured() bool {
	return s.llmConfigured() && s.LLM.ChatModel != ""
}

// llmSpec is the model a role registers, or false when the role is not
// configured.
func (s *KnowledgeService) llmSpec(role string) (knowledge.ModelSpec, bool) {
	if s.LLM == nil || s.LLM.BaseURL == "" {
		return knowledge.ModelSpec{}, false
	}
	spec := knowledge.ModelSpec{Provider: "generic", BaseURL: s.LLM.BaseURL, APIKey: s.LLM.APIKey}
	switch role {
	case KnowledgeModelChat:
		spec.Name, spec.Type = s.LLM.ChatModel, knowledge.ModelTypeKnowledgeQA
	case KnowledgeModelRerank:
		spec.Name, spec.Type = s.LLM.RerankModel, knowledge.ModelTypeRerank
	case KnowledgeModelVision:
		spec.Name, spec.Type = s.LLM.VisionModel, knowledge.ModelTypeVLLM
	default:
		return knowledge.ModelSpec{}, false
	}
	return spec, spec.Name != ""
}

// provisionLLM registers the configured models in the tenant, then gives each
// base that has never had its defaults written the summary model and, for the
// first three default bases, Wiki. A bases whose defaults were written keeps
// what an admin set.
func (s *KnowledgeService) provisionLLM(ctx context.Context, workspaceID pgtype.UUID, ws knowledge.Workspace) error {
	if !s.llmConfigured() {
		return nil
	}
	var errs []error
	chatID := ""
	for _, role := range []string{KnowledgeModelChat, KnowledgeModelRerank, KnowledgeModelVision} {
		spec, ok := s.llmSpec(role)
		if !ok {
			continue
		}
		model, err := ws.EnsureModel(ctx, spec)
		if err != nil {
			errs = append(errs, fmt.Errorf("register %s model %q: %w", role, spec.Name, err))
			continue
		}
		if role == KnowledgeModelChat {
			chatID = model.ID
		}
	}
	if !s.chatConfigured() || chatID == "" {
		return errors.Join(errs...)
	}

	bases, err := s.Queries.ListKnowledgeBases(ctx, workspaceID)
	if err != nil {
		return errors.Join(append(errs, fmt.Errorf("list knowledge bases: %w", err))...)
	}
	wikiOn := 0
	for _, b := range bases {
		if b.WikiEnabled {
			wikiOn++
		}
	}
	for _, b := range bases {
		if b.LlmDefaultsApplied {
			continue
		}
		wiki := slices.Contains(knowledgeWikiKeys, b.Key) && wikiOn < maxKnowledgeWikiBases
		if err := s.applyBaseSettings(ctx, workspaceID, ws, b, chatID, wiki, true); err != nil {
			errs = append(errs, err)
			continue
		}
		if wiki {
			wikiOn++
		}
	}
	return errors.Join(errs...)
}

// applyBaseSettings sets the Wiki and summary switches of one base in WeKnora,
// then records them. chatID is the summary model; it is ignored when summaries
// are off.
func (s *KnowledgeService) applyBaseSettings(ctx context.Context, workspaceID pgtype.UUID, ws knowledge.Workspace, base db.KnowledgeBase, chatID string, wiki, summaries bool) error {
	summaryModel := ""
	if summaries {
		summaryModel = chatID
	}
	patch := knowledge.KnowledgeBasePatch{SummaryModelID: &summaryModel, WikiEnabled: &wiki}
	if _, err := ws.UpdateKnowledgeBase(ctx, base.WeknoraKbID, patch); err != nil {
		return fmt.Errorf("update knowledge base %q: %w", base.Key, err)
	}
	if _, err := s.Queries.SetKnowledgeBaseLLMSettings(ctx, db.SetKnowledgeBaseLLMSettingsParams{
		ID: base.ID, WorkspaceID: workspaceID, WikiEnabled: wiki, SummariesEnabled: summaries,
	}); err != nil {
		return fmt.Errorf("record settings of knowledge base %q: %w", base.Key, err)
	}
	return nil
}

// KnowledgeBaseSettingsPatch changes a base's LLM-backed switches. A nil field
// is left as it is.
type KnowledgeBaseSettingsPatch struct {
	Wiki      *bool
	Summaries *bool
}

// KnowledgeBaseSettingsView is a base's switches after an update.
type KnowledgeBaseSettingsView struct {
	Key              string `json:"key"`
	WikiEnabled      bool   `json:"wiki_enabled"`
	SummariesEnabled bool   `json:"summaries_enabled"`
}

// UpdateBaseSettings turns Wiki or summaries on or off for one base. Turning
// either on needs the chat model, and Wiki is limited to three bases.
func (s *KnowledgeService) UpdateBaseSettings(ctx context.Context, workspaceID pgtype.UUID, key string, patch KnowledgeBaseSettingsPatch) (KnowledgeBaseSettingsView, error) {
	ws, err := s.workspaceClient(ctx, workspaceID)
	if err != nil {
		return KnowledgeBaseSettingsView{}, err
	}
	base, err := s.Queries.GetKnowledgeBaseByKey(ctx, db.GetKnowledgeBaseByKeyParams{WorkspaceID: workspaceID, Key: key})
	if errors.Is(err, pgx.ErrNoRows) {
		return KnowledgeBaseSettingsView{}, ErrKnowledgeBaseNotFound
	}
	if err != nil {
		return KnowledgeBaseSettingsView{}, fmt.Errorf("load knowledge base: %w", err)
	}
	wiki, summaries := base.WikiEnabled, base.SummariesEnabled
	if patch.Wiki != nil {
		wiki = *patch.Wiki
	}
	if patch.Summaries != nil {
		summaries = *patch.Summaries
	}
	if wiki && !base.WikiEnabled {
		n, err := s.Queries.CountWikiEnabledKnowledgeBases(ctx, db.CountWikiEnabledKnowledgeBasesParams{WorkspaceID: workspaceID, ID: base.ID})
		if err != nil {
			return KnowledgeBaseSettingsView{}, fmt.Errorf("count wiki bases: %w", err)
		}
		if n >= maxKnowledgeWikiBases {
			return KnowledgeBaseSettingsView{}, &KnowledgeConflictError{"Wiki is limited to three knowledge bases; turn it off on another base first"}
		}
	}
	chatID := ""
	if wiki || summaries {
		if !s.chatConfigured() {
			return KnowledgeBaseSettingsView{}, ErrKnowledgeLLMNotConfigured
		}
		spec, _ := s.llmSpec(KnowledgeModelChat)
		model, err := ws.EnsureModel(ctx, spec)
		if err != nil {
			return KnowledgeBaseSettingsView{}, fmt.Errorf("chat model: %w", err)
		}
		chatID = model.ID
	}
	if err := s.applyBaseSettings(ctx, workspaceID, ws, base, chatID, wiki, summaries); err != nil {
		return KnowledgeBaseSettingsView{}, err
	}
	return KnowledgeBaseSettingsView{Key: key, WikiEnabled: wiki, SummariesEnabled: summaries}, nil
}

// ModelCheck is one registered model and whether it answers.
type ModelCheck struct {
	Role string `json:"role"`
	Name string `json:"name"`
	OK   bool   `json:"ok"`
	// Error is WeKnora's reason when OK is false; empty otherwise.
	Error string `json:"error"`
}

// ModelChecks is the connection test of every configured model.
type ModelChecks struct {
	Configured bool         `json:"configured"`
	Checks     []ModelCheck `json:"checks"`
}

// TestModels runs each configured model once and reports which ones failed and
// why. The vision model is registered but not run: a probe needs an image. A
// sidecar that does not answer is the returned error, not a failed check.
func (s *KnowledgeService) TestModels(ctx context.Context, workspaceID pgtype.UUID) (ModelChecks, error) {
	out := ModelChecks{Checks: []ModelCheck{}}
	if !s.llmConfigured() {
		return out, nil
	}
	out.Configured = true
	ws, err := s.workspaceClient(ctx, workspaceID)
	if err != nil {
		return out, err
	}
	probes := map[string]knowledge.ModelProbe{
		KnowledgeModelChat:   {Input: "Reply with OK."},
		KnowledgeModelRerank: {Input: "office holidays", Documents: []string{"Office holiday schedule.", "Travel expense policy."}},
	}
	for _, role := range []string{KnowledgeModelChat, KnowledgeModelRerank, KnowledgeModelVision} {
		spec, ok := s.llmSpec(role)
		if !ok {
			continue
		}
		check := ModelCheck{Role: role, Name: spec.Name}
		model, err := ws.EnsureModel(ctx, spec)
		if err == nil {
			if probe, ok := probes[role]; ok {
				err = ws.TestModel(ctx, model.ID, probe)
			}
		}
		switch {
		case errors.Is(err, knowledge.ErrUnavailable):
			return out, err
		case err != nil:
			check.Error = knowledgeFailureText(err)
		default:
			check.OK = true
		}
		out.Checks = append(out.Checks, check)
	}
	return out, nil
}

// knowledgeFailureText is the reason WeKnora gave, or the error itself when it
// gave none.
func knowledgeFailureText(err error) string {
	var apiErr *knowledge.APIError
	if errors.As(err, &apiErr) && apiErr.Message != "" {
		return apiErr.Message
	}
	return err.Error()
}

// ownerLoginUsername is the WeKnora login name of an owner in a workspace. It
// is derived, so a retry finds the same login, and it is unique per owner and
// workspace, which a WeKnora username must be.
func ownerLoginUsername(workspaceID, userID pgtype.UUID) string {
	sum := sha256.Sum256([]byte(util.UUIDToString(workspaceID) + "/" + util.UUIDToString(userID)))
	return "mul-" + hex.EncodeToString(sum[:])[:24]
}

// generateKnowledgePassword returns a random password for a WeKnora login.
func generateKnowledgePassword() (string, error) {
	b := make([]byte, 18)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate password: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// ensureOwnerAccount gives the owner who enabled knowledge a WeKnora login and
// makes it an admin of the workspace tenant, so the owner can use WeKnora's own
// UI (spec 3.3). The password is generated once and sealed before WeKnora is
// asked, so a retry reuses it. It is called from enable and returns an error for
// the caller to log: the login is not needed for knowledge to work.
func (s *KnowledgeService) ensureOwnerAccount(ctx context.Context, workspaceID, userID pgtype.UUID, tenantID string) error {
	if s.Box == nil {
		return nil
	}
	user, err := s.Queries.GetUser(ctx, userID)
	if err != nil {
		return fmt.Errorf("load owner: %w", err)
	}
	key := db.GetKnowledgeOwnerAccountParams{WorkspaceID: workspaceID, UserID: userID}
	row, err := s.Queries.GetKnowledgeOwnerAccount(ctx, key)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := s.recordPendingOwnerAccount(ctx, workspaceID, userID, user.Email); err != nil {
			return err
		}
		row, err = s.Queries.GetKnowledgeOwnerAccount(ctx, key)
	}
	if err != nil {
		return fmt.Errorf("load owner login: %w", err)
	}

	if row.WeknoraUserID == "" {
		if row.PasswordCiphertext == nil {
			return ErrKnowledgeOwnerPasswordShown
		}
		password, err := s.Box.Open(row.PasswordCiphertext)
		if err != nil {
			return fmt.Errorf("open owner password (was MULTICA_KNOWLEDGE_SECRET_KEY changed?): %w", err)
		}
		login, err := s.Client.EnsureUser(ctx, knowledge.UserSpec{
			Username: ownerLoginUsername(workspaceID, userID), Email: row.Email, Password: string(password),
		})
		if err != nil {
			return fmt.Errorf("create owner login: %w", err)
		}
		if _, err := s.Queries.SetKnowledgeOwnerWeKnoraUser(ctx, db.SetKnowledgeOwnerWeKnoraUserParams{
			WorkspaceID: workspaceID, UserID: userID, WeknoraUserID: login.ID,
		}); err != nil {
			return fmt.Errorf("record owner login id: %w", err)
		}
	}
	if err := s.Client.AddTenantMember(ctx, tenantID, row.Email, "admin"); err != nil {
		return fmt.Errorf("add owner to the knowledge tenant: %w", err)
	}
	return nil
}

// recordPendingOwnerAccount seals a new password and stores it before WeKnora is
// asked for the login. A concurrent insert is a no-op; the caller re-reads.
func (s *KnowledgeService) recordPendingOwnerAccount(ctx context.Context, workspaceID, userID pgtype.UUID, email string) error {
	password, err := generateKnowledgePassword()
	if err != nil {
		return err
	}
	sealed, err := s.Box.Seal([]byte(password))
	if err != nil {
		return fmt.Errorf("seal owner password: %w", err)
	}
	if _, err := s.Queries.InsertKnowledgeOwnerAccountPending(ctx, db.InsertKnowledgeOwnerAccountPendingParams{
		WorkspaceID: workspaceID, UserID: userID, Email: email, PasswordCiphertext: sealed,
	}); err != nil {
		return fmt.Errorf("record owner login: %w", err)
	}
	return nil
}

// ensureOwnerAccountLogged runs ensureOwnerAccount for enable and logs a failure.
func (s *KnowledgeService) ensureOwnerAccountLogged(ctx context.Context, workspaceID, userID pgtype.UUID) {
	tenantID, err := s.Queries.GetWorkspaceKnowledgeTenant(ctx, workspaceID)
	if err != nil {
		slog.Warn("knowledge: owner login skipped; tenant not found", "workspace_id", util.UUIDToString(workspaceID), "error", err)
		return
	}
	if err := s.ensureOwnerAccount(ctx, workspaceID, userID, tenantID); err != nil {
		slog.Warn("knowledge: owner login not provisioned; enable again to retry", "workspace_id", util.UUIDToString(workspaceID), "error", err)
	}
}

// KnowledgeOwnerLogin is what an owner needs to open WeKnora's UI once.
type KnowledgeOwnerLogin struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	UIURL    string `json:"ui_url"`
}

// RevealOwnerPassword returns the owner's WeKnora password once. The sealed copy
// is cleared by the same call, so a second request is refused. A reset is not
// offered yet.
func (s *KnowledgeService) RevealOwnerPassword(ctx context.Context, workspaceID, userID pgtype.UUID) (KnowledgeOwnerLogin, error) {
	if ok, _ := s.offeredReason(); !ok {
		return KnowledgeOwnerLogin{}, ErrKnowledgeNotOffered
	}
	row, err := s.Queries.GetKnowledgeOwnerAccount(ctx, db.GetKnowledgeOwnerAccountParams{WorkspaceID: workspaceID, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && row.WeknoraUserID == "") {
		return KnowledgeOwnerLogin{}, ErrKnowledgeOwnerLoginMissing
	}
	if err != nil {
		return KnowledgeOwnerLogin{}, fmt.Errorf("load owner login: %w", err)
	}
	if row.PasswordCiphertext == nil {
		return KnowledgeOwnerLogin{}, ErrKnowledgeOwnerPasswordShown
	}
	password, err := s.Box.Open(row.PasswordCiphertext)
	if err != nil {
		return KnowledgeOwnerLogin{}, fmt.Errorf("open owner password (was MULTICA_KNOWLEDGE_SECRET_KEY changed?): %w", err)
	}
	n, err := s.Queries.ClearKnowledgeOwnerPassword(ctx, db.ClearKnowledgeOwnerPasswordParams{WorkspaceID: workspaceID, UserID: userID})
	if err != nil {
		return KnowledgeOwnerLogin{}, fmt.Errorf("clear owner password: %w", err)
	}
	if n == 0 {
		// Another request revealed it between the read and the clear.
		return KnowledgeOwnerLogin{}, ErrKnowledgeOwnerPasswordShown
	}
	return KnowledgeOwnerLogin{Email: row.Email, Password: string(password), UIURL: s.UIURL}, nil
}
