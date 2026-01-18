# ACP Message Rendering Implementation

This document describes how Multica parses and renders messages from ACP (Agent Client Protocol) agents.

## Architecture Overview

```
ACP Agent Process
       │
       ▼
┌─────────────────────────────┐
│  AcpClientFactory.ts        │  Receives SessionNotification
│  sessionUpdate callback     │
└─────────────────────────────┘
       │
       ▼
┌─────────────────────────────┐
│  SessionStore.ts            │  Persists with sequence numbers
│  ~/.multica/sessions/data/  │
└─────────────────────────────┘
       │
       ▼
┌─────────────────────────────┐
│  Main Process (index.ts)    │  Broadcasts via IPC
│  webContents.send()         │
└─────────────────────────────┘
       │
       ▼
┌─────────────────────────────┐
│  Preload (preload/index.ts) │  Exposes electronAPI
└─────────────────────────────┘
       │
       ▼
┌─────────────────────────────┐
│  useApp.ts hook             │  Accumulates sessionUpdates state
└─────────────────────────────┘
       │
       ▼
┌─────────────────────────────┐
│  ChatView.tsx               │  groupUpdatesIntoMessages()
│  ToolCallItem.tsx           │  Renders UI components
└─────────────────────────────┘
```

## Session Update Types

### ACP Standard Types

| Type                  | Description                                    | Source  |
| --------------------- | ---------------------------------------------- | ------- |
| `agent_message_chunk` | Streaming text content from agent              | ACP SDK |
| `agent_thought_chunk` | Agent's thinking/reasoning (extended thinking) | ACP SDK |
| `tool_call`           | Tool invocation event with initial data        | ACP SDK |
| `tool_call_update`    | Tool status updates (running/completed/failed) | ACP SDK |
| `plan`                | Task list from TodoWrite tool                  | ACP SDK |

### Multica Custom Types

| Type                       | Description                          | Purpose                           |
| -------------------------- | ------------------------------------ | --------------------------------- |
| `user_message`             | User's input message                 | Internal storage, not in ACP spec |
| `error_message`            | Error display (e.g., auth failures)  | UI error rendering                |
| `askuserquestion_response` | Persisted user response to questions | State restoration after restart   |

## Key Files

| File                                           | Purpose                                             |
| ---------------------------------------------- | --------------------------------------------------- |
| `src/main/conductor/AcpClientFactory.ts`       | Creates ACP client, handles sessionUpdate callbacks |
| `src/main/session/SessionStore.ts`             | Persists messages, generates sequence numbers       |
| `src/main/index.ts`                            | Bridges Conductor to Electron IPC                   |
| `src/preload/index.ts`                         | Exposes Electron API to renderer                    |
| `src/renderer/src/hooks/useApp.ts`             | Subscribes to messages, manages state               |
| `src/renderer/src/components/ChatView.tsx`     | Parses updates, renders messages                    |
| `src/renderer/src/components/ToolCallItem.tsx` | Renders individual tool calls                       |
| `src/shared/ipc-channels.ts`                   | IPC channel names                                   |

## Message Parsing Logic

### ChatView.tsx: groupUpdatesIntoMessages()

Location: `src/renderer/src/components/ChatView.tsx:168-490`

This function transforms raw `StoredSessionUpdate[]` into displayable `Message[]`:

1. **Sort by sequence number** - Ensures correct ordering of concurrent updates
2. **Switch on `update.sessionUpdate`** - Dispatches to type-specific handling
3. **Accumulate chunks** - Text/thought chunks are buffered before rendering
4. **Track tool calls** - Stored in `Map<toolCallId, ToolCall>` for in-place updates

### Content Block Types

```typescript
type ContentBlock =
  | { type: 'text'; content: string }
  | { type: 'thought'; content: string }
  | { type: 'image'; data: string; mimeType: string }
  | { type: 'tool_call'; toolCall: ToolCall }
  | { type: 'plan'; entries: PlanEntry[] }
  | { type: 'error'; errorType: 'auth' | 'general'; message: string; ... }
```

## Tool Call Rendering

### Tool Name Resolution Priority

