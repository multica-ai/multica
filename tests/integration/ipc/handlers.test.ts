import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import * as path from 'path'
import * as fs from 'fs'
import { mkdtempSync, rmSync, mkdirSync, writeFileSync, existsSync } from 'fs'
import { tmpdir } from 'os'
import { join } from 'path'

// Mock electron modules
vi.mock('electron', () => ({
  ipcMain: {
    handle: vi.fn()
  },
  dialog: {
    showOpenDialog: vi.fn()
  },
  clipboard: {
    writeText: vi.fn()
  },
  shell: {
    showItemInFolder: vi.fn()
  }
}))

// Mock child_process spawn
vi.mock('child_process', () => ({
  spawn: vi.fn().mockReturnValue({
    on: vi.fn((event, callback) => {
      if (event === 'close') {
        callback(0)
      }
    })
  })
}))

// Mock agent-check
vi.mock('../../../src/main/utils/agent-check', () => ({
  checkAgents: vi.fn().mockReturnValue([{ id: 'opencode', name: 'OpenCode', installed: true }])
}))

import { ipcMain, dialog, clipboard, shell } from 'electron'
import { spawn } from 'child_process'
import { registerIPCHandlers } from '../../../src/main/ipc/handlers'

// Create mock conductor
const createMockConductor = () => ({
  sendPrompt: vi.fn().mockResolvedValue('end_turn'),
  cancelRequest: vi.fn().mockResolvedValue(undefined),
  getRunningSessionIds: vi.fn().mockReturnValue(['session-1']),
  getProcessingSessionIds: vi.fn().mockReturnValue([]),
  createSession: vi.fn().mockResolvedValue({ id: 'new-session' }),
  listSessions: vi.fn().mockResolvedValue([]),
  getSessionData: vi.fn().mockResolvedValue(null),
  loadSession: vi.fn().mockResolvedValue({ id: 'loaded-session' }),
  resumeSession: vi.fn().mockResolvedValue({ id: 'resumed-session' }),
  deleteSession: vi.fn().mockResolvedValue(undefined),
  updateSessionMeta: vi.fn().mockResolvedValue({ id: 'updated-session' }),
  switchSessionAgent: vi.fn().mockResolvedValue({ id: 'switched-session' })
})

