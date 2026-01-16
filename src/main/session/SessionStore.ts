/**
 * SessionStore - Manages session persistence
 *
 * Storage structure:
 * ~/.multica/
 *   └── sessions/
 *       ├── index.json           # Session list index (fast load)
 *       └── data/
 *           ├── {session-id}.json  # Complete session data
 *           └── ...
 */
import { join } from 'path'
import { homedir } from 'os'
import { existsSync, mkdirSync } from 'fs'
import { readFile, writeFile, unlink, rename } from 'fs/promises'
import { randomUUID } from 'crypto'
import type { SessionNotification } from '@agentclientprotocol/sdk'
import type {
  MulticaSession,
  SessionData,
  StoredSessionUpdate,
  CreateSessionParams,
  ListSessionsOptions
} from '../../shared/types'

/**
 * Get default storage path
 * Uses Electron's userData in GUI mode, fallback to ~/.multica for CLI
 */
function getDefaultStoragePath(): string {
  try {
    // Try to use Electron's app.getPath if available
    // eslint-disable-next-line @typescript-eslint/no-require-imports
    const { app } = require('electron')
    return join(app.getPath('userData'), 'sessions')
  } catch {
    // Fallback for CLI mode
    return join(homedir(), '.multica', 'sessions')
  }
}

export class SessionStore {
  private basePath: string
  private indexPath: string
  private dataPath: string

  // In-memory cache
  private sessionsIndex: Map<string, MulticaSession> = new Map()
  private loadedSessions: Map<string, SessionData> = new Map()

  // Write queues to serialize concurrent writes (proper async mutex pattern)
  private writeQueues: Map<string, Promise<void>> = new Map()

  // Sequence counters per session for ordering updates (handles concurrent async updates)
  private sequenceCounters: Map<string, number> = new Map()

  constructor(basePath?: string) {
    this.basePath = basePath ?? getDefaultStoragePath()
    this.indexPath = join(this.basePath, 'index.json')
    this.dataPath = join(this.basePath, 'data')
  }

  /**
   * Initialize storage directories and load index
   */
  async initialize(): Promise<void> {
    // Ensure directories exist
    if (!existsSync(this.basePath)) {
      mkdirSync(this.basePath, { recursive: true })
    }
    if (!existsSync(this.dataPath)) {
      mkdirSync(this.dataPath, { recursive: true })
    }

    // Load index
    await this.loadIndex()
  }

  /**
   * Create a new session
   */
  async create(params: CreateSessionParams): Promise<MulticaSession> {
    const now = new Date().toISOString()
    const session: MulticaSession = {
      id: randomUUID(),
      agentSessionId: params.agentSessionId,
      agentId: params.agentId,
      workingDirectory: params.workingDirectory,
      createdAt: now,
      updatedAt: now,
      status: 'active',
      messageCount: 0
    }

    // Add to index
    this.sessionsIndex.set(session.id, session)

    // Create session data
    const sessionData: SessionData = {
      session,
      updates: []
    }
    this.loadedSessions.set(session.id, sessionData)

    // Persist
    await this.saveIndex()
    await this.saveSessionData(session.id)

    return session
  }

  /**
   * Get session list (from index, doesn't load full data)
   */
  async list(options?: ListSessionsOptions): Promise<MulticaSession[]> {
    let sessions = Array.from(this.sessionsIndex.values())

    // Filter by agent
    if (options?.agentId) {
      sessions = sessions.filter((s) => s.agentId === options.agentId)
    }

    // Filter by status
    if (options?.status) {
      sessions = sessions.filter((s) => s.status === options.status)
    }

    // Sort by updatedAt descending (most recent first)
    sessions.sort((a, b) => new Date(b.updatedAt).getTime() - new Date(a.updatedAt).getTime())

    // Pagination
    const offset = options?.offset ?? 0
    const limit = options?.limit ?? sessions.length
    sessions = sessions.slice(offset, offset + limit)

    return sessions
  }

