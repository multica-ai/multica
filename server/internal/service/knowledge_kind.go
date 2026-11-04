package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Knowledge kinds (spec 3.3). A kind says what a document is, which base it
// goes to by default, how long it stays fresh, and which headings it must
// carry. The default kinds are seeded per workspace at provisioning and by the
// backfill; an admin may then edit name, template, review_days and
// required_sections, but not the key.

// KnowledgeVentureBaseKey is the base that holds every initiative's documents,
// one folder per initiative (see initiativeFolderPattern).
const KnowledgeVentureBaseKey = "ventures"

// KnowledgeMaxReviewDays bounds a kind's freshness interval (ten years).
const KnowledgeMaxReviewDays = 3650

var (
	// ErrKnowledgeKindNotFound: no kind with this key in the workspace.
	ErrKnowledgeKindNotFound = errors.New("knowledge kind not found")

	kindKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{1,63}$`)
	// initiativeFolderPattern is INI-<seq>-<slug>, the name of an initiative's
	// folder in the ventures base.
	initiativeFolderPattern = regexp.MustCompile(`^INI-[1-9][0-9]{0,8}-[a-z0-9]+(-[a-z0-9]+)*$`)
	headingPattern          = regexp.MustCompile(`^#{1,6}[ \t]+(.+?)[ \t#]*$`)
)

// defaultKnowledgeKind is one kind of the spec's taxonomy. Its template is
// generated from the required sections: a heading for each, then a blank line
// for the author.
type defaultKnowledgeKind struct {
	Key, Name, DefaultBase string
	ReviewDays             int
	Required               []string
}

// defaultKnowledgeKinds is the seed, in the order of spec 3.3.
var defaultKnowledgeKinds = []defaultKnowledgeKind{
	{"market_report", "Market report", "market", 90, []string{"Summary", "Market size", "Trends", "Sources"}},
	{"competitor_profile", "Competitor profile", "market", 90, []string{"Summary", "Offering", "Pricing", "Strengths and weaknesses", "Sources"}},
	{"customer_insight", "Customer insight", "market", 180, []string{"Summary", "Evidence", "Segments", "Sources"}},
	{"opportunity_brief", "Opportunity brief", "market", 0, []string{"Thesis", "Market", "Evidence", "Open questions"}},
	{"validation_report", "Validation report", KnowledgeVentureBaseKey, 0, []string{"Question", "Findings", "Recommendation"}},
	{"product_brief", "Product brief", "product", 180, []string{"Problem", "Users", "Proposed solution", "Success measures"}},
	{"analysis", "Analysis", KnowledgeVentureBaseKey, 0, []string{"Assumptions", "Economics", "Feasibility", "Conclusion"}},
	{"risk_register", "Risk register", KnowledgeVentureBaseKey, 90, []string{"Risks", "Mitigations", "Owners"}},
	{"prd", "Product requirements", "product", 180, []string{"Problem", "Requirements", "Non-goals", "Acceptance criteria"}},
	{"roadmap", "Roadmap", "product", 90, []string{"Goals", "Milestones", "Dependencies"}},
	{"launch_plan", "Launch plan", KnowledgeVentureBaseKey, 0, []string{"Audience", "Channels", "Timeline", "Success measures"}},
	{"budget_request", "Budget request", KnowledgeVentureBaseKey, 0, []string{"Amount", "Justification", "Breakdown"}},
	{"adr", "Architecture decision", "engineering", 0, []string{"Context", "Decision", "Consequences"}},
	{"runbook", "Runbook", "engineering", 180, []string{"Purpose", "Steps", "Rollback", "Owners"}},
	{"test_report", "Test report", "engineering", 0, []string{"Scope", "Results", "Failures"}},
	{"release_candidate", "Release candidate", "engineering", 0, []string{"Version", "Changes", "Verification"}},
	{"release_notes", "Release notes", "engineering", 0, []string{"Highlights", "Changes", "Known issues"}},
	{"launch_checklist", "Launch checklist", KnowledgeVentureBaseKey, 0, []string{"Checklist", "Owners", "Go or no-go"}},
	{"gtm_plan", "Go-to-market plan", "market", 180, []string{"Audience", "Positioning", "Channels", "Pricing"}},
	{"metrics_report", "Metrics report", KnowledgeVentureBaseKey, 0, []string{"Period", "Metrics", "Commentary"}},
	{"retrospective", "Retrospective", KnowledgeVentureBaseKey, 0, []string{"What happened", "What we learned", "Actions"}},
	{"postmortem", "Postmortem", "engineering", 0, []string{"Summary", "Timeline", "Root cause", "Actions"}},
	{"decision_record", "Decision record", "decisions", 0, []string{"Decision", "Context"}},
	{"brief_archive", "Brief archive", "decisions", 0, []string{"Headline", "Decisions"}},
	{"policy", "Policy", "company", 365, []string{"Scope", "Rule", "Owner"}},
	{"playbook", "Playbook", "company", 180, []string{"Purpose", "Steps"}},
	{"reference", "Reference", "company", 365, []string{"Summary"}},
}

