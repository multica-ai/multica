import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { MessageContent } from '../../../../src/shared/types/message'
import type { Conductor } from '../../../../src/main/conductor/Conductor'
import type { FileWatcher } from '../../../../src/main/watcher'
import { IPC_CHANNELS } from '../../../../src/shared/ipc-channels'
import { registerIPCHandlers } from '../../../../src/main/ipc/handlers'
import { generateSessionTitle } from '../../../../src/main/conductor/TitleGenerator'

const { ipcMainHandleMock, ipcMainOnMock } = vi.hoisted(() => ({
  ipcMainHandleMock: vi.fn(),
  ipcMainOnMock: vi.fn()
}))

vi.mock('electron', () => ({
  ipcMain: {
    handle: ipcMainHandleMock,
    on: ipcMainOnMock
  },
  dialog: {
    showOpenDialog: vi.fn()
  },
  clipboard: {
    writeText: vi.fn()
  },
  shell: {
    openExternal: vi.fn(),
    showItemInFolder: vi.fn()
  },
  BrowserWindow: vi.fn()
}))

vi.mock('../../../../src/main/conductor/TitleGenerator', () => ({
  generateSessionTitle: vi.fn()
}))

const flushPromises = (): Promise<void> => new Promise((resolve) => setImmediate(resolve))

const getHandler = (channel: string): ((...args: unknown[]) => Promise<unknown>) => {
  const call = ipcMainHandleMock.mock.calls.find(([registered]) => registered === channel)
  if (!call) {
    throw new Error(`Handler not registered for ${channel}`)
  }
  return call[1]
}

describe('IPC session title generation', () => {
  beforeEach(() => {
    ipcMainHandleMock.mockClear()
    ipcMainOnMock.mockClear()
    vi.mocked(generateSessionTitle).mockReset()
  })

  it('avoids concurrent title generation for the same session', async () => {
    const conductor = {
      getSessionData: vi.fn().mockResolvedValue({
        session: { title: undefined, agentId: 'opencode' }
      }),
      updateSessionMeta: vi.fn().mockResolvedValue({ id: 'session-1' }),
      sendPrompt: vi.fn().mockResolvedValue('stop')
    } as unknown as Conductor

    const fileWatcher = {} as FileWatcher
    const getMainWindow = (): null => null

    let resolveTitle: (value: string) => void
    const titlePromise = new Promise<string>((resolve) => {
      resolveTitle = resolve
    })

    vi.mocked(generateSessionTitle).mockReturnValue(titlePromise)

    registerIPCHandlers(conductor, fileWatcher, getMainWindow)
    const handler = getHandler(IPC_CHANNELS.AGENT_PROMPT)

    const content: MessageContent = [{ type: 'text', text: 'hello' }]

    await handler({} as Electron.IpcMainInvokeEvent, 'session-1', content)
    await handler({} as Electron.IpcMainInvokeEvent, 'session-1', content)

    await flushPromises()

    expect(generateSessionTitle).toHaveBeenCalledTimes(1)

    resolveTitle('Auto Title')
    await flushPromises()

    expect(conductor.updateSessionMeta).toHaveBeenCalledTimes(1)
  })

  it('does not overwrite a manual title set during generation', async () => {
    const conductor = {
      getSessionData: vi
        .fn()
        .mockResolvedValueOnce({ session: { title: undefined, agentId: 'opencode' } })
        .mockResolvedValueOnce({ session: { title: 'Manual Title', agentId: 'opencode' } }),
      updateSessionMeta: vi.fn().mockResolvedValue({ id: 'session-1' }),
      sendPrompt: vi.fn().mockResolvedValue('stop')
    } as unknown as Conductor

    const fileWatcher = {} as FileWatcher
    const getMainWindow = (): null => null

    vi.mocked(generateSessionTitle).mockResolvedValue('Auto Title')

    registerIPCHandlers(conductor, fileWatcher, getMainWindow)
    const handler = getHandler(IPC_CHANNELS.AGENT_PROMPT)

    const content: MessageContent = [{ type: 'text', text: 'hello' }]

    await handler({} as Electron.IpcMainInvokeEvent, 'session-1', content)
    await flushPromises()

    expect(generateSessionTitle).toHaveBeenCalledTimes(1)
    expect(conductor.updateSessionMeta).not.toHaveBeenCalled()
  })
})
