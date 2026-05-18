// Package command implements deterministic channel command parsing.
//
// Responsibilities:
//   - Parse slash/source-command input after platform normalization.
//   - Produce structured channel actions for the dispatch pipeline.
//
// Boundaries:
//   - Does not handle ordinary natural-language messages.
//   - Does not build agent prompts or perform mutations directly.
package command

import (
	"context"
	"strings"

	chaction "github.com/multica-ai/multica/server/internal/channel/action"
)

// Request is the stable input for deterministic command resolvers.
type Request struct {
	Text       string
	SourceHint chaction.Source
}

// Resolver turns deterministic channel command text into one action.
type Resolver interface {
	Name() string
	Resolve(ctx context.Context, req Request) (chaction.Result, error)
}

// RuleMatcher matches normalized command text after slash expansion.
type RuleMatcher interface {
	Match(text string) (chaction.Intent, bool)
}

type chainMatcher struct {
	rules []rule
}

// NewRuleMatcher returns the production matcher for deterministic commands.
func NewRuleMatcher() RuleMatcher {
	return &chainMatcher{rules: defaultRules()}
}

func (m *chainMatcher) Match(text string) (chaction.Intent, bool) {
	s := strings.TrimSpace(text)
	if s == "" {
		return chaction.Intent{}, false
	}
	for _, r := range m.rules {
		sub := r.re.FindStringSubmatch(s)
		if sub == nil {
			continue
		}
		params := r.params(sub)
		if params == nil {
			params = map[string]string{}
		}
		return chaction.Intent{
			Kind:       r.kind,
			Confidence: r.confidence,
			Params:     params,
			Source:     chaction.SourceRule,
		}, true
	}
	return chaction.Intent{}, false
}

type RuleResolver struct {
	matcher RuleMatcher
}

// NewRuleResolver creates a deterministic command resolver.
func NewRuleResolver(matcher RuleMatcher) *RuleResolver {
	if matcher == nil {
		matcher = NewRuleMatcher()
	}
	return &RuleResolver{matcher: matcher}
}

func (*RuleResolver) Name() string { return "rule" }

func (r *RuleResolver) Resolve(_ context.Context, req Request) (chaction.Result, error) {
	in, ok := r.matcher.Match(req.Text)
	if !ok {
		return chaction.Result{}, nil
	}
	if req.SourceHint == chaction.SourceCommand {
		in.Source = chaction.SourceCommand
	}
	return chaction.Result{Matched: true, Intent: in}, nil
}
