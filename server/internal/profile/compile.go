// Package profile compiles a user_profile DB row into the prompt string that
// gets injected into agent runs (JEH-304 / JEH-1031).
//
// This is a Go port of packages/cerebro-profile/core/compile.ts. The two MUST
// stay in sync — both produce identical output for identical input. The TS
// version powers the live UI preview; the Go version powers the actual
// injection at task-claim time. If you change one, change the other.
package profile

// CEREBRO-PATCH(profile-compile): cerebro modification of upstream file
// CEREBRO-PATCH(profile-compile-v2): JEH-1031 — replaced tech_pref with 4
// scope ratings (git/code/computer/process) and added append/replace custom
// prompt support.

import (
	"encoding/json"
	"fmt"
	"strings"
)

type Profile struct {
	Persona      string
	Language     string // "da" or "en"
	DisplayName  string // optional — caller supplies user's display name
	LengthPref   int
	AutonomyPref int
	GitPref      int // 1-5
	CodePref     int // 1-5
	ComputerPref int // 1-5
	ProcessPref  int // 1-5
	AntiPatterns []string
	CustomPrompt string
	PromptMode   string // "append" or "replace"
}

// CompileFromRow takes a raw user_profile row (with anti_patterns as JSONB
// bytes) plus the user's display name and returns the compiled prompt.
// Returns ("", nil) when the row is empty/invalid — the caller treats that
// as "no profile, skip injection".
func CompileFromRow(
	persona, language, displayName string,
	lengthPref, autonomyPref int,
	gitPref, codePref, computerPref, processPref int,
	antiPatternsJSON []byte,
	customPrompt, promptMode string,
) (string, error) {
	var antiPatterns []string
	if len(antiPatternsJSON) > 0 {
		if err := json.Unmarshal(antiPatternsJSON, &antiPatterns); err != nil {
			return "", fmt.Errorf("decode anti_patterns: %w", err)
		}
	}
	p := Profile{
		Persona:      persona,
		Language:     language,
		DisplayName:  displayName,
		LengthPref:   lengthPref,
		AutonomyPref: autonomyPref,
		GitPref:      gitPref,
		CodePref:     codePref,
		ComputerPref: computerPref,
		ProcessPref:  processPref,
		AntiPatterns: antiPatterns,
		CustomPrompt: customPrompt,
		PromptMode:   promptMode,
	}
	return Compile(p), nil
}

func Compile(p Profile) string {
	custom := strings.TrimSpace(p.CustomPrompt)

	// Replace mode + non-empty custom prompt → the user's own prompt fully
	// overrides the compiled output. If the custom prompt is empty, we fall
	// back to the compiled version so an agent never sees an empty profile.
	if p.PromptMode == "replace" && custom != "" {
		return custom
	}

	header := personaHeader(p)

	var sb strings.Builder
	sb.WriteString(header)
	sb.WriteString("\n\nSTYLE:\n")
	for _, line := range styleLines(p) {
		sb.WriteString("- ")
		sb.WriteString(line)
		sb.WriteString("\n")
	}
	sb.WriteString("\nAUTONOMY:\n")
	for _, line := range autonomyLines(p) {
		sb.WriteString("- ")
		sb.WriteString(line)
		sb.WriteString("\n")
	}
	sb.WriteString("\nSCOPE: ")
	sb.WriteString(scopeLine(p))

	if len(p.AntiPatterns) > 0 {
		sb.WriteString("\n\nAVOID:\n")
		for _, ap := range p.AntiPatterns {
			sb.WriteString("- ")
			sb.WriteString(ap)
			sb.WriteString("\n")
		}
	}

	out := strings.TrimRight(sb.String(), "\n")

	// Append mode adds the custom prompt as its own section under the
	// compiled structure. Empty custom prompt means nothing to append.
	if custom != "" {
		label := "CUSTOM"
		out += "\n\n" + label + ":\n" + custom
	}
	return out
}

func personaHeader(p Profile) string {
	personaLabel := personaDisplayLabel(p.Persona, p.Language)
	langWord := "english"
	if p.Language == "da" {
		langWord = "dansk"
	}
	if p.DisplayName != "" {
		return fmt.Sprintf("USER: %s (%s, %s)", p.DisplayName, personaLabel, langWord)
	}
	return fmt.Sprintf("USER: %s, %s", personaLabel, langWord)
}

