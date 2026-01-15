/**
 * AcpClientFactory - Creates ACP SDK Client implementations
 *
 * Handles session update notifications and permission requests from agents.
 */
import type {
  Client,
  SessionNotification,
  RequestPermissionRequest,
  RequestPermissionResponse
} from '@agentclientprotocol/sdk'
import type { SessionStore } from '../session/SessionStore'

export interface AcpClientCallbacks {
  onSessionUpdate?: (update: SessionNotification, sequenceNumber?: number) => void
  onPermissionRequest?: (params: RequestPermissionRequest) => Promise<RequestPermissionResponse>
}

export interface AcpClientFactoryOptions {
  sessionStore: SessionStore | null
  callbacks: AcpClientCallbacks
}

/**
 * Create an ACP Client implementation for a session
 *
 * @param sessionId - The Multica session ID (used for persistence)
 * @param options - Factory options including store and callbacks
 */
export function createAcpClient(sessionId: string, options: AcpClientFactoryOptions): Client {
  const { sessionStore, callbacks } = options

  return {
    // Handle session updates from agent
    sessionUpdate: async (params: SessionNotification) => {
      // Log the update type
      const update = params.update
      if ('sessionUpdate' in update) {
        const updateType = update.sessionUpdate
        if (updateType === 'agent_message_chunk') {
          const contentType = update.content?.type || 'unknown'
          const textPreview =
            update.content?.type === 'text' ? update.content.text?.slice(0, 50) : ''
          console.log(`[ACP] ${updateType} (${contentType}): "${textPreview}"`)
        } else if (updateType === 'agent_thought_chunk') {
          const textPreview =
            update.content?.type === 'text' ? update.content.text?.slice(0, 50) : ''
          console.log(`[ACP] ${updateType}: "${textPreview}"`)
        } else if (updateType === 'tool_call') {
          console.log(`[ACP] ${updateType}: ${update.title} [${update.status}]`)
        } else if (updateType === 'tool_call_update') {
          console.log(
            `[ACP] ${updateType}: ${update.title || update.toolCallId} [${update.status}]`
          )
        } else {
          // Log other types briefly
          console.log(`[ACP] ${updateType}`)
        }
      } else {
        console.log(`[ACP] raw update:`, params)
      }

      // Store raw update to SessionStore (if available) and get sequence number
      let sequenceNumber: number | undefined
      if (sessionStore) {
        try {
          const storedUpdate = await sessionStore.appendUpdate(sessionId, params)
          sequenceNumber = storedUpdate.sequenceNumber
        } catch (err) {
          console.error('[Conductor] Failed to store session update:', err)
        }
      }

      // Trigger UI callback with sequence number for ordering
      if (callbacks.onSessionUpdate) {
        callbacks.onSessionUpdate(params, sequenceNumber)
      }
    },

    // Handle permission requests from agent
    requestPermission: async (
      params: RequestPermissionRequest
    ): Promise<RequestPermissionResponse> => {
      if (callbacks.onPermissionRequest) {
        return callbacks.onPermissionRequest(params)
      }
      // Default: auto-approve (V1 simplification)
      console.log(`[Conductor] Auto-approving: ${params.toolCall.title}`)
      return {
        outcome: {
          outcome: 'selected',
          optionId: params.options[0]?.optionId ?? ''
        }
      }
    }
  }
}
