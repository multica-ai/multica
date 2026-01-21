/**
 * Tests for uiStore - specifically showHiddenFiles state
 */
import { beforeEach, describe, expect, it } from 'vitest'
import {
  useUIStore,
  SIDEBAR_MIN_WIDTH,
  SIDEBAR_MAX_WIDTH,
  RIGHT_PANEL_MIN_WIDTH,
  RIGHT_PANEL_MAX_WIDTH
} from '../../../../src/renderer/src/stores/uiStore'

describe('uiStore', () => {
  beforeEach(() => {
    // Reset to default state
    useUIStore.setState({
      sidebarOpen: true,
      sidebarWidth: 256,
      rightPanelOpen: true,
      rightPanelWidth: 320,
      showHiddenFiles: false
    })
  })

  describe('showHiddenFiles', () => {
    it('defaults to false (hidden files are not shown)', () => {
      expect(useUIStore.getState().showHiddenFiles).toBe(false)
    })

    it('toggles showHiddenFiles from false to true', () => {
      useUIStore.getState().toggleShowHiddenFiles()
      expect(useUIStore.getState().showHiddenFiles).toBe(true)
    })

    it('toggles showHiddenFiles from true to false', () => {
      useUIStore.setState({ showHiddenFiles: true })
      useUIStore.getState().toggleShowHiddenFiles()
      expect(useUIStore.getState().showHiddenFiles).toBe(false)
    })

    it('toggles multiple times correctly', () => {
      expect(useUIStore.getState().showHiddenFiles).toBe(false)

      useUIStore.getState().toggleShowHiddenFiles()
      expect(useUIStore.getState().showHiddenFiles).toBe(true)

      useUIStore.getState().toggleShowHiddenFiles()
      expect(useUIStore.getState().showHiddenFiles).toBe(false)

      useUIStore.getState().toggleShowHiddenFiles()
      expect(useUIStore.getState().showHiddenFiles).toBe(true)
    })
  })

  describe('sidebar state', () => {
    it('toggles sidebar open state', () => {
      expect(useUIStore.getState().sidebarOpen).toBe(true)
      useUIStore.getState().toggleSidebar()
      expect(useUIStore.getState().sidebarOpen).toBe(false)
    })

    it('sets sidebar open state directly', () => {
      useUIStore.getState().setSidebarOpen(false)
      expect(useUIStore.getState().sidebarOpen).toBe(false)
    })

    it('clamps sidebar width to min/max constraints', () => {
      useUIStore.getState().setSidebarWidth(100) // below min
      expect(useUIStore.getState().sidebarWidth).toBe(SIDEBAR_MIN_WIDTH)

      useUIStore.getState().setSidebarWidth(1000) // above max
      expect(useUIStore.getState().sidebarWidth).toBe(SIDEBAR_MAX_WIDTH)

      useUIStore.getState().setSidebarWidth(300) // within range
      expect(useUIStore.getState().sidebarWidth).toBe(300)
    })
  })

  describe('right panel state', () => {
    it('toggles right panel open state', () => {
      expect(useUIStore.getState().rightPanelOpen).toBe(true)
      useUIStore.getState().toggleRightPanel()
      expect(useUIStore.getState().rightPanelOpen).toBe(false)
    })

    it('sets right panel open state directly', () => {
      useUIStore.getState().setRightPanelOpen(false)
      expect(useUIStore.getState().rightPanelOpen).toBe(false)
    })

    it('clamps right panel width to min/max constraints', () => {
      useUIStore.getState().setRightPanelWidth(100) // below min
      expect(useUIStore.getState().rightPanelWidth).toBe(RIGHT_PANEL_MIN_WIDTH)

      useUIStore.getState().setRightPanelWidth(1000) // above max
      expect(useUIStore.getState().rightPanelWidth).toBe(RIGHT_PANEL_MAX_WIDTH)

      useUIStore.getState().setRightPanelWidth(350) // within range
      expect(useUIStore.getState().rightPanelWidth).toBe(350)
    })
  })
})
