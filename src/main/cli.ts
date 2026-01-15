#!/usr/bin/env npx tsx
/**
 * Multica CLI - Test and interact with Conductor
 *
 * Usage:
 *   pnpm cli                           # Interactive mode
 *   pnpm cli prompt "message"          # One-shot prompt
 *   pnpm cli sessions                  # List sessions
 *   pnpm cli resume <id>               # Resume and enter interactive mode
 *
 * Interactive commands:
 *   /help          Show help
 *   /sessions      List sessions
 *   /new [cwd]     Create new session
 *   /resume <id>   Resume session
 *   /delete <id>   Delete session
 *   /history       Show current session history
 *   /agent <name>  Switch agent
 *   /agents        List available agents
 *   /status        Show current status
 *   /cancel        Cancel current request
 *   /quit          Exit
 */
import * as readline from 'node:readline'
import { execSync } from 'node:child_process'
import { resolve, join, basename } from 'node:path'
import { existsSync, writeFileSync, mkdirSync } from 'node:fs'
import { homedir, platform } from 'node:os'
import { Conductor } from './conductor'
import { DEFAULT_AGENTS } from './config'
import type { MulticaSession } from '../shared/types'

// ANSI colors
const c = {
  reset: '\x1b[0m',
  bold: '\x1b[1m',
  dim: '\x1b[2m',
  red: '\x1b[31m',
  green: '\x1b[32m',
  yellow: '\x1b[33m',
  blue: '\x1b[34m',
  magenta: '\x1b[35m',
  cyan: '\x1b[36m',
}

interface CLIState {
  conductor: Conductor
  currentSession: MulticaSession | null
  defaultAgentId: string // Agent to use for new sessions
  isProcessing: boolean
  isCancelling: boolean
  logFile: string | null
  sessionLog: Array<{ timestamp: string; type: string; data: unknown }>
}

function print(msg: string) {
  console.log(msg)
}

function printError(msg: string) {
  console.log(`${c.red}Error: ${msg}${c.reset}`)
}

function printSuccess(msg: string) {
  console.log(`${c.green}${msg}${c.reset}`)
}

function printInfo(msg: string) {
  console.log(`${c.cyan}${msg}${c.reset}`)
}

function printDim(msg: string) {
  console.log(`${c.dim}${msg}${c.reset}`)
}

function formatDate(iso: string): string {
  const d = new Date(iso)
  return d.toLocaleString()
}

function truncate(str: string, len: number): string {
  if (str.length <= len) return str
  return str.slice(0, len - 3) + '...'
}

// ============ Commands ============

async function cmdHelp() {
  print(`
${c.bold}Multica CLI${c.reset} - Test Conductor functionality

${c.bold}Commands:${c.reset}
  ${c.cyan}/help${c.reset}              Show this help
  ${c.cyan}/sessions${c.reset}          List all sessions
  ${c.cyan}/new [cwd]${c.reset}         Create new session in directory (default: current)
  ${c.cyan}/resume <id>${c.reset}       Resume an existing session
  ${c.cyan}/delete <id>${c.reset}       Delete a session
  ${c.cyan}/history${c.reset}           Show current session message history
  ${c.cyan}/default <name>${c.reset}    Set default agent for new sessions
  ${c.cyan}/agents${c.reset}            List available agents
  ${c.cyan}/doctor${c.reset}            Check agent installations
  ${c.cyan}/status${c.reset}            Show current status
  ${c.cyan}/cancel${c.reset}            Cancel current request
  ${c.cyan}/quit${c.reset}              Exit CLI

${c.bold}Usage:${c.reset}
  Type any text to send as a prompt to the current session.
  Each session runs its own agent process.
  Press Ctrl+C to cancel a running request.
  Press Ctrl+C twice to force quit.
`)
}

