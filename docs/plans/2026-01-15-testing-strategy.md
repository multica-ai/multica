# Testing Strategy Design

## Goals

- Prevent regression when modifying code
- Verify core logic (Conductor, SessionStore, etc.)
- UI testing deferred

## Approach

- **Mixed granularity**: Unit tests for utilities, integration tests for business flows
- **Framework**: Vitest

## Test Structure

```
tests/
├── unit/                    # Pure functions and utilities
│   ├── main/
│   │   └── session/
│   │       └── SessionStore.test.ts
│   └── shared/
│       └── utils.test.ts
│
├── integration/             # Module collaboration
│   ├── conductor/
│   │   └── Conductor.test.ts
│   └── ipc/
│       └── handlers.test.ts
│
└── setup/
    ├── vitest.setup.ts
    └── mocks/
        ├── electron.ts
        └── acp-sdk.ts
```

## Test Targets

### Unit Tests

- SessionStore: create, save, load, list, error handling
- Utility functions: agent-check, cn()

### Integration Tests

- Conductor: init flow, session lifecycle, message chain, cancellation
- IPC Handlers: channel validation, error propagation, permission flow

## Mock Strategy

| Real                   | Mocked           |
| ---------------------- | ---------------- |
| SessionStore internals | File system (fs) |
| Conductor logic        | ACP SDK          |
| IPC handler logic      | Electron IPC     |

## Configuration

### Dependencies

```bash
pnpm add -D vitest @vitest/coverage-v8
```

### Scripts

```json
{
  "test": "vitest",
  "test:run": "vitest run",
  "test:coverage": "vitest run --coverage"
}
```
