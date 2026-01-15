# AI Agent Guidelines for Multica

This document provides guidelines for AI coding agents working on this repository. Following these guidelines helps maintain code quality and prevents regressions.

## Testing Requirements

When modifying code in this repository, you MUST:

1. **Write tests for all new functionality** - Every new function, class, or module must have corresponding unit tests

2. **Update tests when modifying existing code** - If you change behavior, update the related tests to reflect the new behavior

3. **Run tests before committing** - Execute `pnpm test:run` and ensure all tests pass

4. **Maintain coverage** - New code should have at least 80% test coverage. The CI enforces a minimum of 50% overall coverage.

## Test File Conventions

Follow these conventions for test file locations:

| Source File          | Test File Location               |
| -------------------- | -------------------------------- |
| `src/main/**/*.ts`   | `tests/unit/main/**/*.test.ts`   |
| `src/shared/**/*.ts` | `tests/unit/shared/**/*.test.ts` |
| Integration tests    | `tests/integration/**/*.test.ts` |

### Example

If you modify `src/main/conductor/Conductor.ts`, ensure tests exist at `tests/unit/main/conductor/Conductor.test.ts` or create them if they don't exist.

## Before Committing

Run these commands to verify your changes:

```bash
# TypeScript must compile without errors
pnpm typecheck

# No ESLint errors
pnpm lint

# Code must be properly formatted
pnpm format:check

# All tests must pass
pnpm test:run

# (Optional) Check coverage
pnpm test:coverage
```

## CI Pipeline

The following checks run automatically on every pull request:

1. **TypeScript** - Code must compile without errors
2. **ESLint** - No linting errors allowed
3. **Prettier** - Code must be properly formatted
4. **Tests** - All tests must pass
5. **Coverage** - Minimum 50% code coverage required
6. **Build** - Application must build successfully

PRs that fail any of these checks should not be merged.

## Writing Good Tests

### Unit Tests

- Test one thing per test case
- Use descriptive test names that explain the expected behavior
- Mock external dependencies (Electron APIs, file system, network)
- Test both success and error cases

### Example Test Structure

```typescript
import { describe, it, expect, vi, beforeEach } from 'vitest'

describe('MyModule', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  describe('myFunction', () => {
    it('should return expected result when given valid input', () => {
      const result = myFunction('valid input')
      expect(result).toBe('expected output')
    })

    it('should throw error when given invalid input', () => {
      expect(() => myFunction('')).toThrow('Invalid input')
    })
  })
})
```

## Common Mocks

The test setup provides mocks for:

- Electron APIs (`app`, `ipcMain`, `BrowserWindow`, etc.)
- ACP SDK (`@agentclientprotocol/sdk`)
- File system operations

See `tests/setup/mocks/` for available mocks.