async function cmdSessions(state: CLIState) {
  const sessions = await state.conductor.listSessions()

  if (sessions.length === 0) {
    printInfo('No sessions found.')
    return
  }

  print(`\n${c.bold}Sessions:${c.reset}`)
  print(`${'─'.repeat(80)}`)

  for (const s of sessions) {
    const isCurrent = state.currentSession?.id === s.id
    const marker = isCurrent ? `${c.green}→${c.reset}` : ' '
    const status =
      s.status === 'active'
        ? `${c.green}●${c.reset}`
        : s.status === 'error'
          ? `${c.red}●${c.reset}`
          : `${c.dim}●${c.reset}`

    const title = s.title || basename(s.workingDirectory)
    const agent = DEFAULT_AGENTS[s.agentId]?.name || s.agentId
    const shortId = s.id.slice(0, 8)

    print(
      `${marker} ${status} ${c.bold}${truncate(title, 30)}${c.reset}  ${c.dim}[${shortId}]${c.reset}`
    )
    print(`    ${c.dim}Agent: ${agent} | Dir: ${truncate(s.workingDirectory, 40)}${c.reset}`)
    print(`    ${c.dim}Updated: ${formatDate(s.updatedAt)} | Messages: ${s.messageCount}${c.reset}`)
  }
  print(`${'─'.repeat(80)}`)
}

async function cmdNewSession(state: CLIState, cwd?: string) {
  const targetCwd = cwd ? resolve(cwd) : process.cwd()

  if (!existsSync(targetCwd)) {
    printError(`Directory does not exist: ${targetCwd}`)
    return
  }

  const config = DEFAULT_AGENTS[state.defaultAgentId]
  if (!config) {
    printError(`Unknown agent: ${state.defaultAgentId}`)
    return
  }

  printInfo(`Creating session with ${config.name} in ${targetCwd}...`)
  const session = await state.conductor.createSession(targetCwd, config)
  state.currentSession = session

  printSuccess(`Session created: ${session.id.slice(0, 8)}`)
  printInfo(`Agent: ${config.name}`)
  printInfo(`Working directory: ${session.workingDirectory}`)
}

async function cmdResumeSession(state: CLIState, sessionId: string) {
  if (!sessionId) {
    printError('Usage: /resume <session-id>')
    return
  }

  // Find session by partial ID
  const sessions = await state.conductor.listSessions()
  const match = sessions.find((s) => s.id.startsWith(sessionId))

  if (!match) {
    printError(`Session not found: ${sessionId}`)
    return
  }

  const agentConfig = DEFAULT_AGENTS[match.agentId]
  if (!agentConfig) {
    printError(`Unknown agent: ${match.agentId}`)
    return
  }

  printInfo(`Resuming session ${match.id.slice(0, 8)} with ${agentConfig.name}...`)
  const session = await state.conductor.resumeSession(match.id)
  state.currentSession = session

  printSuccess(`Session resumed: ${session.id.slice(0, 8)}`)
  printInfo(`Agent: ${agentConfig.name}`)
  printInfo(`Working directory: ${session.workingDirectory}`)
  printDim('Note: Agent state is not restored. Previous messages are stored for display.')
}

async function cmdDeleteSession(state: CLIState, sessionId: string) {
  if (!sessionId) {
    printError('Usage: /delete <session-id>')
    return
  }

  const sessions = await state.conductor.listSessions()
  const match = sessions.find((s) => s.id.startsWith(sessionId))

  if (!match) {
    printError(`Session not found: ${sessionId}`)
    return
  }

  await state.conductor.deleteSession(match.id)

  if (state.currentSession?.id === match.id) {
    state.currentSession = null
  }

  printSuccess(`Session deleted: ${match.id.slice(0, 8)}`)
}

async function cmdHistory(state: CLIState) {
  if (!state.currentSession) {
    printError('No active session. Use /new or /resume first.')
    return
  }

  const data = await state.conductor.getSessionData(state.currentSession.id)
  if (!data || data.updates.length === 0) {
    printInfo('No messages in this session.')
    return
  }

  print(`\n${c.bold}Session History:${c.reset}`)
  print(`${'─'.repeat(80)}`)

  for (const stored of data.updates) {
    const update = stored.update.update
    if (!update || !('sessionUpdate' in update)) continue

    switch (update.sessionUpdate) {
      case 'agent_message_chunk':
        if (update.content.type === 'text') {
          process.stdout.write(update.content.text)
        }
        break
      case 'tool_call':
        print(`\n${c.yellow}🔧 ${update.title}${c.reset}`)
        break
      case 'tool_call_update':
        if (update.status === 'completed') {
          print(`${c.dim}   ✓ completed${c.reset}`)
        }
        break
    }
  }
  print(`\n${'─'.repeat(80)}`)
}

