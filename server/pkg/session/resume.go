package session

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ResumeTokenCap caps the injected session context at roughly 2,000 tokens.
// Tokens are approximated as whitespace-delimited words × 1.3 — the same
// heuristic the broader project uses for prompt-size estimates. Capping here
// protects the agent's context window from being flooded by long histories;
// older data is summarised in the conversation_summary column instead.
const ResumeTokenCap = 2000

// ResumeContext captures the information pulled from a previous session and
// surfaced to a new run via BuildResumePrompt. It is the source of truth for
// the "Previous Session Context" block injected at run start.
type ResumeContext struct {
	Session           *Session
	Summary           string
	Branch            string
	WorkingDirectory  string
	FilesModified     []string
	Decisions         []string
	TokensEstimated   int
	ResumedFromRunNum int
}

// HasContent reports whether the context has any field worth injecting.
// Empty context is suppressed entirely so resumed runs render no spurious
// "Previous Session Context" header.
func (c ResumeContext) HasContent() bool {
	if c.Summary != "" {
		return true
	}
	if c.Branch != "" || c.WorkingDirectory != "" {
		return true
	}
	if len(c.FilesModified) > 0 {
		return true
	}
	if len(c.Decisions) > 0 {
		return true
	}
	return false
}

// ResumeResult is returned by Resumer.Resume. It carries the injected
// context block (when a previous session was found) and the new session
// row that subsequent completion writes should update.
type ResumeResult struct {
	Context    *ResumeContext
	NewSession *Session
	// IsResume is true when an existing active session was loaded. False
	// means this is a fresh first run and a new session was created.
	IsResume bool
}

// Resumer orchestrates the resume flow: locate the active session for
// (issueID, agentID), build a context block, and create a new session row
// (or no-op when no resume is needed). The actual state save happens via
// Complete after the run finishes.
type Resumer struct {
	svc    SessionService
	config Config
}

// NewResumer wires a Resumer to a SessionService. The service is responsible
// for persistence; the resumer is a pure orchestrator on top.
func NewResumer(svc SessionService, cfg Config) *Resumer {
	return &Resumer{svc: svc, config: cfg}
}

// Resume loads the active session for (issueID, agentID) and returns a
// ResumeResult. The caller is expected to inject result.Context into the
// system prompt (only when non-nil) and pass result.NewSession to
// Complete after the run.
//
// Behavior:
//   - Active session found → build context, create a NEW session row that
//     increments run_number. The active session is left as-is until Complete
//     marks it inactive. This keeps a full run-history audit trail even when
//     the new run starts.
//   - No active session → create a fresh session with run_number=1.
//   - Lookup or create fails → return an error and the caller falls back
//     to a fresh run (backward compatibility).
func (r *Resumer) Resume(ctx context.Context, issueID, agentID uuid.UUID) (*ResumeResult, error) {
	active, err := r.svc.GetActiveSession(ctx, issueID, agentID)
	if err != nil {
		return nil, fmt.Errorf("resume: load active session: %w", err)
	}

	newSess, err := r.svc.CreateSession(ctx, issueID, agentID)
	if err != nil {
		return nil, fmt.Errorf("resume: create session: %w", err)
	}

	res := &ResumeResult{NewSession: newSess}

	if active == nil {
		return res, nil
	}

	// Build the resume context from the previous active session.
	resume := buildResumeContext(active, r.config)
	if resume.HasContent() {
		res.Context = &resume
		res.IsResume = true
	}
	return res, nil
}

// Complete finalises the session lifecycle after a run finishes. It
// persists the conversation summary + state to the new session. The
// function is intentionally tolerant: a failure to write should never
// block the task from completing.
func (r *Resumer) Complete(
	ctx context.Context,
	sessionID uuid.UUID,
	expectedVersion int,
	summary string,
	state *StateData,
	branch, workingDir string,
	filesModified []string,
) error {
	if state != nil {
		r.compressForState(state)
	}

	bPtr := strPtr(branch)
	wPtr := strPtr(workingDir)

	var summaryPtr *string
	if summary != "" {
		summaryPtr = &summary
	}

	return r.svc.UpdateSessionWithPayload(ctx, sessionID, UpdatePayload{
		StateData:       state,
		Summary:         summaryPtr,
		FilesModified:   filesModified,
		WorkingDir:      wPtr,
		Branch:          bPtr,
		ExpectedVersion: expectedVersion,
	})
}

// ExpirePrevious deactivates prior active sessions for the given
// (issueID, agentID) pair. Called after a successful completion so
// the partial index idx_sessions_active contains at most one row.
func (r *Resumer) ExpirePrevious(ctx context.Context, issueID, agentID uuid.UUID) error {
	return r.svc.ResetSession(ctx, issueID, agentID)
}

