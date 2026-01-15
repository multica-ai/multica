# AI Code Safety System Design

## Overview

A three-layer defense mechanism to prevent AI-generated code from breaking the codebase during multi-person collaboration.

## Problem Statement

When multiple people collaborate using AI to generate code, changes made by AI in one area can inadvertently break other parts of the codebase. We need a comprehensive defense mechanism to:

1. Ensure AI always writes tests after modifications
2. Verify all changes pass CI before merging

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                    DEFENSE LAYERS                           │
├─────────────────────────────────────────────────────────────┤
│  Layer 1: AI Instructions (AGENTS.md + .claude/)            │
│  ↓ Tells AI agents to write tests                           │
├─────────────────────────────────────────────────────────────┤
│  Layer 2: Pre-commit Hook (husky + custom script)           │
│  ↓ Warns developer if tests are missing                     │
├─────────────────────────────────────────────────────────────┤
│  Layer 3: GitHub Actions CI                                 │
│  ↓ Blocks merge if any check fails                          │
│    - TypeScript compilation                                 │
│    - ESLint linting                                         │
│    - Prettier formatting                                    │
│    - Vitest tests                                           │
│    - Coverage threshold (50% → increasing)                  │
│    - Build verification                                     │
└─────────────────────────────────────────────────────────────┘
```

## Layer 1: AI Instructions

### AGENTS.md (Universal)

A markdown file at the project root that any AI coding agent can reference. Contains:

- Testing requirements for all code changes
- Test file naming conventions
- Commands to run before committing
- Coverage expectations

### Claude-specific Config

`.claude/settings.local.json` with Claude Code-specific behaviors that reference AGENTS.md.

## Layer 2: Pre-commit Hook

### Behavior

1. Detects which files are staged for commit
2. If any `src/main/**` or `src/shared/**` files are modified, checks if corresponding test files are also modified
3. If tests are missing, prints a **warning** (advisory only, does not block)
4. Runs quick TypeScript and ESLint checks on staged files

### Tools

- `husky` - Git hooks manager
- `lint-staged` - Run linters on staged files only
- Custom `scripts/check-tests.ts` - Test file checker

## Layer 3: GitHub Actions CI

### Triggers

- Every push to any branch
- Every pull request to `main`

### Checks

1. TypeScript compilation (`pnpm typecheck`)
2. ESLint (`pnpm lint`)
3. Prettier format check (`pnpm format:check`)
4. Vitest tests with coverage (`pnpm test:coverage`)
5. Coverage threshold enforcement (50% minimum)
6. Build verification (`pnpm build`)

### Coverage Strategy

- Initial threshold: 50%
- Plan to increase incrementally over time (e.g., +5% each quarter)

## Files to Create

| File                          | Purpose                   |
| ----------------------------- | ------------------------- |
| `AGENTS.md`                   | Universal AI instructions |
| `.claude/settings.local.json` | Claude Code config        |
| `.github/workflows/ci.yml`    | GitHub Actions workflow   |
| `.husky/pre-commit`           | Pre-commit hook           |
| `scripts/check-tests.ts`      | Test file checker script  |

## Files to Modify

| File               | Change                                     |
| ------------------ | ------------------------------------------ |
| `package.json`     | Add husky, lint-staged; add prepare script |
| `vitest.config.ts` | Add coverage thresholds                    |

## New npm Scripts

```json
{
  "prepare": "husky",
  "format:check": "prettier --check ."
}
```

## Success Criteria

1. All AI agents receive clear instructions to write tests
2. Developers are warned (not blocked) when committing without tests
3. CI prevents merging code that:
   - Has TypeScript errors
   - Has ESLint errors
   - Has formatting issues
   - Fails tests
   - Falls below 50% coverage
   - Fails to build
