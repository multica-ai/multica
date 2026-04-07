package service

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// DefaultSkill represents a bundled default skill.
type DefaultSkill struct {
	Name        string
	Description string
	Content     string
}

// BundledDefaultSkills returns the set of pre-bundled skills.
func BundledDefaultSkills() []DefaultSkill {
	return []DefaultSkill{
		{
			Name:        "code-review",
			Description: "Review code changes for quality, security, and best practices",
			Content: `# Code Review

When asked to review code, follow this structured approach:

## Review Checklist
1. **Correctness** — Does the code do what it claims?
2. **Security** — Any injection, XSS, or auth issues?
3. **Performance** — Obvious N+1 queries, unnecessary allocations?
4. **Readability** — Clear naming, reasonable function size?
5. **Tests** — Are edge cases covered?

## Output Format
Provide feedback as:
- **Critical**: Must fix before merge
- **Suggestion**: Would improve but not blocking
- **Praise**: What was done well

Keep feedback constructive and specific with file:line references.`,
		},
		{
			Name:        "test-generation",
			Description: "Generate comprehensive tests for existing code",
			Content: `# Test Generation

When asked to generate tests:

1. Read the source code thoroughly
2. Identify all public functions/methods
3. For each function, create tests covering:
   - Happy path (expected inputs)
   - Edge cases (empty, nil, boundary values)
   - Error cases (invalid inputs, failures)
4. Use table-driven tests when multiple similar cases exist
5. Mock external dependencies, not internal code
6. Name tests descriptively: Test_FunctionName_Scenario_ExpectedResult

For Go: use standard testing package with testify assertions.
For TypeScript: use Vitest with describe/it blocks.`,
		},
		{
			Name:        "refactoring",
			Description: "Refactor code to improve structure without changing behavior",
			Content: `# Refactoring

When asked to refactor code:

## Principles
1. **One thing at a time** — Don't mix refactoring with feature changes
2. **Preserve behavior** — Run tests before and after
3. **Small steps** — Each commit should be independently correct

## Common Refactors
- Extract method/function for repeated code
- Rename for clarity
- Simplify conditionals
- Remove dead code
- Break up large functions (>50 lines)
- Reduce parameter count with structs/options

## Anti-patterns to Fix
- God objects/functions
- Deep nesting (prefer early returns)
- String-typed enums (use proper types)
- Commented-out code (delete it, git has history)

Always explain the "why" behind each refactor.`,
		},
	}
}

// SeedDefaultSkills ensures the bundled default skills exist in the database.
func SeedDefaultSkills(ctx context.Context, q *db.Queries) {
	defaults := BundledDefaultSkills()

	for _, skill := range defaults {
		// Check if already exists.
		existing, _ := q.ListGlobalLocalSkills(ctx)
		found := false
		for _, e := range existing {
			if e.Name == skill.Name && e.IsDefault {
				found = true
				break
			}
		}
		if found {
			continue
		}

		_, err := q.CreateLocalSkill(ctx, db.CreateLocalSkillParams{
			WorkspaceID: pgtype.UUID{}, // global
			ProjectPath: pgtype.Text{}, // global
			Name:        skill.Name,
			Description: skill.Description,
			Content:     skill.Content,
			IsDefault:   true,
		})
		if err != nil {
			slog.Warn("failed to seed default skill", "name", skill.Name, "error", err)
		} else {
			slog.Info("seeded default skill", "name", skill.Name)
		}
	}
}
