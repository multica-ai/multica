/**
 * AcpClientFactory - Creates ACP SDK Client implementations
 *
 * Handles session update notifications and permission requests from agents.
 */
import type {
  Client,
  SessionNotification,
  RequestPermissionRequest,
  RequestPermissionResponse,
  SessionModeId,
  ModelId
} from '@agentclientprotocol/sdk'
import type { AvailableCommand } from '@agentclientprotocol/sdk/dist/schema/types.gen'
import type { ISessionStore } from './types'

export interface AcpClientCallbacks {
  onSessionUpdate?: (
    update: SessionNotification,
    multicaSessionId: string,
    sequenceNumber?: number
  ) => void
  onPermissionRequest?: (params: RequestPermissionRequest) => Promise<RequestPermissionResponse>
  /** Called when server sends a mode update notification */
  onModeUpdate?: (modeId: SessionModeId) => void
  /** Called when server sends a model update notification */
  onModelUpdate?: (modelId: ModelId) => void
  /** Called when server sends available commands update */
  onAvailableCommandsUpdate?: (commands: AvailableCommand[]) => void
}

export interface AcpClientFactoryOptions {
  sessionStore: ISessionStore | null
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
        // Handle mode update notification
        if (updateType === 'current_mode_update' && callbacks.onModeUpdate) {
          const modeUpdate = update as { currentModeId?: SessionModeId }
          if (modeUpdate.currentModeId) {
            callbacks.onModeUpdate(modeUpdate.currentModeId)
          }
        }
        // Handle available commands update notification
        if (updateType === 'available_commands_update' && callbacks.onAvailableCommandsUpdate) {
          const commandsUpdate = update as { availableCommands?: AvailableCommand[] }
          if (commandsUpdate.availableCommands) {
            callbacks.onAvailableCommandsUpdate(commandsUpdate.availableCommands)
          }
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

      // Trigger UI callback with Multica session ID and sequence number for ordering
      if (callbacks.onSessionUpdate) {
        callbacks.onSessionUpdate(params, sessionId, sequenceNumber)
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
