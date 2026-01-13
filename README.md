# Multica

A GUI client for ACP-compatible coding agents.

Multica uses the [Agent Client Protocol (ACP)](https://github.com/anthropics/agent-client-protocol) to communicate with various coding agents like OpenCode, Codex, and Gemini CLI.

## Supported Agents

| Agent | Command | Notes |
|-------|---------|-------|
| [OpenCode](https://github.com/anomalyco/opencode) | `opencode acp` | |
| [Codex CLI (ACP)](https://github.com/zed-industries/codex-acp) | `codex-acp` | Community ACP wrapper |
| [Gemini CLI](https://github.com/google-gemini/gemini-cli) | `gemini acp` | |

## Setup

```bash
pnpm install
```

## Development

```bash
# Start Electron app in dev mode
pnpm dev

# Type check
pnpm typecheck
```

## CLI

Multica includes a comprehensive CLI for testing and interacting with agents:

```bash
pnpm cli                          # Interactive mode
pnpm cli prompt "message"         # One-shot prompt
pnpm cli sessions                 # List sessions
pnpm cli resume <id>              # Resume session
pnpm cli agents                   # List available agents
```

### Interactive Mode

Start an interactive REPL session:

```bash
pnpm cli
```

Available commands in interactive mode:

| Command | Description |
|---------|-------------|
| `/help` | Show help |
| `/sessions` | List all sessions |
| `/new [cwd]` | Create new session (default: current directory) |
| `/resume <id>` | Resume session by ID prefix |
| `/delete <id>` | Delete a session |
| `/history` | Show current session message history |
| `/agent <name>` | Switch to a different agent |
| `/agents` | List available agents |
| `/status` | Show current status |
| `/cancel` | Cancel current request |
| `/quit` | Exit CLI |

### One-Shot Prompt

Send a single prompt and exit:

```bash
pnpm cli prompt "What is 2+2?"
pnpm cli prompt "List files" --cwd=/tmp
```

### Options

| Option | Description |
|--------|-------------|
| `--cwd=PATH` | Working directory for the agent |
| `--log` | Save session log to `logs/` directory |
| `--log=PATH` | Save session log to specified file |

### Cancellation

- Press `Ctrl+C` once to send a cancel request to the agent
- Press `Ctrl+C` twice to force quit

## Build

```bash
# macOS
pnpm build:mac

# Windows
pnpm build:win

# Linux
pnpm build:linux
```

## Architecture

```
Multica (Electron)
├── Renderer Process (React)
│   └── UI Components (Chat, Settings, etc.)
│
├── Main Process
│   ├── Conductor (orchestrates agent communication)
│   │   ├── SessionStore (session persistence)
│   │   └── ClientSideConnection (ACP SDK)
│   │         └── AgentProcess (subprocess management)
│   │               └── opencode/codex-acp/gemini (stdio)
│   │
│   └── IPC Handlers (session, agent, config)
│
└── Preload (contextBridge)
    └── electronAPI (exposed to renderer)
```

### Session Management

Multica maintains its own session layer on top of ACP:

```
~/.multica/sessions/
├── index.json              # Session list (fast load)
└── data/
    └── {session-id}.json   # Full session data + updates
```

**Key design decisions:**
- **Client-side storage**: Multica stores raw `session/update` data for UI display
- **Agent-agnostic**: Each agent manages its own internal state separately
- **Resume behavior**: Creates new ACP session, displays stored history in UI

### IPC API

```typescript
// Session management
electronAPI.createSession(cwd)
electronAPI.listSessions(options?)
electronAPI.getSession(id)
electronAPI.resumeSession(id)
electronAPI.deleteSession(id)

// Agent control
electronAPI.startAgent(agentId)
electronAPI.stopAgent()
electronAPI.sendPrompt(sessionId, content)
electronAPI.cancelRequest(sessionId)
```

## License

MIT
