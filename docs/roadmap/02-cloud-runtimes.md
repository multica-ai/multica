# Feature 2: Cloud Agent Runtimes

## Problem

Multica kræver i dag en lokal daemon der spawner agent-CLIs (claude, codex, opencode).
Det betyder:
- Agenten kører kun når din maskine er tændt
- Du skal have CLI'erne installeret lokalt
- Ingen model-agnostisk runtime — du er låst til de CLIs der er tilgængelige
- Ingen cloud-native execution

## What Exists Today

- `agent.Backend` interface i `server/pkg/agent/agent.go`:
  ```go
  type Backend interface {
      Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error)
  }
  ```
- 5 lokale backends: claude, codex, opencode, openclaw, hermes
- Alle spawner CLI-processer via `exec.Command`
- `Provider` felt på runtimes — allerede polymorfisk
- Daemon poller server for tasks, claimer, eksekverer, streamer output
- `ExecOptions` inkluderer Cwd, Model, Timeout, SystemPrompt, ResumeSessionID

## Scope

### Connector 1: Claude Remote (~1 uge)

Anthropic Remote Agents — Claude Code i skyen.

**Implementation:**
- Ny `claudeRemoteBackend` der implementerer `agent.Backend`
- Kalder Anthropic's remote agent API i stedet for at spawne lokal `claude` process
- Mapper `ExecOptions` til remote agent parametre
- Streamer `Message` events tilbage (samme typer som CLI-parseren allerede forstår)
- CLAUDE.md injiceres via API parameter i stedet for fil i workdir

**Trade-offs:**
- (+) Zero infrastruktur — Anthropic hoster sandbox, tools, git
- (+) Samme tool capabilities som lokal Claude Code
- (+) Kører 24/7 uden lokal maskine
- (-) Låst til Claude-modeller
- (-) Anthropic-afhængighed for execution

**Config:**
```
ANTHROPIC_API_KEY=sk-ant-...
```
Per agent: vælg "Claude Remote" som provider i UI.

### Connector 2: Pi Agents (~1-2 uger)

Model-agnostisk agent runtime via Pi Agents / Claude Agent SDK.

**Implementation:**
- Ny `piAgentBackend` der implementerer `agent.Backend`
- Spinner en Pi Agent-session op med valgt model (via OpenRouter, Claude API, OpenAI, etc.)
- Tool definitions: Bash, Read, Write, Edit, Grep, Glob — mappes til Pi Agents tool format
- Sandbox: enten E2B (cloud) eller lokal (Docker container)
- Streamer events tilbage via Pi Agents event stream

**Trade-offs:**
- (+) Model-agnostisk — brug Claude, GPT-4o, Gemini, Llama, Mistral
- (+) Kontrol over tool definitions og sandboxing
- (-) Mere kompleks — du ejer tool execution-laget
- (-) Sandbox-management (E2B koster, Docker kræver infra)

**Config:**
```
OPENROUTER_API_KEY=sk-or-...   # eller ANTHROPIC_API_KEY, OPENAI_API_KEY
PI_AGENT_SANDBOX=e2b           # eller "docker" eller "local"
```
Per agent: vælg model + provider i UI.

### Runtime Selection i UI

Eksisterende agent-config i UI udvides med:

```
Provider:  [Local Claude] [Local Codex] [Claude Remote] [Pi Agent]
Model:     [claude-sonnet-4-6] [gpt-4o] [gemini-2.5-pro] ...
```

Daemon registrerer lokale runtimes som i dag. Cloud runtimes registreres af serveren direkte.

## Architecture

```
                    ┌─────────────┐
                    │  Multica UI  │
                    └──────┬──────┘
                           │ assign issue
                    ┌──────▼──────┐
                    │   Server    │
                    └──────┬──────┘
                           │ task claimed
              ┌────────────┼────────────┐
              ▼            ▼            ▼
        ┌──────────┐ ┌──────────┐ ┌──────────┐
        │  Local   │ │  Claude  │ │ Pi Agent │
        │  Daemon  │ │  Remote  │ │ Runtime  │
        └────┬─────┘ └────┬─────┘ └────┬─────┘
             │            │            │
        spawn CLI    Anthropic     OpenRouter
                      cloud         + E2B
```

## Files to Create/Modify

- Ny: `server/pkg/agent/claude_remote.go` — Claude Remote backend
- Ny: `server/pkg/agent/piagent.go` — Pi Agent backend
- Modify: `server/pkg/agent/agent.go` — registrér nye backend typer
- Modify: `server/internal/daemon/daemon.go` — cloud runtime task routing
- Modify: DB migration — tilføj provider types
- Modify: Frontend agent config — provider/model dropdown

## Open Questions

- Skal cloud runtimes claimes af serveren direkte, eller af en cloud daemon?
- Hvordan håndteres repo checkout i cloud? (git clone i sandbox vs. pre-cached)
- Billing/usage tracking per cloud provider
