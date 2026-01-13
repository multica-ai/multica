#!/usr/bin/env npx tsx
/**
 * CLI test script for ACP communication
 * Usage: pnpm test:acp "Your prompt here" [--agent=opencode] [--cwd=/path/to/dir] [--log=path]
 */
import { resolve, join } from 'node:path'
import { existsSync, writeFileSync, mkdirSync } from 'node:fs'
import { Conductor } from './conductor'
import { DEFAULT_AGENTS } from './config'

interface ParsedArgs {
  prompt: string
  agent: string
  cwd: string
  logFile: string | null
}

function parseArgs(args: string[]): ParsedArgs {
  let prompt = ''
  let agent = 'opencode'
  let cwd = process.cwd()
  let logFile: string | null = null

  for (const arg of args) {
    if (arg.startsWith('--agent=')) {
      agent = arg.slice(8)
    } else if (arg.startsWith('--cwd=')) {
      cwd = resolve(arg.slice(6))
    } else if (arg.startsWith('--log=')) {
      logFile = resolve(arg.slice(6))
    } else if (arg === '--log') {
      // Default log file location
      const logsDir = join(process.cwd(), 'logs')
      if (!existsSync(logsDir)) {
        mkdirSync(logsDir, { recursive: true })
      }
      logFile = join(logsDir, `acp-session-${Date.now()}.json`)
    } else if (!arg.startsWith('-')) {
      prompt = arg
    }
  }

  return { prompt, agent, cwd, logFile }
}

async function main() {
  const { prompt, agent: agentId, cwd, logFile } = parseArgs(process.argv.slice(2))

  if (!prompt) {
    console.log('Usage: pnpm test:acp "Your prompt here" [options]')
    console.log('')
    console.log('Options:')
    console.log('  --agent=NAME   Agent to use (default: opencode)')
    console.log('  --cwd=PATH     Working directory for the agent')
    console.log('  --log          Save session log to logs/ directory')
    console.log('  --log=PATH     Save session log to specified file')
    console.log('')
    console.log('Examples:')
    console.log('  pnpm test:acp "What is 2+2?"')
    console.log('  pnpm test:acp "List files" --cwd=/tmp')
    console.log('  pnpm test:acp "Hello" --agent=codex --log')
    process.exit(1)
  }

  const agentConfig = DEFAULT_AGENTS[agentId]

  if (!agentConfig) {
    console.error(`Unknown agent: ${agentId}`)
    console.log('Available agents:', Object.keys(DEFAULT_AGENTS).join(', '))
    process.exit(1)
  }

  if (!existsSync(cwd)) {
    console.error(`Directory does not exist: ${cwd}`)
    process.exit(1)
  }

  console.log(`\n🚀 Starting ${agentConfig.name}...`)
  if (logFile) {
    console.log(`📄 Logging to: ${logFile}`)
  }

  // Track tool calls for displaying updates
  const toolCalls = new Map<string, string>()

  // Session log for debugging
  interface SessionLogEntry {
    timestamp: string
    type: 'session_update' | 'error' | 'info'
    data: unknown
  }
  const sessionLog: SessionLogEntry[] = []

  function log(type: SessionLogEntry['type'], data: unknown) {
    sessionLog.push({
      timestamp: new Date().toISOString(),
      type,
      data,
    })
  }

  const conductor = new Conductor({
    onSessionUpdate: (params) => {
      // Log the raw update for debugging
      log('session_update', params)

      const update = params.update
      switch (update.sessionUpdate) {
        case 'agent_message_chunk':
          if (update.content.type === 'text') {
            process.stdout.write(update.content.text)
          } else {
            console.log(`[${update.content.type}]`)
          }
          break

        case 'tool_call': {
          toolCalls.set(update.toolCallId, update.title)
          console.log(`\n┌─ 🔧 ${update.title} [${update.status}]`)
          if (update.kind) {
            console.log(`│  Kind: ${update.kind}`)
          }
          if (update.rawInput) {
            const input = typeof update.rawInput === 'string'
              ? update.rawInput
              : JSON.stringify(update.rawInput, null, 2)
            const lines = input.split('\n')
            lines.forEach((line, i) => {
              if (i < 10) console.log(`│  ${line}`)
              else if (i === 10) console.log(`│  ... (${lines.length - 10} more lines)`)
            })
          }
          break
        }

        case 'tool_call_update': {
          const title = toolCalls.get(update.toolCallId) || update.toolCallId
          const status = update.status || 'updating'

          // Show status change
          if (update.status) {
            console.log(`├─ 🔧 ${title} [${status}]`)
          }

          // Show output content
          if (update.content && Array.isArray(update.content)) {
            for (const content of update.content) {
              if (content.type === 'content') {
                // content.content can be an array of content blocks
                const blocks = content.content
                if (Array.isArray(blocks)) {
                  for (const c of blocks) {
                    if (c.type === 'text' && c.text) {
                      const lines = c.text.split('\n')
                      lines.slice(0, 15).forEach(line => console.log(`│  ${line}`))
                      if (lines.length > 15) {
                        console.log(`│  ... (${lines.length - 15} more lines)`)
                      }
                    }
                  }
                }
              } else if (content.type === 'terminal') {
                console.log(`│  [Terminal: ${content.terminalId}]`)
              } else if (content.type === 'diff') {
                console.log(`│  [Diff: ${content.path}]`)
              }
            }
          }

          // Show raw output if available
          if (update.rawOutput) {
            const output = typeof update.rawOutput === 'string'
              ? update.rawOutput
              : JSON.stringify(update.rawOutput, null, 2)
            const lines = output.split('\n')
            lines.slice(0, 15).forEach(line => console.log(`│  ${line}`))
            if (lines.length > 15) {
              console.log(`│  ... (${lines.length - 15} more lines)`)
            }
          }

          if (status === 'completed') {
            console.log(`└─ ✓ ${title} completed`)
          }
          break
        }

        case 'agent_thought_chunk':
          if (update.content.type === 'text') {
            process.stdout.write(`💭 ${update.content.text}`)
          }
          break

        case 'plan':
          console.log(`\n📋 Plan: ${'title' in update ? update.title : 'Thinking...'}`)
          break

        default:
          // Ignore other update types
          break
      }
    },
  })

  try {
    // Start the agent
    await conductor.startAgent(agentConfig)

    // Create a session with specified working directory
    console.log(`📁 Working directory: ${cwd}`)
    const session = await conductor.createSession(cwd)
    console.log(`📝 Session: ${session.id}`)

    // Send the prompt
    console.log(`\n💬 User: ${prompt}\n`)
    console.log('🤖 Agent:')

    const stopReason = await conductor.sendPrompt(session.id, prompt)

    console.log(`\n\n✅ Completed (${stopReason})`)
  } catch (error) {
    console.error('\n❌ Error:', error)
    process.exit(1)
  } finally {
    // Save session log if requested
    if (logFile && sessionLog.length > 0) {
      writeFileSync(logFile, JSON.stringify(sessionLog, null, 2))
      console.log(`\n📄 Session log saved to: ${logFile}`)
    }
    await conductor.stopAgent()
    process.exit(0)
  }
}

main().catch(console.error)
