/**
 * DatabaseStore - SQLite-based storage for projects and sessions
 *
 * Uses sql.js (pure JavaScript SQLite) for compatibility with all Electron versions.
 * Storage location: ~/Library/Application Support/Multica/multica.db (Electron userData)
 * Fallback for tests: ~/.multica/multica.db
 */
import { join } from 'path'
import { homedir } from 'os'
import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'fs'
import { randomUUID } from 'crypto'
import initSqlJs, { Database as SqlJsDatabase } from 'sql.js'
import type { SessionNotification } from '@agentclientprotocol/sdk'
import type {
  MulticaSession,
  SessionData,
  StoredSessionUpdate,
  CreateSessionParams,
  ListSessionsOptions,
  MulticaProject,
  CreateProjectParams,
  ListProjectsOptions,
  ProjectWithSessions
} from '../../shared/types'
import type { ISessionStore } from '../conductor/types'

/**
 * Get default database directory path
 */
function getDefaultStoragePath(): string {
  try {
    // eslint-disable-next-line @typescript-eslint/no-require-imports
    const { app } = require('electron')
    return app.getPath('userData')
  } catch {
    return join(homedir(), '.multica')
  }
}

export class DatabaseStore implements ISessionStore {
  private db: SqlJsDatabase | null = null
  private dbPath: string

  constructor(storagePath?: string) {
    // storagePath is a directory, DB file is inside it
    const dir = storagePath ?? getDefaultStoragePath()
    this.dbPath = join(dir, 'multica.db')

    // Ensure directory exists
    if (!existsSync(dir)) {
      mkdirSync(dir, { recursive: true })
    }
  }

  /**
   * Initialize database schema
   */
  async initialize(): Promise<void> {
    // Initialize sql.js
    const SQL = await initSqlJs()

    // Load existing database or create new one
    if (existsSync(this.dbPath)) {
      const fileBuffer = readFileSync(this.dbPath)
      this.db = new SQL.Database(fileBuffer)
    } else {
      this.db = new SQL.Database()
    }

    // Create projects table
    this.db.run(`
      CREATE TABLE IF NOT EXISTS projects (
        id TEXT PRIMARY KEY,
        name TEXT NOT NULL,
        working_directory TEXT NOT NULL UNIQUE,
        created_at TEXT NOT NULL,
        updated_at TEXT NOT NULL,
        is_expanded INTEGER DEFAULT 1,
        sort_order INTEGER DEFAULT 0
      )
    `)

    // Migration: add sort_order column if it doesn't exist
    try {
      this.db.run('ALTER TABLE projects ADD COLUMN sort_order INTEGER DEFAULT 0')
    } catch {
      // Column already exists, ignore
    }

    // Migration: add is_archived column to sessions if it doesn't exist
    try {
      this.db.run('ALTER TABLE sessions ADD COLUMN is_archived INTEGER DEFAULT 0')
    } catch {
      // Column already exists, ignore
    }

    // Create sessions table
    this.db.run(`
      CREATE TABLE IF NOT EXISTS sessions (
        id TEXT PRIMARY KEY,
        project_id TEXT NOT NULL,
        agent_session_id TEXT,
        agent_id TEXT NOT NULL,
        created_at TEXT NOT NULL,
        updated_at TEXT NOT NULL,
        status TEXT NOT NULL DEFAULT 'active',
        is_archived INTEGER DEFAULT 0,
        title TEXT,
        message_count INTEGER DEFAULT 0,
        FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE
      )
    `)

    // Create session_updates table
    this.db.run(`
      CREATE TABLE IF NOT EXISTS session_updates (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        session_id TEXT NOT NULL,
        timestamp TEXT NOT NULL,
        sequence_number INTEGER,
        update_data TEXT NOT NULL,
        FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
      )
    `)

    // Create indexes
    this.db.run(`CREATE INDEX IF NOT EXISTS idx_sessions_project_id ON sessions(project_id)`)
    this.db.run(`CREATE INDEX IF NOT EXISTS idx_sessions_updated_at ON sessions(updated_at DESC)`)
    this.db.run(`CREATE INDEX IF NOT EXISTS idx_projects_updated_at ON projects(updated_at DESC)`)
    this.db.run(
      `CREATE INDEX IF NOT EXISTS idx_session_updates_session_id ON session_updates(session_id)`
    )
    this.db.run(
      `CREATE INDEX IF NOT EXISTS idx_session_updates_sequence ON session_updates(session_id, sequence_number)`
    )
    this.db.run(
      `CREATE INDEX IF NOT EXISTS idx_sessions_archived ON sessions(project_id, is_archived)`
    )

    // Enable foreign keys (must be done per connection in SQLite)
    this.db.run('PRAGMA foreign_keys = ON')

    // Save to file
    this.saveToFile()
  }

