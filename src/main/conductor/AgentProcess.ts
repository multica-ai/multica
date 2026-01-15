/**
 * AgentProcess - Manages a single ACP agent subprocess
 */
import { spawn, ChildProcess } from 'node:child_process'
import { Writable, Readable } from 'node:stream'
import type { AgentConfig } from '../../shared/types'
import { getEnhancedPath } from '../utils/path'

export class AgentProcess {
  private process: ChildProcess | null = null
  private config: AgentConfig
  private exitCallbacks: Array<(code: number | null, signal: string | null) => void> = []

  constructor(config: AgentConfig) {
    this.config = config
  }

  /**
   * Start the agent subprocess
   */
  async start(): Promise<void> {
    const startTime = Date.now()

    if (this.process) {
      throw new Error('Agent process already running')
    }

    const { command, args, env } = this.config

    const t1 = Date.now()
    this.process = spawn(command, args, {
      stdio: ['pipe', 'pipe', 'inherit'], // stdin, stdout piped; stderr inherited
      env: { ...process.env, ...env, PATH: getEnhancedPath() },
    })
    console.log(`[AgentProcess] [TIMING] spawn() took ${Date.now() - t1}ms`)

    this.process.on('exit', (code, signal) => {
      this.exitCallbacks.forEach((cb) => cb(code, signal))
      this.process = null
    })

    this.process.on('error', (err) => {
      console.error(`[AgentProcess] Failed to start ${command}:`, err.message)
      this.process = null
    })

    // Wait a bit to ensure process started successfully
    const t2 = Date.now()
    await new Promise<void>((resolve, reject) => {
      const timeout = setTimeout(() => {
        if (this.process) {
          resolve()
        } else {
          reject(new Error(`Failed to start ${command}`))
        }
      }, 100)

      this.process!.on('error', (err) => {
        clearTimeout(timeout)
        reject(err)
      })
    })
    console.log(`[AgentProcess] [TIMING] startup wait took ${Date.now() - t2}ms`)

    console.log(`[AgentProcess] [TIMING] Total start() took ${Date.now() - startTime}ms`)
    console.log(`[AgentProcess] Started ${command} ${args.join(' ')} (pid: ${this.process.pid})`)
  }

  /**
   * Stop the agent subprocess
   */
  async stop(): Promise<void> {
    if (!this.process) {
      return
    }

    const pid = this.process.pid
    console.log(`[AgentProcess] Stopping process (pid: ${pid})`)

    return new Promise((resolve) => {
      const timeout = setTimeout(() => {
        // Force kill if graceful shutdown takes too long
        console.log(`[AgentProcess] Force killing process (pid: ${pid})`)
        this.process?.kill('SIGKILL')
      }, 5000)

      this.process!.on('exit', (code, signal) => {
        clearTimeout(timeout)
        console.log(`[AgentProcess] Process exited (pid: ${pid}, code: ${code}, signal: ${signal})`)
        resolve()
      })

      this.process!.kill('SIGTERM')
    })
  }

  /**
   * Get stdin as a Web WritableStream for ACP SDK
   */
  getStdinWeb(): WritableStream<Uint8Array> {
    if (!this.process?.stdin) {
      throw new Error('Agent process not running or stdin not available')
    }
    return Writable.toWeb(this.process.stdin) as WritableStream<Uint8Array>
  }

  /**
   * Get stdout as a Web ReadableStream for ACP SDK
   */
  getStdoutWeb(): ReadableStream<Uint8Array> {
    if (!this.process?.stdout) {
      throw new Error('Agent process not running or stdout not available')
    }
    return Readable.toWeb(this.process.stdout) as ReadableStream<Uint8Array>
  }

  /**
   * Check if the process is running
   */
  isRunning(): boolean {
    return this.process !== null
  }

  /**
   * Register a callback for when the process exits
   */
  onExit(callback: (code: number | null, signal: string | null) => void): void {
    this.exitCallbacks.push(callback)
  }

  /**
   * Get the process ID
   */
  getPid(): number | undefined {
    return this.process?.pid
  }
}
