import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import path from 'path'
import { FileWatcher } from '../../../../src/main/watcher/FileWatcher'
import { IPC_CHANNELS } from '../../../../src/shared/ipc-channels'
import * as git from '../../../../src/main/utils/git'
import * as fs from 'fs'

type WatchRecord = {
  path: string
  listener?: (eventType: string, filename?: string) => void
  handlers: Record<string, Array<(error: Error) => void>>
  close: ReturnType<typeof vi.fn>
}

const watchRecords: WatchRecord[] = []

vi.mock('fs', () => {
  const watch = vi.fn(
    (watchedPath: string, optionsOrListener: unknown, maybeListener?: unknown) => {
      const listener =
        typeof optionsOrListener === 'function'
          ? (optionsOrListener as (eventType: string, filename?: string) => void)
          : (maybeListener as (eventType: string, filename?: string) => void)

      const record: WatchRecord = {
        path: watchedPath,
        listener,
        handlers: {},
        close: vi.fn()
      }

      watchRecords.push(record)

      return {
        on: vi.fn((event: string, handler: (error: Error) => void) => {
          if (!record.handlers[event]) {
            record.handlers[event] = []
          }
          record.handlers[event].push(handler)
          return undefined
        }),
        close: record.close
      }
    }
  )

  return {
    watch,
    existsSync: vi.fn(() => true)
  }
})

vi.mock('../../../../src/main/utils/git', () => ({
  getGitHeadPath: vi.fn()
}))

describe('FileWatcher', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.clearAllMocks()
    watchRecords.length = 0
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('watches git HEAD and notifies renderer on changes', () => {
    const headPath = '/repo/.git/HEAD'
    vi.mocked(git.getGitHeadPath).mockReturnValue(headPath)

    const send = vi.fn()
    const mainWindow = {
      isDestroyed: () => false,
      webContents: { send }
    }

    const watcher = new FileWatcher({
      debounceMs: 0,
      getMainWindow: () => mainWindow
    })

    watcher.watch('session-1', '/repo')

    expect(fs.watch).toHaveBeenCalledWith('/repo', { recursive: true }, expect.any(Function))
    expect(fs.watch).toHaveBeenCalledWith(headPath, expect.any(Function))

    const headRecord = watchRecords.find((record) => record.path === headPath)
    expect(headRecord?.listener).toBeDefined()

    headRecord?.listener?.('change')
    vi.runAllTimers()

    expect(send).toHaveBeenCalledWith(IPC_CHANNELS.FS_FILE_CHANGED, {
      directory: path.dirname(headPath),
      eventType: 'change',
      path: headPath,
      sessionIds: ['session-1']
    })
  })

  it('closes git HEAD watcher after last session unwatch', () => {
    const headPath = '/repo/.git/HEAD'
    vi.mocked(git.getGitHeadPath).mockReturnValue(headPath)

    const mainWindow = {
      isDestroyed: () => false,
      webContents: { send: vi.fn() }
    }

    const watcher = new FileWatcher({
      debounceMs: 0,
      getMainWindow: () => mainWindow
    })

    watcher.watch('session-1', '/repo')
    watcher.watch('session-2', '/repo')

    const headRecord = watchRecords.find((record) => record.path === headPath)
    expect(headRecord).toBeDefined()

    watcher.unwatch('session-1')
    expect(headRecord?.close).not.toHaveBeenCalled()

    watcher.unwatch('session-2')
    expect(headRecord?.close).toHaveBeenCalledTimes(1)
  })
})