  /**
   * Save database to file
   */
  private saveToFile(): void {
    if (!this.db) return
    const data = this.db.export()
    const buffer = Buffer.from(data)
    writeFileSync(this.dbPath, buffer)
  }

  /**
   * Close the database connection
   */
  close(): void {
    if (this.db) {
      this.saveToFile()
      this.db.close()
      this.db = null
    }
  }

  /**
   * Ensure database is initialized
   */
  private ensureDb(): SqlJsDatabase {
    if (!this.db) {
      throw new Error('Database not initialized. Call initialize() first.')
    }
    return this.db
  }

  // ==================== Project Methods (ISessionStore interface) ====================

  /**
   * Create a new project
   */
  async createProject(params: CreateProjectParams): Promise<MulticaProject> {
    const db = this.ensureDb()
    const now = new Date().toISOString()
    const id = randomUUID()

    // Default name to folder name
    const name = params.name ?? params.workingDirectory.split('/').pop() ?? 'Untitled'

    // Get next sort order (lowest number = highest in list, new projects go to top)
    const minOrderStmt = db.prepare(
      'SELECT COALESCE(MIN(sort_order), 0) - 1 as min_order FROM projects'
    )
    minOrderStmt.step()
    const minOrderResult = minOrderStmt.getAsObject() as { min_order: number }
    const sortOrder = minOrderResult.min_order
    minOrderStmt.free()

    db.run(
      `INSERT INTO projects (id, name, working_directory, created_at, updated_at, is_expanded, sort_order)
       VALUES (?, ?, ?, ?, ?, 1, ?)`,
      [id, name, params.workingDirectory, now, now, sortOrder]
    )

    this.saveToFile()

    return {
      id,
      name,
      workingDirectory: params.workingDirectory,
      createdAt: now,
      updatedAt: now,
      isExpanded: true,
      sortOrder
    }
  }

  /**
   * Get or create a project by working directory
   */
  async getOrCreateProject(workingDirectory: string): Promise<MulticaProject> {
    const existing = this.getProjectByWorkingDirectorySync(workingDirectory)
    if (existing) {
      return existing
    }
    return this.createProject({ workingDirectory })
  }

  /**
   * Get a project by ID
   */
  async getProject(projectId: string): Promise<MulticaProject | null> {
    return this.getProjectSync(projectId)
  }

  /**
   * List all projects
   */
  async listProjects(options?: ListProjectsOptions): Promise<MulticaProject[]> {
    const db = this.ensureDb()

    let sql = `
      SELECT id, name, working_directory, created_at, updated_at, is_expanded, sort_order
      FROM projects
      ORDER BY sort_order ASC
    `

    if (options?.limit) {
      sql += ` LIMIT ${options.limit}`
      if (options.offset) {
        sql += ` OFFSET ${options.offset}`
      }
    }

    const stmt = db.prepare(sql)
    const projects: MulticaProject[] = []

    while (stmt.step()) {
      const row = stmt.getAsObject() as ProjectRow
      projects.push(this.rowToProject(row))
    }
    stmt.free()

    return projects
  }