func personaDisplayLabel(persona, language string) string {
	labels := map[string]map[string]string{
		"utalmodig": {"da": "Den utålmodige", "en": "The impatient"},
		"ekspert":   {"da": "Eksperten", "en": "The expert"},
		"grundig":   {"da": "Den grundige", "en": "The thorough"},
		"larling":   {"da": "Lærlingen", "en": "The apprentice"},
	}
	if m, ok := labels[persona]; ok {
		if v, ok := m[language]; ok {
			return v
		}
	}
	return persona
}

type bucket int

const (
	bucketLow bucket = iota
	bucketMid
	bucketHigh
)

func bucketSlider(value int) bucket {
	if value <= 33 {
		return bucketLow
	}
	if value <= 66 {
		return bucketMid
	}
	return bucketHigh
}

func styleLines(p Profile) []string {
	b := bucketSlider(p.LengthPref)
	if p.Language == "en" {
		switch b {
		case bucketLow:
			return []string{
				"Default 1–3 sentences. Use a table/list for >5 items.",
				`Skip greetings and "let me know"-closings.`,
				"Match names from the code exactly. No invented terms.",
			}
		case bucketMid:
			return []string{
				"Default 3–6 sentences. Add structure for >5 items.",
				"Match names from the code exactly. No invented terms.",
			}
		default:
			return []string{
				"Explain reasoning and trade-offs. Use sections for longer answers.",
				"Match names from the code exactly. No invented terms.",
			}
		}
	}
	switch b {
	case bucketLow:
		return []string{
			"Default 1–3 sætninger. Vis data i tabel/liste hvis >5 punkter.",
			`Spring hilsner og "let me know"-closings over.`,
			"Match navne fra koden eksakt. Ingen opfundne ord.",
		}
	case bucketMid:
		return []string{
			"Default 3–6 sætninger. Brug struktur ved >5 punkter.",
			"Match navne fra koden eksakt. Ingen opfundne ord.",
		}
	default:
		return []string{
			"Forklar ræsonnement og trade-offs. Brug sektioner ved længere svar.",
			"Match navne fra koden eksakt. Ingen opfundne ord.",
		}
	}
}

func autonomyLines(p Profile) []string {
	b := bucketSlider(p.AutonomyPref)
	if p.Language == "en" {
		switch b {
		case bucketHigh:
			return []string{
				"Reversible: just do it, show what happened.",
				"Destructive: ask first.",
				"Don't ask about things you can find or do yourself.",
			}
		case bucketMid:
			return []string{
				"Reversible: do it, summarise the change.",
				"Destructive or ambiguous: confirm first.",
			}
		default:
			return []string{
				"Confirm plan before non-trivial changes.",
				"Destructive: always ask first.",
			}
		}
	}
	switch b {
	case bucketHigh:
		return []string{
			"Reversibelt: bare kør, vis hvad der skete.",
			"Destruktivt: spørg først.",
			"Spørg ikke om ting du selv kan finde/gøre.",
		}
	case bucketMid:
		return []string{
			"Reversibelt: gør det, opsummér ændringen.",
			"Destruktivt eller tvetydigt: bekræft først.",
		}
	default:
		return []string{
			"Bekræft plan før ikke-trivielle ændringer.",
			"Destruktivt: spørg altid først.",
		}
	}
}

// scopeLine renders the four self-rated scope dimensions on one line.
// Each value is clamped to 1-5; the validator at the API boundary already
// enforces the range, but the clamp protects the prompt format against any
// bypass.
func scopeLine(p Profile) string {
	clamp := func(v int) int {
		if v < 1 {
			return 1
		}
		if v > 5 {
			return 5
		}
		return v
	}
	return fmt.Sprintf(
		"git:%d, code:%d, computer:%d, process:%d",
		clamp(p.GitPref),
		clamp(p.CodePref),
		clamp(p.ComputerPref),
		clamp(p.ProcessPref),
	)
}