async function cmdAgents() {
  print(`\n${c.bold}Available Agents:${c.reset}`)
  for (const [id, config] of Object.entries(DEFAULT_AGENTS)) {
    const status = config.enabled ? `${c.green}enabled${c.reset}` : `${c.dim}disabled${c.reset}`
    print(`  ${c.cyan}${id}${c.reset} - ${config.name} (${status})`)
    print(`    ${c.dim}Command: ${config.command} ${config.args.join(' ')}${c.reset}`)
  }
}

/**
 * Check if a command exists in the system PATH
 */
function commandExists(cmd: string): { exists: boolean; path?: string; version?: string } {
  const isWindows = platform() === 'win32'
  const whichCmd = isWindows ? 'where' : 'which'

  try {
    const path = execSync(`${whichCmd} ${cmd}`, {
      encoding: 'utf-8',
      stdio: ['pipe', 'pipe', 'pipe'],
    }).trim().split('\n')[0]

    // Try to get version
    let version: string | undefined
    try {
      const versionOutput = execSync(`${cmd} --version`, {
        encoding: 'utf-8',
        stdio: ['pipe', 'pipe', 'pipe'],
        timeout: 5000,
      }).trim()
      // Extract first line or first meaningful part
      version = versionOutput.split('\n')[0].slice(0, 50)
    } catch {
      // Some commands don't support --version
    }

    return { exists: true, path, version }
  } catch {
    return { exists: false }
  }
}

interface AgentCheckResult {
  id: string
  name: string
  command: string
  installed: boolean
  path?: string
  version?: string
  installHint?: string
}

/**
 * Check all agents installation status
 */
async function cmdDoctor(): Promise<AgentCheckResult[]> {
  print(`\n${c.bold}Multica Doctor${c.reset} - Checking agent installations\n`)
  print(`${'─'.repeat(60)}`)

  const results: AgentCheckResult[] = []

  // Install hints for each agent
  const installHints: Record<string, string> = {
    opencode: 'go install github.com/anomalyco/opencode@latest',
    codex: 'npm install -g codex-acp',
    gemini: 'npm install -g @anthropic-ai/gemini-cli',
  }

  for (const [id, config] of Object.entries(DEFAULT_AGENTS)) {
    const check = commandExists(config.command)

    const result: AgentCheckResult = {
      id,
      name: config.name,
      command: config.command,
      installed: check.exists,
      path: check.path,
      version: check.version,
      installHint: installHints[id],
    }
    results.push(result)

    // Display result
    const statusIcon = check.exists ? `${c.green}✓${c.reset}` : `${c.red}✗${c.reset}`
    const statusText = check.exists
      ? `${c.green}installed${c.reset}`
      : `${c.red}not found${c.reset}`

    print(`${statusIcon} ${c.bold}${config.name}${c.reset} (${config.command})`)
    print(`  Status: ${statusText}`)

    if (check.exists) {
      if (check.path) {
        print(`  ${c.dim}Path: ${check.path}${c.reset}`)
      }
      if (check.version) {
        print(`  ${c.dim}Version: ${check.version}${c.reset}`)
      }
    } else if (installHints[id]) {
      print(`  ${c.dim}Install: ${installHints[id]}${c.reset}`)
    }
    print('')
  }

  print(`${'─'.repeat(60)}`)

  // Summary
  const installed = results.filter((r) => r.installed).length
  const total = results.length

  if (installed === total) {
    printSuccess(`All ${total} agents are installed!`)
  } else if (installed > 0) {
    printInfo(`${installed}/${total} agents installed`)
  } else {
    printError(`No agents installed. Install at least one to use Multica.`)
  }

  return results
}

