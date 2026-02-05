/**
 * Hub IPC handlers for Electron main process.
 *
 * Creates and manages a Hub instance that connects to the Gateway.
 * This follows the same pattern as the Console app.
 */
import { ipcMain, type BrowserWindow } from 'electron'
import { Hub } from '../../../../src/hub/hub.js'
import type { ConnectionState } from '@multica/sdk'
import type { AsyncAgent } from '../../../../src/agent/async-agent.js'

// Singleton Hub instance
let hub: Hub | null = null
let defaultAgentId: string | null = null
let mainWindowRef: BrowserWindow | null = null

// Track which agents have active IPC subscriptions (for local direct chat)
// Value is the unsubscribe function returned by agent.subscribe()
const ipcAgentSubscriptions = new Map<string, () => void>()

/**
 * Safe log function that catches EPIPE errors.
 * Electron main process stdout can be closed unexpectedly.
 */
function safeLog(...args: unknown[]): void {
  try {
    console.log(...args)
  } catch {
    // Ignore EPIPE errors when stdout is closed
  }
}

/**
 * Initialize Hub on app startup.
 * Creates Hub and a default Agent automatically.
 */
export async function initializeHub(): Promise<void> {
  if (hub) {
    safeLog('[Desktop] Hub already initialized')
    return
  }

  const gatewayUrl = process.env['GATEWAY_URL'] ?? 'http://localhost:3000'
  safeLog(`[Desktop] Initializing Hub, connecting to Gateway: ${gatewayUrl}`)

  hub = new Hub(gatewayUrl)

  // Create default agent if none exists
  const agents = hub.listAgents()
  if (agents.length === 0) {
    safeLog('[Desktop] Creating default agent...')
    const agent = hub.createAgent()
    defaultAgentId = agent.sessionId
    safeLog(`[Desktop] Default agent created: ${defaultAgentId}`)
  } else {
    defaultAgentId = agents[0]
    safeLog(`[Desktop] Using existing agent: ${defaultAgentId}`)
  }
}

/**
 * Get or create the Hub instance.
 */
function getHub(): Hub {
  if (!hub) {
    const gatewayUrl = process.env['GATEWAY_URL'] ?? 'http://localhost:3000'
    safeLog(`[Desktop] Creating Hub, connecting to Gateway: ${gatewayUrl}`)
    hub = new Hub(gatewayUrl)
  }
  return hub
}

/**
 * Get the default agent.
 */
function getDefaultAgent(): AsyncAgent | null {
  if (!hub || !defaultAgentId) return null
  return hub.getAgent(defaultAgentId) ?? null
}

/**
 * Hub info returned to renderer.
 */
export interface HubInfo {
  hubId: string
  url: string
  connectionState: ConnectionState
  agentCount: number
}

/**
 * Agent info returned to renderer.
 */
export interface AgentInfo {
  id: string
  closed: boolean
}

/**
 * Register all Hub-related IPC handlers.
 */
