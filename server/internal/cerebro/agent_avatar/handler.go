package agentavatar

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/storage"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const defaultAvatarModel = "openai/gpt-5-image-mini"
const gatewayChatCompletionsPath = "/api/ai/proxy/v1/chat/completions"

// Avatar visual identity is derived deterministically from the agent name so
// re-generations stay stable without storing a seed. The background colour is
// the primary way to tell agents apart at a glance (Jesper's requirement:
// "om ikke andet baggrundsfarver"), so backgrounds carry a bold, clearly
// distinct hue while clothing stays professionally neutral. The person's gender
// is inferred from the agent's first name (genderForName) rather than hashed, so
// an agent named "Sara" reads as a woman and "Lars" as a man.

// backgroundColors are the solid colour backdrops behind each portrait. They
// are vivid and spread evenly right across the colour wheel — Jesper asked for
// backgrounds that look *much more different* from each other and are easy to
// tell agents apart by. The earlier 16-colour palette forced collisions (the
// workspace has 23 agents), so six agents ended up sharing the same green. This
// 24-hue palette is large enough to give every current agent a unique colour
// when avatars are regenerated as a batch (assignDistinctBackgrounds), and the
// hues step ~15° around the wheel so neighbours stay distinguishable.
//
// Rendering the colour brightly is on the prompt, not this list: the previous
// "vivid" names still came out as dark studio backdrops because the prompt
// asked for "studio background" + "soft studio lighting". buildPromptWithBackground
// now forces a flat, evenly-lit, frame-filling colour instead.
var backgroundColors = []string{
	"vivid red", "bright orange", "golden amber", "bright yellow",
	"chartreuse green", "lime green", "emerald green", "spring green",
	"teal", "turquoise cyan", "sky blue", "azure blue",
	"vivid cobalt blue", "royal blue", "deep indigo", "violet purple",
	"bright magenta", "fuchsia pink", "hot pink", "rose red",
	"coral", "salmon orange", "mint green", "lavender purple",
}

// clothingColors stay neutral so the background carries the colour identity and
// never clashes with it.
var clothingColors = []string{
	"a navy blazer", "a charcoal blazer", "a dark grey suit",
	"a black turtleneck", "a crisp white shirt", "a deep navy sweater",
}

// hairStyles vary the subject so two agents never look like the same person,
// while keeping the Scandinavian brief.
var hairStyles = []string{
	"short blond", "light brown", "dark brown",
	"auburn", "ash grey", "short black",
}

// workspaceLoader reads a workspace (for its gateway settings). *db.Queries
// satisfies it; tests supply a stub.
type workspaceLoader interface {
	GetWorkspace(ctx context.Context, id pgtype.UUID) (db.Workspace, error)
}

type agentStore interface {
	ListAgents(ctx context.Context, workspaceID pgtype.UUID) ([]db.Agent, error)
	UpdateAgent(ctx context.Context, arg db.UpdateAgentParams) (db.Agent, error)
}

// Handler generates photorealistic agent avatars via the Firtal Data Registry AI Gateway.
type Handler struct {
	storage    storage.Storage
	workspaces workspaceLoader
	agents     agentStore
	httpClient *http.Client
	mu         sync.Mutex
	backfills  map[string]*backfillJob
}

// New creates a Handler. store persists the generated PNG; workspaces supplies
// the per-workspace gateway credentials (with env fallback).
func New(store storage.Storage, workspaces workspaceLoader) *Handler {
	h := &Handler{
		storage:    store,
		workspaces: workspaces,
		httpClient: http.DefaultClient,
		backfills:  map[string]*backfillJob{},
	}
	if agents, ok := workspaces.(agentStore); ok {
		h.agents = agents
	}
	return h
}

type generateRequest struct {
	AgentName    string `json:"agent_name"`
	CustomPrompt string `json:"custom_prompt,omitempty"`
}