  /**
   * List all projects with their sessions
   */
  async listProjectsWithSessions(): Promise<ProjectWithSessions[]> {
    const projects = await this.listProjects()
    const result: ProjectWithSessions[] = []

    for (const project of projects) {
      const sessions = await this.list({ projectId: project.id })
      result.push({ project, sessions })
    }

    return result
  }

  /**
   * Update a project
   */
  async updateProject(
    projectId: string,
    updates: Partial<Pick<MulticaProject, 'name' | 'isExpanded'>>
  ): Promise<MulticaProject> {
    const db = this.ensureDb()
    const project = this.getProjectSync(projectId)
    if (!project) {
      throw new Error(`Project not found: ${projectId}`)
    }

    const setClauses: string[] = []
    const values: unknown[] = []

    // Only update updated_at for meaningful changes (not UI state like isExpanded)
    if (updates.name !== undefined) {
      setClauses.push('name = ?')
      values.push(updates.name)
      setClauses.push('updated_at = ?')
      values.push(new Date().toISOString())
    }
    if (updates.isExpanded !== undefined) {
      setClauses.push('is_expanded = ?')
      values.push(updates.isExpanded ? 1 : 0)
    }

    if (setClauses.length === 0) {
      return project
    }

    values.push(projectId)

    db.run(`UPDATE projects SET ${setClauses.join(', ')} WHERE id = ?`, values)

    this.saveToFile()

    return this.getProjectSync(projectId)!
  }

  /**
   * Toggle project expanded state
   */
  async toggleProjectExpanded(projectId: string): Promise<MulticaProject> {
    const project = this.getProjectSync(projectId)
    if (!project) {
      throw new Error(`Project not found: ${projectId}`)
    }

    return this.updateProject(projectId, { isExpanded: !project.isExpanded })
  }

  /**
   * Delete a project (cascades to sessions)
   */
  async deleteProject(projectId: string): Promise<void> {
    const db = this.ensureDb()

    // Due to sql.js not supporting ON DELETE CASCADE reliably,
    // we need to manually delete related records
    const sessions = await this.list({ projectId })
    for (const session of sessions) {
      db.run('DELETE FROM session_updates WHERE session_id = ?', [session.id])
    }
    db.run('DELETE FROM sessions WHERE project_id = ?', [projectId])
    db.run('DELETE FROM projects WHERE id = ?', [projectId])

    this.saveToFile()
  }

  /**
   * Reorder projects by setting sort_order based on the provided order
   * @param projectIds Array of project IDs in the desired order (first = top)
   */
  async reorderProjects(projectIds: string[]): Promise<void> {
    const db = this.ensureDb()

    // Update sort_order for each project based on its position in the array
    for (let i = 0; i < projectIds.length; i++) {
      db.run('UPDATE projects SET sort_order = ? WHERE id = ?', [i, projectIds[i]])
    }

    this.saveToFile()
  }

  // ==================== Session Methods (ISessionStore interface) ====================

  /**
   * Create a new session
   */
  async create(params: CreateSessionParams): Promise<MulticaSession> {
    const db = this.ensureDb()
    const now = new Date().toISOString()
    const id = randomUUID()

    db.run(
      `INSERT INTO sessions (id, project_id, agent_session_id, agent_id, created_at, updated_at, status, message_count)
       VALUES (?, ?, ?, ?, ?, ?, 'active', 0)`,
      [id, params.projectId, params.agentSessionId, params.agentId, now, now]
    )

    this.saveToFile()

    // Get the project to include workingDirectory
    const project = this.getProjectSync(params.projectId)

    return {
      id,
      agentSessionId: params.agentSessionId,
      projectId: params.projectId,
      agentId: params.agentId,
      createdAt: now,
      updatedAt: now,
      status: 'active',
      isArchived: false,
      messageCount: 0,
      workingDirectory: project?.workingDirectory
    }
  }

  /**
   * Get complete session data (session + updates)
   */
  async get(sessionId: string): Promise<SessionData | null> {
    const session = this.getSessionSync(sessionId)
    if (!session) return null

    const updates = this.getSessionUpdatesSync(sessionId)

    return { session, updates }
  }

