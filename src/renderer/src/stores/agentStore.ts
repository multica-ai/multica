/**
 * Agent installation state management
 *
 * Centralized store for agent check results, shared between:
 * - Settings (displays installation status, triggers install)
 * - AgentModelSelector (displays available agents)
 */
import { create } from 'zustand'
import type { AgentCheckResult } from '../../../shared/electron-api'

interface AgentStore {
  /** Agent check results keyed by agent ID */
  agents: Map<string, AgentCheckResult>

  /** Loading state */
  isLoading: boolean

  /** Last load timestamp (for cache invalidation) */
  lastLoadedAt: number | null

  /** Load all agents (calls checkAgents API) */
  loadAgents: () => Promise<void>

  /** Refresh a single agent (calls checkAgent API) */
  refreshAgent: (agentId: string) => Promise<void>

  /** Get agent by ID */
  getAgent: (agentId: string) => AgentCheckResult | undefined

  /** Get all agents as array */
  getAllAgents: () => AgentCheckResult[]

  /** Check if agent is installed */
  isAgentInstalled: (agentId: string) => boolean
}

export const useAgentStore = create<AgentStore>((set, get) => ({
  agents: new Map(),
  isLoading: false,
  lastLoadedAt: null,

  loadAgents: async () => {
    set({ isLoading: true })
    try {
      const results = await window.electronAPI.checkAgents()
      const agentsMap = new Map(results.map((r) => [r.id, r]))
      set({ agents: agentsMap, lastLoadedAt: Date.now() })
    } catch (err) {
      console.error('[AgentStore] Failed to load agents:', err)
    } finally {
      set({ isLoading: false })
    }
  },

  refreshAgent: async (agentId: string) => {
    const result = await window.electronAPI.checkAgent(agentId)
    if (result) {
      set((state) => {
        const newAgents = new Map(state.agents)
        newAgents.set(agentId, result)
        return { agents: newAgents }
      })
    }
  },

  getAgent: (agentId: string) => get().agents.get(agentId),

  getAllAgents: () => Array.from(get().agents.values()),

  isAgentInstalled: (agentId: string) => {
    const agent = get().agents.get(agentId)
    // Return true only if agent is found and explicitly installed
    return agent?.installed === true
  }
}))
