import { vi } from 'vitest'

// Mock electron module globally
vi.mock('electron', () => ({
  app: {
    getPath: vi.fn().mockReturnValue('/mock/user/data')
  }
}))