  /**
   * List sessions
   */
  async list(options?: ListSessionsOptions): Promise<MulticaSession[]> {
    const db = this.ensureDb()
    const conditions: string[] = []
    const params: unknown[] = []

    if (options?.projectId) {
      conditions.push('s.project_id = ?')
      params.push(options.projectId)
    }
    if (options?.agentId) {
      conditions.push('s.agent_id = ?')
      params.push(options.agentId)
    }
    if (options?.status) {
      conditions.push('s.status = ?')
      params.push(options.status)
    }
    // By default, exclude archived sessions unless explicitly requested
    if (!options?.includeArchived) {
      conditions.push('s.is_archived = 0')
    }

    let sql = `
      SELECT s.id, s.project_id, s.agent_session_id, s.agent_id, s.created_at, s.updated_at, s.status, s.title, s.message_count, s.is_archived, p.working_directory
      FROM sessions s
      JOIN projects p ON s.project_id = p.id
    `

    if (conditions.length > 0) {
      sql += ` WHERE ${conditions.join(' AND ')}`
    }

    sql += ' ORDER BY s.updated_at DESC'

    if (options?.limit) {
      sql += ` LIMIT ${options.limit}`
      if (options.offset) {
        sql += ` OFFSET ${options.offset}`
      }
    }

    const stmt = db.prepare(sql)
    if (params.length > 0) {
      stmt.bind(params)
    }

    const sessions: MulticaSession[] = []
    while (stmt.step()) {
      const row = stmt.getAsObject() as SessionRowWithWorkingDir
      sessions.push(this.rowToSession(row))
    }
    stmt.free()

    return sessions
  }

  /**
   * Update session metadata
   */
  async updateMeta(
    sessionId: string,
    updates: Partial<Pick<MulticaSession, 'title' | 'status' | 'agentSessionId' | 'agentId'>>
  ): Promise<MulticaSession> {
    const db = this.ensureDb()
    const session = this.getSessionSync(sessionId)
    if (!session) {
      throw new Error(`Session not found: ${sessionId}`)
    }

    const setClauses: string[] = ['updated_at = ?']
    const values: unknown[] = [new Date().toISOString()]

    if (updates.title !== undefined) {
      setClauses.push('title = ?')
      values.push(updates.title)
    }
    if (updates.status !== undefined) {
      setClauses.push('status = ?')
      values.push(updates.status)
    }
    if (updates.agentSessionId !== undefined) {
      setClauses.push('agent_session_id = ?')
      values.push(updates.agentSessionId)
    }
    if (updates.agentId !== undefined) {
      setClauses.push('agent_id = ?')
      values.push(updates.agentId)
    }

    values.push(sessionId)

    db.run(`UPDATE sessions SET ${setClauses.join(', ')} WHERE id = ?`, values)

    this.saveToFile()

    return this.getSessionSync(sessionId)!
  }

  /**
   * Delete a session
   */
  async delete(sessionId: string): Promise<void> {
    const db = this.ensureDb()

    // Delete session updates first (manual cascade)
    db.run('DELETE FROM session_updates WHERE session_id = ?', [sessionId])
    db.run('DELETE FROM sessions WHERE id = ?', [sessionId])

    this.saveToFile()
  }

  /**
   * Archive a session
   */
  async archiveSession(sessionId: string): Promise<void> {
    const db = this.ensureDb()
    const session = this.getSessionSync(sessionId)
    if (!session) {
      throw new Error(`Session not found: ${sessionId}`)
    }

    db.run('UPDATE sessions SET is_archived = 1, updated_at = ? WHERE id = ?', [
      new Date().toISOString(),
      sessionId
    ])

    this.saveToFile()
  }

  /**
   * Unarchive a session (restore from archive)
   */
  async unarchiveSession(sessionId: string): Promise<void> {
    const db = this.ensureDb()
    const session = this.getSessionSync(sessionId)
    if (!session) {
      throw new Error(`Session not found: ${sessionId}`)
    }

    db.run('UPDATE sessions SET is_archived = 0, updated_at = ? WHERE id = ?', [
      new Date().toISOString(),
      sessionId
    ])

    this.saveToFile()
  }