// buildResumeContext turns a Session row into a ResumeContext.
func buildResumeContext(s *Session, cfg Config) ResumeContext {
	ctx := ResumeContext{
		Session:           s,
		Summary:           ptrStr(s.ConversationSummary),
		Branch:            ptrStr(s.Branch),
		WorkingDirectory:  ptrStr(s.WorkingDirectory),
		FilesModified:     s.FilesModified,
		ResumedFromRunNum: s.RunNumber,
	}

	if len(s.State) > 0 {
		var st StateData
		if err := json.Unmarshal(s.State, &st); err == nil {
			ctx.Decisions = extractDecisions(&st)
		}
	}

	ctx.TokensEstimated = estimateTokens(ctx)
	if ctx.TokensEstimated > ResumeTokenCap {
		// Cap total context: reduce summary proportionally, then trim decisions.
		summaryTokens := int(float64(len(strings.Fields(ctx.Summary))) * 1.3)
		if summaryTokens > 0 {
			// Allocate half the cap to summary, half to decisions+files.
			summaryBudget := ResumeTokenCap / 2
			ctx.Summary = truncateByTokens(ctx.Summary, summaryBudget)
		}
		// Trim decisions if still over cap after summary truncation.
		ctx.TokensEstimated = estimateTokens(ctx)
		for ctx.TokensEstimated > ResumeTokenCap && len(ctx.Decisions) > 1 {
			ctx.Decisions = ctx.Decisions[:len(ctx.Decisions)-1]
			ctx.TokensEstimated = estimateTokens(ctx)
		}
		// Trim files as last resort.
		for ctx.TokensEstimated > ResumeTokenCap && len(ctx.FilesModified) > 0 {
			ctx.FilesModified = ctx.FilesModified[:len(ctx.FilesModified)-1]
			ctx.TokensEstimated = estimateTokens(ctx)
		}
	}
	return ctx
}

// extractDecisions returns a small list of notable items pulled from the
// session state. We do not parse the full message history — the
// conversation_summary field is the source of truth.
func extractDecisions(st *StateData) []string {
	if st == nil {
		return nil
	}
	var out []string
	for _, tr := range st.ToolResults {
		if strings.HasPrefix(strings.ToLower(tr.ToolName), "decision") && tr.Output != "" {
			out = append(out, truncate(tr.Output, 200))
		}
	}
	if len(out) > 5 {
		out = out[:5]
	}
	return out
}

// BuildResumePrompt renders the ResumeContext into the markdown block the
// system prompt appends. The output is intentionally short.
func (c ResumeContext) BuildResumePrompt() string {
	if !c.HasContent() {
		return ""
	}

	var b strings.Builder
	b.WriteString("## Previous Session Context (Run #")
	b.WriteString(fmt.Sprintf("%d", c.ResumedFromRunNum))
	b.WriteString(")\n")
	if c.Session != nil {
		b.WriteString("Session started: ")
		b.WriteString(c.Session.CreatedAt.UTC().Format(time.RFC3339))
		b.WriteString("\n")
		b.WriteString("Last active: ")
		b.WriteString(c.Session.LastActiveAt.UTC().Format(time.RFC3339))
		b.WriteString("\n")
	}

	if c.Summary != "" {
		b.WriteString("\n### Summary\n")
		b.WriteString(c.Summary)
		b.WriteString("\n")
	}

	if c.Branch != "" || c.WorkingDirectory != "" || len(c.FilesModified) > 0 {
		b.WriteString("\n### Working State\n")
		if c.Branch != "" {
			b.WriteString("- Branch: ")
			b.WriteString(c.Branch)
			b.WriteString("\n")
		}
		if c.WorkingDirectory != "" {
			b.WriteString("- Working Directory: ")
			b.WriteString(c.WorkingDirectory)
			b.WriteString("\n")
		}
		if len(c.FilesModified) > 0 {
			b.WriteString("- Files Modified:\n")
			for _, f := range c.FilesModified {
				b.WriteString("  - ")
				b.WriteString(f)
				b.WriteString("\n")
			}
		}
	}

	if len(c.Decisions) > 0 {
		b.WriteString("\n### Key Decisions\n")
		for _, d := range c.Decisions {
			b.WriteString("- ")
			b.WriteString(d)
			b.WriteString("\n")
		}
	}
	return b.String()
}

func (r *Resumer) compressForState(data *StateData) {
	if data == nil || len(data.Messages) <= r.config.MaxMessagesBeforeCompress {
		return
	}
	splitIdx := len(data.Messages) - r.config.MaxMessagesBeforeCompress
	data.Messages = data.Messages[splitIdx:]
}

func estimateTokens(c ResumeContext) int {
	words := 0
	if c.Summary != "" {
		words += len(strings.Fields(c.Summary))
	}
	if c.Branch != "" {
		words++
	}
	if c.WorkingDirectory != "" {
		words++
	}
	words += len(c.FilesModified)
	for _, d := range c.Decisions {
		words += len(strings.Fields(d))
	}
	return int(float64(words) * 1.3)
}

func truncateByTokens(s string, maxTokens int) string {
	if s == "" {
		return s
	}
	words := strings.Fields(s)
	maxWords := int(float64(maxTokens) / 1.3)
	if maxWords <= 0 {
		return ""
	}
	if len(words) <= maxWords {
		return s
	}
	return strings.Join(words[:maxWords], " ") + "…"
}

func truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if len(s) <= n {
		return s
	}
	if n > 3 {
		return s[:n-1] + "…"
	}
	return s[:n]
}

func ptrStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
