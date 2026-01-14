import { app, shell, BrowserWindow, ipcMain } from 'electron'
import { join } from 'path'
import { electronApp, optimizer, is } from '@electron-toolkit/utils'
import icon from '../../resources/icon.png?asset'
import { registerIPCHandlers } from './ipc/handlers'
import { Conductor } from './conductor/Conductor'
import { IPC_CHANNELS } from '../shared/ipc-channels'
import type { PermissionResponse } from '../shared/electron-api'

// Global conductor instance
let conductor: Conductor
let mainWindow: BrowserWindow | null = null

// Pending permission requests (requestId -> resolve function)
const pendingPermissionRequests = new Map<string, (response: PermissionResponse) => void>()

function createWindow(): BrowserWindow {
  // Create the browser window.
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
      sandbox: true,
    }
  })

  window.on('ready-to-show', () => {
    window.show()
  })

  window.webContents.setWindowOpenHandler((details) => {
    shell.openExternal(details.url)
    return { action: 'deny' }
  })

  // HMR for renderer base on electron-vite cli.
  // Load the remote URL for development or the local html file for production.
  if (is.dev && process.env['ELECTRON_RENDERER_URL']) {
    window.loadURL(process.env['ELECTRON_RENDERER_URL'])
  } else {
    window.loadFile(join(__dirname, '../renderer/index.html'))
  }

  return window
}

// This method will be called when Electron has finished
// initialization and is ready to create browser windows.
// Some APIs can only be used after this event occurs.
app.whenReady().then(async () => {
  // Set app user model id for windows
  electronApp.setAppUserModelId('com.multica')

  // Default open or close DevTools by F12 in development
  // and ignore CommandOrControl + R in production.
  app.on('browser-window-created', (_, window) => {
    optimizer.watchWindowShortcuts(window)
  })

  // Initialize conductor with event handlers
  conductor = new Conductor({
    events: {
      onSessionUpdate: (params) => {
        // Forward ALL session updates to renderer (not just agent_message_chunk)
        if (mainWindow && !mainWindow.isDestroyed()) {
          // Log for debugging
          const updateType = params.update && 'sessionUpdate' in params.update ? params.update.sessionUpdate : 'unknown'
          console.log(`[Main->Renderer] Sending update: ${updateType}, ACP sessionId: ${params.sessionId}`)

          // Send the full SessionNotification so renderer can handle all update types
          mainWindow.webContents.send(IPC_CHANNELS.AGENT_MESSAGE, {
            sessionId: params.sessionId,
            update: params.update,  // Full update object
            done: false,
          })
        }
      },
      onStatusChange: () => {
        // Broadcast status change to renderer (for isProcessing state)
        if (mainWindow && !mainWindow.isDestroyed()) {
          const status = {
            runningSessions: conductor.getRunningSessionIds().length,
            sessionIds: conductor.getRunningSessionIds(),
            processingSessionIds: conductor.getProcessingSessionIds(),
          }
          mainWindow.webContents.send(IPC_CHANNELS.AGENT_STATUS, status)
        }
      },
      onPermissionRequest: async (params) => {
        // Generate unique request ID
        const { randomUUID } = await import('crypto')
        const requestId = randomUUID()

        console.log(`[Permission] Request ${requestId}: ${params.toolCall.title}`)
        console.log(`[Permission]   Options:`, params.options.map(o => `${o.name} (${o.optionId})`).join(', '))

        // Send permission request to renderer
        if (mainWindow && !mainWindow.isDestroyed()) {
          mainWindow.webContents.send(IPC_CHANNELS.PERMISSION_REQUEST, {
            requestId,
            sessionId: params.sessionId,
            multicaSessionId: params.sessionId, // ACP session ID
            toolCall: {
              toolCallId: params.toolCall.toolCallId,
              title: params.toolCall.title,
              kind: params.toolCall.kind,
              status: params.toolCall.status,
              rawInput: params.toolCall.rawInput,
            },
            options: params.options.map(o => ({
              optionId: o.optionId,
              name: o.name,
              kind: o.kind,
            })),
          })
        }

        // Wait for response from renderer
        return new Promise((resolve) => {
          pendingPermissionRequests.set(requestId, (response) => {
            console.log(`[Permission] Response ${requestId}: ${response.optionId}`)
            resolve({
              outcome: {
                outcome: 'selected',
                optionId: response.optionId,
              },
            })
          })

          // Timeout after 5 minutes (auto-deny)
          setTimeout(() => {
            if (pendingPermissionRequests.has(requestId)) {
              console.log(`[Permission] Timeout ${requestId}, auto-denying`)
              pendingPermissionRequests.delete(requestId)
              // Find a deny option or use first option
              const denyOption = params.options.find(o => (o.kind as string) === 'deny') || params.options[0]
              resolve({
                outcome: {
                  outcome: 'selected',
                  optionId: denyOption?.optionId ?? '',
                },
              })
            }
          }, 5 * 60 * 1000)
        })
      },
    },
  })
  await conductor.initialize()

  // Handle permission responses from renderer
  ipcMain.on(IPC_CHANNELS.PERMISSION_RESPONSE, (_event, response: PermissionResponse) => {
    console.log(`[Permission] Received response for ${response.requestId}: ${response.optionId}`)
    const resolver = pendingPermissionRequests.get(response.requestId)
    if (resolver) {
      pendingPermissionRequests.delete(response.requestId)
      resolver(response)
    }
  })

  // Register IPC handlers
  registerIPCHandlers(conductor)

  mainWindow = createWindow()

  app.on('activate', function () {
    // On macOS it's common to re-create a window in the app when the
    // dock icon is clicked and there are no other windows open.
    if (BrowserWindow.getAllWindows().length === 0) createWindow()
  })
})

// Quit when all windows are closed, except on macOS. There, it's common
// for applications and their menu bar to stay active until the user quits
// explicitly with Cmd + Q.
app.on('window-all-closed', () => {
  if (process.platform !== 'darwin') {
    app.quit()
  }
})

// In this file you can include the rest of your app's specific main process
// code. You can also put them in separate files and require them here.