  /**
   * Get a single session's complete data
   */
  async get(sessionId: string): Promise<SessionData | null> {
    // Check cache first
    if (this.loadedSessions.has(sessionId)) {
      return this.loadedSessions.get(sessionId)!
    }

    // Check if session exists in index
    if (!this.sessionsIndex.has(sessionId)) {
      return null
    }

    // Load from disk
    const dataPath = this.getSessionDataPath(sessionId)
    if (!existsSync(dataPath)) {
      return null
    }

    try {
      const content = await readFile(dataPath, 'utf-8')
      const sessionData = JSON.parse(content) as SessionData
      this.loadedSessions.set(sessionId, sessionData)
      // Initialize sequence counter from loaded data
      this.initSequenceCounter(sessionId, sessionData.updates)
      return sessionData
    } catch {
      console.error(`[SessionStore] Failed to load session: ${sessionId}`)
      return null
    }
  }

  /**
   * Append a session update
   */
  async appendUpdate(sessionId: string, update: SessionNotification): Promise<StoredSessionUpdate> {
    // Ensure session is loaded
    const sessionData = await this.get(sessionId)
    if (!sessionData) {
      throw new Error(`Session not found: ${sessionId}`)
    }

    // Get next sequence number for this session (monotonically increasing)
    const currentSeq = this.sequenceCounters.get(sessionId) ?? 0
    const nextSeq = currentSeq + 1
    this.sequenceCounters.set(sessionId, nextSeq)

    // Append update with sequence number
    const storedUpdate: StoredSessionUpdate = {
      timestamp: new Date().toISOString(),
      sequenceNumber: nextSeq,
      update
    }
    sessionData.updates.push(storedUpdate)

    // Update metadata
    sessionData.session.updatedAt = storedUpdate.timestamp
    sessionData.session.messageCount = this.countMessages(sessionData.updates)

    // Update index
    this.sessionsIndex.set(sessionId, sessionData.session)

    // Persist (debounce in production, immediate for now)
    await this.saveSessionData(sessionId)
    await this.saveIndex()

    return storedUpdate
  }

  /**
   * Initialize sequence counter when loading session from disk
   */
  private initSequenceCounter(sessionId: string, updates: StoredSessionUpdate[]): void {
    // Find the highest sequence number in existing updates
    let maxSeq = 0
    for (const update of updates) {
      if (update.sequenceNumber && update.sequenceNumber > maxSeq) {
        maxSeq = update.sequenceNumber
      }
    }
    this.sequenceCounters.set(sessionId, maxSeq)
  }

  /**
   * Update session metadata
   */
  async updateMeta(sessionId: string, updates: Partial<MulticaSession>): Promise<MulticaSession> {
    const sessionData = await this.get(sessionId)
    if (!sessionData) {
      throw new Error(`Session not found: ${sessionId}`)
    }

    // Apply updates (only allowed fields)
    if (updates.title !== undefined) {
      sessionData.session.title = updates.title
    }
    if (updates.status !== undefined) {
      sessionData.session.status = updates.status
    }
    if (updates.agentSessionId !== undefined) {
      sessionData.session.agentSessionId = updates.agentSessionId
    }
    if (updates.agentId !== undefined) {
      sessionData.session.agentId = updates.agentId
    }
    if (updates.workingDirectory !== undefined) {
      sessionData.session.workingDirectory = updates.workingDirectory
    }

    // Update timestamp
    sessionData.session.updatedAt = new Date().toISOString()

    // Update index
    this.sessionsIndex.set(sessionId, sessionData.session)

    // Persist
    await this.saveSessionData(sessionId)
    await this.saveIndex()

    return sessionData.session
  }

  /**
   * Delete a session
   */
  async delete(sessionId: string): Promise<void> {
    // Remove from index
    this.sessionsIndex.delete(sessionId)

    // Remove from cache
    this.loadedSessions.delete(sessionId)

    // Remove data file
    const dataPath = this.getSessionDataPath(sessionId)
    if (existsSync(dataPath)) {
      await unlink(dataPath)
    }

    // Persist index
    await this.saveIndex()
  }

