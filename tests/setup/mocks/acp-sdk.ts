import { vi } from 'vitest'

// eslint-disable-next-line @typescript-eslint/explicit-function-return-type
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
  // eslint-disable-next-line @typescript-eslint/explicit-function-return-type
) => ({
  sessionId: 'mock-session-id',
  update: {
    sessionUpdate: type,
    ...data
  }
})
