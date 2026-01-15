# Claude Code Instructions

This file contains instructions for Claude Code when working on this repository.

## Important: Read AGENTS.md First

Before making any code changes, read and follow the guidelines in [AGENTS.md](./AGENTS.md).

## Key Requirements

1. **Always write tests** - Every code change must include corresponding unit tests
2. **Run checks before committing** - Use `pnpm typecheck && pnpm lint && pnpm test:run`
3. **Follow test conventions** - See AGENTS.md for test file location patterns

## Quick Reference

```bash
# Development
pnpm dev              # Start development server
pnpm build            # Build for production

# Quality checks
pnpm typecheck        # TypeScript compilation check
pnpm lint             # ESLint check
pnpm format:check     # Prettier format check
pnpm test:run         # Run all tests
pnpm test:coverage    # Run tests with coverage report
```