async function cmdSetDefaultAgent(state: CLIState, agentId: string) {
  if (!agentId) {
    printError('Usage: /default <name>')
    print(`Current default: ${state.defaultAgentId}`)
    return
  }

  const config = DEFAULT_AGENTS[agentId]
  if (!config) {
    printError(`Unknown agent: ${agentId}`)
    print(`Available: ${Object.keys(DEFAULT_AGENTS).join(', ')}`)
    return
  }

  state.defaultAgentId = agentId
  printSuccess(`Default agent set to ${config.name}`)
  printInfo('New sessions will use this agent. Use /new to create a session.')
}

async function cmdStatus(state: CLIState) {
  print(`\n${c.bold}Status:${c.reset}`)

  // Default agent
  const defaultConfig = DEFAULT_AGENTS[state.defaultAgentId]
  print(`  Default Agent: ${c.cyan}${defaultConfig?.name || state.defaultAgentId}${c.reset}`)

  // Running sessions
  const runningIds = state.conductor.getRunningSessionIds()
  print(`  Running Sessions: ${c.green}${runningIds.length}${c.reset}`)

  // Current session
  if (state.currentSession) {
    const isRunning = state.conductor.isSessionRunning(state.currentSession.id)
    const agentConfig = state.conductor.getSessionAgent(state.currentSession.id)
    print(`  Current Session: ${c.green}${state.currentSession.id.slice(0, 8)}${c.reset}`)
    print(`    Agent: ${agentConfig?.name || 'unknown'} (${isRunning ? `${c.green}running${c.reset}` : `${c.red}stopped${c.reset}`})`)
    print(`    Directory: ${state.currentSession.workingDirectory}`)
    print(`    Status: ${state.currentSession.status}`)
  } else {
    print(`  Current Session: ${c.dim}None${c.reset}`)
  }

  // Processing
  if (state.isProcessing) {
    print(`  ${c.yellow}Processing request...${c.reset}`)
  }
}

async function cmdCancel(state: CLIState) {
  if (!state.isProcessing) {
    printInfo('No request in progress.')
    return
  }

  if (!state.currentSession) {
    printInfo('No active session.')
    return
  }

  printInfo('Sending cancel request...')
  await state.conductor.cancelRequest(state.currentSession.id)
  state.isCancelling = true
}

// ============ Prompt Handling ============

async function sendPrompt(state: CLIState, content: string): Promise<void> {
  if (!state.currentSession) {
    printError('No active session. Use /new or /resume first.')
    return
  }

  if (state.isProcessing) {
    printError('Already processing a request. Use /cancel to abort.')
    return
  }

  state.isProcessing = true
  state.isCancelling = false

  print(`\n${c.blue}🤖 Agent:${c.reset}`)

  try {
    // Convert string to MessageContent format
    const messageContent = [{ type: 'text' as const, text: content }]
    const stopReason = await state.conductor.sendPrompt(state.currentSession.id, messageContent)
    print(`\n\n${c.dim}[${stopReason}]${c.reset}\n`)
  } catch (err) {
    printError(`${err}`)
  } finally {
    state.isProcessing = false
  }
}

// ============ Main ============