```typescript
// ChatView.tsx:299-300
const meta = update._meta as { claudeCode?: { toolName?: string } }

// Priority order:
1. meta?.claudeCode?.toolName  // Claude Code specific
2. update.kind                  // Standard ACP kind (Codex uses this)
3. update.title                 // Fallback display name
```

### Supported Tools (ToolCallItem.tsx)

| toolName                      | Icon          | Display            |
| ----------------------------- | ------------- | ------------------ |
| `read`                        | FileText      | Read file          |
| `write`                       | FilePen       | Write file         |
| `edit`                        | FilePen       | Edit file          |
| `bash`, `execute`             | Terminal      | Terminal command   |
| `grep`                        | Search        | Content search     |
| `glob`                        | Search        | File pattern match |
| `search`                      | Search        | Generic search     |
| `websearch`                   | Globe         | Web search         |
| `webfetch`, `fetch`           | Globe         | Fetch URL          |
| `task`                        | Bot           | Sub-agent task     |
| `todowrite`                   | ListTodo      | Task list          |
| `askuserquestion`, `question` | MessageSquare | User prompt        |
| (default)                     | Circle        | Unknown tool       |

## Agent-Specific Differences

### Claude Code vs Codex vs OpenCode

| Feature           | Claude Code                 | Codex                | OpenCode          |
| ----------------- | --------------------------- | -------------------- | ----------------- |
| Tool name source  | `_meta.claudeCode.toolName` | `kind` field         | `kind` or `title` |
| Command execution | `bash`                      | `execute`            | varies            |
| Question tool     | `AskUserQuestion`           | -                    | `question`        |
| Kind in updates   | Sometimes                   | Yes (tool_call only) | varies            |

### Codex Kind Caching

Codex's `tool_call_update` events don't include the `kind` field, only the initial `tool_call` does. Solution in `useApp.ts:145-155`:

```typescript
// Cache kind from tool_call event
if (update?.sessionUpdate === 'tool_call' && toolCallId && update?.kind) {
  toolKindMapRef.current.set(toolCallId, kind)
}

// Retrieve cached kind for tool_call_update
if (update?.sessionUpdate === 'tool_call_update' && toolCallId) {
  const storedKind = toolKindMapRef.current.get(toolCallId)
  // Use storedKind for file tree refresh decisions
}
```

## Sequence Number System

### Purpose

Handles concurrent async updates arriving out of order.

### Implementation

- `SessionStore.ts` assigns monotonically increasing sequence numbers
- `ChatView.tsx:171-178` sorts updates by sequence number before processing
- Ensures correct message reconstruction regardless of arrival order

## Internal Messages

Messages with `_internal: true` are sent to agent but NOT displayed in UI:

```typescript
// ChatView.tsx:227-233
const userUpdate = update as { content?: unknown; _internal?: boolean }
if (userUpdate._internal) {
  break // Skip internal messages - not displayed in UI
}
```

Used by the G-3 mechanism for AskUserQuestion answers.

## Error Handling

### Authentication Errors

Detected in `useApp.ts:342-366` and converted to `error_message` updates:

```typescript
if (isAuthError(errorMessage)) {
  const errorUpdate = {
    sessionUpdate: 'error_message',
    errorType: 'auth',
    agentId: currentSession.agentId,
    authCommand: AGENT_AUTH_COMMANDS[agentId],
    message: errorMessage
  }
  setSessionUpdates((prev) => [...prev, errorUpdate])
}
```

### General Errors

`Conductor.ts:402-441` converts errors to `agent_message_chunk` for inline display.

## Adding Support for New Agents

To support a new ACP agent:

1. **Tool name mapping**: Add cases to `getDisplayInfo()` in `ToolCallItem.tsx`
2. **Question tool**: Add tool name to `QUESTION_TOOL_NAMES` in `tool-names.ts`
3. **Auth command**: Add to `AGENT_AUTH_COMMANDS` in `config/defaults.ts`
4. **Kind caching**: If agent doesn't include `kind` in updates, use the Codex pattern

## Performance Considerations

1. **Sequence number ordering**: O(n log n) sort, prevents race conditions
2. **Tool call reference updates**: Same object reference in Map, triggers React re-renders
3. **Chunk accumulation**: Reduces component re-renders during streaming
4. **Collapsible messages**: Messages with 2+ tools/thoughts collapse when complete
