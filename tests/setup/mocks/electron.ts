import { vi } from 'vitest'

export const mockElectron = {
  app: {
    getPath: vi.fn().mockReturnValue('/mock/user/data'),
    getName: vi.fn().mockReturnValue('multica'),
    getVersion: vi.fn().mockReturnValue('0.1.0')
  },
  ipcMain: {
    handle: vi.fn(),
    on: vi.fn()
  },
  BrowserWindow: vi.fn().mockImplementation(() => ({
    loadURL: vi.fn(),
    webContents: {
      send: vi.fn()
    }
  }))
}
