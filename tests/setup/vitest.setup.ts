import { vi } from 'vitest'

// Mock electron module globally
vi.mock('electron', () => ({
  app: {
    getPath: vi.fn().mockReturnValue('/mock/user/data')
  }
}))

// Mock electron-log/main
vi.mock('electron-log/main', () => ({
  default: {
    initialize: vi.fn(),
    transports: {
      file: { level: false },
      console: { level: 'debug' }
    },
    errorHandler: {
      startCatching: vi.fn()
    },
    info: vi.fn(),
    warn: vi.fn(),
    error: vi.fn(),
    debug: vi.fn()
  }
}))

// Mock @electron-toolkit/utils
vi.mock('@electron-toolkit/utils', () => ({
  is: {
    dev: true
  }
}))
