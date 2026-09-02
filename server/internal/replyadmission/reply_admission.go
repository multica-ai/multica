// Package replyadmission contains the server-owned rules for agent replies.
//
// The check lives outside the HTTP handler so every server writer can apply
// the same decision. In particular, a CLI/API request and a completion
// fallback must not be able to disagree about whether a reply is admissible.
package replyadmission

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/multica-ai/multica/server/internal/util"
)

// RequestFingerprint is the server-owned identity of a comment write for
// idempotency purposes. It deliberately includes the resolved actor and
// source task, not just the user-provided body: the same key must not replay a
// write under a different authenticated identity or task context.
type RequestFingerprint struct {
	IssueID          string   `json:"issue_id"`
	WorkspaceID      string   `json:"workspace_id"`
	AuthorType       string   `json:"author_type"`
	AuthorID         string   `json:"author_id"`
	Content          string   `json:"content"`
	Type             string   `json:"type"`
	ParentID         string   `json:"parent_id"`
	SourceTaskID     string   `json:"source_task_id"`
	AttachmentIDs    []string `json:"attachment_ids"`
	SuppressAgentIDs []string `json:"suppress_agent_ids"`
}

// Fingerprint returns a stable, bounded representation suitable for the
// workspace-scoped idempotency table. json.Marshal on this fixed struct keeps
// field order deterministic and avoids ambiguous delimiter-based hashes.
func Fingerprint(request RequestFingerprint) string {
	b, _ := json.Marshal(request)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Parent is the server-derived context of the comment being replied to.
// Callers must load this from the database; no client-supplied classification
// or requester id is trusted.
type Parent struct {
	ID          string
	IssueID     string
	WorkspaceID string
	AuthorType  string
	AuthorID    string
	Content     string
}

const (
	// PolicyVersion is emitted with admission decisions so dashboards and
	// clients can distinguish a changed classifier from a changed outcome.
	PolicyVersion = "reply-admission-v1"

	ClassificationNotApplicable   = "not_applicable"
	ClassificationAcknowledgement = "acknowledgement"
	ClassificationSubstantive     = "substantive"

	RequirementNone             = "none"
	RequirementRequesterMention = "requester_mention"

	ReasonNotApplicable           = "not_applicable"
	ReasonAcknowledgement         = "acknowledgement"
	ReasonRequesterMentionPresent = "requester_mention_present"
	ReasonMissingRequesterMention = "missing_requester_mention"
)

// Decision is the structured, server-derived admission result. Callers may
// use it for metrics and a machine-readable response, but they cannot supply
// or override any of these fields.
type Decision struct {
	Admitted       bool
	Classification string
	Requirement    string
	Reason         string
	RequesterID    string
	PolicyVersion  string
}

func (d Decision) Outcome() string {
	if d.Admitted {
		return "allowed"
	}
	return "rejected"
}

// MissingRequesterMentionError is returned when a substantive answer to an
// explicit agent opinion/review request omits the requesting agent mention.
type MissingRequesterMentionError struct {
	RequesterID string
}

const MissingRequesterMentionCode = "agent_reply_admission_required"

func (e *MissingRequesterMentionError) Code() string { return MissingRequesterMentionCode }

func (e *MissingRequesterMentionError) Error() string {
	return fmt.Sprintf(
		"substantive reply to an agent opinion/review request must mention the requesting agent in the same comment (mention://agent/%s)",
		e.RequesterID,
	)
}

var (
	// An opinion marker alone is not enough: the parent must also look like a
	// request or question. Keep "review" out of this marker set because a
	// factual question such as "Is the review complete?" is not asking for an
	// opinion. Review requests are handled separately below.
	opinionMarkerRE = regexp.MustCompile(`(?i)\b(opinion|think|thoughts?|feedback|assess(?:ment)?|perspective|agree|disagree|interpretation|critique|weigh\s+in|view|recommend(?:ation)?|advise|advice|suggest(?:ion)?)\b`)
	requestMarkerRE = regexp.MustCompile(`(?i)(\?|\b(can you|could you|would you|do you|what do you|what(?:'s| is) your|what are your|tell me|give me|share your|let me know|weigh in|please\s+(give|share|tell|provide|assess|critique))\b)`)
	// A direct review imperative is an explicit request even when it does not
	// contain an opinion word ("Please review this."). The request verb is
	// required so status statements such as "the review is complete" remain
	// exempt.
	reviewRequestRE = regexp.MustCompile(`(?i)(\b(?:please\s+review|(?:can|could|would)\s+(?:you\s+)?review)\b|\b(?:please\s+)?review\s+(?:this|it|the\s+(?:proposal|document|plan|options?|changes?|approach|implementation))\b|\breview\b[^\n.!?]{0,80}\b(?:let me know|what do you think|what(?:'s| is) your|weigh in)\b)`)
	takeRequestRE   = regexp.MustCompile(`(?i)\bwhat(?:'s| is) your take\b`)
	stanceRequestRE = regexp.MustCompile(`(?i)\b(?:how do you feel|what would you do|would you choose)\b`)
)

// Check applies the fail-closed reply admission rule. It returns nil when:
//   - the parent is not an agent-authored explicit opinion/review request;
//   - the response is a short acknowledgement; or
//   - the response contains a canonical mention://agent/<requester-id> link.
//
// A reply-to-reply is not exempt merely because it is nested. The direct
// parent is the request being answered, which prevents the observed nested
// opinion handoff from silently dropping the next-agent mention.
func Check(parent Parent, response string) error {
	decision := Evaluate(parent, response)
	if decision.Admitted {
		return nil
	}
	return &MissingRequesterMentionError{RequesterID: decision.RequesterID}
}

// Evaluate derives the reply policy from the stored parent and the exact
// response bytes. The caller cannot mark a substantive reply as an
// acknowledgement, or claim that a mention exists; both are recomputed here.
func Evaluate(parent Parent, response string) Decision {
	decision := Decision{
		Admitted:       true,
		Classification: ClassificationNotApplicable,
		Requirement:    RequirementNone,
		Reason:         ReasonNotApplicable,
		RequesterID:    parent.AuthorID,
		PolicyVersion:  PolicyVersion,
	}
	if !isExplicitOpinionRequest(parent) {
		return decision
	}
	if isAcknowledgement(response) {
		decision.Classification = ClassificationAcknowledgement
		decision.Reason = ReasonAcknowledgement
		return decision
	}
	decision.Classification = ClassificationSubstantive
	decision.Requirement = RequirementRequesterMention
	if hasRequesterMention(response, parent.AuthorID) {
		decision.Reason = ReasonRequesterMentionPresent
		return decision
	}
	decision.Admitted = false
	decision.Reason = ReasonMissingRequesterMention
	return decision
}

func isExplicitOpinionRequest(parent Parent) bool {
	if parent.AuthorType != "agent" || parent.AuthorID == "" || strings.TrimSpace(parent.Content) == "" {
		return false
	}
	if reviewRequestRE.MatchString(parent.Content) || takeRequestRE.MatchString(parent.Content) || stanceRequestRE.MatchString(parent.Content) {
		return true
	}
	return opinionMarkerRE.MatchString(parent.Content) && requestMarkerRE.MatchString(parent.Content)
}

func hasRequesterMention(content, requesterID string) bool {
	for _, mention := range util.ParseMentions(stripCodeSpans(content)) {
		if mention.Type == "agent" && mention.ID == requesterID {
			return true
		}
	}
	return false
}

// isAcknowledgement is deliberately conservative. Only short, normalized
// acknowledgement phrases are exempt; an answer that adds analysis, a plan,
// evidence, or a substantive decision must carry the requester mention.
func isAcknowledgement(content string) bool {
	// Remove canonical links before token normalization so an agent may
	// acknowledge and mention in one short message without being gated. Do it
	// after removing code spans: a copied example in backticks is not a real
	// mention and must not affect either admission or acknowledgement parsing.
	normalized := strings.ToLower(strings.TrimSpace(util.MentionRe.ReplaceAllString(stripCodeSpans(content), " ")))
	var b strings.Builder
	for _, r := range normalized {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte(' ')
		}
	}
	words := strings.Fields(b.String())
	if len(words) == 0 || len(words) > 4 {
		return false
	}
	phrase := strings.Join(words, " ")
	switch phrase {
	case "ack", "acknowledged", "noted", "thanks", "thank you", "received", "got it", "understood", "will do", "on it", "okay", "ok", "agreed", "pass", "looks good", "lgtm":
		return true
	default:
		return false
	}
}

