# ACP GUI Client - System Design Document

> **Version**: 1.0  
> **Date**: January 2026  
> **Status**: Draft

---

## Table of Contents

1. [Executive Summary](#1-executive-summary)
2. [Goals and Non-Goals](#2-goals-and-non-goals)
3. [Background](#3-background)
4. [System Architecture](#4-system-architecture)
5. [Component Design](#5-component-design)
6. [Data Flow](#6-data-flow)
7. [Interface Definitions](#7-interface-definitions)
8. [Technology Stack](#8-technology-stack)
9. [Implementation Phases](#9-implementation-phases)
10. [Security Considerations](#10-security-considerations)
11. [Future Roadmap](#11-future-roadmap)
12. [Open Questions](#12-open-questions)
13. [Appendix](#13-appendix)

---

## 1. Executive Summary

This document describes the system design for an Electron-based GUI application that provides a unified interface for interacting with ACP-compatible coding agents (such as opencode, Codex CLI, Gemini CLI, etc.).

The application acts as an **ACP Client** that spawns and communicates with coding agents via the [Agent Client Protocol](https://agentclientprotocol.com/), enabling users to leverage AI-powered code assistance without being tied to a specific IDE or editor.

**Key Value Proposition**: A standalone, editor-agnostic GUI that allows non-developer users to interact with powerful coding agents through a simple chat interface, with the flexibility to switch between different ACP-compatible agents.

---

## 2. Goals and Non-Goals

### 2.1 Goals

| ID  | Goal                                                                             | Priority |
| --- | -------------------------------------------------------------------------------- | -------- |
| G1  | Provide a clean, intuitive chat interface for interacting with coding agents     | P0       |
| G2  | Support ACP protocol for agent communication                                     | P0       |
| G3  | Enable seamless switching between different ACP-compatible agents                | P1       |
| G4  | Maintain session context across interactions within a conversation               | P0       |
| G5  | Support streaming responses for real-time feedback                               | P0       |
| G6  | Design for extensibility (future LLM Provider integration, file system controls) | P1       |

### 2.2 Non-Goals (V1)

| ID  | Non-Goal                             | Rationale                                |
| --- | ------------------------------------ | ---------------------------------------- |
| NG1 | Building a full-featured code editor | Out of scope; focus on agent interaction |
| NG2 | Direct LLM API calls (LLM Provider)  | Deferred to future versions              |
| NG3 | Multi-agent orchestration            | V1 supports single agent at a time       |
| NG4 | Remote agent support                 | Focus on local subprocess agents first   |
| NG5 | User authentication / cloud sync     | Desktop-first, local-only in V1          |

---

## 3. Background

### 3.1 What is ACP?

The **Agent Client Protocol (ACP)** is an open standard developed by Zed Industries and JetBrains that standardizes communication between code editors/IDEs and AI coding agents. It is analogous to the Language Server Protocol (LSP) but for AI agents.

**Key Characteristics**:

- Built on **JSON-RPC 2.0**
- Transport via **stdio** (stdin/stdout) for local agents
- **Bidirectional** communication (both client and agent can initiate requests)
- Supports **streaming** via notifications
- Reuses **MCP (Model Context Protocol)** data types where applicable

### 3.2 Why Build This?

Current coding agents (Claude Code, Codex, opencode, etc.) are typically accessed through:

- Terminal/CLI interfaces
- IDE-specific plugins
- Web interfaces

This creates friction for:

- Non-developers who want AI code assistance
- Users who prefer a dedicated GUI over terminal
- Users who want to switch between agents without changing tools

Our application addresses these gaps by providing a **standalone GUI** that works with any ACP-compatible agent.

### 3.3 Target Users

| User Type                          | Needs                                              |
| ---------------------------------- | -------------------------------------------------- |
| Non-developer creators             | Simple interface, no terminal knowledge required   |
| Developers trying different agents | Easy switching between opencode, Codex, Gemini CLI |
| Teams evaluating coding agents     | Consistent interface for comparison                |

---

## 4. System Architecture

### 4.1 High-Level Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│                         Electron Application                        │
│                                                                     │
│  ┌───────────────────────────────────────────────────────────────┐  │
│  │                    Renderer Process (React)                   │  │
│  │                                                               │  │
│  │  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐   │  │
│  │  │  Chat View  │  │  Settings   │  │  File Change Review │   │  │
│  │  │             │  │    View     │  │       (V2)          │   │  │
│  │  └─────────────┘  └─────────────┘  └─────────────────────┘   │  │
│  │                                                               │  │
│  └───────────────────────────┬───────────────────────────────────┘  │
│                              │                                      │
│                              │ IPC (contextBridge)                  │
│                              │                                      │
│  ┌───────────────────────────▼───────────────────────────────────┐  │
│  │                      Main Process                             │  │
│  │                                                               │  │
│  │  ┌─────────────────────────────────────────────────────────┐  │  │
│  │  │                     Conductor                           │  │  │
│  │  │                                                         │  │  │
│  │  │  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐  │  │  │
│  │  │  │   Session    │  │    Agent     │  │   Config     │  │  │  │
│  │  │  │   Manager    │  │   Lifecycle  │  │   Manager    │  │  │  │
│  │  │  └──────────────┘  └──────────────┘  └──────────────┘  │  │  │
│  │  │                                                         │  │  │
│  │  └─────────────────────────┬───────────────────────────────┘  │  │
│  │                            │                                  │  │
│  │                            │ ACP (JSON-RPC over stdio)        │  │
│  │                            │                                  │  │
│  │  ┌─────────────────────────▼───────────────────────────────┐  │  │
│  │  │              ACP Agent (Child Process)                  │  │  │
│  │  │                                                         │  │  │
│  │  │     opencode acp  |  codex --acp  |  gemini-cli acp    │  │  │
│  │  │                                                         │  │  │
│  │  └─────────────────────────────────────────────────────────┘  │  │
│  │                                                               │  │
│  └───────────────────────────────────────────────────────────────┘  │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

### 4.2 Architecture Principles

| Principle                  | Description                                              |
| -------------------------- | -------------------------------------------------------- |
| **Separation of Concerns** | GUI logic in Renderer, agent communication in Main       |
| **Protocol Compliance**    | Strict adherence to ACP specification                    |
| **Extensibility**          | Interfaces designed for future expansion                 |
| **Simplicity First**       | V1 focuses on core functionality with minimal complexity |
| **User Safety**            | File operations will require explicit approval (V2)      |

---

## 5. Component Design

### 5.1 Renderer Process Components

#### 5.1.1 Chat View

**Responsibility**: Display conversation history and handle user input.

```typescript
// Components structure
src/renderer/
├── components/
│   ├── ChatView/
│   │   ├── ChatView.tsx          // Main container
│   │   ├── MessageList.tsx       // Scrollable message history
│   │   ├── MessageItem.tsx       // Individual message rendering
│   │   ├── InputArea.tsx         // User input with send button
│   │   └── StreamingIndicator.tsx // Typing/thinking indicator
│   └── ...
```

**Key Features**:

- Markdown rendering for agent responses
- Code syntax highlighting
- Streaming text display (token by token)
- Diff visualization for file changes (V2)

#### 5.1.2 Settings View

**Responsibility**: Agent configuration and application preferences.

```typescript
interface AgentConfig {
  id: string
  name: string
  command: string
  args: string[]
  env?: Record<string, string>
  enabled: boolean
}

interface AppSettings {
  activeAgentId: string
  agents: AgentConfig[]
  theme: 'light' | 'dark' | 'system'
  workingDirectory: string
}
```

#### 5.1.3 IPC Bridge (Preload)

**Responsibility**: Secure communication between Renderer and Main process.

```typescript
// src/preload/index.ts
import { contextBridge, ipcRenderer } from 'electron'

contextBridge.exposeInMainWorld('electronAPI', {
  // Agent communication
  sendPrompt: (sessionId: string, content: string) =>
    ipcRenderer.invoke('agent:prompt', sessionId, content),

  cancelRequest: (sessionId: string) => ipcRenderer.invoke('agent:cancel', sessionId),

  // Session management
  createSession: (workingDirectory: string) =>
    ipcRenderer.invoke('session:create', workingDirectory),

  closeSession: (sessionId: string) => ipcRenderer.invoke('session:close', sessionId),

  // Agent lifecycle
  switchAgent: (agentId: string) => ipcRenderer.invoke('agent:switch', agentId),

  // Event listeners
  onAgentMessage: (callback: (event: ACPEvent) => void) =>
    ipcRenderer.on('agent:message', (_, event) => callback(event)),

  onAgentError: (callback: (error: Error) => void) =>
    ipcRenderer.on('agent:error', (_, error) => callback(error))
})
```

---

### 5.2 Main Process Components

#### 5.2.1 Conductor

**Responsibility**: Central orchestrator for all agent-related operations.

```typescript
// src/main/conductor/Conductor.ts

import { ACPClient } from '@anthropic/acp-sdk' // Assumed package name

class Conductor {
  private client: ACPClient | null = null
  private sessions: Map<string, Session> = new Map()
  private config: ConductorConfig

  constructor(config: ConductorConfig) {
    this.config = config
  }

  /**
   * Start the configured ACP agent as a subprocess
   */
  async startAgent(agentId: string): Promise<void> {
    const agentConfig = this.config.agents[agentId]
    if (!agentConfig) {
      throw new Error(`Unknown agent: ${agentId}`)
    }

    // Stop existing agent if running
    await this.stopAgent()

    // Initialize ACP client with subprocess transport
    this.client = new ACPClient({
      transport: 'stdio',
      command: agentConfig.command,
      args: agentConfig.args,
      env: agentConfig.env
    })

    // Perform ACP initialization handshake
    await this.client.initialize({
      clientInfo: {
        name: 'ACP-GUI',
        version: '1.0.0'
      },
      capabilities: {
        // Declare client capabilities
      }
    })
  }

  /**
   * Stop the current agent subprocess
   */
  async stopAgent(): Promise<void> {
    if (this.client) {
      await this.client.dispose()
      this.client = null
    }
  }

  /**
   * Create a new conversation session
   */
  async createSession(workingDirectory: string): Promise<string> {
    if (!this.client) {
      throw new Error('No agent is running')
    }

    const session = await this.client.createSession({
      workingDirectory
    })

    this.sessions.set(session.id, session)
    return session.id
  }

  /**
   * Send a prompt to the agent
   */
  async sendPrompt(
    sessionId: string,
    content: string,
    onUpdate: (update: SessionUpdate) => void
  ): Promise<void> {
    const session = this.sessions.get(sessionId)
    if (!session) {
      throw new Error(`Unknown session: ${sessionId}`)
    }

    // Send prompt and handle streaming updates
    await session.prompt({
      content: [{ type: 'text', text: content }],
      onUpdate
    })
  }

  /**
   * Cancel an ongoing request
   */
  async cancelRequest(sessionId: string): Promise<void> {
    const session = this.sessions.get(sessionId)
    if (session) {
      await session.cancel()
    }
  }
}
```

#### 5.2.2 Session Store (Implemented)

**Responsibility**: Persist and manage session data with conversation history.

**Storage Structure**:

```
~/.multica/sessions/
├── index.json              # Session list index (fast load)
└── data/
    └── {session-id}.json   # Complete session data + updates
```

**Key Design Decisions**:

- **Client-side storage**: Multica stores raw ACP `session/update` data
- **Agent-agnostic**: Each agent (opencode, codex, gemini) manages its own internal state
- **Resume behavior**: Creates new ACP session, displays stored history in UI only

```typescript
// src/main/session/SessionStore.ts

interface MulticaSession {
  id: string // Multica-generated UUID
  agentSessionId: string // Agent-returned session ID
  agentId: string // Agent used (opencode/codex/gemini)
  workingDirectory: string
  createdAt: string // ISO 8601
  updatedAt: string
  status: 'active' | 'completed' | 'error'
  title?: string
  messageCount: number
}

interface StoredSessionUpdate {
  timestamp: string
  update: SessionNotification // Raw ACP data
}

interface SessionData {
  session: MulticaSession
  updates: StoredSessionUpdate[]
}

class SessionStore {
  async initialize(): Promise<void>
  async create(params: CreateSessionParams): Promise<MulticaSession>
  async list(options?: ListSessionsOptions): Promise<MulticaSession[]>
  async get(sessionId: string): Promise<SessionData | null>
  async appendUpdate(sessionId: string, update: SessionNotification): Promise<void>
  async updateMeta(sessionId: string, updates: Partial<MulticaSession>): Promise<MulticaSession>
  async delete(sessionId: string): Promise<void>
}
```

**Session Resume Flow**:

1. Load session data from SessionStore
2. Start agent if not running
3. Create new ACP session (agent has no memory of previous conversation)
4. Update `agentSessionId` mapping
5. UI displays stored conversation history

#### 5.2.3 Config Manager

**Responsibility**: Persist and retrieve application configuration.

```typescript
// src/main/config/ConfigManager.ts

import { app } from 'electron'
import * as fs from 'fs/promises'
import * as path from 'path'

interface Config {
  version: string
  activeAgentId: string
  agents: Record<string, AgentConfig>
  ui: UIConfig
}

const DEFAULT_CONFIG: Config = {
  version: '1.0.0',
  activeAgentId: 'opencode',
  agents: {
    opencode: {
      id: 'opencode',
      name: 'OpenCode',
      command: 'opencode',
      args: ['acp'],
      enabled: true
    },
    codex: {
      id: 'codex',
      name: 'Codex CLI',
      command: 'codex',
      args: ['--acp'],
      enabled: true
    },
    gemini: {
      id: 'gemini',
      name: 'Gemini CLI',
      command: 'gemini',
      args: ['acp'],
      enabled: true
    }
  },
  ui: {
    theme: 'system',
    fontSize: 14
  }
}

class ConfigManager {
  private configPath: string
  private config: Config

  constructor() {
    this.configPath = path.join(app.getPath('userData'), 'config.json')
    this.config = DEFAULT_CONFIG
  }

  async load(): Promise<Config> {
    try {
      const data = await fs.readFile(this.configPath, 'utf-8')
      this.config = { ...DEFAULT_CONFIG, ...JSON.parse(data) }
    } catch (error) {
      // Use default config if file doesn't exist
      await this.save()
    }
    return this.config
  }

  async save(): Promise<void> {
    await fs.writeFile(this.configPath, JSON.stringify(this.config, null, 2))
  }

  get(): Config {
    return this.config
  }

  async update(partial: Partial<Config>): Promise<Config> {
    this.config = { ...this.config, ...partial }
    await this.save()
    return this.config
  }
}
```

---

### 5.3 Future Components (V2+)

#### 5.3.1 File System Provider

**Responsibility**: Mediate file system access with user approval.

```typescript
// src/main/filesystem/FileSystemProvider.ts

interface FileOperation {
  type: 'read' | 'write' | 'delete' | 'list'
  path: string
  content?: string
}

interface FileSystemProvider {
  readFile(path: string): Promise<string>
  writeFile(path: string, content: string): Promise<void>
  deleteFile(path: string): Promise<void>
  listDirectory(path: string): Promise<string[]>
}

// V1: Direct passthrough (agent handles file operations)
class DirectFileSystemProvider implements FileSystemProvider {
  // Agent has direct access via its own file system tools
}

// V2: Permissioned access with UI approval
class PermissionedFileSystemProvider implements FileSystemProvider {
  constructor(private approvalCallback: (op: FileOperation) => Promise<boolean>) {}

  async writeFile(path: string, content: string): Promise<void> {
    const approved = await this.approvalCallback({
      type: 'write',
      path,
      content
    })

    if (!approved) {
      throw new Error('User denied file write operation')
    }

    await fs.writeFile(path, content)
  }

  // ... other methods
}
```

#### 5.3.2 LLM Provider

**Responsibility**: Direct LLM API calls for lightweight tasks.

```typescript
// src/main/llm/LLMProvider.ts (V2+)

interface LLMProvider {
  complete(prompt: string, options?: CompletionOptions): Promise<string>
  stream(prompt: string, onToken: (token: string) => void): Promise<void>
}

interface LLMConfig {
  provider: 'openrouter' | 'openai' | 'anthropic'
  apiKey: string
  model: string
}

// Deferred to V2 - Conductor will route between ACP agents and direct LLM calls
```

---

## 6. Data Flow

### 6.1 User Prompt Flow (Happy Path)

```
┌──────────┐     ┌──────────┐     ┌───────────┐     ┌─────────────┐
│   User   │     │ Renderer │     │   Main    │     │ ACP Agent   │
│          │     │ Process  │     │  Process  │     │ (opencode)  │
└────┬─────┘     └────┬─────┘     └─────┬─────┘     └──────┬──────┘
     │                │                 │                  │
     │ 1. Type prompt │                 │                  │
     │───────────────>│                 │                  │
     │                │                 │                  │
     │                │ 2. IPC invoke   │                  │
     │                │ 'agent:prompt'  │                  │
     │                │────────────────>│                  │
     │                │                 │                  │
     │                │                 │ 3. JSON-RPC      │
     │                │                 │ session/prompt   │
     │                │                 │ (via stdin)      │
     │                │                 │─────────────────>│
     │                │                 │                  │
     │                │                 │ 4. session/update│
     │                │                 │ notifications    │
     │                │                 │ (via stdout)     │
     │                │                 │<─ ─ ─ ─ ─ ─ ─ ─ ─│
     │                │                 │                  │
     │                │ 5. IPC event    │                  │
     │                │ 'agent:message' │                  │
     │                │<─ ─ ─ ─ ─ ─ ─ ─ │                  │
     │                │                 │                  │
     │ 6. Streaming   │                 │                  │
     │    UI update   │                 │                  │
     │<─ ─ ─ ─ ─ ─ ─ ─│                 │                  │
     │                │                 │                  │
```

### 6.2 Agent Switching Flow

```
┌──────────┐     ┌──────────┐     ┌───────────┐     ┌─────────────┐
│   User   │     │ Renderer │     │   Main    │     │ New Agent   │
└────┬─────┘     └────┬─────┘     └─────┬─────┘     └──────┬──────┘
     │                │                 │                  │
     │ 1. Select new  │                 │                  │
     │    agent       │                 │                  │
     │───────────────>│                 │                  │
     │                │                 │                  │
     │                │ 2. IPC invoke   │                  │
     │                │ 'agent:switch'  │                  │
     │                │────────────────>│                  │
     │                │                 │                  │
     │                │                 │ 3. Kill old      │
     │                │                 │    agent process │
     │                │                 │──────X           │
     │                │                 │                  │
     │                │                 │ 4. Spawn new     │
     │                │                 │    agent         │
     │                │                 │─────────────────>│
     │                │                 │                  │
     │                │                 │ 5. ACP           │
     │                │                 │    initialize    │
     │                │                 │<────────────────>│
     │                │                 │                  │
     │                │ 6. Success      │                  │
     │                │<────────────────│                  │
     │                │                 │                  │
     │ 7. UI updated  │                 │                  │
     │<───────────────│                 │                  │
```

### 6.3 ACP Message Types

| Direction      | Method                          | Purpose                           |
| -------------- | ------------------------------- | --------------------------------- |
| Client → Agent | `initialize`                    | Handshake and capability exchange |
| Client → Agent | `session/new`                   | Create new conversation           |
| Client → Agent | `session/prompt`                | Send user message                 |
| Client → Agent | `session/cancel`                | Cancel ongoing request            |
| Agent → Client | `session/update` (notification) | Streaming response content        |
| Agent → Client | `client/requestPermission`      | Request file/tool approval        |

---

## 7. Interface Definitions

### 7.1 IPC Channels

```typescript
// src/shared/ipc-channels.ts

export const IPC_CHANNELS = {
  // Agent communication
  AGENT_PROMPT: 'agent:prompt',
  AGENT_CANCEL: 'agent:cancel',
  AGENT_SWITCH: 'agent:switch',
  AGENT_MESSAGE: 'agent:message',
  AGENT_ERROR: 'agent:error',
  AGENT_STATUS: 'agent:status',

  // Session management
  SESSION_CREATE: 'session:create',
  SESSION_CLOSE: 'session:close',
  SESSION_LIST: 'session:list',

  // Configuration
  CONFIG_GET: 'config:get',
  CONFIG_UPDATE: 'config:update',

  // File system (V2)
  FILE_APPROVAL_REQUEST: 'file:approval-request',
  FILE_APPROVAL_RESPONSE: 'file:approval-response'
} as const
```

### 7.2 Shared Types

```typescript
// src/shared/types.ts

// Agent configuration
export interface AgentConfig {
  id: string
  name: string
  command: string
  args: string[]
  env?: Record<string, string>
  enabled: boolean
}

// Session state
export interface SessionInfo {
  id: string
  workingDirectory: string
  agentId: string
  createdAt: string
  isActive: boolean
}

// Message types for UI
export interface ChatMessage {
  id: string
  role: 'user' | 'assistant'
  content: MessageContent[]
  timestamp: string
  status: 'pending' | 'streaming' | 'complete' | 'error'
}

export type MessageContent =
  | { type: 'text'; text: string }
  | { type: 'code'; language: string; code: string }
  | { type: 'diff'; filePath: string; hunks: DiffHunk[] }
  | { type: 'tool_call'; name: string; status: 'pending' | 'approved' | 'denied' | 'complete' }

// Agent status
export type AgentStatus =
  | { state: 'stopped' }
  | { state: 'starting'; agentId: string }
  | { state: 'running'; agentId: string; sessionCount: number }
  | { state: 'error'; error: string }

// File operation approval (V2)
export interface FileApprovalRequest {
  requestId: string
  operation: 'read' | 'write' | 'delete'
  path: string
  content?: string
  reason?: string
}

export interface FileApprovalResponse {
  requestId: string
  approved: boolean
  remember?: 'once' | 'session' | 'always'
}
```

### 7.3 Electron API (Renderer)

```typescript
// src/shared/electron-api.d.ts

export interface ElectronAPI {
  // Agent communication
  sendPrompt(sessionId: string, content: string): Promise<void>
  cancelRequest(sessionId: string): Promise<void>
  switchAgent(agentId: string): Promise<void>

  // Session management
  createSession(workingDirectory: string): Promise<SessionInfo>
  closeSession(sessionId: string): Promise<void>
  listSessions(): Promise<SessionInfo[]>

  // Configuration
  getConfig(): Promise<AppConfig>
  updateConfig(config: Partial<AppConfig>): Promise<AppConfig>

  // Event listeners
  onAgentMessage(callback: (message: AgentMessage) => void): () => void
  onAgentStatus(callback: (status: AgentStatus) => void): () => void
  onAgentError(callback: (error: Error) => void): () => void

  // File approval (V2)
  onFileApprovalRequest(callback: (request: FileApprovalRequest) => void): () => void
  respondToFileApproval(response: FileApprovalResponse): Promise<void>
}

declare global {
  interface Window {
    electronAPI: ElectronAPI
  }
}
```

---

## 8. Technology Stack

### 8.1 Core Technologies

| Layer                 | Technology                   | Rationale                                    |
| --------------------- | ---------------------------- | -------------------------------------------- |
| **Framework**         | Electron 28+                 | Cross-platform desktop app, mature ecosystem |
| **Renderer**          | React 18 + TypeScript        | Component-based UI, strong typing            |
| **Styling**           | Tailwind CSS                 | Rapid UI development, consistent design      |
| **State**             | Zustand                      | Lightweight, simple state management         |
| **IPC**               | Electron IPC + contextBridge | Secure renderer-main communication           |
| **ACP**               | @acp/typescript-sdk          | Official ACP protocol implementation         |
| **Markdown**          | react-markdown + rehype      | Rich text rendering                          |
| **Code Highlighting** | Shiki                        | VS Code-quality syntax highlighting          |

### 8.2 Development Tools

| Tool              | Purpose                        |
| ----------------- | ------------------------------ |
| Vite              | Fast bundling for renderer     |
| electron-builder  | App packaging and distribution |
| ESLint + Prettier | Code quality                   |
| Vitest            | Unit testing                   |
| Playwright        | E2E testing                    |

### 8.3 Project Structure

```
acp-gui/
├── package.json
├── electron-builder.yml
├── vite.config.ts
├── tsconfig.json
│
├── src/
│   ├── main/                      # Electron main process
│   │   ├── index.ts               # Entry point
│   │   ├── conductor/
│   │   │   ├── Conductor.ts
│   │   │   ├── SessionManager.ts
│   │   │   └── index.ts
│   │   ├── config/
│   │   │   └── ConfigManager.ts
│   │   ├── ipc/
│   │   │   └── handlers.ts        # IPC handler registration
│   │   └── utils/
│   │
│   ├── preload/                   # Preload scripts
│   │   └── index.ts
│   │
│   ├── renderer/                  # React application
│   │   ├── index.html
│   │   ├── main.tsx
│   │   ├── App.tsx
│   │   ├── components/
│   │   │   ├── ChatView/
│   │   │   ├── Settings/
│   │   │   ├── Sidebar/
│   │   │   └── common/
│   │   ├── hooks/
│   │   │   ├── useAgent.ts
│   │   │   └── useSession.ts
│   │   ├── stores/
│   │   │   ├── chatStore.ts
│   │   │   └── settingsStore.ts
│   │   └── styles/
│   │
│   └── shared/                    # Shared types and constants
│       ├── types.ts
│       ├── ipc-channels.ts
│       └── electron-api.d.ts
│
├── resources/                     # App icons, assets
│
└── tests/
    ├── unit/
    └── e2e/
```

---

## 9. Implementation Phases

### Phase 1: Foundation (Week 1-2)

**Goal**: Basic Electron app with ACP integration

| Task | Description         | Deliverable                         |
| ---- | ------------------- | ----------------------------------- |
| 1.1  | Project scaffolding | Electron + React + TypeScript setup |
| 1.2  | IPC layer           | preload script with contextBridge   |
| 1.3  | Conductor basic     | Spawn opencode acp subprocess       |
| 1.4  | ACP handshake       | initialize + session/new            |
| 1.5  | Minimal UI          | Input box + message display         |

**Milestone**: Send a prompt to opencode and display response

### Phase 2: Core Features (Week 3-4)

**Goal**: Functional chat experience

| Task | Description        | Deliverable                         |
| ---- | ------------------ | ----------------------------------- |
| 2.1  | Streaming support  | Real-time token display             |
| 2.2  | Session management | Multiple conversations              |
| 2.3  | Markdown rendering | Rich response formatting            |
| 2.4  | Code highlighting  | Syntax highlighting for code blocks |
| 2.5  | Cancel support     | Abort ongoing requests              |

**Milestone**: Complete chat experience with streaming

### Phase 3: Polish & Configuration (Week 5-6)

**Goal**: Production-ready V1

| Task | Description     | Deliverable                   |
| ---- | --------------- | ----------------------------- |
| 3.1  | Settings UI     | Agent configuration interface |
| 3.2  | Agent switching | Seamless agent change         |
| 3.3  | Error handling  | Graceful error recovery       |
| 3.4  | Persistence     | Config and session storage    |
| 3.5  | Packaging       | Distributable app builds      |

**Milestone**: V1.0 release

### Phase 4: V2 Features (Future)

| Feature          | Description                        |
| ---------------- | ---------------------------------- |
| File approval UI | Review and approve file operations |
| LLM Provider     | Direct API calls for simple tasks  |
| Multi-agent      | Concurrent agent sessions          |
| History search   | Search past conversations          |
| Plugins          | User-installable extensions        |

---

## 10. Security Considerations

### 10.1 Electron Security Best Practices

| Practice          | Implementation                             |
| ----------------- | ------------------------------------------ |
| Context Isolation | `contextIsolation: true` in webPreferences |
| Node Integration  | `nodeIntegration: false` in renderer       |
| Preload Scripts   | Use contextBridge for IPC                  |
| Remote Module     | Disabled (deprecated)                      |
| Sandbox           | `sandbox: true` for renderer               |

```typescript
// src/main/index.ts
const mainWindow = new BrowserWindow({
  webPreferences: {
    nodeIntegration: false,
    contextIsolation: true,
    sandbox: true,
    preload: path.join(__dirname, '../preload/index.js')
  }
})
```

### 10.2 Agent Security

| Concern            | Mitigation                           |
| ------------------ | ------------------------------------ |
| File system access | V2: Explicit approval UI             |
| Network access     | Agent-specific; document in settings |
| Command execution  | V2: Terminal command approval        |
| Data exfiltration  | Local-only in V1; no cloud sync      |

### 10.3 Configuration Security

| Data            | Storage               | Encryption        |
| --------------- | --------------------- | ----------------- |
| Agent configs   | Local JSON            | None (no secrets) |
| API keys (V2)   | electron-store        | System keychain   |
| Session history | Local SQLite (future) | Optional          |

---

## 11. Future Roadmap

### 11.1 V2 Features

```
┌─────────────────────────────────────────────────────────────────┐
│                          V2 Architecture                        │
│                                                                 │
│  ┌───────────────────────────────────────────────────────────┐  │
│  │                     Renderer Process                      │  │
│  │  ┌─────────────┐  ┌─────────────┐  ┌─────────────────┐   │  │
│  │  │  Chat View  │  │  Approval   │  │  File Explorer  │   │  │
│  │  │             │  │   Dialog    │  │    (Read-only)  │   │  │
│  │  └─────────────┘  └─────────────┘  └─────────────────┘   │  │
│  └───────────────────────────────────────────────────────────┘  │
│                                                                 │
│  ┌───────────────────────────────────────────────────────────┐  │
│  │                      Main Process                         │  │
│  │  ┌─────────────────────────────────────────────────────┐  │  │
│  │  │                    Conductor                        │  │  │
│  │  │  ┌────────────┐  ┌────────────┐  ┌──────────────┐  │  │  │
│  │  │  │  Router    │  │    ACP     │  │     LLM      │  │  │  │
│  │  │  │            │  │   Adapter  │  │   Provider   │  │  │  │
│  │  │  └────────────┘  └────────────┘  └──────────────┘  │  │  │
│  │  │                                                     │  │  │
│  │  │  ┌──────────────────────────────────────────────┐  │  │  │
│  │  │  │          File System Provider               │  │  │  │
│  │  │  │        (with approval workflow)             │  │  │  │
│  │  │  └──────────────────────────────────────────────┘  │  │  │
│  │  └─────────────────────────────────────────────────────┘  │  │
│  └───────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
```

### 11.2 Feature Roadmap

| Version  | Features                                      |
| -------- | --------------------------------------------- |
| **V1.0** | Basic chat, opencode support, agent switching |
| **V1.1** | Session persistence, conversation history     |
| **V2.0** | File approval UI, LLM Provider integration    |
| **V2.1** | MCP server support, tool marketplace          |
| **V3.0** | Multi-agent orchestration, custom workflows   |

### 11.3 Platform Expansion

| Platform            | Timeline | Notes                           |
| ------------------- | -------- | ------------------------------- |
| macOS               | V1.0     | Primary development platform    |
| Windows             | V1.0     | Electron cross-platform         |
| Linux               | V1.0     | AppImage distribution           |
| Web (remote agents) | V3.0+    | When ACP remote support matures |

---

## 12. Open Questions

| ID  | Question                                               | Status      | Decision                                                                                                             |
| --- | ------------------------------------------------------ | ----------- | -------------------------------------------------------------------------------------------------------------------- |
| Q1  | Official ACP TypeScript SDK package name?              | ✅ Resolved | `@agentclientprotocol/sdk`                                                                                           |
| Q2  | Should we support multiple concurrent sessions in V1?  | ✅ Resolved | Yes, SessionStore supports multiple sessions                                                                         |
| Q3  | How to handle agent crashes gracefully?                | Open        | Auto-restart with notification                                                                                       |
| Q4  | Conversation history storage format?                   | ✅ Resolved | JSON files (index.json + per-session data files)                                                                     |
| Q5  | Should working directory be per-session or global?     | ✅ Resolved | Per-session                                                                                                          |
| Q6  | Should Multica restore agent internal state on resume? | Open        | Currently: No. Creates new ACP session, UI shows history only. Future: Consider `session/load` if agents support it. |

---

## 13. Appendix

### 13.1 ACP Protocol Reference

**Initialization Handshake**:

```json
// Client → Agent
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "initialize",
  "params": {
    "protocolVersion": 1,
    "clientInfo": {
      "name": "ACP-GUI",
      "version": "1.0.0"
    },
    "capabilities": {}
  }
}

// Agent → Client
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "protocolVersion": 1,
    "serverInfo": {
      "name": "opencode",
      "version": "0.1.0"
    },
    "capabilities": {
      "streaming": true,
      "tools": ["file", "terminal", "search"]
    }
  }
}
```

**Session Creation**:

```json
// Client → Agent
{
  "jsonrpc": "2.0",
  "id": 2,
  "method": "session/new",
  "params": {
    "workingDirectory": "/Users/user/projects/myapp"
  }
}

// Agent → Client
{
  "jsonrpc": "2.0",
  "id": 2,
  "result": {
    "sessionId": "sess_abc123"
  }
}
```

**Sending Prompt**:

```json
// Client → Agent
{
  "jsonrpc": "2.0",
  "id": 3,
  "method": "session/prompt",
  "params": {
    "sessionId": "sess_abc123",
    "content": [
      {
        "type": "text",
        "text": "Refactor the login function to use async/await"
      }
    ]
  }
}
```

**Streaming Update (Notification)**:

```json
// Agent → Client (no id = notification)
{
  "jsonrpc": "2.0",
  "method": "session/update",
  "params": {
    "sessionId": "sess_abc123",
    "content": [
      {
        "type": "text",
        "text": "I'll refactor the login function..."
      }
    ],
    "done": false
  }
}
```

### 13.2 References

- [Agent Client Protocol Specification](https://agentclientprotocol.com/)
- [ACP GitHub Repository](https://github.com/agentclientprotocol/agent-client-protocol)
- [OpenCode Documentation](https://opencode.ai/docs/)
- [Electron Security Best Practices](https://www.electronjs.org/docs/latest/tutorial/security)
- [JSON-RPC 2.0 Specification](https://www.jsonrpc.org/specification)
- [Model Context Protocol (MCP)](https://modelcontextprotocol.io/)

### 13.3 Glossary

| Term          | Definition                                                      |
| ------------- | --------------------------------------------------------------- |
| **ACP**       | Agent Client Protocol - standardizes editor-agent communication |
| **MCP**       | Model Context Protocol - standardizes tool/data access for AI   |
| **Conductor** | Central component managing agent lifecycle and communication    |
| **Session**   | A single conversation context with an agent                     |
| **IPC**       | Inter-Process Communication between Electron processes          |
| **stdio**     | Standard input/output streams for subprocess communication      |

---

_Document End_