describe('IPC Handlers', () => {
  let tempDir: string
  let handlers: Map<string, Function>
  let mockConductor: ReturnType<typeof createMockConductor>

  beforeEach(() => {
    vi.clearAllMocks()
    handlers = new Map()
    mockConductor = createMockConductor()

    // Capture all registered handlers
    vi.mocked(ipcMain.handle).mockImplementation((channel: string, handler: Function) => {
      handlers.set(channel, handler)
    })

    // Register handlers with mock conductor
    registerIPCHandlers(mockConductor as any)

    // Create temp directory for file system tests
    tempDir = mkdtempSync(join(tmpdir(), 'ipc-test-'))
  })

  afterEach(() => {
    if (tempDir && existsSync(tempDir)) {
      rmSync(tempDir, { recursive: true, force: true })
    }
  })

  describe('handler registration', () => {
    it('should register all expected handlers', () => {
      const expectedChannels = [
        'agent:prompt',
        'agent:cancel',
        'agent:status',
        'session:create',
        'session:list',
        'session:get',
        'session:load',
        'session:resume',
        'session:delete',
        'session:update',
        'session:switch-agent',
        'config:get',
        'config:update',
        'dialog:select-directory',
        'system:check-agents',
        'system:check-agent',
        'agent:install',
        'fs:list-directory',
        'fs:detect-apps',
        'fs:open-with',
        'terminal:run'
      ]

      for (const channel of expectedChannels) {
        expect(handlers.has(channel), `Handler for ${channel} should be registered`).toBe(true)
      }
    })
  })

  describe('agent handlers', () => {
    it('agent:prompt should call conductor.sendPrompt', async () => {
      const handler = handlers.get('agent:prompt')!
      await handler({}, 'session-1', 'Hello')

      expect(mockConductor.sendPrompt).toHaveBeenCalledWith('session-1', 'Hello')
    })

    it('agent:cancel should call conductor.cancelRequest', async () => {
      const handler = handlers.get('agent:cancel')!
      await handler({}, 'session-1')

      expect(mockConductor.cancelRequest).toHaveBeenCalledWith('session-1')
    })

    it('agent:status should return running session info', async () => {
      const handler = handlers.get('agent:status')!
      const result = await handler({})

      expect(result).toMatchObject({
        runningSessions: 1,
        sessionIds: ['session-1'],
        processingSessionIds: []
      })
    })
  })

  describe('session handlers', () => {
    it('session:create should create a session with valid agent', async () => {
      const handler = handlers.get('session:create')!
      await handler({}, '/test/dir', 'opencode')

      expect(mockConductor.createSession).toHaveBeenCalled()
    })

    it('session:create should add directoryExists to session', async () => {
      mockConductor.createSession.mockResolvedValue({
        id: 'new-session',
        workingDirectory: tempDir
      })

      const handler = handlers.get('session:create')!
      const result = await handler({}, tempDir, 'opencode')

      expect(result.directoryExists).toBe(true)
    })

    it('session:create should return directoryExists false for non-existent directory', async () => {
      mockConductor.createSession.mockResolvedValue({
        id: 'new-session',
        workingDirectory: '/nonexistent/directory/path'
      })

      const handler = handlers.get('session:create')!
      const result = await handler({}, '/nonexistent/directory/path', 'opencode')

      expect(result.directoryExists).toBe(false)
    })

    it('session:create should throw for unknown agent', async () => {
      const handler = handlers.get('session:create')!

      await expect(handler({}, '/test/dir', 'unknown-agent')).rejects.toThrow('Unknown agent')
    })

    it('session:list should call conductor.listSessions', async () => {
      const handler = handlers.get('session:list')!
      await handler({}, { limit: 10 })

      expect(mockConductor.listSessions).toHaveBeenCalledWith({ limit: 10 })
    })

    it('session:delete should call conductor.deleteSession', async () => {
      const handler = handlers.get('session:delete')!
      const result = await handler({}, 'session-1')

      expect(mockConductor.deleteSession).toHaveBeenCalledWith('session-1')
      expect(result).toEqual({ success: true })
    })

    it('session:resume should add directoryExists to session', async () => {
      mockConductor.resumeSession.mockResolvedValue({
        id: 'session-1',
        workingDirectory: tempDir
      })

      const handler = handlers.get('session:resume')!
      const result = await handler({}, 'session-1')

      expect(result.directoryExists).toBe(true)
    })

    it('session:resume should return directoryExists false for non-existent directory', async () => {
      mockConductor.resumeSession.mockResolvedValue({
        id: 'session-1',
        workingDirectory: '/nonexistent/directory/path'
      })

      const handler = handlers.get('session:resume')!
      const result = await handler({}, 'session-1')

      expect(result.directoryExists).toBe(false)
    })

    it('session:switch-agent should add directoryExists to session', async () => {
      mockConductor.switchSessionAgent.mockResolvedValue({
        id: 'session-1',
        workingDirectory: tempDir,
        agentId: 'claude-code'
      })

      const handler = handlers.get('session:switch-agent')!
      const result = await handler({}, 'session-1', 'claude-code')

      expect(result.directoryExists).toBe(true)
      expect(mockConductor.switchSessionAgent).toHaveBeenCalledWith('session-1', 'claude-code')
    })

    it('session:switch-agent should return directoryExists false for non-existent directory', async () => {
      mockConductor.switchSessionAgent.mockResolvedValue({
        id: 'session-1',
        workingDirectory: '/nonexistent/directory/path',
        agentId: 'claude-code'
      })

      const handler = handlers.get('session:switch-agent')!
      const result = await handler({}, 'session-1', 'claude-code')

      expect(result.directoryExists).toBe(false)
    })

    it('session:list should add directoryExists to sessions', async () => {
      // Mock conductor to return sessions with existing and non-existing directories
      mockConductor.listSessions.mockResolvedValue([
        { id: 'session-1', workingDirectory: tempDir },
        { id: 'session-2', workingDirectory: '/nonexistent/directory/path' }
      ])

      const handler = handlers.get('session:list')!
      const result = await handler({})

      expect(result).toHaveLength(2)
      expect(result[0].directoryExists).toBe(true)
      expect(result[1].directoryExists).toBe(false)
    })

    it('session:load should add directoryExists to session', async () => {
      mockConductor.loadSession.mockResolvedValue({
        id: 'session-1',
        workingDirectory: tempDir
      })

      const handler = handlers.get('session:load')!
      const result = await handler({}, 'session-1')

      expect(result.directoryExists).toBe(true)
    })

    it('session:load should return directoryExists false for non-existent directory', async () => {
      mockConductor.loadSession.mockResolvedValue({
        id: 'session-1',
        workingDirectory: '/nonexistent/directory/path'
      })

      const handler = handlers.get('session:load')!
      const result = await handler({}, 'session-1')

      expect(result.directoryExists).toBe(false)
    })

    it('session:update should validate workingDirectory exists', async () => {
      const handler = handlers.get('session:update')!

      await expect(
        handler({}, 'session-1', { workingDirectory: '/nonexistent/directory/path' })
      ).rejects.toThrow('Selected directory does not exist')
    })

    it('session:update should allow updating workingDirectory to existing path', async () => {
      mockConductor.updateSessionMeta.mockResolvedValue({
        id: 'session-1',
        workingDirectory: tempDir
      })

      const handler = handlers.get('session:update')!
      const result = await handler({}, 'session-1', { workingDirectory: tempDir })

      expect(mockConductor.updateSessionMeta).toHaveBeenCalledWith('session-1', {
        workingDirectory: tempDir
      })
      expect(result.directoryExists).toBe(true)
    })

    it('session:update should add directoryExists to result', async () => {
      mockConductor.updateSessionMeta.mockResolvedValue({
        id: 'session-1',
        workingDirectory: tempDir,
        title: 'Updated'
      })

      const handler = handlers.get('session:update')!
      const result = await handler({}, 'session-1', { title: 'Updated' })

      expect(result.directoryExists).toBe(true)
    })
  })

  describe('config handlers', () => {
    it('config:get should return default config', async () => {
      const handler = handlers.get('config:get')!
      const result = await handler({})

      expect(result).toMatchObject({
        version: '0.1.0',
        defaultAgentId: 'opencode'
      })
      expect(result.agents).toBeDefined()
    })
  })

  describe('file system handlers', () => {
    it('fs:list-directory should list files and directories', async () => {
      // Create test files
      mkdirSync(join(tempDir, 'subdir'))
      writeFileSync(join(tempDir, 'file.txt'), 'content')
      writeFileSync(join(tempDir, 'script.js'), 'code')

      const handler = handlers.get('fs:list-directory')!
      const result = await handler({}, tempDir)

      expect(result.length).toBe(3)
      // Directories should come first
      expect(result[0].type).toBe('directory')
      expect(result[0].name).toBe('subdir')
      // Files should be sorted alphabetically
      const files = result.filter((n: any) => n.type === 'file')
      expect(files[0].name).toBe('file.txt')
      expect(files[0].extension).toBe('txt')
    })

    it('fs:list-directory should reject invalid paths', async () => {
      const handler = handlers.get('fs:list-directory')!

      // Relative path
      const result1 = await handler({}, 'relative/path')
      expect(result1).toEqual([])

      // Path with traversal
      const result2 = await handler({}, '/some/path/../other')
      expect(result2).toEqual([])
    })

    it('fs:list-directory should return empty array for non-existent directory', async () => {
      const handler = handlers.get('fs:list-directory')!
      const result = await handler({}, '/nonexistent/path/that/does/not/exist')

      expect(result).toEqual([])
    })

    it('fs:detect-apps should return available apps', async () => {
      const handler = handlers.get('fs:detect-apps')!
      const result = await handler({})

      // Should always include Finder, Terminal, and Copy path
      const appIds = result.map((a: any) => a.id)
      expect(appIds).toContain('finder')
      expect(appIds).toContain('terminal')
      expect(appIds).toContain('copy-path')
    })

    it('fs:open-with should reject invalid paths', async () => {
      const handler = handlers.get('fs:open-with')!

      await expect(handler({}, { path: 'relative/path', appId: 'finder' })).rejects.toThrow(
        'Invalid path'
      )
    })

    it('fs:open-with with finder should call shell.showItemInFolder', async () => {
      const handler = handlers.get('fs:open-with')!
      const testPath = join(tempDir, 'test.txt')
      writeFileSync(testPath, 'test')

      await handler({}, { path: testPath, appId: 'finder' })

      expect(shell.showItemInFolder).toHaveBeenCalledWith(testPath)
    })

    it('fs:open-with with copy-path should call clipboard.writeText', async () => {
      const handler = handlers.get('fs:open-with')!
      const testPath = join(tempDir, 'test.txt')
      writeFileSync(testPath, 'test')

      await handler({}, { path: testPath, appId: 'copy-path' })

      expect(clipboard.writeText).toHaveBeenCalledWith(testPath)
    })
  })

  describe('dialog handlers', () => {
    it('dialog:select-directory should return selected path', async () => {
      vi.mocked(dialog.showOpenDialog).mockResolvedValue({
        canceled: false,
        filePaths: ['/selected/path']
      })

      const handler = handlers.get('dialog:select-directory')!
      const result = await handler({})

      expect(result).toBe('/selected/path')
    })

    it('dialog:select-directory should return null if canceled', async () => {
      vi.mocked(dialog.showOpenDialog).mockResolvedValue({
        canceled: true,
        filePaths: []
      })

      const handler = handlers.get('dialog:select-directory')!
      const result = await handler({})

      expect(result).toBeNull()
    })
  })

  describe('system handlers', () => {
    it('system:check-agents should return agent check results', async () => {
      const handler = handlers.get('system:check-agents')!
      const result = await handler({})

      expect(result).toEqual([{ id: 'opencode', name: 'OpenCode', installed: true }])
    })
  })
})