type generateResponse struct {
	URL string `json:"url"`
}

type backfillJob struct {
	WorkspaceID string    `json:"workspace_id"`
	Status      string    `json:"status"`
	Total       int       `json:"total"`
	Generated   int       `json:"generated"`
	Skipped     int       `json:"skipped"`
	Failed      int       `json:"failed"`
	Errors      []string  `json:"errors"`
	StartedAt   time.Time `json:"started_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type backfillStatusResponse struct {
	WorkspaceID string   `json:"workspace_id"`
	Status      string   `json:"status"`
	Missing     int      `json:"missing"`
	Total       int      `json:"total"`
	Generated   int      `json:"generated"`
	Skipped     int      `json:"skipped"`
	Failed      int      `json:"failed"`
	Errors      []string `json:"errors"`
}

// Generate handles POST /api/agents/generate-avatar.
// Accepts agent_name (for auto-prompt colour selection) and an optional
// custom_prompt. Returns the CDN URL of the uploaded PNG.
func (h *Handler) Generate(w http.ResponseWriter, r *http.Request) {
	wsID := middleware.WorkspaceIDFromContext(r.Context())
	if wsID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	var req generateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	cfg := h.gatewayConfig(r.Context(), wsID)
	if cfg.baseURL == "" || cfg.apiKey == "" {
		writeError(w, http.StatusServiceUnavailable, "image generation not configured: data registry AI gateway URL/key unset")
		return
	}

	url, err := h.generateAndStore(r.Context(), wsID, req.AgentName, req.CustomPrompt, "")
	if err != nil {
		slog.Error("avatar generation failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "image generation failed")
		return
	}

	writeJSON(w, http.StatusOK, generateResponse{URL: url})
}

// GenerateForAgent creates and stores an avatar for a single agent, then
// persists only avatar_url via the COALESCE-based upstream UpdateAgent query.
func (h *Handler) GenerateForAgent(ctx context.Context, wsID string, agent db.Agent) (string, error) {
	if h.agents == nil {
		return "", fmt.Errorf("agent store is not configured")
	}
	if agent.AvatarUrl.Valid && strings.TrimSpace(agent.AvatarUrl.String) != "" {
		return agent.AvatarUrl.String, nil
	}
	return h.storeAvatarForAgent(ctx, wsID, agent, "")
}

// RegenerateForAgent forces a fresh avatar for an agent even when one already
// exists, using an explicit background colour. Batch regeneration uses this so
// it can overwrite the old (dark, colliding) avatars with the new bold,
// distinct ones.
func (h *Handler) RegenerateForAgent(ctx context.Context, wsID string, agent db.Agent, background string) (string, error) {
	if h.agents == nil {
		return "", fmt.Errorf("agent store is not configured")
	}
	return h.storeAvatarForAgent(ctx, wsID, agent, background)
}

// storeAvatarForAgent generates an avatar with the given background, uploads it,
// and persists only avatar_url via the COALESCE-based UpdateAgent query.
func (h *Handler) storeAvatarForAgent(ctx context.Context, wsID string, agent db.Agent, background string) (string, error) {
	url, err := h.generateAndStore(ctx, wsID, agent.Name, "", background)
	if err != nil {
		return "", err
	}
	if _, err := h.agents.UpdateAgent(ctx, db.UpdateAgentParams{
		ID:        agent.ID,
		AvatarUrl: pgtype.Text{String: url, Valid: true},
	}); err != nil {
		return "", fmt.Errorf("update agent avatar_url: %w", err)
	}
	return url, nil
}

// GenerateForAgentAsync runs the slow image generation outside the create-agent
// request path. It intentionally uses a fresh background context because the
// incoming request context is cancelled as soon as the 201 response is written.
func (h *Handler) GenerateForAgentAsync(wsID string, agent db.Agent) {
	if h == nil || h.agents == nil || h.storage == nil {
		return
	}
	if agent.AvatarUrl.Valid && strings.TrimSpace(agent.AvatarUrl.String) != "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if _, err := h.GenerateForAgent(ctx, wsID, agent); err != nil {
			slog.Warn("async agent avatar generation failed", "agent_id", util.UUIDToString(agent.ID), "workspace_id", wsID, "error", err)
		}
	}()
}

// backfillRequest is the optional body of POST /api/agents/backfill-avatars.
// An empty body keeps the original behaviour: generate avatars only for agents
// missing one. mode="all" regenerates every agent (overwriting existing avatars
// with the new bold, distinct palette), minus any exclude_agent_ids — this is
// how "regenerate all historical avatars except Mia" is driven.
type backfillRequest struct {
	Mode            string   `json:"mode"`
	ExcludeAgentIDs []string `json:"exclude_agent_ids"`
}

// Backfill starts a workspace-scoped background job. Default mode regenerates
// only agents missing an avatar; mode="all" force-regenerates every agent
// (minus excludes) with distinct background colours.
func (h *Handler) Backfill(w http.ResponseWriter, r *http.Request) {
	wsID, ok := h.requireWorkspaceAdmin(w, r)
	if !ok {
		return
	}
	var req backfillRequest
	if r.Body != nil {
		// Body is optional; ignore decode errors for an empty/absent body so the
		// original no-body "backfill missing" call keeps working.
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	force := req.Mode == "all"

	agents, err := h.targetAgents(r.Context(), wsID, force, req.ExcludeAgentIDs)
	if err != nil {
		slog.Warn("list agents for avatar backfill failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list agents")
		return
	}

	now := time.Now().UTC()
	h.mu.Lock()
	if existing := h.backfills[wsID]; existing != nil && existing.Status == "running" {
		resp := backfillResponseLocked(wsID, len(agents), existing)
		h.mu.Unlock()
		writeJSON(w, http.StatusAccepted, resp)
		return
	}
	job := &backfillJob{
		WorkspaceID: wsID,
		Status:      "running",
		Total:       len(agents),
		StartedAt:   now,
		UpdatedAt:   now,
	}
	h.backfills[wsID] = job
	resp := backfillResponseLocked(wsID, len(agents), job)
	h.mu.Unlock()

	go h.runBackfill(wsID, agents, job, force)
	writeJSON(w, http.StatusAccepted, resp)
}

// targetAgents resolves the agents a backfill should act on. force=true targets
// every agent; otherwise only those missing an avatar. Either way, agents whose
// ID is in exclude are dropped (used to keep Mia's hand-picked avatar).
func (h *Handler) targetAgents(ctx context.Context, wsID string, force bool, exclude []string) ([]db.Agent, error) {
	if force {
		all, err := h.allAgents(ctx, wsID)
		if err != nil {
			return nil, err
		}
		return filterExcluded(all, exclude), nil
	}
	missing, err := h.missingAvatarAgents(ctx, wsID)
	if err != nil {
		return nil, err
	}
	return filterExcluded(missing, exclude), nil
}

// filterExcluded drops agents whose ID (canonical UUID string) is in exclude.
func filterExcluded(agents []db.Agent, exclude []string) []db.Agent {
	if len(exclude) == 0 {
		return agents
	}
	skip := make(map[string]bool, len(exclude))
	for _, id := range exclude {
		skip[strings.TrimSpace(id)] = true
	}
	out := make([]db.Agent, 0, len(agents))
	for _, a := range agents {
		if skip[util.UUIDToString(a.ID)] {
			continue
		}
		out = append(out, a)
	}
	return out
}

// BackfillStatus returns current progress for the workspace's backfill job.
func (h *Handler) BackfillStatus(w http.ResponseWriter, r *http.Request) {
	wsID, ok := h.requireWorkspaceAdmin(w, r)
	if !ok {
		return
	}
	missing, err := h.missingAvatarAgents(r.Context(), wsID)
	if err != nil {
		slog.Warn("list agents for avatar backfill status failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list agents")
		return
	}
	h.mu.Lock()
	resp := backfillResponseLocked(wsID, len(missing), h.backfills[wsID])
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) generateAndStore(ctx context.Context, wsID, agentName, customPrompt, bgOverride string) (string, error) {
	cfg := h.gatewayConfig(ctx, wsID)
	if cfg.baseURL == "" || cfg.apiKey == "" {
		return "", fmt.Errorf("image generation not configured: data registry AI gateway URL/key unset")
	}
	prompt := buildPromptWithBackground(agentName, customPrompt, bgOverride)
	imgBytes, err := callGateway(ctx, h.httpClient, cfg, prompt)
	if err != nil {
		return "", err
	}
	id, _ := uuid.NewV7()
	key := fmt.Sprintf("workspaces/%s/avatars/%s.png", wsID, id)
	url, err := h.storage.Upload(ctx, key, imgBytes, "image/png", id.String()+".png")
	if err != nil {
		return "", fmt.Errorf("upload avatar: %w", err)
	}
	return url, nil
}

func (h *Handler) missingAvatarAgents(ctx context.Context, wsID string) ([]db.Agent, error) {
	if h.agents == nil {
		return nil, fmt.Errorf("agent store is not configured")
	}
	workspaceUUID, err := util.ParseUUID(wsID)
	if err != nil {
		return nil, err
	}
	agents, err := h.agents.ListAgents(ctx, workspaceUUID)
	if err != nil {
		return nil, err
	}
	missing := make([]db.Agent, 0)
	for _, agent := range agents {
		if !agent.AvatarUrl.Valid || strings.TrimSpace(agent.AvatarUrl.String) == "" {
			missing = append(missing, agent)
		}
	}
	return missing, nil
}

func (h *Handler) allAgents(ctx context.Context, wsID string) ([]db.Agent, error) {
	if h.agents == nil {
		return nil, fmt.Errorf("agent store is not configured")
	}
	workspaceUUID, err := util.ParseUUID(wsID)
	if err != nil {
		return nil, err
	}
	return h.agents.ListAgents(ctx, workspaceUUID)
}

func (h *Handler) runBackfill(wsID string, agents []db.Agent, job *backfillJob, force bool) {
	// On a force run, hand each agent a distinct background colour so the whole
	// set is easy to tell apart; the missing-only run keeps the name-hashed colour.
	var backgrounds []string
	if force {
		backgrounds = assignDistinctBackgrounds(len(agents))
	}
	for i, agent := range agents {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		var err error
		if force {
			_, err = h.RegenerateForAgent(ctx, wsID, agent, backgrounds[i])
		} else {
			_, err = h.GenerateForAgent(ctx, wsID, agent)
		}
		cancel()

		h.mu.Lock()
		if err != nil {
			job.Failed++
			if len(job.Errors) < 5 {
				job.Errors = append(job.Errors, fmt.Sprintf("%s: %v", agent.Name, err))
			}
		} else {
			job.Generated++
		}
		job.UpdatedAt = time.Now().UTC()
		h.mu.Unlock()
	}
	h.mu.Lock()
	job.Status = "done"
	job.UpdatedAt = time.Now().UTC()
	h.mu.Unlock()
}

func backfillResponseLocked(wsID string, missing int, job *backfillJob) backfillStatusResponse {
	if job == nil {
		return backfillStatusResponse{
			WorkspaceID: wsID,
			Status:      "idle",
			Missing:     missing,
			Errors:      []string{},
		}
	}
	return backfillStatusResponse{
		WorkspaceID: job.WorkspaceID,
		Status:      job.Status,
		Missing:     missing,
		Total:       job.Total,
		Generated:   job.Generated,
		Skipped:     job.Skipped,
		Failed:      job.Failed,
		Errors:      append([]string(nil), job.Errors...),
	}
}

func (h *Handler) requireWorkspaceAdmin(w http.ResponseWriter, r *http.Request) (string, bool) {
	wsID := middleware.WorkspaceIDFromContext(r.Context())
	if wsID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return "", false
	}
	member, ok := middleware.MemberFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusForbidden, "workspace membership is required")
		return "", false
	}
	if member.Role != "owner" && member.Role != "admin" {
		writeError(w, http.StatusForbidden, "only workspace owners and admins can backfill agent avatars")
		return "", false
	}
	return wsID, true
}

// buildPrompt returns the generation prompt for an agent, choosing the
// background colour deterministically from the name. Used by single-avatar
// generation (agent create + the one-off generate endpoint). Batch regeneration
// uses buildPromptWithBackground directly so it can hand each agent a distinct
// colour instead of the name-hash (which collides badly across 23 agents).
func buildPrompt(agentName, customPrompt string) string {
	return buildPromptWithBackground(agentName, customPrompt, "")
}

// buildPromptWithBackground builds the generation prompt. A custom prompt is
// used as-is; otherwise a name-seeded prompt produces a photorealistic
// Scandinavian headshot. The background colour is the per-agent identifier:
// bgOverride wins when set (batch regeneration assigns a distinct colour per
// agent), otherwise it is hashed from the name.
//
// The subject's gender is taken from the agent's first name so the portrait
// actually matches the person — "Sara" reads as a woman, "Lars" as a man — and
// the name is also handed to the model as a reinforcing signal.
//
// The background clause is deliberate: the model otherwise renders a dark,
// dimly-lit studio backdrop where the colour barely shows (so "vivid cobalt
// blue" came out almost black). We now force a flat, fully-saturated colour
// that fills the whole frame and is lit bright and even, with the soft studio
// lighting scoped to the face only. The closing clause keeps the model on
// photographs rather than the flat illustrations it otherwise drifts to.
func buildPromptWithBackground(agentName, customPrompt, bgOverride string) string {
	if customPrompt != "" {
		return customPrompt
	}
	background := backgroundFor(agentName, bgOverride)
	clothing := clothingColors[nameIndex(agentName, "clothes", len(clothingColors))]
	hair := hairStyles[nameIndex(agentName, "hair", len(hairStyles))]

	first := firstNameToken(agentName)
	gender, known := genderForName(agentName)

	subject := "a person"
	var genderClause string
	switch {
	case known && first != "":
		subject = "a " + gender
		genderClause = fmt.Sprintf(
			"The subject is a %s — render a clearly %s face and build matching the "+
				"Scandinavian first name %q. ", gender, genderAdjective(gender), first)
	case known:
		subject = "a " + gender
		genderClause = fmt.Sprintf("The subject is a %s — render a clearly %s face. ", gender, genderAdjective(gender))
	case first != "":
		genderClause = fmt.Sprintf(
			"Depict a real person whose gender and appearance are consistent with the "+
				"Scandinavian first name %q. ", first)
	}

	return fmt.Sprintf(
		"Photorealistic professional headshot portrait of %s with Scandinavian features, "+
			"%s hair, wearing %s. "+
			"The entire background is one flat, solid, vivid, fully saturated %s, "+
			"lit bright and perfectly even from edge to edge so the colour reads boldly at first glance — "+
			"absolutely no dark studio backdrop, no vignette, no gradient and no shadows on the background. "+
			"Keep the lighting on the face soft and flattering, sharp focus on the eyes, "+
			"shallow depth of field on the subject only, "+
			"high-quality DSLR photograph, square 1:1 format, centered, single person. "+
			"%s"+
			"A realistic photo of a real person — not an illustration, cartoon, drawing, or 3D render.",
		subject, hair, clothing, background, genderClause,
	)
}

// backgroundFor returns the background colour for an agent: the explicit
// override when set (batch regeneration), otherwise a name-hashed pick.
func backgroundFor(agentName, override string) string {
	if override != "" {
		return override
	}
	return backgroundColors[nameIndex(agentName, "bg", len(backgroundColors))]
}

// assignDistinctBackgrounds returns one background colour per agent so that a
// batch regeneration never repeats a colour while agents fit in the palette.
// This is what actually delivers "easy to tell agents apart": the name-hash
// collides (six agents shared one green across 23 names), but assigning by
// position guarantees distinct colours for the first len(backgroundColors)
// agents and only wraps after that.
func assignDistinctBackgrounds(count int) []string {
	out := make([]string, count)
	for i := 0; i < count; i++ {
		out[i] = backgroundColors[i%len(backgroundColors)]
	}
	return out
}

// genderAdjective turns the "man"/"woman" subject noun into the adjective used
// for the face/build clause ("male"/"female"), so the prompt stays grammatical.
func genderAdjective(gender string) string {
	if gender == "woman" {
		return "female"
	}
	return "male"
}

// firstNameToken returns the first whitespace-delimited token of an agent name,
// trimmed of surrounding punctuation. Agent names are often "Sara - CTO" or
// "Lars - Head of Legal", so only the leading token carries the personal name.
func firstNameToken(name string) string {
	fields := strings.Fields(name)
	if len(fields) == 0 {
		return ""
	}
	return strings.Trim(fields[0], `.,;:!?"'()[]{}`)
}

