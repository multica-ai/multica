import { vi } from 'vitest'

export const createMockAcpClient = () => ({
  createSession: vi.fn().mockResolvedValue({
    sessionId: 'mock-session-id'
  }),
  sendMessage: vi.fn().mockResolvedValue(undefined),
  cancelSession: vi.fn().mockResolvedValue(undefined),
  close: vi.fn().mockResolvedValue(undefined)
})

export const createMockSessionNotification = (
  type: 'agent_message_chunk' | 'user_message_chunk' | 'session_end',
  data: Record<string, unknown> = {}
) => ({
  sessionId: 'mock-session-id',
  update: {
    sessionUpdate: type,
    ...data
  }
})
