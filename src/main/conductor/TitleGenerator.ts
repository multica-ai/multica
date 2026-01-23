/**
 * TitleGenerator - Generates session titles using Agent CLI
 *
 * Uses the same Agent that the session is using, but in a separate CLI invocation
 * to avoid polluting the session's context.
 */
import { spawn } from 'node:child_process'
import { getEnhancedPath } from '../utils/path'

/** Timeout for CLI execution (60 seconds) */
const CLI_TIMEOUT_MS = 60000

/**
 * Command configuration for each supported Agent
 */
interface AgentCommand {
  command: string
  args: string[]
  parseOutput: (stdout: string) => string
}

/**
 * Build the title generation prompt
 */
function buildPrompt(userMessage: string): string {
  return `Generate a short title (3-6 words) for the user message below. Chinese: max 6 Chinese characters. Output ONLY the title, nothing else.

<user-message>
${userMessage}
</user-message>`
}

/**
 * Build CLI command for the specified agent
 */
function buildAgentCommand(agentId: string, prompt: string): AgentCommand | null {
  switch (agentId) {
    case 'claude-code':
      return {
        command: 'claude',
        args: [
          '-p',
          prompt,
          '--output-format',
          'text',
          '--tools',
          '',
          '--permission-mode',
          'dontAsk',
          '--no-session-persistence'
        ],
        parseOutput: (stdout: string) => stdout.trim()
      }

    case 'opencode':
      return {
        command: 'opencode',
        args: ['run', prompt, '-m', 'opencode/gpt-5-nano', '--agent', 'title', '--format', 'json'],
        parseOutput: (stdout: string) => {
          // Parse JSON event stream, find the last text event
          const lines = stdout.split('\n').filter(Boolean)
          for (let i = lines.length - 1; i >= 0; i--) {
            try {
              const event = JSON.parse(lines[i])
              if (event.type === 'text' && event.part?.text) {
                return event.part.text.trim()
              }
            } catch {
              // Skip invalid JSON lines
            }
          }
          return ''
        }
      }

    case 'codex':
      return {
        command: 'codex',
        args: ['exec', prompt, '--sandbox', 'read-only', '--json'],
        parseOutput: (stdout: string) => {
          // Parse JSON event stream, find the last agent_message
          const lines = stdout.split('\n').filter(Boolean)
          for (let i = lines.length - 1; i >= 0; i--) {
            try {
              const event = JSON.parse(lines[i])
              if (event.type === 'item.completed' && event.item?.type === 'agent_message') {
                return event.item.text.trim()
              }
            } catch {
              // Skip invalid JSON lines
            }
          }
          return ''
        }
      }

    default:
      return null
  }
}

interface CommandResult {
  stdout: string
  stderr: string
}

function runCommand(command: string, args: string[]): Promise<CommandResult> {
  return new Promise((resolve, reject) => {
    let settled = false
    let stdout = ''
    let stderr = ''

    const child = spawn(command, args, {
      env: { ...process.env, PATH: getEnhancedPath() },
      stdio: ['ignore', 'pipe', 'pipe']
    })

    const timeoutId = setTimeout(() => {
      if (settled) {
        return
      }
      settled = true
      child.kill('SIGTERM')
      const error = new Error('Command timed out') as NodeJS.ErrnoException & { stderr?: string }
      error.code = 'ETIMEDOUT'
      error.stderr = stderr
      reject(error)
    }, CLI_TIMEOUT_MS)

    child.stdout?.on('data', (chunk) => {
      stdout += chunk.toString()
    })

    child.stderr?.on('data', (chunk) => {
      stderr += chunk.toString()
    })

    child.on('error', (error) => {
      if (settled) {
        return
      }
      settled = true
      clearTimeout(timeoutId)
      ;(error as NodeJS.ErrnoException & { stderr?: string }).stderr = stderr
      reject(error)
    })

    child.on('close', (code, signal) => {
      if (settled) {
        return
      }
      settled = true
      clearTimeout(timeoutId)
      if (code === 0 && !signal) {
        resolve({ stdout, stderr })
        return
      }
      const error = new Error('Command failed') as NodeJS.ErrnoException & {
        stderr?: string
        signal?: NodeJS.Signals | null
      }
      error.code = code === null ? undefined : String(code)
      error.signal = signal
      error.stderr = stderr
      reject(error)
    })
  })
}

/**
 * Generate a session title using the Agent CLI
 *
 * @param agentId - The agent ID (e.g., 'claude-code', 'opencode', 'codex')
 * @param userMessage - The first user message in the session
 * @returns Generated title, or null if generation fails (title should not be changed)
 */
export async function generateSessionTitle(
  agentId: string,
  userMessage: string
): Promise<string | null> {
  const prompt = buildPrompt(userMessage)
  const agentCommand = buildAgentCommand(agentId, prompt)

  if (!agentCommand) {
    console.log(`[TitleGenerator] Unknown agent: ${agentId}, skipping title generation`)
    return null
  }

  const { command, args, parseOutput } = agentCommand

  console.log(`[TitleGenerator] Generating title for session using ${agentId}`)
  console.log(`[TitleGenerator] Command: ${command} ${args[0]} ...`)

  try {
    const { stdout } = await runCommand(command, args)

    const title = parseOutput(stdout)

    if (title) {
      console.log(`[TitleGenerator] Generated title: "${title}"`)
      return title
    }

    console.log(`[TitleGenerator] Empty output, skipping title update`)
    return null
  } catch (error) {
    if (error && typeof error === 'object') {
      const stderr = 'stderr' in error ? (error.stderr as string | undefined) : undefined
      if (stderr) {
        console.error(`[TitleGenerator] CLI stderr: ${stderr}`)
      }
    }
    console.error(`[TitleGenerator] CLI execution failed:`, error)
    return null
  }
}
