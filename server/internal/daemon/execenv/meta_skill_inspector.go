package execenv

import (
	"strings"

	"github.com/multica-ai/multica/server/internal/cerebro/contextduplication"
)

// CEREBRO-PATCH(prompt-inspector): Expose the composed runtime brief for the read-only Cerebro inspector.
// CEREBRO-PATCH(prompt-inspector-layers): Include repository files, sidecars, and composed prompt totals.

// PromptInjectionLayer is one file or block the daemon writes or merges at task prepare.
type PromptInjectionLayer struct {
	ID              string                       `json:"id"`
	Title           string                       `json:"title"`
	SourceKind      string                       `json:"source_kind"`     // repository | server | sidecar
	InjectionPoint  string                       `json:"injection_point"` // where it lands in the workdir
	FilePath        string                       `json:"file_path"`
	SourceURL       string                       `json:"source_url,omitempty"`
	Content         string                       `json:"content"`
	Tokens          int64                        `json:"tokens"`
	Unavailable     bool                         `json:"unavailable,omitempty"`
	UnavailableNote string                       `json:"unavailable_note,omitempty"`
	Sections        []contextduplication.Section `json:"sections,omitempty"`
}

// RepoPromptFile is optional repository-authored markdown fetched for the inspector.
type RepoPromptFile struct {
	Content   string
	SourceURL string
	Found     bool
}

// RepoPromptSnapshot holds repository files the inspector may display.
type RepoPromptSnapshot struct {
	CLAUDE RepoPromptFile
	AGENTS RepoPromptFile
}

// MetaSkillInspection is the full composed agent context split into injection layers.
type MetaSkillInspection struct {
	Provider       string                       `json:"provider"`
	Layers         []PromptInjectionLayer       `json:"layers"`
	ComposedTokens int64                        `json:"composed_tokens"`
	Sections       []contextduplication.Section `json:"sections"`
	Duplication    contextduplication.Result    `json:"duplication"`
	FullMarkdown   string                       `json:"full_markdown"`
}

// InspectMetaSkill builds the injection layers the daemon prepares for a task and scores
// duplication across all visible prompt text (repository files, runtime brief, sidecars).
func InspectMetaSkill(provider string, ctx TaskContextForEnv, repo *RepoPromptSnapshot) MetaSkillInspection {
	if provider == "" {
		provider = "claude"
	}
	if repo == nil {
		repo = &RepoPromptSnapshot{}
	}

	runtimePath := runtimeConfigFilename(provider)
	runtimeBrief := buildMetaSkillContent(provider, ctx)
	runtimeSections := contextduplication.ParseSections(runtimeBrief)
	issueContext := renderIssueContext(provider, ctx)

	var layers []PromptInjectionLayer
	var allSections []contextduplication.Section

	appendLayer := func(layer PromptInjectionLayer) {
		layers = append(layers, layer)
		if len(layer.Sections) > 0 {
			for _, s := range layer.Sections {
				allSections = append(allSections, contextduplication.Section{
					ID:      layer.ID + "/" + s.ID,
					Title:   layer.Title + " · " + s.Title,
					Content: s.Content,
					Tokens:  s.Tokens,
				})
			}
			return
		}
		if strings.TrimSpace(layer.Content) != "" {
			allSections = append(allSections, contextduplication.Section{
				ID:      layer.ID,
				Title:   layer.Title,
				Content: layer.Content,
				Tokens:  layer.Tokens,
			})
		}
	}

	// Repository-authored instructions (prepended to the runtime config file when present in the workdir).
	if repo.CLAUDE.Found || repo.CLAUDE.Content != "" {
		appendLayer(repoLayer(
			"repo_claude_md",
			"Repository · CLAUDE.md",
			"CLAUDE.md",
			repo.CLAUDE,
			"Prepended to "+runtimePath+" in the agent workdir before the Multica runtime block is injected.",
			provider == "claude",
		))
	} else if provider == "claude" {
		appendLayer(PromptInjectionLayer{
			ID:              "repo_claude_md",
			Title:           "Repository · CLAUDE.md",
			SourceKind:      "repository",
			InjectionPoint:  "Prepended to CLAUDE.md when the checked-out repository contains this file.",
			FilePath:        "CLAUDE.md",
			Unavailable:     true,
			UnavailableNote: "No CLAUDE.md found at the repository default branch (or the file could not be fetched).",
		})
	}

	if repo.AGENTS.Found || repo.AGENTS.Content != "" {
		usedByProvider := provider != "claude" && provider != "gemini"
		note := "Present in the repository; merged into AGENTS.md for Codex/Cursor-style providers."
		if provider == "claude" {
			note = "Present in the repository; Claude reads CLAUDE.md (not AGENTS.md) unless you symlink or duplicate rules."
		}
		appendLayer(repoLayer(
			"repo_agents_md",
			"Repository · AGENTS.md",
			"AGENTS.md",
			repo.AGENTS,
			note,
			usedByProvider,
		))
	} else if provider != "gemini" {
		appendLayer(PromptInjectionLayer{
			ID:              "repo_agents_md",
			Title:           "Repository · AGENTS.md",
			SourceKind:      "repository",
			InjectionPoint:  "Used as the native instructions file for non-Claude providers when present in the workdir.",
			FilePath:        "AGENTS.md",
			Unavailable:     true,
			UnavailableNote: "No AGENTS.md found at the repository default branch (or the file could not be fetched).",
		})
	}

	// Server-built Multica runtime brief (appended inside marker block).
	appendLayer(PromptInjectionLayer{
		ID:             "multica_runtime_brief",
		Title:          "Multica runtime brief",
		SourceKind:     "server",
		InjectionPoint: "Appended inside " + runtimePath + " between <!-- BEGIN MULTICA-RUNTIME --> markers at task prepare (see execenv/runtime_config.go).",
		FilePath:       runtimePath + " (managed block)",
		SourceURL:      "https://github.com/firtal-group/firtal-cerebro/blob/main/server/internal/daemon/execenv/runtime_config.go",
		Content:        runtimeBrief,
		Tokens:         contextduplication.EstimateTokens(runtimeBrief),
		Sections:       runtimeSections,
	})

	// Sidecar issue context (separate file; facts also summarized in the runtime brief).
	appendLayer(PromptInjectionLayer{
		ID:             "sidecar_issue_context",
		Title:          "Issue context (sidecar)",
		SourceKind:     "sidecar",
		InjectionPoint: "Written to .agent_context/issue_context.md during execenv writeContextFiles (skipped if the path already exists).",
		FilePath:       ".agent_context/issue_context.md",
		SourceURL:      "https://github.com/firtal-group/firtal-cerebro/blob/main/server/internal/daemon/execenv/context.go",
		Content:        issueContext,
		Tokens:         contextduplication.EstimateTokens(issueContext),
	})

	dup := contextduplication.Analyze(allSections)
	composed := sumLayerTokens(layers)
	dup = alignDuplicationToComposedTokens(dup, composed)
	full := composeInspectorPreview(provider, repo, runtimeBrief)

	return MetaSkillInspection{
		Provider:       provider,
		Layers:         layers,
		ComposedTokens: composed,
		Sections:       runtimeSections,
		Duplication:    dup,
		FullMarkdown:   full,
	}
}