  /**
   * List archived sessions for a project
   */
  async listArchivedSessions(projectId: string): Promise<MulticaSession[]> {
    const db = this.ensureDb()
    const stmt = db.prepare(`
      SELECT s.id, s.project_id, s.agent_session_id, s.agent_id, s.created_at, s.updated_at, s.status, s.title, s.message_count, s.is_archived, p.working_directory
      FROM sessions s
      JOIN projects p ON s.project_id = p.id
      WHERE s.project_id = ? AND s.is_archived = 1
      ORDER BY s.updated_at DESC
    `)
    stmt.bind([projectId])

    const sessions: MulticaSession[] = []
    while (stmt.step()) {
      const row = stmt.getAsObject() as SessionRowWithWorkingDir
      sessions.push(this.rowToSession(row))
    }
    stmt.free()

    return sessions
  }

  /**
   * Append a session update
   */
  async appendUpdate(sessionId: string, update: SessionNotification): Promise<StoredSessionUpdate> {
    const db = this.ensureDb()
    const session = this.getSessionSync(sessionId)
    if (!session) {
      throw new Error(`Session not found: ${sessionId}`)
    }

    const timestamp = new Date().toISOString()

    // Get next sequence number
    const seqStmt = db.prepare(`
      SELECT COALESCE(MAX(sequence_number), 0) + 1 as next_seq
      FROM session_updates WHERE session_id = ?
    `)
    seqStmt.bind([sessionId])
    seqStmt.step()
    const seqResult = seqStmt.getAsObject() as { next_seq: number }
    const sequenceNumber = seqResult.next_seq
    seqStmt.free()

    // Insert update
    db.run(
      `INSERT INTO session_updates (session_id, timestamp, sequence_number, update_data)
       VALUES (?, ?, ?, ?)`,
      [sessionId, timestamp, sequenceNumber, JSON.stringify(update)]
    )

    // Update session metadata
    const messageCount = this.countSessionMessages(sessionId)
    db.run(`UPDATE sessions SET updated_at = ?, message_count = ? WHERE id = ?`, [
      timestamp,
      messageCount,
      sessionId
    ])

    this.saveToFile()

    return {
      timestamp,
      sequenceNumber,
      update
    }
  }

  /**
   * Get session by agent session ID
   */
  getByAgentSessionId(agentSessionId: string): MulticaSession | null {
    const db = this.ensureDb()
    const stmt = db.prepare(`
      SELECT s.id, s.project_id, s.agent_session_id, s.agent_id, s.created_at, s.updated_at, s.status, s.title, s.message_count, s.is_archived, p.working_directory
      FROM sessions s
      JOIN projects p ON s.project_id = p.id
      WHERE s.agent_session_id = ?
    `)
    stmt.bind([agentSessionId])

    if (stmt.step()) {
      const row = stmt.getAsObject() as SessionRowWithWorkingDir
      stmt.free()
      return this.rowToSession(row)
    }

    stmt.free()
    return null
  }

  // ==================== Private Helper Methods ====================

  /**
   * Get a project by ID (sync)
   */
  private getProjectSync(projectId: string): MulticaProject | null {
    const db = this.ensureDb()
    const stmt = db.prepare(`
      SELECT id, name, working_directory, created_at, updated_at, is_expanded, sort_order
      FROM projects WHERE id = ?
    `)
    stmt.bind([projectId])

    if (stmt.step()) {
      const row = stmt.getAsObject() as ProjectRow
      stmt.free()
      return this.rowToProject(row)
    }

    stmt.free()
    return null
  }