// KnowledgeKindView is one kind as the API reports it.
type KnowledgeKindView struct {
	Key              string   `json:"key"`
	Name             string   `json:"name"`
	DefaultBaseKey   string   `json:"default_base_key"`
	Template         string   `json:"template"`
	ReviewDays       int      `json:"review_days"`
	RequiredSections []string `json:"required_sections"`
}

// KnowledgeKindPatch is an admin edit. A nil field is left as it is.
type KnowledgeKindPatch struct {
	Name             *string
	Template         *string
	ReviewDays       *int
	RequiredSections *[]string
	// DefaultBaseKey must name a live knowledge base of the workspace. An empty
	// value clears the default.
	DefaultBaseKey *string
}

// KnowledgeMissingSectionsError is a draft refused for lacking the headings
// its kind requires. Missing lists them in the kind's spelling.
type KnowledgeMissingSectionsError struct {
	Kind    string
	Missing []string
}

func (e *KnowledgeMissingSectionsError) Error() string {
	return fmt.Sprintf("the %s document lacks required sections: %s", e.Kind, strings.Join(e.Missing, ", "))
}

// defaultTemplate is the skeleton a new kind starts from.
func defaultTemplate(name string, required []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s title\n\n", name)
	for _, s := range required {
		fmt.Fprintf(&b, "## %s\n\n", s)
	}
	return b.String()
}

// MissingSections returns the required headings that content does not carry.
// A heading matches when its text equals the section's, ignoring case and
// surrounding spaces, at any level; a heading inside a code fence does not
// count.
func MissingSections(required []string, content string) []string {
	present := map[string]bool{}
	inFence := false
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if m := headingPattern.FindStringSubmatch(trimmed); m != nil {
			present[normalizeHeading(m[1])] = true
		}
	}
	var missing []string
	for _, s := range required {
		if !present[normalizeHeading(s)] {
			missing = append(missing, s)
		}
	}
	return missing
}

func normalizeHeading(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

func kindView(k db.KnowledgeKind) KnowledgeKindView {
	required := k.RequiredSections
	if required == nil {
		required = []string{}
	}
	return KnowledgeKindView{
		Key: k.Key, Name: k.Name, DefaultBaseKey: k.DefaultBaseKey, Template: k.Template,
		ReviewDays: int(k.ReviewDays), RequiredSections: required,
	}
}

// SeedKnowledgeKinds inserts the default kinds the workspace does not have.
// It is called at provisioning and by the taxonomy backfill; existing rows,
// including admin edits, are never overwritten.
func (s *KnowledgeService) SeedKnowledgeKinds(ctx context.Context, workspaceID pgtype.UUID) error {
	for _, def := range defaultKnowledgeKinds {
		if _, err := s.Queries.SeedKnowledgeKind(ctx, db.SeedKnowledgeKindParams{
			WorkspaceID:      workspaceID,
			Key:              def.Key,
			Name:             def.Name,
			DefaultBaseKey:   def.DefaultBase,
			Template:         defaultTemplate(def.Name, def.Required),
			ReviewDays:       int32(def.ReviewDays),
			RequiredSections: def.Required,
		}); err != nil {
			return fmt.Errorf("seed knowledge kind %q: %w", def.Key, err)
		}
	}
	return nil
}

// SeedKnowledgeKindIn writes one kind when the workspace does not have it, inside
// q's transaction, and reports whether it wrote one. An existing kind, which an
// admin may have edited, is left alone, as the provisioning seed leaves it. The
// company import creates the kinds its export carries through it (spec 4.9).
func SeedKnowledgeKindIn(ctx context.Context, q *db.Queries, p db.SeedKnowledgeKindParams) (bool, error) {
	n, err := q.SeedKnowledgeKind(ctx, p)
	if err != nil {
		return false, fmt.Errorf("seed knowledge kind %q: %w", p.Key, err)
	}
	return n > 0, nil
}

// ListKnowledgeKinds returns the workspace's kinds, by key. A workspace without
// knowledge has none.
func (s *KnowledgeService) ListKnowledgeKinds(ctx context.Context, workspaceID pgtype.UUID) ([]KnowledgeKindView, error) {
	rows, err := s.Queries.ListKnowledgeKinds(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list knowledge kinds: %w", err)
	}
	out := make([]KnowledgeKindView, 0, len(rows))
	for _, r := range rows {
		out = append(out, kindView(r))
	}
	return out, nil
}

// loadKnowledgeKind returns one kind of the workspace.
func (s *KnowledgeService) loadKnowledgeKind(ctx context.Context, workspaceID pgtype.UUID, key string) (db.KnowledgeKind, error) {
	k, err := s.Queries.GetKnowledgeKind(ctx, db.GetKnowledgeKindParams{WorkspaceID: workspaceID, Key: key})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.KnowledgeKind{}, ErrKnowledgeKindNotFound
	}
	if err != nil {
		return db.KnowledgeKind{}, fmt.Errorf("load knowledge kind: %w", err)
	}
	return k, nil
}

