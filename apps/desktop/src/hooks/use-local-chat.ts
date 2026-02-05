import { useState, useEffect, useCallback, useRef } from 'react'
import { useChat } from '@multica/hooks/use-chat'
import type {
  StreamPayload,
  ExecApprovalRequestPayload,
  ApprovalDecision,
} from '@multica/sdk'

export function useLocalChat() {
  const chat = useChat()
  const chatRef = useRef(chat)
  chatRef.current = chat
  const [agentId, setAgentId] = useState<string | null>(null)
  const [isLoading, setIsLoading] = useState(false)
  const [isLoadingHistory, setIsLoadingHistory] = useState(true)
  const [initError, setInitError] = useState<string | null>(null)
  const initRef = useRef(false)

  // Initialize hub and get default agent ID
  useEffect(() => {
    if (initRef.current) return
    initRef.current = true

    window.electronAPI.hub.init()
      .then((result) => {
        const r = result as { defaultAgentId?: string }
        console.log('[LocalChat] hub.init → defaultAgentId:', r.defaultAgentId)
        if (r.defaultAgentId) {
          setAgentId(r.defaultAgentId)
        } else {
          setInitError('No default agent available')
          setIsLoadingHistory(false)
        }
      })
      .catch((err: Error) => {
        setInitError(err.message)
        setIsLoadingHistory(false)
      })
  }, [])

  // Subscribe to events + fetch history once agentId is available
  useEffect(() => {
    if (!agentId) return

    // Subscribe to agent events
    window.electronAPI.localChat.subscribe(agentId).catch(() => {})

    // Listen for stream events
    window.electronAPI.localChat.onEvent((data) => {
      // Cast IPC event to StreamPayload (same shape: { agentId, streamId, event })
      const payload = data as unknown as StreamPayload
      if (!payload.event) return

      chatRef.current.handleStream(payload)
      if (payload.event.type === 'message_start') setIsLoading(true)
      if (payload.event.type === 'message_end') setIsLoading(false)
    })

    // Listen for exec approval requests
    window.electronAPI.localChat.onApproval((approval) => {
      chatRef.current.addApproval(approval as ExecApprovalRequestPayload)
    })

    // Fetch history
    window.electronAPI.localChat.getHistory(agentId)
      .then((result) => {
        console.log('[LocalChat] getHistory result:', result.messages?.length, 'messages, sample:', result.messages?.[0])
        if (result.messages?.length) {
          chatRef.current.setHistory(result.messages as never[], agentId)
        }
      })
      .catch(() => {})
      .finally(() => setIsLoadingHistory(false))

    return () => {
      window.electronAPI.localChat.offEvent()
      window.electronAPI.localChat.offApproval()
      window.electronAPI.localChat.unsubscribe(agentId).catch(() => {})
    }
  }, [agentId])

  const sendMessage = useCallback(
    (text: string) => {
      const trimmed = text.trim()
      if (!trimmed || !agentId) return
      chatRef.current.addUserMessage(trimmed, agentId)
      chatRef.current.setError(null)
      window.electronAPI.localChat.send(agentId, trimmed).catch(() => {})
      setIsLoading(true)
    },
    [agentId],
  )

  const resolveApproval = useCallback(
    (approvalId: string, decision: ApprovalDecision) => {
      chatRef.current.removeApproval(approvalId)
      window.electronAPI.localChat.resolveExecApproval(approvalId, decision).catch(() => {})
    },
    [],
  )

  return {
    agentId,
    initError,
    messages: chat.messages,
    streamingIds: chat.streamingIds,
    isLoading,
    isLoadingHistory,
    error: chat.error,
    pendingApprovals: chat.pendingApprovals,
    sendMessage,
    resolveApproval,
  }
}
