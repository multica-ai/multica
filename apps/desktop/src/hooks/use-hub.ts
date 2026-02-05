import { useState, useEffect, useCallback } from 'react'

// ============================================================================
// Types matching the IPC response from main process
// ============================================================================

export type ConnectionState = 'disconnected' | 'connecting' | 'connected' | 'reconnecting' | 'registered'

export interface HubInfo {
  hubId: string
  url: string
  connectionState: ConnectionState
  agentCount: number
}

export interface AgentInfo {
  id: string
  closed: boolean
}

export interface UseHubReturn {
  /** Hub information */
  hubInfo: HubInfo | null
  /** List of agents */
  agents: AgentInfo[]
  /** Loading state */
  loading: boolean
  /** Error state */
  error: string | null

  /** Initialize the Hub (called automatically on mount) */
  initHub: () => Promise<void>
  /** Refresh Hub info and agents list */
  refresh: () => Promise<void>
  /** Reconnect to a different Gateway URL */
  reconnect: (url: string) => Promise<void>
  /** Create a new agent */
  createAgent: (id?: string) => Promise<AgentInfo | null>
  /** Close an agent */
  closeAgent: (id: string) => Promise<boolean>
  /** Send a message to an agent */
  sendMessage: (agentId: string, content: string) => Promise<boolean>
}

/**
 * Hook for managing Hub connection and agents via IPC.
 *
 * This hook communicates with the Electron main process to:
 * - Initialize and manage the Hub singleton
 * - Create, list, and close agents
 * - Send messages to agents
 */
export function useHub(): UseHubReturn {
  const [hubInfo, setHubInfo] = useState<HubInfo | null>(null)
  const [agents, setAgents] = useState<AgentInfo[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  // Initialize Hub and fetch info
  const initHub = useCallback(async () => {
    try {
      setLoading(true)
      setError(null)

      // Initialize Hub (use new electronAPI if available)
      if (window.electronAPI) {
        await window.electronAPI.hub.init()
        const info = await window.electronAPI.hub.info()
        setHubInfo(info as HubInfo)
        const agentList = await window.electronAPI.hub.listAgents()
        setAgents(agentList as AgentInfo[])
      } else {
        await window.ipcRenderer.invoke('hub:init')
        const info = await window.ipcRenderer.invoke('hub:info')
        setHubInfo(info)
        const agentList = await window.ipcRenderer.invoke('hub:listAgents')
        setAgents(agentList)
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to initialize Hub')
    } finally {
      setLoading(false)
    }
  }, [])

  // Initial load
  useEffect(() => {
    initHub()
  }, [initHub])

  // Subscribe to connection state changes pushed from main process
  useEffect(() => {
    const handler = (state: string) => {
      setHubInfo((prev) => prev ? { ...prev, connectionState: state as HubInfo['connectionState'] } : prev)
    }
    window.electronAPI?.hub.onConnectionStateChanged(handler)
    return () => {
      window.electronAPI?.hub.offConnectionStateChanged()
    }
  }, [])

  // Refresh Hub info and agents
  const refresh = useCallback(async () => {
    try {
      setError(null)

      if (window.electronAPI) {
        const info = await window.electronAPI.hub.info()
        setHubInfo(info as HubInfo)
        const agentList = await window.electronAPI.hub.listAgents()
        setAgents(agentList as AgentInfo[])
      } else {
        const info = await window.ipcRenderer.invoke('hub:info')
        setHubInfo(info)
        const agentList = await window.ipcRenderer.invoke('hub:listAgents')
        setAgents(agentList)
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to refresh Hub info')
    }
  }, [])

  // Reconnect to different Gateway
  const reconnect = useCallback(async (url: string) => {
    try {
      setError(null)
      if (window.electronAPI) {
        await window.electronAPI.hub.reconnect(url)
      } else {
        await window.ipcRenderer.invoke('hub:reconnect', url)
      }
      await refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to reconnect')
    }
  }, [refresh])

  // Create a new agent
  const createAgent = useCallback(async (id?: string): Promise<AgentInfo | null> => {
    try {
      setError(null)
      const result = window.electronAPI
        ? await window.electronAPI.hub.createAgent(id)
        : await window.ipcRenderer.invoke('hub:createAgent', id)

      const typedResult = result as { error?: string; id?: string; closed?: boolean }
      if (typedResult.error) {
        setError(typedResult.error)
        return null
      }

      // Refresh agents list
      await refresh()

      return result as AgentInfo
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create agent')
      return null
    }
  }, [refresh])

  // Close an agent
  const closeAgent = useCallback(async (id: string): Promise<boolean> => {
    try {
      setError(null)
      const result = window.electronAPI
        ? await window.electronAPI.hub.closeAgent(id)
        : await window.ipcRenderer.invoke('hub:closeAgent', id)

      const typedResult = result as { ok?: boolean }
      if (!typedResult.ok) {
        setError(`Failed to close agent: ${id}`)
        return false
      }

      // Refresh agents list
      await refresh()

      return true
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to close agent')
      return false
    }
  }, [refresh])

  // Send message to agent
  const sendMessage = useCallback(async (agentId: string, content: string): Promise<boolean> => {
    try {
      setError(null)
      const result = window.electronAPI
        ? await window.electronAPI.hub.sendMessage(agentId, content)
        : await window.ipcRenderer.invoke('hub:sendMessage', agentId, content)

      const typedResult = result as { error?: string }
      if (typedResult.error) {
        setError(typedResult.error)
        return false
      }

      return true
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to send message')
      return false
    }
  }, [])

  return {
    hubInfo,
    agents,
    loading,
    error,
    initHub,
    refresh,
    reconnect,
    createAgent,
    closeAgent,
    sendMessage,
  }
}

export default useHub