  /**
   * Get session by agent session ID
   */
  getByAgentSessionId(agentSessionId: string): MulticaSession | null {
    for (const session of this.sessionsIndex.values()) {
      if (session.agentSessionId === agentSessionId) {
        return session
      }
    }
    return null
  }

  // --- Private methods ---

  private getSessionDataPath(sessionId: string): string {
    return join(this.dataPath, `${sessionId}.json`)
  }

  private async loadIndex(): Promise<void> {
    if (!existsSync(this.indexPath)) {
      this.sessionsIndex = new Map()
      return
    }

    try {
      const content = await readFile(this.indexPath, 'utf-8')
      const sessions = JSON.parse(content) as MulticaSession[]
      this.sessionsIndex = new Map(sessions.map((s) => [s.id, s]))
    } catch {
      console.error('[SessionStore] Failed to load index, starting fresh')
      this.sessionsIndex = new Map()
    }
  }

  private async saveIndex(): Promise<void> {
    await this.atomicWrite('__index__', this.indexPath, async () => {
      const sessions = Array.from(this.sessionsIndex.values())
      return JSON.stringify(sessions, null, 2)
    })
  }

  private async saveSessionData(sessionId: string): Promise<void> {
    const sessionData = this.loadedSessions.get(sessionId)
    if (!sessionData) return

    const dataPath = this.getSessionDataPath(sessionId)
    await this.atomicWrite(sessionId, dataPath, async () => {
      return JSON.stringify(sessionData, null, 2)
    })
  }

  /**
   * Atomic write with proper serialization to prevent concurrent writes and corruption.
   *
   * Uses a chained promise pattern to ensure writes to the same file are serialized.
   * Each write waits for the previous write to complete before starting.
   */
  private async atomicWrite(
    lockKey: string,
    filePath: string,
    getData: () => Promise<string>
  ): Promise<void> {
    // Create the actual write operation
    const doWrite = async (): Promise<void> => {
      // Use unique temp file name: timestamp + random suffix to avoid collisions
      // even when multiple writes happen within the same millisecond
      const tempPath = `${filePath}.tmp.${Date.now()}.${randomUUID().slice(0, 8)}`
      try {
        const data = await getData()
        await writeFile(tempPath, data)
        await rename(tempPath, filePath)
      } catch (err) {
        // Clean up temp file on error
        try {
          if (existsSync(tempPath)) {
            await unlink(tempPath)
          }
        } catch {
          // Ignore cleanup errors
        }
        throw err
      }
    }

    // Get the current write queue for this key (or resolved promise if none)
    const previousWrite = this.writeQueues.get(lockKey) ?? Promise.resolve()

    // Chain this write after the previous one.
    // Catch previous errors so they don't block the chain - each write should
    // be independent and a failure in one shouldn't prevent subsequent writes.
    const thisWrite = previousWrite.catch(() => {}).then(doWrite)

    // Immediately update the queue (synchronous) before any await.
    // Store a non-rejecting version for chaining purposes.
    this.writeQueues.set(
      lockKey,
      thisWrite.catch(() => {})
    )

    // Return the actual promise which can reject if this write fails
    return thisWrite
  }

  private countMessages(updates: StoredSessionUpdate[]): number {
    // Count content updates (simplified - could be more accurate)
    let count = 0
    for (const u of updates) {
      const update = u.update.update
      if (update && 'sessionUpdate' in update) {
        // Count agent message chunks as messages
        const sessionUpdateType = update.sessionUpdate
        if (
          sessionUpdateType === 'agent_message_chunk' ||
          sessionUpdateType === 'user_message_chunk'
        ) {
          count++
        }
      }
    }
    return Math.max(1, Math.ceil(count / 10)) // Rough estimate
  }
}