// sumLayerTokens is the full composed system prompt size (all injection layers).
func sumLayerTokens(layers []PromptInjectionLayer) int64 {
	var total int64
	for _, layer := range layers {
		if layer.Unavailable {
			continue
		}
		total += layer.Tokens
	}
	return total
}

// alignDuplicationToComposedTokens uses the sum of all layers as TotalTokens so
// headline totals include repository files and sidecars, not only parsed sections.
func alignDuplicationToComposedTokens(dup contextduplication.Result, composed int64) contextduplication.Result {
	if composed <= 0 {
		return dup
	}
	dup.TotalTokens = composed
	if dup.DuplicateTokens > composed {
		dup.DuplicateTokens = composed
	}
	dup.DuplicatePct = float64(dup.DuplicateTokens) / float64(composed) * 100
	return dup
}

func runtimeConfigFilename(provider string) string {
	switch provider {
	case "claude":
		return "CLAUDE.md"
	case "gemini":
		return "GEMINI.md"
	default:
		return "AGENTS.md"
	}
}

func repoLayer(id, title, path string, file RepoPromptFile, injectionPoint string, primary bool) PromptInjectionLayer {
	layer := PromptInjectionLayer{
		ID:             id,
		Title:          title,
		SourceKind:     "repository",
		InjectionPoint: injectionPoint,
		FilePath:       path,
		SourceURL:      file.SourceURL,
		Content:        file.Content,
		Tokens:         contextduplication.EstimateTokens(file.Content),
	}
	if !primary && strings.TrimSpace(file.Content) != "" {
		layer.InjectionPoint = injectionPoint + " Not the primary file for this provider preview."
	}
	return layer
}

// composeInspectorPreview simulates the merged runtime config file (user content + Multica block).
func composeInspectorPreview(provider string, repo *RepoPromptSnapshot, runtimeBrief string) string {
	var b strings.Builder
	switch provider {
	case "claude":
		if repo.CLAUDE.Found {
			b.WriteString(strings.TrimRight(repo.CLAUDE.Content, "\n"))
			b.WriteString(runtimeManagedSeparator)
		}
	case "gemini":
		// GEMINI.md merge uses the same mechanism; repo file fetch not wired for gemini in v1.
	default:
		if repo.AGENTS.Found {
			b.WriteString(strings.TrimRight(repo.AGENTS.Content, "\n"))
			b.WriteString(runtimeManagedSeparator)
		}
	}
	block := runtimeMarkerBegin + "\n" + strings.TrimRight(runtimeBrief, "\n") + "\n" + runtimeMarkerEnd + "\n"
	b.WriteString(block)
	if b.Len() == 0 {
		return block
	}
	return b.String()
}
