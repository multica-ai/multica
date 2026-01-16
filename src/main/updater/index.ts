/**
 * Auto-updater module using electron-updater
 * Checks for updates from GitHub releases and handles download/install
 */
import { autoUpdater, UpdateInfo, ProgressInfo } from 'electron-updater'
import { BrowserWindow } from 'electron'
import { IPC_CHANNELS } from '../../shared/ipc-channels'

export interface UpdateStatus {
  status: 'checking' | 'available' | 'not-available' | 'downloading' | 'downloaded' | 'error'
  info?: UpdateInfo
  progress?: ProgressInfo
  error?: string
}

export class AutoUpdater {
  private mainWindow: (() => BrowserWindow | null) | null = null

  constructor(forceDevUpdateConfig = false) {
    // Configure auto-updater
    autoUpdater.autoDownload = false
    autoUpdater.autoInstallOnAppQuit = true

    // Enable update checking in dev mode for testing
    if (forceDevUpdateConfig) {
      autoUpdater.forceDevUpdateConfig = true
      console.log('[AutoUpdater] Force dev update config enabled')
    }

    // Enable logging
    autoUpdater.logger = {
      info: (msg) => console.log('[AutoUpdater]', msg),
      warn: (msg) => console.warn('[AutoUpdater]', msg),
      error: (msg) => console.error('[AutoUpdater]', msg),
      debug: (msg) => console.log('[AutoUpdater:debug]', msg)
    }

    // Set up event handlers
    autoUpdater.on('checking-for-update', () => {
      this.sendStatus({ status: 'checking' })
    })

    autoUpdater.on('update-available', (info: UpdateInfo) => {
      this.sendStatus({ status: 'available', info })
    })

    autoUpdater.on('update-not-available', (info: UpdateInfo) => {
      this.sendStatus({ status: 'not-available', info })
    })

    autoUpdater.on('download-progress', (progress: ProgressInfo) => {
      this.sendStatus({ status: 'downloading', progress })
    })

    autoUpdater.on('update-downloaded', (info: UpdateInfo) => {
      this.sendStatus({ status: 'downloaded', info })
    })

    autoUpdater.on('error', (err: Error) => {
      this.sendStatus({ status: 'error', error: err.message })
    })
  }

  /**
   * Set the main window reference for sending IPC messages
   */
  setMainWindow(getWindow: () => BrowserWindow | null): void {
    this.mainWindow = getWindow
  }

  /**
   * Check for updates
   */
  async checkForUpdates(): Promise<void> {
    try {
      await autoUpdater.checkForUpdates()
    } catch (err) {
      console.error('[AutoUpdater] Check for updates failed:', err)
      this.sendStatus({
        status: 'error',
        error: err instanceof Error ? err.message : 'Unknown error'
      })
    }
  }

  /**
   * Download the available update
   */
  async downloadUpdate(): Promise<void> {
    try {
      await autoUpdater.downloadUpdate()
    } catch (err) {
      console.error('[AutoUpdater] Download update failed:', err)
      this.sendStatus({
        status: 'error',
        error: err instanceof Error ? err.message : 'Download failed'
      })
    }
  }

  /**
   * Quit and install the downloaded update
   */
  quitAndInstall(): void {
    autoUpdater.quitAndInstall()
  }

  /**
   * Send update status to renderer
   */
  private sendStatus(status: UpdateStatus): void {
    const window = this.mainWindow?.()
    if (window && !window.isDestroyed()) {
      window.webContents.send(IPC_CHANNELS.UPDATE_STATUS, status)
    }
  }
}

// Factory function to create updater with options
export function createUpdater(forceDevUpdateConfig = false): AutoUpdater {
  return new AutoUpdater(forceDevUpdateConfig)
}