// UpdateKnowledgeKind is the admin edit. The template must carry every
// required heading, or a document written from it would be refused.
func (s *KnowledgeService) UpdateKnowledgeKind(ctx context.Context, workspaceID pgtype.UUID, key string, patch KnowledgeKindPatch) (KnowledgeKindView, error) {
	cur, err := s.loadKnowledgeKind(ctx, workspaceID, key)
	if err != nil {
		return KnowledgeKindView{}, err
	}
	name, template, reviewDays, required := cur.Name, cur.Template, int(cur.ReviewDays), cur.RequiredSections
	defaultBase := cur.DefaultBaseKey
	if patch.DefaultBaseKey != nil {
		defaultBase = strings.ToLower(strings.TrimSpace(*patch.DefaultBaseKey))
	}
	if patch.Name != nil {
		name = strings.TrimSpace(*patch.Name)
	}
	if patch.Template != nil {
		template = *patch.Template
	}
	if patch.ReviewDays != nil {
		reviewDays = *patch.ReviewDays
	}
	if patch.RequiredSections != nil {
		required = *patch.RequiredSections
	}
	switch {
	case name == "" || utf8.RuneCountInString(name) > 64:
		return KnowledgeKindView{}, &KnowledgeInvalidError{"name must be 1 to 64 characters"}
	case reviewDays < 0 || reviewDays > KnowledgeMaxReviewDays:
		return KnowledgeKindView{}, &KnowledgeInvalidError{fmt.Sprintf("review_days must be between 0 and %d", KnowledgeMaxReviewDays)}
	case len(required) > 12:
		return KnowledgeKindView{}, &KnowledgeInvalidError{"at most 12 required sections"}
	}
	seen := map[string]bool{}
	cleaned := make([]string, 0, len(required))
	for _, r := range required {
		r = strings.TrimSpace(r)
		switch {
		case r == "" || utf8.RuneCountInString(r) > 80:
			return KnowledgeKindView{}, &KnowledgeInvalidError{"each required section is 1 to 80 characters"}
		case seen[normalizeHeading(r)]:
			return KnowledgeKindView{}, &KnowledgeInvalidError{"required sections must be unique: " + r}
		}
		seen[normalizeHeading(r)] = true
		cleaned = append(cleaned, r)
	}
	if missing := MissingSections(cleaned, template); len(missing) > 0 {
		return KnowledgeKindView{}, &KnowledgeInvalidError{"the template lacks required headings: " + strings.Join(missing, ", ")}
	}
	if defaultBase != "" {
		_, err := s.Queries.GetKnowledgeBaseByKey(ctx, db.GetKnowledgeBaseByKeyParams{WorkspaceID: workspaceID, Key: defaultBase})
		if errors.Is(err, pgx.ErrNoRows) {
			return KnowledgeKindView{}, &KnowledgeInvalidError{"default_base_key " + defaultBase + " is not a knowledge base of this workspace"}
		}
		if err != nil {
			return KnowledgeKindView{}, fmt.Errorf("load default knowledge base: %w", err)
		}
	}
	row, err := s.Queries.UpdateKnowledgeKind(ctx, db.UpdateKnowledgeKindParams{
		WorkspaceID: workspaceID, Key: key, Name: name, Template: template,
		ReviewDays: int32(reviewDays), RequiredSections: cleaned, DefaultBaseKey: defaultBase,
	})
	if err != nil {
		return KnowledgeKindView{}, fmt.Errorf("update knowledge kind: %w", err)
	}
	return kindView(row), nil
}

// kindDefaultBase returns the base a kind writes to by default, or "" when the
// kind has none. A kind key that does not exist is ErrKnowledgeKindNotFound.
func (s *KnowledgeService) kindDefaultBase(ctx context.Context, workspaceID pgtype.UUID, key string) (string, error) {
	k, err := s.loadKnowledgeKind(ctx, workspaceID, key)
	if err != nil {
		return "", err
	}
	return k.DefaultBaseKey, nil
}

// InitiativeFolderName is the folder of an initiative in the ventures base:
// INI-<seq>-<slug>. The caller that owns the initiative supplies the parts.
func InitiativeFolderName(seq int, title string) string {
	return fmt.Sprintf("INI-%d-%s", seq, slugify(title))
}

// initiativeFolder derives the folder of a document's initiative from the
// initiative itself, so a caller names the initiative and not its folder.
func (s *KnowledgeService) initiativeFolder(ctx context.Context, workspaceID, initiativeID pgtype.UUID) (string, error) {
	ini, err := s.Queries.GetInitiativeInWorkspace(ctx, db.GetInitiativeInWorkspaceParams{ID: initiativeID, WorkspaceID: workspaceID})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", &KnowledgeInvalidError{"initiative_id is not an initiative of this workspace"}
	}
	if err != nil {
		return "", fmt.Errorf("load initiative: %w", err)
	}
	return InitiativeFolderName(int(ini.Seq), ini.Title), nil
}

// slugify lowers a title to ASCII words joined by hyphens, at most 40 runes. A
// title with no ASCII letters or digits slugs to "initiative".
func slugify(title string) string {
	var b strings.Builder
	dash := true
	for _, r := range strings.ToLower(title) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case !dash:
			b.WriteByte('-')
			dash = true
		}
		if b.Len() >= 40 {
			break
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		return "initiative"
	}
	return slug
}