// stripCodeSpans removes inline and fenced Markdown code while preserving the
// surrounding text. Mention links inside code are examples, not delivered
// mentions, so they must not satisfy the admission gate.
func stripCodeSpans(content string) string {
	var b strings.Builder
	b.Grow(len(content))
	inFence := false
	fenceChar := byte(0)
	fenceLen := 0
	inInline := false
	atLineStart := true
	for i := 0; i < len(content); {
		if inFence {
			if atLineStart {
				j := i
				for j < len(content) && j-i < 3 && (content[j] == ' ' || content[j] == '\t') {
					j++
				}
				if j < len(content) && content[j] == fenceChar {
					n := j
					for n < len(content) && content[n] == fenceChar {
						n++
					}
					close := n-j >= fenceLen
					for k := n; close && k < len(content) && content[k] != '\n'; k++ {
						close = content[k] == ' ' || content[k] == '\t' || content[k] == '\r'
					}
					if close {
						for k := i; k < n; k++ {
							b.WriteByte(' ')
						}
						i = n
						inFence = false
						atLineStart = false
						continue
					}
				}
			}
			if content[i] == '\n' {
				b.WriteByte('\n')
				i++
				atLineStart = true
			} else {
				b.WriteByte(' ')
				i++
				atLineStart = false
			}
			continue
		}

		if atLineStart {
			// CommonMark indented code blocks are not parsed by util.ParseMentions,
			// so blank the whole line before looking for inline mentions.
			j := i
			indent := 0
			for j < len(content) && (content[j] == ' ' || content[j] == '\t') {
				if content[j] == '\t' {
					indent += 4
				} else {
					indent++
				}
				j++
			}
			if indent >= 4 {
				for i < len(content) && content[i] != '\n' {
					b.WriteByte(' ')
					i++
				}
				continue
			}

			// Fenced code may use either backticks or tildes, with up to three
			// leading spaces. Only line-start fences are recognized; a mid-line
			// backtick sequence remains an ordinary inline code span.
			j = i
			for j < len(content) && j-i < 3 && (content[j] == ' ' || content[j] == '\t') {
				j++
			}
			if j < len(content) && (content[j] == '`' || content[j] == '~') {
				n := j
				for n < len(content) && content[n] == content[j] {
					n++
				}
				if n-j >= 3 {
					for k := i; k < n; k++ {
						b.WriteByte(' ')
					}
					i = n
					inFence = true
					fenceChar = content[j]
					fenceLen = n - j
					atLineStart = false
					continue
				}
			}
		}

		if content[i] == '\n' {
			b.WriteByte('\n')
			i++
			atLineStart = true
			continue
		}
		if !inInline && content[i] == '`' {
			inInline = !inInline
			b.WriteByte(' ')
			i++
			atLineStart = false
			continue
		}
		if inFence || inInline {
			b.WriteByte(' ')
			i++
			atLineStart = false
			continue
		}
		b.WriteByte(content[i])
		i++
		atLineStart = false
	}
	return b.String()
}
