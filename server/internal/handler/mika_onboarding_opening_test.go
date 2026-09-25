package handler

import (
	"strings"
	"testing"
)

// Every language the endpoint accepts must have an opening. A missing entry
// would render the empty string, and the member's first ever message from Mika
// would be a blank bubble under three starter cards.
func TestMikaOnboardingOpeningCoversEveryAcceptedLanguage(t *testing.T) {
	for language := range mikaOnboardingLanguages {
		opening := buildMikaOnboardingOpening(language, "Mika", "Venus")
		if strings.TrimSpace(opening) == "" {
			t.Fatalf("language %q has no opening", language)
		}
		if strings.Contains(opening, "%!") {
			t.Fatalf("language %q has a broken format verb: %s", language, opening)
		}
		if !strings.Contains(opening, "Venus") {
			t.Errorf("language %q dropped the workspace name: %s", language, opening)
		}
		if !strings.Contains(opening, "Mika") {
			t.Errorf("language %q dropped the agent name: %s", language, opening)
		}
		if !strings.Contains(opening, "Multica") {
			t.Errorf("language %q never names the product: %s", language, opening)
		}
	}
}

// Owners may rename Mika, so the opening reads the agent's current display
// name. Hardcoding "Mika" would have a renamed agent introduce itself under a
// name the member never chose.
func TestMikaOnboardingOpeningUsesTheCurrentDisplayName(t *testing.T) {
	opening := buildMikaOnboardingOpening("en", "Ada", "Venus")
	if !strings.Contains(opening, "I'm Ada,") {
		t.Fatalf("opening does not introduce the renamed agent:\n%s", opening)
	}
	if strings.Contains(opening, "Mika") {
		t.Fatalf("opening still says Mika after a rename:\n%s", opening)
	}
}

// A blank name is not a state the product should render around: fall back to
// the default rather than emit "I'm , your Chief of Staff".
func TestMikaOnboardingOpeningFallsBackToTheDefaultName(t *testing.T) {
	opening := buildMikaOnboardingOpening("en", "   ", "Venus")
	if !strings.Contains(opening, "I'm Mika,") {
		t.Fatalf("blank name did not fall back to the product default:\n%s", opening)
	}
}

// Workspace names are member-typed and chat renders assistant content as
// markdown, so an unescaped name reformats Mika's first sentence.
func TestMikaOnboardingOpeningEscapesMemberTypedNames(t *testing.T) {
	opening := buildMikaOnboardingOpening("en", "Mika", "**Ops** `prod`")

	if strings.Contains(opening, "**Ops**") {
		t.Errorf("emphasis in the workspace name survived unescaped:\n%s", opening)
	}
	if strings.Contains(opening, "`prod`") {
		t.Errorf("a code span in the workspace name survived unescaped:\n%s", opening)
	}
	for _, want := range []string{`\*\*Ops\*\*`, "\\`prod\\`"} {
		if !strings.Contains(opening, want) {
			t.Errorf("expected %q in the escaped opening:\n%s", want, opening)
		}
	}
}

// The escaper must not double-escape its own output: a name containing a
// literal backslash stays one backslash to the reader.
func TestEscapeMarkdownInlineHandlesBackslashesFirst(t *testing.T) {
	if got := escapeMarkdownInline(`a\*b`); got != `a\\\*b` {
		t.Fatalf("escapeMarkdownInline(`a\\*b`) = %q, want %q", got, `a\\\*b`)
	}
}