async function runInteractiveMode(state: CLIState) {
  const rl = readline.createInterface({
    input: process.stdin,
    output: process.stdout,
  })

  // Handle Ctrl+C
  let ctrlCCount = 0
  rl.on('SIGINT', async () => {
    ctrlCCount++
    if (ctrlCCount >= 2) {
      print('\n\nForce quit.')
      await cleanup(state)
      process.exit(0)
    }

    if (state.isProcessing && state.currentSession) {
      print('\n')
      await cmdCancel(state)
      setTimeout(() => {
        ctrlCCount = 0
      }, 2000)
    } else {
      print('\nPress Ctrl+C again to quit.')
      setTimeout(() => {
        ctrlCCount = 0
      }, 2000)
    }
  })

  print(`\n${c.bold}Multica CLI${c.reset} - Type /help for commands\n`)

  // Show status
  await cmdStatus(state)
  print('')

  const prompt = () => {
    const sessionMarker = state.currentSession
      ? `${c.green}[${state.currentSession.id.slice(0, 8)}]${c.reset}`
      : `${c.dim}[no session]${c.reset}`
    const agentMarker = state.defaultAgentId

    rl.question(`${sessionMarker} ${c.dim}${agentMarker}${c.reset} > `, async (input) => {
      const trimmed = input.trim()

      if (!trimmed) {
        prompt()
        return
      }

      try {
        if (trimmed.startsWith('/')) {
          const [cmd, ...args] = trimmed.slice(1).split(/\s+/)
          const arg = args.join(' ')

          switch (cmd.toLowerCase()) {
            case 'help':
            case 'h':
            case '?':
              await cmdHelp()
              break
            case 'sessions':
            case 'ls':
              await cmdSessions(state)
              break
            case 'new':
            case 'n':
              await cmdNewSession(state, arg || undefined)
              break
            case 'resume':
            case 'r':
              await cmdResumeSession(state, arg)
              break
            case 'delete':
            case 'rm':
              await cmdDeleteSession(state, arg)
              break
            case 'history':
            case 'hist':
              await cmdHistory(state)
              break
            case 'default':
            case 'd':
              await cmdSetDefaultAgent(state, arg)
              break
            case 'agents':
              await cmdAgents()
              break
            case 'doctor':
            case 'check':
              await cmdDoctor()
              break
            case 'status':
            case 's':
              await cmdStatus(state)
              break
            case 'cancel':
            case 'c':
              await cmdCancel(state)
              break
            case 'quit':
            case 'exit':
            case 'q':
              print('Goodbye!')
              await cleanup(state)
              process.exit(0)
            default:
              printError(`Unknown command: /${cmd}`)
              print('Type /help for available commands.')
          }
        } else {
          await sendPrompt(state, trimmed)
        }
      } catch (err) {
        printError(`${err}`)
      }

      prompt()
    })
  }

  prompt()
}

async function runOneShotPrompt(state: CLIState, prompt: string, options: { cwd?: string }) {
  const cwd = options.cwd || process.cwd()

  const config = DEFAULT_AGENTS[state.defaultAgentId]
  if (!config) {
    printError(`Unknown agent: ${state.defaultAgentId}`)
    process.exit(1)
  }

  // Create session (agent starts automatically)
  printInfo(`Creating session with ${config.name} in ${cwd}...`)
  const session = await state.conductor.createSession(cwd, config)
  state.currentSession = session

  // Send prompt
  print(`\n${c.blue}User:${c.reset} ${prompt}`)
  print(`\n${c.blue}Agent:${c.reset}`)

  await sendPrompt(state, prompt)

  await cleanup(state)
  process.exit(0)
}

async function cleanup(state: CLIState) {
  if (state.logFile && state.sessionLog.length > 0) {
    writeFileSync(state.logFile, JSON.stringify(state.sessionLog, null, 2))
    printInfo(`Session log saved to: ${state.logFile}`)
  }
  await state.conductor.stopAllSessions()
}