// genderForName infers the portrait gender from the agent's first name. Known
// names map to "man"/"woman"; unknown names return ok=false so the prompt asks
// the image model (which knows far more names than any baked-in list) to infer
// the gender from the name itself, instead of forcing a coin-flip that used to
// make "Sara" come out male.
func genderForName(name string) (string, bool) {
	key := normalizeNameKey(firstNameToken(name))
	if key == "" {
		return "", false
	}
	if g, ok := firstNameGenders[key]; ok {
		return g, true
	}
	return "", false
}

// normalizeNameKey lowercases a name token and strips everything that is not a
// letter (handles "GPT-Boy" → "gptboy", keeps Danish æ/ø/å) for map lookup.
func normalizeNameKey(token string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(token) {
		if unicode.IsLetter(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// firstNameGenders maps common first names (Danish/Nordic first, then frequent
// international names and the workspace's named agents) to a portrait gender.
// Keys are normalised via normalizeNameKey. Unknown names fall through to the
// model-infers-from-name path in buildPrompt, so this list does not need to be
// exhaustive — it only has to be correct.
var firstNameGenders = map[string]string{
	// Workspace agents with a clear personal name.
	"sara": "woman", "mia": "woman", "nora": "woman", "nova": "woman",
	"sofie": "woman", "tine": "woman", "charlotte": "woman", "charlene": "woman",
	"philippa": "woman", "brian": "man", "finn": "man", "franz": "man",
	"kristian": "man", "lars": "man", "preben": "man", "larry": "man",
	"lando": "man", "jarvis": "man", "ultron": "man", "filip": "man",

	// Common Danish/Nordic women.
	"anne": "woman", "anna": "woman", "ane": "woman", "mette": "woman",
	"camilla": "woman", "louise": "woman", "katrine": "woman", "line": "woman",
	"maria": "woman", "marie": "woman", "ida": "woman", "emma": "woman",
	"clara": "woman", "karla": "woman", "freja": "woman", "alma": "woman",
	"agnes": "woman", "ella": "woman", "josefine": "woman", "laura": "woman",
	"caroline": "woman", "julie": "woman", "cecilie": "woman", "amalie": "woman",
	"signe": "woman", "sofia": "woman", "victoria": "woman", "isabella": "woman",
	"frederikke": "woman", "rikke": "woman", "pernille": "woman", "helle": "woman",
	"hanne": "woman", "lene": "woman", "lone": "woman", "susanne": "woman",
	"bente": "woman", "birgit": "woman", "kirsten": "woman", "gitte": "woman",
	"dorte": "woman", "jette": "woman", "winnie": "woman", "trine": "woman",
	"stine": "woman", "nanna": "woman", "astrid": "woman", "ingrid": "woman",
	"sigrid": "woman", "liv": "woman", "tuva": "woman", "thea": "woman",
	"naja": "woman", "rebecca": "woman", "sarah": "woman", "amanda": "woman",
	"michelle": "woman", "nicoline": "woman", "olivia": "woman", "mathilde": "woman",
	"silje": "woman", "ronja": "woman", "saga": "woman", "vera": "woman",

	// Common Danish/Nordic men.
	"jesper": "man", "anders": "man", "thomas": "man",
	"michael": "man", "henrik": "man", "martin": "man", "morten": "man",
	"jens": "man", "niels": "man", "peter": "man", "per": "man",
	"søren": "man", "soren": "man", "rasmus": "man", "mads": "man",
	"mikkel": "man", "frederik": "man", "christian": "man", "jakob": "man",
	"jacob": "man", "magnus": "man", "oliver": "man", "william": "man",
	"noah": "man", "victor": "man", "oscar": "man", "carl": "man",
	"emil": "man", "august": "man", "alexander": "man", "sebastian": "man",
	"tobias": "man", "elias": "man", "johan": "man", "erik": "man",
	"bjørn": "man", "bjorn": "man", "gustav": "man", "kasper": "man",
	"casper": "man", "klaus": "man", "bo": "man", "ole": "man",
	"poul": "man", "paul": "man", "kim": "man", "flemming": "man",
	"torben": "man", "bent": "man", "knud": "man", "egon": "man",
	"verner": "man", "georg": "man", "axel": "man", "viggo": "man",
	"valdemar": "man", "felix": "man", "benjamin": "man", "daniel": "man",
	"david": "man", "simon": "man", "andreas": "man", "philip": "man",
	"philippe": "man", "marcus": "man", "nikolaj": "man", "patrick": "man",

	// Frequent international names.
	"john": "man", "james": "man", "robert": "man", "richard": "man",
	"george": "man", "edward": "man", "harry": "man", "jack": "man",
	"max": "man", "leo": "man", "louis": "man", "arthur": "man",
	"mary": "woman", "elizabeth": "woman", "jane": "woman", "kate": "woman",
	"katherine": "woman", "grace": "woman", "lucy": "woman",
	"emily": "woman", "chloe": "woman", "lily": "woman", "rose": "woman",
}

// nameIndex deterministically maps an agent name into [0,n) for a given trait
// salt. The salt makes background/clothing/gender/hair vary independently, so
// two agents that collide on one trait still differ on the others. FNV-1a gives
// a well-distributed hash — a hand-rolled one bunched names onto the same colour.
func nameIndex(name, salt string, n int) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(salt + "\x00" + name))
	return int(h.Sum32() % uint32(n))
}

type gatewayResponse struct {
	Choices []struct {
		Message struct {
			Images  []imageRef `json:"images"`
			Content any        `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// imageRef extracts a base64 data URL from a gateway image entry. OpenRouter
// (which the Data Registry gateway proxies) returns each image as an object
// shaped like {"type":"image_url","image_url":{"url":"data:image/png;base64,…"}}.
// Some providers/tests emit a bare string instead, so accept both forms.
type imageRef struct {
	URL string
}

func (r *imageRef) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		r.URL = s
		return nil
	}
	var obj struct {
		URL      string `json:"url"`
		ImageURL struct {
			URL string `json:"url"`
		} `json:"image_url"`
	}
	if err := json.Unmarshal(b, &obj); err != nil {
		return err
	}
	if obj.ImageURL.URL != "" {
		r.URL = obj.ImageURL.URL
	} else {
		r.URL = obj.URL
	}
	return nil
}

type gatewayConfig struct {
	baseURL string
	apiKey  string
	model   string
}

func gatewayConfigFromEnv() gatewayConfig {
	model := strings.TrimSpace(os.Getenv("FIRTAL_DATA_REGISTRY_AVATAR_MODEL"))
	if model == "" {
		model = defaultAvatarModel
	}
	return gatewayConfig{
		baseURL: strings.TrimRight(strings.TrimSpace(os.Getenv("FIRTAL_DATA_REGISTRY_AI_GATEWAY_URL")), "/"),
		apiKey:  strings.TrimSpace(os.Getenv("FIRTAL_DATA_REGISTRY_AI_GATEWAY_KEY")),
		model:   model,
	}
}

// workspaceGatewaySettings mirrors the firtal_gateway block of a workspace's
// settings JSON — the same source the gateway runtime reads its credentials
// from. Avatar generation must use this so the key configured in the workspace
// Settings UI is honoured, instead of a separate (and easily stale) server env var.
type workspaceGatewaySettings struct {
	FirtalGateway *struct {
		GatewayURL string `json:"gateway_url"`
		APIKey     string `json:"api_key"`
	} `json:"firtal_gateway"`
}

// gatewayConfig resolves credentials for the workspace: the workspace-settings
// gateway URL/key take precedence, falling back to the server env (which also
// supplies the model default). This mirrors FirtalGatewayConfigFromWorkspaceSettings
// used by the gateway runtime, keeping avatar generation on the same credential.
func (h *Handler) gatewayConfig(ctx context.Context, wsID string) gatewayConfig {
	cfg := gatewayConfigFromEnv()
	if h.workspaces == nil {
		return cfg
	}
	id, err := util.ParseUUID(wsID)
	if err != nil {
		return cfg
	}
	ws, err := h.workspaces.GetWorkspace(ctx, id)
	if err != nil || len(ws.Settings) == 0 {
		return cfg
	}
	var s workspaceGatewaySettings
	if err := json.Unmarshal(ws.Settings, &s); err != nil || s.FirtalGateway == nil {
		return cfg
	}
	if u := strings.TrimRight(strings.TrimSpace(s.FirtalGateway.GatewayURL), "/"); u != "" {
		cfg.baseURL = u
	}
	if k := strings.TrimSpace(s.FirtalGateway.APIKey); k != "" {
		cfg.apiKey = k
	}
	return cfg
}

func callGateway(ctx context.Context, client *http.Client, cfg gatewayConfig, prompt string) ([]byte, error) {
	if client == nil {
		client = http.DefaultClient
	}
	body := map[string]any{
		"model":      cfg.model,
		"modalities": []string{"image"},
		"messages": []map[string]any{
			{"role": "user", "content": prompt},
		},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.baseURL+gatewayChatCompletionsPath, bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+cfg.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-trace-name", "agent-avatar-generate")
	req.Header.Set("x-tags", "multica,agent-avatar")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call data registry AI gateway: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 20<<20))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("data registry AI gateway returned HTTP %d: %.512s", resp.StatusCode, string(respBody))
	}

	var parsed gatewayResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	if len(parsed.Choices) == 0 || len(parsed.Choices[0].Message.Images) == 0 {
		return nil, fmt.Errorf("no image in data registry AI gateway response")
	}

	imgData := parsed.Choices[0].Message.Images[0].URL
	// Strip "data:image/png;base64," prefix when present.
	if idx := strings.Index(imgData, ","); idx != -1 {
		imgData = imgData[idx+1:]
	}
	decoded, err := base64.StdEncoding.DecodeString(imgData)
	if err != nil {
		// Fall back to URL-safe encoding (some providers use it).
		decoded, err = base64.URLEncoding.DecodeString(imgData)
		if err != nil {
			return nil, fmt.Errorf("decode base64 image: %w", err)
		}
	}
	return decoded, nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
