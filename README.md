# Multica

**Multiplexed Information and Computing Agent**

A native desktop client that brings coding agent capabilities to everyone through a visual interface.

English | [简体中文](./README.zh-CN.md) | [繁體中文](./README.zh-TW.md) | [日本語](./README.ja.md) | [한국어](./README.ko.md)

[**Download the latest release**](https://github.com/multica-ai/multica/releases)

![Multica Screenshot](docs/images/screenshot.png)

## Why "Multica"?

The name is inspired by [Multics](https://en.wikipedia.org/wiki/Multics) (Multiplexed Information and Computing Service), a pioneering operating system created in 1964. Although Multics never achieved widespread adoption, it laid the foundation for modern operating systems, including concepts like the hierarchical file system. Unix itself was derived from Multics (Uniplexed Information and Computing Service -> Unics -> Unix).

**The metaphor:** Just as Multics was created to solve the problem of multi-user time-sharing on computing resources, Multica is designed to solve the problem of multi-model/multi-agent collaboration for knowledge workers.

## The Problem

Coding agents (like Claude Code, Codex CLI, OpenCode) have become incredibly powerful in 2025, capable of solving complex tasks far beyond just writing code. However, 95% of knowledge workers are locked out of these capabilities due to three core barriers:

**1. Interaction Mismatch**

- CLI-based tools require understanding of terminal concepts, file paths, and environment variables
- Current tools focus on code output (diffs, commits, linting) rather than business outcomes
- Knowledge workers care about results (charts, reports, analysis), not the scripts that generate them

**2. Local Environment Challenges**

- Web-based agents can't access local files, folders, or native applications
- Setting up Python, Node.js, or other dependencies is a significant barrier
- Missing the "just works" sandbox environment that handles all dependencies

**3. Privacy & Trust**

- Sensitive business data (financial analysis, legal documents, medical records) can't be uploaded to third-party servers
- Need a model where data stays local while intelligence comes from the cloud

Multica bridges this gap by providing a visual, native desktop interface that leverages coding agents' capabilities while keeping your data local.

## Features

- Native macOS application with a clean, intuitive interface
- Support for multiple AI agents through the [Agent Client Protocol (ACP)](https://github.com/anthropics/agent-client-protocol)
- Local-first: your data never leaves your machine
- Session management with history and resume capabilities
- Built-in CLI for power users and testing

## Supported Agents

| Agent                                                          | Command           | Install                                             |
| -------------------------------------------------------------- | ----------------- | --------------------------------------------------- |
| [Claude Code](https://github.com/anthropics/claude-code)       | `claude-code-acp` | `npm install -g @anthropics/claude-code-acp`        |
| [OpenCode](https://github.com/opencode-ai/opencode)            | `opencode acp`    | `go install github.com/opencode-ai/opencode@latest` |
| [Codex CLI (ACP)](https://github.com/zed-industries/codex-acp) | `codex-acp`       | `npm install -g codex-acp`                          |

## Quick Start

```bash
# Install dependencies
pnpm install

# Check which agents are installed
pnpm cli doctor

# Start the desktop app
pnpm dev
```

## CLI

Multica includes a comprehensive CLI for testing and interacting with agents:

```bash
pnpm cli                          # Interactive mode
pnpm cli prompt "message"         # One-shot prompt
pnpm cli sessions                 # List sessions
pnpm cli resume <id>              # Resume session
pnpm cli agents                   # List available agents
pnpm cli doctor                   # Check agent installations
```

### Interactive Mode

Start an interactive REPL session:

```bash
pnpm cli
```

Available commands:

| Command         | Description                                     |
| --------------- | ----------------------------------------------- |
| `/help`         | Show help                                       |
| `/new [cwd]`    | Create new session (default: current directory) |
| `/sessions`     | List all sessions                               |
| `/resume <id>`  | Resume session by ID prefix                     |
| `/delete <id>`  | Delete a session                                |
| `/history`      | Show current session message history            |
| `/agent <name>` | Switch to a different agent                     |
| `/agents`       | List available agents                           |
| `/doctor`       | Check agent installations                       |
| `/status`       | Show current status                             |
| `/cancel`       | Cancel current request                          |
| `/quit`         | Exit CLI                                        |

### One-Shot Prompt

Send a single prompt and exit:

```bash
pnpm cli prompt "What is 2+2?"
pnpm cli prompt "List files" --cwd=/tmp
```

### Options

| Option       | Description                           |
| ------------ | ------------------------------------- |
| `--cwd=PATH` | Working directory for the agent       |
| `--log`      | Save session log to `logs/` directory |
| `--log=PATH` | Save session log to specified file    |

## Development

```bash
# Start Electron app in dev mode
pnpm dev

# Type check
pnpm typecheck

# Run tests
pnpm test
```

## Build

```bash
pnpm build:mac      # macOS
pnpm build:win      # Windows
pnpm build:linux    # Linux
```

## Architecture

```
Multica (Electron)
+-- Renderer Process (React)
|   +-- UI Components (Chat, Settings, etc.)
|
+-- Main Process
|   +-- Conductor (orchestrates agent communication)
|   |   +-- SessionStore (session persistence)
|   |   +-- ClientSideConnection (ACP SDK)
|   |         +-- AgentProcess (subprocess management)
|   |               +-- opencode/codex-acp/gemini (stdio)
|   |
|   +-- IPC Handlers (session, agent, config)
|
+-- Preload (contextBridge)
    +-- electronAPI (exposed to renderer)
```

### Session Management

Multica maintains its own session layer on top of ACP:

```
~/.multica/sessions/
+-- index.json              # Session list (fast load)
+-- data/
    +-- {session-id}.json   # Full session data + updates
```

**Key design decisions:**

- **Client-side storage**: Multica stores raw `session/update` data for UI display
- **Agent-agnostic**: Each agent manages its own internal state separately
- **Resume behavior**: Creates new ACP session, displays stored history in UI

## License

Apache-2.0
