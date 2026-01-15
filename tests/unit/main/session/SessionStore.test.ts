import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { join } from 'path'
import { mkdtempSync, rmSync, existsSync, readFileSync } from 'fs'
import { tmpdir } from 'os'
import { SessionStore } from '../../../../src/main/session/SessionStore'

describe('SessionStore', () => {
  let tempDir: string
  let store: SessionStore

  beforeEach(async () => {
    // Create a real temp directory for each test
    tempDir = mkdtempSync(join(tmpdir(), 'sessionstore-test-'))
    store = new SessionStore(tempDir)
    await store.initialize()
  })

  afterEach(() => {
    // Clean up temp directory
    if (tempDir && existsSync(tempDir)) {
      rmSync(tempDir, { recursive: true, force: true })
    }
  })

  describe('initialize', () => {
    it('should create storage directories', async () => {
      expect(existsSync(tempDir)).toBe(true)
      expect(existsSync(join(tempDir, 'data'))).toBe(true)
    })

    it('should handle empty storage gracefully', async () => {
      const sessions = await store.list()
      expect(sessions).toEqual([])
    })
  })

  describe('create', () => {
    it('should create a session with correct structure', async () => {
      const session = await store.create({
        agentSessionId: 'agent-123',
        agentId: 'opencode',
        workingDirectory: '/test/project'
      })

      expect(session).toMatchObject({
        id: expect.any(String),
        agentSessionId: 'agent-123',
        agentId: 'opencode',
        workingDirectory: '/test/project',
        status: 'active',
        messageCount: 0
      })
      expect(session.createdAt).toBeDefined()
      expect(session.updatedAt).toBeDefined()
    })

    it('should persist session to disk', async () => {
      const session = await store.create({
        agentSessionId: 'agent-123',
        agentId: 'opencode',
        workingDirectory: '/test/project'
      })

      // Check index file exists
      const indexPath = join(tempDir, 'index.json')
      expect(existsSync(indexPath)).toBe(true)

      // Check session data file exists
      const dataPath = join(tempDir, 'data', `${session.id}.json`)
      expect(existsSync(dataPath)).toBe(true)
    })

    it('should generate unique IDs for each session', async () => {
      const session1 = await store.create({
        agentSessionId: 'agent-1',
        agentId: 'opencode',
        workingDirectory: '/test'
      })
      const session2 = await store.create({
        agentSessionId: 'agent-2',
        agentId: 'opencode',
        workingDirectory: '/test'
      })

      expect(session1.id).not.toBe(session2.id)
    })
  })

  describe('list', () => {
    it('should return all sessions sorted by updatedAt descending', async () => {
      // Create first session
      await store.create({
        agentSessionId: 'agent-1',
        agentId: 'opencode',
        workingDirectory: '/test'
      })

      // Small delay to ensure different timestamps
      await new Promise((resolve) => setTimeout(resolve, 10))

      // Create second session (more recent)
      await store.create({
        agentSessionId: 'agent-2',
        agentId: 'opencode',
        workingDirectory: '/test'
      })

      const sessions = await store.list()
      expect(sessions.length).toBe(2)
      // Most recent (session2) should be first
      expect(sessions[0].agentSessionId).toBe('agent-2')
      expect(sessions[1].agentSessionId).toBe('agent-1')
    })

    it('should filter by agentId', async () => {
      await store.create({
        agentSessionId: 'agent-1',
        agentId: 'opencode',
        workingDirectory: '/test'
      })
      await store.create({
        agentSessionId: 'agent-2',
        agentId: 'gemini',
        workingDirectory: '/test'
      })

      const sessions = await store.list({ agentId: 'opencode' })
      expect(sessions.length).toBe(1)
      expect(sessions[0].agentId).toBe('opencode')
    })

    it('should filter by status', async () => {
      const session1 = await store.create({
        agentSessionId: 'agent-1',
        agentId: 'opencode',
        workingDirectory: '/test'
      })
      await store.create({
        agentSessionId: 'agent-2',
        agentId: 'opencode',
        workingDirectory: '/test'
      })

      await store.updateMeta(session1.id, { status: 'completed' })

      const activeSessions = await store.list({ status: 'active' })
      expect(activeSessions.length).toBe(1)

      const completedSessions = await store.list({ status: 'completed' })
      expect(completedSessions.length).toBe(1)
    })

    it('should support pagination', async () => {
      for (let i = 0; i < 5; i++) {
        await store.create({
          agentSessionId: `agent-${i}`,
          agentId: 'opencode',
          workingDirectory: '/test'
        })
      }

      const page1 = await store.list({ limit: 2, offset: 0 })
      expect(page1.length).toBe(2)

      const page2 = await store.list({ limit: 2, offset: 2 })
      expect(page2.length).toBe(2)

      const page3 = await store.list({ limit: 2, offset: 4 })
      expect(page3.length).toBe(1)
    })
  })

  describe('get', () => {
    it('should return session data for existing session', async () => {
      const created = await store.create({
        agentSessionId: 'agent-123',
        agentId: 'opencode',
        workingDirectory: '/test'
      })

      const sessionData = await store.get(created.id)
      expect(sessionData).not.toBeNull()
      expect(sessionData?.session.id).toBe(created.id)
      expect(sessionData?.updates).toEqual([])
    })

    it('should return null for non-existent session', async () => {
      const sessionData = await store.get('non-existent-id')
      expect(sessionData).toBeNull()
    })

    it('should cache loaded sessions', async () => {
      const created = await store.create({
        agentSessionId: 'agent-123',
        agentId: 'opencode',
        workingDirectory: '/test'
      })

      // First get loads from disk
      const data1 = await store.get(created.id)
      // Second get should return cached data
      const data2 = await store.get(created.id)

      expect(data1).toBe(data2) // Same reference
    })
  })

  describe('updateMeta', () => {
    it('should update allowed fields', async () => {
      const created = await store.create({
        agentSessionId: 'agent-123',
        agentId: 'opencode',
        workingDirectory: '/test'
      })

      const updated = await store.updateMeta(created.id, {
        title: 'My Session',
        status: 'completed'
      })

      expect(updated.title).toBe('My Session')
      expect(updated.status).toBe('completed')
      expect(new Date(updated.updatedAt).getTime()).toBeGreaterThanOrEqual(
        new Date(created.updatedAt).getTime()
      )
    })

    it('should throw for non-existent session', async () => {
      await expect(store.updateMeta('non-existent', { title: 'Test' })).rejects.toThrow(
        'Session not found'
      )
    })
  })

  describe('delete', () => {
    it('should remove session from index and disk', async () => {
      const session = await store.create({
        agentSessionId: 'agent-123',
        agentId: 'opencode',
        workingDirectory: '/test'
      })

      const dataPath = join(tempDir, 'data', `${session.id}.json`)
      expect(existsSync(dataPath)).toBe(true)

      await store.delete(session.id)

      // Should be removed from list
      const sessions = await store.list()
      expect(sessions.find((s) => s.id === session.id)).toBeUndefined()

      // File should be deleted
      expect(existsSync(dataPath)).toBe(false)
    })
  })

  describe('getByAgentSessionId', () => {
    it('should find session by agent session ID', async () => {
      await store.create({
        agentSessionId: 'agent-123',
        agentId: 'opencode',
        workingDirectory: '/test'
      })

      const found = store.getByAgentSessionId('agent-123')
      expect(found).not.toBeNull()
      expect(found?.agentSessionId).toBe('agent-123')
    })

    it('should return null for unknown agent session ID', async () => {
      const found = store.getByAgentSessionId('unknown')
      expect(found).toBeNull()
    })
  })

  describe('persistence across instances', () => {
    it('should load sessions from disk on new instance', async () => {
      // Create session with first store instance
      const created = await store.create({
        agentSessionId: 'agent-123',
        agentId: 'opencode',
        workingDirectory: '/test'
      })
      await store.updateMeta(created.id, { title: 'Persistent Session' })

      // Create new store instance pointing to same directory
      const newStore = new SessionStore(tempDir)
      await newStore.initialize()

      // Should find the session
      const sessions = await newStore.list()
      expect(sessions.length).toBe(1)
      expect(sessions[0].title).toBe('Persistent Session')
    })
  })
})