export function registerHubIpcHandlers(): void {
  /**
   * Initialize the Hub (creates singleton if not exists).
   */
  ipcMain.handle('hub:init', async () => {
    await initializeHub()
    const h = getHub()
    return {
      hubId: h.hubId,
      url: h.url,
      connectionState: h.connectionState,
      defaultAgentId,
    }
  })

  /**
   * Get Hub status info.
   */
  ipcMain.handle('hub:info', async (): Promise<HubInfo> => {
    const h = getHub()
    return {
      hubId: h.hubId,
      url: h.url,
      connectionState: h.connectionState,
      agentCount: h.listAgents().length,
    }
  })

  /**
   * Get Hub status with default agent info (for home page).
   */
  ipcMain.handle('hub:getStatus', async () => {
    const h = getHub()
    const agent = getDefaultAgent()

    return {
      hubId: h.hubId,
      status: h.connectionState === 'connected' ? 'ready' : h.connectionState,
      agentCount: h.listAgents().length,
      gatewayConnected: h.connectionState === 'connected',
      gatewayUrl: h.url,
      defaultAgent: agent
        ? {
            agentId: agent.sessionId,
            status: agent.closed ? 'closed' : 'idle',
          }
        : null,
    }
  })

  /**
   * Get default agent info.
   */
  ipcMain.handle('hub:getAgentInfo', async () => {
    const agent = getDefaultAgent()
    if (!agent) {
      return null
    }
    return {
      agentId: agent.sessionId,
      status: agent.closed ? 'closed' : 'idle',
    }
  })

  /**
   * Reconnect Hub to a different Gateway URL.
   */
  ipcMain.handle('hub:reconnect', async (_event, url: string) => {
    const h = getHub()
    h.reconnect(url)
    return { url: h.url }
  })

  /**
   * List all agents.
   */
  ipcMain.handle('hub:listAgents', async (): Promise<AgentInfo[]> => {
    const h = getHub()
    const agentIds = h.listAgents()
    return agentIds.map((id) => {
      const agent = h.getAgent(id)
      return {
        id,
        closed: agent?.closed ?? true,
      }
    })
  })

  /**
   * Create a new agent.
   */
  ipcMain.handle('hub:createAgent', async (_event, id?: string) => {
    const h = getHub()
    const agent = h.createAgent(id)
    return {
      id: agent.sessionId,
      closed: agent.closed,
    }
  })

  /**
   * Get a specific agent.
   */
  ipcMain.handle('hub:getAgent', async (_event, id: string) => {
    const h = getHub()
    const agent = h.getAgent(id)
    if (!agent) {
      return { error: `Agent not found: ${id}` }
    }
    return {
      id: agent.sessionId,
      closed: agent.closed,
    }
  })

  /**
   * Close/delete an agent.
   */
  ipcMain.handle('hub:closeAgent', async (_event, id: string) => {
    const h = getHub()
    const result = h.closeAgent(id)
    return { ok: result }
  })

  /**
   * Send a message to an agent (for remote clients via Gateway).
   * Note: For local direct chat, use 'localChat:send' instead.
   */
  ipcMain.handle('hub:sendMessage', async (_event, agentId: string, content: string) => {
    const h = getHub()
    const agent = h.getAgent(agentId)
    if (!agent) {
      return { error: `Agent not found: ${agentId}` }
    }
    if (agent.closed) {
      return { error: `Agent is closed: ${agentId}` }
    }
    agent.write(content)
    return { ok: true }
  })

  /**
   * Subscribe to local agent events (for direct IPC chat without Gateway).
   * Uses agent.subscribe() which supports multiple subscribers.
   */
  ipcMain.handle('localChat:subscribe', async (_event, agentId: string) => {
    const h = getHub()
    const agent = h.getAgent(agentId)
    if (!agent) {
      return { error: `Agent not found: ${agentId}` }
    }
    if (agent.closed) {
      return { error: `Agent is closed: ${agentId}` }
    }

    // Already subscribed?
    if (ipcAgentSubscriptions.has(agentId)) {
      return { ok: true, alreadySubscribed: true }
    }

    // Track current stream ID for message grouping
    let currentStreamId: string | null = null

    // Subscribe to agent events using the multi-subscriber mechanism
    const unsubscribe = agent.subscribe((event) => {
      if (!mainWindowRef || mainWindowRef.isDestroyed()) {
        return
      }

      // Compaction events: forward with no stream tracking
      const isCompactionEvent =
        event.type === 'compaction_start' || event.type === 'compaction_end'
      if (isCompactionEvent) {
        safeLog(`[IPC] Sending compaction event to renderer: ${event.type}`)
        mainWindowRef.webContents.send('localChat:event', {
          agentId,
          streamId: null,
          event,
        })
        return
      }

      // Filter events same as Hub.consumeAgent()
      const maybeMessage = (event as { message?: { role?: string } }).message
      const isAssistantMessage = maybeMessage?.role === 'assistant'
      const shouldForward =
        ((event.type === 'message_start' || event.type === 'message_update' || event.type === 'message_end') && isAssistantMessage)
        || event.type === 'tool_execution_start'
        || event.type === 'tool_execution_end'

      if (!shouldForward) return

      // Track stream ID for message grouping (extract from event.message.id, same as Hub.beginStream)
      if (event.type === 'message_start') {
        const msgId = (event as { message?: { id?: string } }).message?.id
        currentStreamId = msgId ?? `stream-${Date.now()}`
        safeLog(`[IPC] Starting stream: ${currentStreamId}`)
      }

      safeLog(`[IPC] Sending event to renderer: ${event.type}, streamId: ${currentStreamId}`)
      mainWindowRef.webContents.send('localChat:event', {
        agentId,
        streamId: currentStreamId,
        event,
      })

      if (event.type === 'message_end') {
        safeLog(`[IPC] Ending stream: ${currentStreamId}`)
        currentStreamId = null
      }
    })

    ipcAgentSubscriptions.set(agentId, unsubscribe)

    // Register local approval handler so exec approval requests route via IPC
    h.setLocalApprovalHandler(agentId, (payload) => {
      if (!mainWindowRef || mainWindowRef.isDestroyed()) return
      safeLog(`[IPC] Sending approval request to renderer: ${payload.approvalId}`)
      mainWindowRef.webContents.send('localChat:approval', payload)
    })

    safeLog(`[IPC] Local chat subscribed to agent: ${agentId}`)

    return { ok: true }
  })

  /**
   * Unsubscribe from local agent events.
   */
  ipcMain.handle('localChat:unsubscribe', async (_event, agentId: string) => {
    const unsubscribe = ipcAgentSubscriptions.get(agentId)
    if (unsubscribe) {
      unsubscribe()
    }
    ipcAgentSubscriptions.delete(agentId)
    getHub().removeLocalApprovalHandler(agentId)
    safeLog(`[IPC] Local chat unsubscribed from agent: ${agentId}`)
    return { ok: true }
  })

  /**
   * Get message history for local chat.
   * Returns raw AgentMessageItem[] so the renderer can render content blocks,
   * tool results, thinking blocks, etc. — same format as the Gateway RPC.
   */
  ipcMain.handle('localChat:getHistory', async (_event, agentId: string) => {
    const h = getHub()
    const agent = h.getAgent(agentId)
    if (!agent) {
      return { messages: [] }
    }

    try {
      await agent.ensureInitialized()
      const messages = agent.getMessages()
      return { messages }
    } catch {
      return { messages: [] }
    }
  })

  /**
   * Send a message via local direct IPC (no Gateway).
   * Events will be pushed to renderer via 'localChat:event' channel.
   */
  ipcMain.handle('localChat:send', async (_event, agentId: string, content: string) => {
    const h = getHub()
    const agent = h.getAgent(agentId)
    if (!agent) {
      return { error: `Agent not found: ${agentId}` }
    }
    if (agent.closed) {
      return { error: `Agent is closed: ${agentId}` }
    }

    // Must be subscribed first to receive events
    if (!ipcAgentSubscriptions.has(agentId)) {
      return { error: 'Not subscribed to agent events. Call subscribe first.' }
    }

    agent.write(content)
    safeLog(`[IPC] Local chat message sent to agent: ${agentId}`)
    return { ok: true }
  })

  /**
   * Resolve an exec approval request for local chat.
   */
  ipcMain.handle('localChat:resolveExecApproval', async (_event, approvalId: string, decision: string) => {
    const h = getHub()
    const ok = h.resolveExecApproval(approvalId, decision as 'allow-once' | 'allow-always' | 'deny')
    return { ok }
  })

  /**
   * Register a one-time token for device verification.
   * Called by the QR code component when a token is generated or refreshed.
   */
  ipcMain.handle('hub:registerToken', async (_event, token: string, agentId: string, expiresAt: number) => {
    const h = getHub()
    h.registerToken(token, agentId, expiresAt)
    return { ok: true }
  })

  /**
   * List all verified (whitelisted) devices.
   */
  ipcMain.handle('hub:listDevices', async () => {
    const h = getHub()
    return h.deviceStore.listDevices()
  })

  /**
   * Revoke a device from the whitelist.
   */
  ipcMain.handle('hub:revokeDevice', async (_event, deviceId: string) => {
    const h = getHub()
    const ok = h.deviceStore.revokeDevice(deviceId)
    // Notify renderer that device list changed
    if (ok && mainWindowRef && !mainWindowRef.isDestroyed()) {
      mainWindowRef.webContents.send('hub:devices-changed')
    }
    return { ok }
  })

}