async function main() {
  const args = process.argv.slice(2)

  // Parse global options
  let logFile: string | null = null
  let cwd = process.cwd()
  const filteredArgs: string[] = []

  for (const arg of args) {
    if (arg.startsWith('--log=')) {
      logFile = resolve(arg.slice(6))
    } else if (arg === '--log') {
      const logsDir = join(process.cwd(), 'logs')
      if (!existsSync(logsDir)) {
        mkdirSync(logsDir, { recursive: true })
      }
      logFile = join(logsDir, `cli-session-${Date.now()}.json`)
    } else if (arg.startsWith('--cwd=')) {
      cwd = resolve(arg.slice(6))
    } else {
      filteredArgs.push(arg)
    }
  }

  // Storage path for sessions
  const storagePath = join(homedir(), '.multica', 'sessions')

  const toolCalls = new Map<string, string>()

  const conductor = new Conductor({
    storagePath,
    events: {
      onSessionUpdate: (params) => {
        const update = params.update
        switch (update.sessionUpdate) {
          case 'agent_message_chunk':
            if (update.content.type === 'text') {
              process.stdout.write(update.content.text)
            } else {
              print(`[${update.content.type}]`)
            }
            break

          case 'tool_call': {
            toolCalls.set(update.toolCallId, update.title)
            print(`\n${c.yellow}┌─ 🔧 ${update.title}${c.reset} ${c.dim}[${update.status}]${c.reset}`)
            if (update.kind) {
              print(`${c.dim}│  Kind: ${update.kind}${c.reset}`)
            }
            if (update.rawInput) {
              const input =
                typeof update.rawInput === 'string'
                  ? update.rawInput
                  : JSON.stringify(update.rawInput, null, 2)
              const lines = input.split('\n')
              lines.slice(0, 10).forEach((line) => print(`${c.dim}│  ${line}${c.reset}`))
              if (lines.length > 10) print(`${c.dim}│  ... (${lines.length - 10} more lines)${c.reset}`)
            }
            break
          }

          case 'tool_call_update': {
            const title = toolCalls.get(update.toolCallId) || update.toolCallId
            if (update.status === 'completed') {
              print(`${c.dim}└─ ✓ ${title} completed${c.reset}`)
            } else if (update.status) {
              print(`${c.dim}├─ ${title} [${update.status}]${c.reset}`)
            }
            break
          }

          case 'agent_thought_chunk':
            if (update.content.type === 'text') {
              print(`${c.magenta}💭 ${update.content.text}${c.reset}`)
            }
            break

          case 'plan':
            print(`\n${c.cyan}📋 Plan: ${'title' in update ? update.title : 'Thinking...'}${c.reset}`)
            break
        }
      },
    },
  })

  // Initialize conductor (loads session index)
  await conductor.initialize()

  const state: CLIState = {
    conductor,
    currentSession: null,
    defaultAgentId: 'opencode',
    isProcessing: false,
    isCancelling: false,
    logFile,
    sessionLog: [],
  }

  // Handle subcommands
  const subcommand = filteredArgs[0]

  switch (subcommand) {
    case 'prompt':
    case 'p': {
      const prompt = filteredArgs.slice(1).join(' ')
      if (!prompt) {
        printError('Usage: pnpm cli prompt "your message"')
        process.exit(1)
      }
      await runOneShotPrompt(state, prompt, { cwd })
      break
    }

    case 'sessions':
    case 'ls': {
      await cmdSessions(state)
      process.exit(0)
    }

    case 'resume':
    case 'r': {
      const sessionId = filteredArgs[1]
      if (!sessionId) {
        printError('Usage: pnpm cli resume <session-id>')
        process.exit(1)
      }
      await cmdResumeSession(state, sessionId)
      await runInteractiveMode(state)
      break
    }

    case 'agents': {
      await cmdAgents()
      process.exit(0)
    }

    case 'doctor':
    case 'check': {
      await cmdDoctor()
      process.exit(0)
    }

    case 'help':
    case '--help':
    case '-h': {
      print(`
${c.bold}Multica CLI${c.reset}

${c.bold}Usage:${c.reset}
  pnpm cli                          Interactive mode
  pnpm cli prompt "message"         One-shot prompt
  pnpm cli sessions                 List sessions
  pnpm cli resume <id>              Resume session
  pnpm cli agents                   List available agents
  pnpm cli doctor                   Check agent installations

${c.bold}Options:${c.reset}
  --cwd=PATH    Working directory
  --log         Save session log
  --log=PATH    Save session log to specific file

${c.bold}Examples:${c.reset}
  pnpm cli                                  # Start interactive mode
  pnpm cli prompt "What is 2+2?"            # Quick prompt
  pnpm cli prompt "List files" --cwd=/tmp   # Prompt with cwd
  pnpm cli sessions                         # List all sessions
  pnpm cli resume abc123                    # Resume session by ID prefix
`)
      process.exit(0)
    }

    default: {
      // Interactive mode
      await runInteractiveMode(state)
    }
  }
}

main().catch((err) => {
  printError(`${err}`)
  process.exit(1)
})
