/**
 * Tests for agentStore - agent installation state management
 */
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { useAgentStore } from '../../../../src/renderer/src/stores/agentStore'
import type { AgentCheckResult } from '../../../../src/shared/electron-api'

// Mock window.electronAPI
const mockCheckAgents = vi.fn()
const mockCheckAgent = vi.fn()

vi.stubGlobal('window', {
  electronAPI: {
    checkAgents: mockCheckAgents,
    checkAgent: mockCheckAgent
  }
})

describe('agentStore', () => {
  beforeEach(() => {
    // Reset store to default state
    useAgentStore.setState({
      agents: new Map(),
      isLoading: false,
      lastLoadedAt: null
    })
    // Clear mocks
    vi.clearAllMocks()
  })

  describe('initial state', () => {
    it('starts with empty agents map', () => {
      expect(useAgentStore.getState().agents.size).toBe(0)
    })

    it('starts with isLoading false', () => {
      expect(useAgentStore.getState().isLoading).toBe(false)
    })

    it('starts with lastLoadedAt null', () => {
      expect(useAgentStore.getState().lastLoadedAt).toBeNull()
    })
  })

  describe('loadAgents', () => {
    it('loads agents from API and stores them', async () => {
      const mockResults: AgentCheckResult[] = [
        { id: 'claude-code', name: 'Claude Code', installed: true },
        { id: 'codex', name: 'Codex', installed: false }
      ]
      mockCheckAgents.mockResolvedValue(mockResults)

      await useAgentStore.getState().loadAgents()

      expect(mockCheckAgents).toHaveBeenCalled()
      expect(useAgentStore.getState().agents.size).toBe(2)
      expect(useAgentStore.getState().agents.get('claude-code')?.installed).toBe(true)
      expect(useAgentStore.getState().agents.get('codex')?.installed).toBe(false)
    })

    it('sets isLoading during load', async () => {
      mockCheckAgents.mockImplementation(
        () => new Promise((resolve) => setTimeout(() => resolve([]), 10))
      )

      const loadPromise = useAgentStore.getState().loadAgents()
      expect(useAgentStore.getState().isLoading).toBe(true)

      await loadPromise
      expect(useAgentStore.getState().isLoading).toBe(false)
    })

    it('updates lastLoadedAt on success', async () => {
      mockCheckAgents.mockResolvedValue([])
      const before = Date.now()

      await useAgentStore.getState().loadAgents()

      const lastLoadedAt = useAgentStore.getState().lastLoadedAt
      expect(lastLoadedAt).not.toBeNull()
      expect(lastLoadedAt).toBeGreaterThanOrEqual(before)
    })

    it('handles API errors gracefully', async () => {
      mockCheckAgents.mockRejectedValue(new Error('Network error'))

      // Should not throw
      await useAgentStore.getState().loadAgents()

      // isLoading should be reset to false
      expect(useAgentStore.getState().isLoading).toBe(false)
      // agents should remain empty
      expect(useAgentStore.getState().agents.size).toBe(0)
    })
  })

  describe('refreshAgent', () => {
    it('updates a single agent in the store', async () => {
      // Set initial state with an agent
      useAgentStore.setState({
        agents: new Map([
          ['claude-code', { id: 'claude-code', name: 'Claude Code', installed: false }]
        ])
      })

      const updatedResult: AgentCheckResult = {
        id: 'claude-code',
        name: 'Claude Code',
        installed: true
      }
      mockCheckAgent.mockResolvedValue(updatedResult)

      await useAgentStore.getState().refreshAgent('claude-code')

      expect(mockCheckAgent).toHaveBeenCalledWith('claude-code')
      expect(useAgentStore.getState().agents.get('claude-code')?.installed).toBe(true)
    })

    it('adds new agent if not exists', async () => {
      const newResult: AgentCheckResult = {
        id: 'new-agent',
        name: 'New Agent',
        installed: true
      }
      mockCheckAgent.mockResolvedValue(newResult)

      await useAgentStore.getState().refreshAgent('new-agent')

      expect(useAgentStore.getState().agents.get('new-agent')?.installed).toBe(true)
    })

    it('does nothing if API returns null', async () => {
      useAgentStore.setState({
        agents: new Map([
          ['claude-code', { id: 'claude-code', name: 'Claude Code', installed: false }]
        ])
      })
      mockCheckAgent.mockResolvedValue(null)

      await useAgentStore.getState().refreshAgent('claude-code')

      // Original value should be preserved
      expect(useAgentStore.getState().agents.get('claude-code')?.installed).toBe(false)
    })
  })

  describe('getAgent', () => {
    it('returns agent by id', () => {
      useAgentStore.setState({
        agents: new Map([
          ['claude-code', { id: 'claude-code', name: 'Claude Code', installed: true }]
        ])
      })

      const agent = useAgentStore.getState().getAgent('claude-code')
      expect(agent?.id).toBe('claude-code')
    })

    it('returns undefined for unknown agent', () => {
      const agent = useAgentStore.getState().getAgent('unknown')
      expect(agent).toBeUndefined()
    })
  })

  describe('getAllAgents', () => {
    it('returns all agents as array', () => {
      useAgentStore.setState({
        agents: new Map([
          ['claude-code', { id: 'claude-code', name: 'Claude Code', installed: true }],
          ['codex', { id: 'codex', name: 'Codex', installed: false }]
        ])
      })

      const agents = useAgentStore.getState().getAllAgents()
      expect(agents).toHaveLength(2)
      expect(agents.map((a) => a.id)).toContain('claude-code')
      expect(agents.map((a) => a.id)).toContain('codex')
    })

    it('returns empty array when no agents', () => {
      const agents = useAgentStore.getState().getAllAgents()
      expect(agents).toEqual([])
    })
  })

  describe('isAgentInstalled', () => {
    beforeEach(() => {
      useAgentStore.setState({
        agents: new Map([
          ['installed-agent', { id: 'installed-agent', name: 'Installed', installed: true }],
          ['not-installed', { id: 'not-installed', name: 'Not Installed', installed: false }]
        ])
      })
    })

    it('returns true for installed agent', () => {
      expect(useAgentStore.getState().isAgentInstalled('installed-agent')).toBe(true)
    })

    it('returns false for not installed agent', () => {
      expect(useAgentStore.getState().isAgentInstalled('not-installed')).toBe(false)
    })

    it('returns false for unknown agent', () => {
      // This is the important edge case - unknown agents should return false
      expect(useAgentStore.getState().isAgentInstalled('unknown')).toBe(false)
    })
  })
})
