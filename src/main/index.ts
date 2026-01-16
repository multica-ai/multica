import fixPath from 'fix-path'

// Fix PATH for macOS GUI apps (must be called before any command execution)
// GUI apps don't inherit PATH from shell dotfiles (.zshrc, .bashrc, etc.)
fixPath()

import { app, shell, BrowserWindow, ipcMain } from 'electron'
import { join } from 'path'
import { electronApp, optimizer, is } from '@electron-toolkit/utils'
import icon from '../../resources/icon.png?asset'
import { registerIPCHandlers } from './ipc/handlers'
import { Conductor } from './conductor/Conductor'
import { IPC_CHANNELS } from '../shared/ipc-channels'
import type { PermissionResponse } from '../shared/electron-api'
import { PermissionManager } from './permission'
import { createUpdater, AutoUpdater } from './updater'

// Global instances
let conductor: Conductor
let mainWindow: BrowserWindow | null = null
let permissionManager: PermissionManager
let updater: AutoUpdater

function createWindow(): BrowserWindow {
  const window = new BrowserWindow({
    width: 1200,
    height: 800,
    minWidth: 800,
    minHeight: 600,
    show: false,
    autoHideMenuBar: true,
    titleBarStyle: 'hiddenInset',
    trafficLightPosition: { x: 16, y: 12 },
    ...(process.platform === 'linux' ? { icon } : {}),
    webPreferences: {
      preload: join(__dirname, '../preload/index.js'),
      contextIsolation: true,
      nodeIntegration: false,
      sandbox: true
    }
  })

  window.on('ready-to-show', () => {
    window.show()
  })

  window.webContents.setWindowOpenHandler((details) => {
    shell.openExternal(details.url)
    return { action: 'deny' }
  })

  // HMR for renderer base on electron-vite cli
  if (is.dev && process.env['ELECTRON_RENDERER_URL']) {
    window.loadURL(process.env['ELECTRON_RENDERER_URL'])
  } else {
    window.loadFile(join(__dirname, '../renderer/index.html'))
  }

  return window
}

// This method will be called when Electron has finished initialization
app.whenReady().then(async () => {
  // Set app user model id for windows
  electronApp.setAppUserModelId('com.multica')

  // Default open or close DevTools by F12 in development
  app.on('browser-window-created', (_, window) => {
    optimizer.watchWindowShortcuts(window)
  })

  // Initialize conductor with event handlers
  conductor = new Conductor({
    events: {
      onSessionUpdate: (params, sequenceNumber) => {
        // Forward ALL session updates to renderer with sequence number for ordering
        if (mainWindow && !mainWindow.isDestroyed()) {
          mainWindow.webContents.send(IPC_CHANNELS.AGENT_MESSAGE, {
            sessionId: params.sessionId,
            sequenceNumber,
            update: params.update,
            done: false
          })
        }

        // Delegate question tool workaround to PermissionManager
        permissionManager.handleSessionUpdate({
          sessionId: params.sessionId,
          update: params.update as Parameters<
            typeof permissionManager.handleSessionUpdate
          >[0]['update']
        })
      },
      onStatusChange: () => {
        // Broadcast status change to renderer (for isProcessing state)
        if (mainWindow && !mainWindow.isDestroyed()) {
          const status = {
            runningSessions: conductor.getRunningSessionIds().length,
            sessionIds: conductor.getRunningSessionIds(),
            processingSessionIds: conductor.getProcessingSessionIds()
          }
          mainWindow.webContents.send(IPC_CHANNELS.AGENT_STATUS, status)
        }
      },
      onSessionMetaUpdated: (session) => {
        // Notify renderer when session metadata changes
        if (mainWindow && !mainWindow.isDestroyed()) {
          mainWindow.webContents.send(IPC_CHANNELS.SESSION_META_UPDATED, session)
        }
      },
      onPermissionRequest: async (params) => {
        return permissionManager.handlePermissionRequest(params)
      }
    }
  })

  // Initialize PermissionManager after Conductor is created
  permissionManager = new PermissionManager(conductor, () => mainWindow)

  await conductor.initialize()

  // Handle permission responses from renderer
  ipcMain.on(IPC_CHANNELS.PERMISSION_RESPONSE, (_event, response: PermissionResponse) => {
    permissionManager.handlePermissionResponse(response)
  })

  // Register IPC handlers
  registerIPCHandlers(conductor)

  mainWindow = createWindow()

  // Notify renderer when app window gains focus
  mainWindow.on('focus', () => {
    if (mainWindow && !mainWindow.isDestroyed()) {
      mainWindow.webContents.send(IPC_CHANNELS.APP_FOCUS)
    }
  })

  // Initialize auto-updater
  // Set FORCE_DEV_UPDATE=true to test updates in dev mode
  const forceDevUpdate = process.env.FORCE_DEV_UPDATE === 'true'
  updater = createUpdater(forceDevUpdate)
  updater.setMainWindow(() => mainWindow)

  // Auto-check for updates (in production or when forced)
  if (!is.dev || forceDevUpdate) {
    mainWindow.once('ready-to-show', () => {
      updater.checkForUpdates()
    })
  }

  // Register update IPC handlers
  ipcMain.handle(IPC_CHANNELS.UPDATE_CHECK, async () => {
    await updater.checkForUpdates()
  })

  ipcMain.handle(IPC_CHANNELS.UPDATE_DOWNLOAD, async () => {
    await updater.downloadUpdate()
  })

  ipcMain.handle(IPC_CHANNELS.UPDATE_INSTALL, () => {
    updater.quitAndInstall()
  })

  app.on('activate', function () {
    // On macOS it's common to re-create a window when dock icon is clicked
    if (BrowserWindow.getAllWindows().length === 0) createWindow()
  })
})

// Quit when all windows are closed, except on macOS
app.on('window-all-closed', () => {
  if (process.platform !== 'darwin') {
    app.quit()
  }
})