/**
 * Set up device confirmation flow between Hub (main process) and renderer.
 * Also stores window reference for local chat IPC events.
 * Must be called after both Hub initialization and window creation.
 */
export function setupDeviceConfirmation(mainWindow: Electron.BrowserWindow): void {
  // Store reference for local chat IPC
  mainWindowRef = mainWindow
  const h = getHub()
  const pendingConfirms = new Map<string, (allowed: boolean) => void>()

  // Listen for renderer responses to device confirm dialogs
  ipcMain.on('hub:device-confirm-response', (_event, deviceId: string, allowed: boolean) => {
    const resolve = pendingConfirms.get(deviceId)
    if (resolve) {
      pendingConfirms.delete(deviceId)
      resolve(allowed)
    }
  })

  // Register confirm handler on Hub — sends request to renderer, awaits response
  h.setConfirmHandler((deviceId: string, _agentId: string, meta) => {
    return new Promise<boolean>((resolve) => {
      // Auto-reject if user doesn't respond within 60 seconds
      const timeout = setTimeout(() => {
        pendingConfirms.delete(deviceId)
        resolve(false)
      }, 60_000)
      pendingConfirms.set(deviceId, (allowed: boolean) => {
        clearTimeout(timeout)
        resolve(allowed)
        // Notify renderer that device list changed when a device is approved
        if (allowed && !mainWindow.isDestroyed()) {
          mainWindow.webContents.send('hub:devices-changed')
        }
      })
      mainWindow.webContents.send('hub:device-confirm-request', deviceId, meta)
    })
  })

  // Forward connection state changes to renderer
  h.onConnectionStateChange((state) => {
    if (!mainWindow.isDestroyed()) {
      mainWindow.webContents.send('hub:connection-state-changed', state)
    }
  })
}

/**
 * Cleanup Hub resources.
 */
export function cleanupHub(): void {
  // Unsubscribe all IPC listeners
  for (const unsubscribe of ipcAgentSubscriptions.values()) {
    unsubscribe()
  }
  ipcAgentSubscriptions.clear()

  if (hub) {
    safeLog('[Desktop] Shutting down Hub')
    hub.shutdown()
    hub = null
  }
}

/**
 * Get the current Hub instance (for use by other IPC modules).
 */
export function getCurrentHub(): Hub | null {
  return hub
}
