import { describe, it, expect, vi, beforeEach, afterEach, type Mock } from 'vitest'

// Mock electron - use factory function that returns fresh mock
vi.mock('electron', () => ({
  app: {
    setBadgeCount: vi.fn()
  }
}))

// Mock crypto.randomUUID to return predictable IDs
vi.mock('crypto', () => ({
  randomUUID: vi.fn().mockReturnValue('test-request-id-1')
}))

import { PermissionManager } from '../../../../src/main/permission/PermissionManager'
import type { Conductor } from '../../../../src/main/conductor/Conductor'
import type { BrowserWindow } from 'electron'
import { app } from 'electron'
import { randomUUID } from 'crypto'

describe('PermissionManager', () => {
  let permissionManager: PermissionManager
  let mockConductor: Conductor
  let mockMainWindow: BrowserWindow
  let mockSend: Mock
  let getMainWindow: () => BrowserWindow | null
  const originalPlatform = process.platform

  // Type assertion for mocked app
  const mockSetBadgeCount = app.setBadgeCount as Mock
  const mockRandomUUID = randomUUID as Mock

  beforeEach(() => {
    vi.clearAllMocks()

    mockSend = vi.fn()
    mockMainWindow = {
      isDestroyed: vi.fn().mockReturnValue(false),
      webContents: {
        send: mockSend
      }
    } as unknown as BrowserWindow

    getMainWindow = () => mockMainWindow

    mockConductor = {
      getMulticaSessionIdByAcp: vi.fn().mockReturnValue('multica-session-123'),
      storeAskUserQuestionResponse: vi.fn()
    } as unknown as Conductor

    permissionManager = new PermissionManager(mockConductor, getMainWindow)
  })

  afterEach(() => {
    // Restore platform
    Object.defineProperty(process, 'platform', { value: originalPlatform })
  })

  describe('dock badge count (macOS)', () => {
    beforeEach(() => {
      // Set platform to darwin for macOS tests
      Object.defineProperty(process, 'platform', { value: 'darwin' })
      // Recreate permission manager to pick up platform change
      permissionManager = new PermissionManager(mockConductor, getMainWindow)
    })

    it('should set badge count to 1 when first permission request is added', async () => {
      // Set up predictable request ID
      mockRandomUUID.mockReturnValueOnce('request-1')

      const requestPromise = permissionManager.handlePermissionRequest({
        sessionId: 'acp-session-1',
        toolCall: {
          toolCallId: 'tool-1',
          title: 'Run bash command',
          kind: 'bash',
          status: 'pending'
        },
        options: [
          { optionId: 'allow', name: 'Allow', kind: 'allow' },
          { optionId: 'deny', name: 'Deny', kind: 'deny' }
        ]
      })

      // Wait a tick for async operations
      await vi.waitFor(() => {
        expect(mockSend).toHaveBeenCalled()
      })

      // Badge should be set to 1 after request is added
      expect(mockSetBadgeCount).toHaveBeenCalledWith(1)

      // Simulate response to clean up
      permissionManager.handlePermissionResponse({
        requestId: 'request-1',
        optionId: 'allow'
      })

      await requestPromise
    })

    it('should set badge count to 0 after permission response', async () => {
      // Set up predictable request ID
      mockRandomUUID.mockReturnValueOnce('request-2')

      const requestPromise = permissionManager.handlePermissionRequest({
        sessionId: 'acp-session-1',
        toolCall: {
          toolCallId: 'tool-1',
          title: 'Run bash command',
          kind: 'bash',
          status: 'pending'
        },
        options: [
          { optionId: 'allow', name: 'Allow', kind: 'allow' },
          { optionId: 'deny', name: 'Deny', kind: 'deny' }
        ]
      })

      // Wait for request to be sent
      await vi.waitFor(() => {
        expect(mockSend).toHaveBeenCalled()
      })

      // Clear mocks to check badge update after response
      mockSetBadgeCount.mockClear()

      // Simulate response
      permissionManager.handlePermissionResponse({
        requestId: 'request-2',
        optionId: 'allow'
      })

      await requestPromise

      // Badge should be set to 0 after response
      expect(mockSetBadgeCount).toHaveBeenCalledWith(0)
    })

    it('should increment badge count for multiple pending requests', async () => {
      // Set up predictable request IDs
      mockRandomUUID.mockReturnValueOnce('request-3').mockReturnValueOnce('request-4')

      // First request
      const request1Promise = permissionManager.handlePermissionRequest({
        sessionId: 'acp-session-1',
        toolCall: {
          toolCallId: 'tool-1',
          title: 'Run bash command',
          kind: 'bash',
          status: 'pending'
        },
        options: [
          { optionId: 'allow', name: 'Allow', kind: 'allow' },
          { optionId: 'deny', name: 'Deny', kind: 'deny' }
        ]
      })

      // Wait for first request
      await vi.waitFor(() => {
        expect(mockSetBadgeCount).toHaveBeenCalledWith(1)
      })

      // Second request
      const request2Promise = permissionManager.handlePermissionRequest({
        sessionId: 'acp-session-2',
        toolCall: {
          toolCallId: 'tool-2',
          title: 'Write file',
          kind: 'write',
          status: 'pending'
        },
        options: [
          { optionId: 'allow', name: 'Allow', kind: 'allow' },
          { optionId: 'deny', name: 'Deny', kind: 'deny' }
        ]
      })

      // Wait for second request
      await vi.waitFor(() => {
        expect(mockSetBadgeCount).toHaveBeenCalledWith(2)
      })

      // Clean up - respond to both
      permissionManager.handlePermissionResponse({ requestId: 'request-3', optionId: 'allow' })
      expect(mockSetBadgeCount).toHaveBeenLastCalledWith(1)

      permissionManager.handlePermissionResponse({ requestId: 'request-4', optionId: 'allow' })
      expect(mockSetBadgeCount).toHaveBeenLastCalledWith(0)

      await Promise.all([request1Promise, request2Promise])
    })
  })

  describe('dock badge count (non-macOS)', () => {
    beforeEach(() => {
      // Set platform to linux
      Object.defineProperty(process, 'platform', { value: 'linux' })
      // Recreate permission manager to pick up platform change
      permissionManager = new PermissionManager(mockConductor, getMainWindow)
    })

    it('should not call setBadgeCount on non-macOS platforms', async () => {
      // Set up predictable request ID
      mockRandomUUID.mockReturnValueOnce('request-5')

      const requestPromise = permissionManager.handlePermissionRequest({
        sessionId: 'acp-session-1',
        toolCall: {
          toolCallId: 'tool-1',
          title: 'Run bash command',
          kind: 'bash',
          status: 'pending'
        },
        options: [
          { optionId: 'allow', name: 'Allow', kind: 'allow' },
          { optionId: 'deny', name: 'Deny', kind: 'deny' }
        ]
      })

      // Wait for request to be sent
      await vi.waitFor(() => {
        expect(mockSend).toHaveBeenCalled()
      })

      // Should not call setBadgeCount on Linux
      expect(mockSetBadgeCount).not.toHaveBeenCalled()

      // Clean up
      permissionManager.handlePermissionResponse({ requestId: 'request-5', optionId: 'allow' })

      await requestPromise

      // Still should not have called setBadgeCount
      expect(mockSetBadgeCount).not.toHaveBeenCalled()
    })
  })
})