  /**
   * Get a project by working directory (sync)
   */
  private getProjectByWorkingDirectorySync(workingDirectory: string): MulticaProject | null {
    const db = this.ensureDb()
    const stmt = db.prepare(`
      SELECT id, name, working_directory, created_at, updated_at, is_expanded, sort_order
      FROM projects WHERE working_directory = ?
    `)
    stmt.bind([workingDirectory])

    if (stmt.step()) {
      const row = stmt.getAsObject() as ProjectRow
      stmt.free()
      return this.rowToProject(row)
    }

    stmt.free()
    return null
  }

  /**
   * Get a session by ID (sync)
   */
  private getSessionSync(sessionId: string): MulticaSession | null {
    const db = this.ensureDb()
    const stmt = db.prepare(`
      SELECT s.id, s.project_id, s.agent_session_id, s.agent_id, s.created_at, s.updated_at, s.status, s.title, s.message_count, s.is_archived, p.working_directory
      FROM sessions s
      JOIN projects p ON s.project_id = p.id
      WHERE s.id = ?
    `)
    stmt.bind([sessionId])

    if (stmt.step()) {
      const row = stmt.getAsObject() as SessionRowWithWorkingDir
      stmt.free()
      return this.rowToSession(row)
    }

    stmt.free()
    return null
  }

  /**
   * Get all updates for a session (sync)
   */
  private getSessionUpdatesSync(sessionId: string): StoredSessionUpdate[] {
    const db = this.ensureDb()
    const stmt = db.prepare(`
      SELECT timestamp, sequence_number, update_data
      FROM session_updates
      WHERE session_id = ?
      ORDER BY sequence_number ASC
    `)
    stmt.bind([sessionId])

    const updates: StoredSessionUpdate[] = []
    while (stmt.step()) {
      const row = stmt.getAsObject() as SessionUpdateRow
      updates.push({
        timestamp: row.timestamp,
        sequenceNumber: row.sequence_number,
        update: JSON.parse(row.update_data)
      })
    }
    stmt.free()

    return updates
  }

  /**
   * Count messages in a session (simplified heuristic)
   */
  private countSessionMessages(sessionId: string): number {
    const db = this.ensureDb()
    const stmt = db.prepare(`
      SELECT COUNT(*) as count FROM session_updates
      WHERE session_id = ?
      AND (
        json_extract(update_data, '$.update.sessionUpdate') = 'agent_message_chunk'
        OR json_extract(update_data, '$.update.sessionUpdate') = 'user_message_chunk'
      )
    `)
    stmt.bind([sessionId])
    stmt.step()
    const result = stmt.getAsObject() as { count: number }
    stmt.free()
    return Math.max(1, Math.ceil(result.count / 10))
  }

  /**
   * Convert database row to MulticaProject
   */
  private rowToProject(row: ProjectRow): MulticaProject {
    return {
      id: row.id,
      name: row.name,
      workingDirectory: row.working_directory,
      createdAt: row.created_at,
      updatedAt: row.updated_at,
      isExpanded: row.is_expanded === 1,
      sortOrder: row.sort_order ?? 0
    }
  }

  /**
   * Convert database row to MulticaSession
   */
  private rowToSession(row: SessionRowWithWorkingDir): MulticaSession {
    return {
      id: row.id,
      agentSessionId: row.agent_session_id,
      projectId: row.project_id,
      agentId: row.agent_id,
      createdAt: row.created_at,
      updatedAt: row.updated_at,
      status: row.status as 'active' | 'completed' | 'error',
      isArchived: row.is_archived === 1,
      title: row.title ?? undefined,
      messageCount: row.message_count,
      workingDirectory: row.working_directory
    }
  }
}

// Type definitions for database rows
interface ProjectRow {
  id: string
  name: string
  working_directory: string
  created_at: string
  updated_at: string
  is_expanded: number
  sort_order: number
}

interface SessionRowWithWorkingDir {
  id: string
  project_id: string
  agent_session_id: string
  agent_id: string
  created_at: string
  updated_at: string
  status: string
  title: string | null
  message_count: number
  working_directory: string
  is_archived: number
}

interface SessionUpdateRow {
  timestamp: string
  sequence_number: number
  update_data: string
}
