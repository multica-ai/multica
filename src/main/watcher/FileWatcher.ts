/**
 * FileWatcher - Manages native fs.watch file system watchers for sessions
 *
 * Uses native fs.watch with recursive: true to leverage platform-specific
 * efficient watching mechanisms:
 * - macOS: FSEvents (kernel-level API, no per-file descriptor needed)
 * - Linux: inotify
 * - Windows: ReadDirectoryChangesW
 *
 * This avoids the EMFILE (too many open files) error that can occur with
 * chokidar when watching large directories with many subdirectories.
 *
 * Responsibilities:
 * - Create watchers for session working directories
 * - Debounce file change events
 * - Filter out ignored directories (node_modules, .git, etc.)
 * - Notify renderer process of changes
 * - Clean up watchers when sessions close
 */
import { watch, existsSync, type FSWatcher } from 'fs'
import path from 'path'
import type { BrowserWindow } from 'electron'
import { IPC_CHANNELS } from '../../shared/ipc-channels'

interface FileWatcherOptions {
  debounceMs?: number
  getMainWindow: () => BrowserWindow | null
}

// Directories to ignore - checked by path component
const IGNORED_DIRS = new Set([
  'node_modules',
  '.git',
  'dist',
  'build',
  '.next',
  '.cache',
  'coverage',
  '.vscode',
  '.idea',
  'venv',
  '__pycache__',
  'target',
  '.gradle',
  'out',
  '.turbo',
  '.svn',
  '.hg'
])

// Files to ignore by name
const IGNORED_FILES = new Set(['.DS_Store', 'Thumbs.db'])

const DEFAULT_DEBOUNCE_MS = 100

export class FileWatcher {
  private watchers: Map<string, FSWatcher> = new Map() // directory -> FSWatcher
  private sessionToDirectory: Map<string, string> = new Map() // sessionId -> directory
  private directoryToSessions: Map<string, Set<string>> = new Map() // directory -> sessionIds
  private debounceTimers: Map<string, NodeJS.Timeout> = new Map() // debounceKey -> timer
  private debounceMs: number
  private getMainWindow: () => BrowserWindow | null

  constructor(options: FileWatcherOptions) {
    this.debounceMs = options.debounceMs ?? DEFAULT_DEBOUNCE_MS
    this.getMainWindow = options.getMainWindow
  }

  /**
   * Check if a relative path should be ignored based on its components
   */
  private shouldIgnore(relativePath: string): boolean {
    const parts = relativePath.split(path.sep)
    // Check if any path component is an ignored directory
    for (const part of parts) {
      if (IGNORED_DIRS.has(part)) {
        return true
      }
    }
    // Check if the filename is ignored
    const filename = parts[parts.length - 1]
    if (filename && IGNORED_FILES.has(filename)) {
      return true
    }
    // Ignore log files
    if (filename && filename.endsWith('.log')) {
      return true
    }
    return false
  }

  /**
   * Debounce a handler by key
   */
  private debounce(key: string, handler: () => void): void {
    const existing = this.debounceTimers.get(key)
    if (existing) {
      clearTimeout(existing)
    }

    const timer = setTimeout(() => {
      this.debounceTimers.delete(key)
      handler()
    }, this.debounceMs)

    this.debounceTimers.set(key, timer)
  }

  /**
   * Start watching a directory for a session
   */
  watch(sessionId: string, directory: string): void {
    // Normalize directory path
    const normalizedDir = path.normalize(directory)

    // Check if directory exists (reference: Craft Agents pattern)
    if (!existsSync(normalizedDir)) {
      console.warn(`[FileWatcher] Directory does not exist: ${normalizedDir}`)
      return
    }

    // Track session -> directory mapping
    this.sessionToDirectory.set(sessionId, normalizedDir)

    // Add to directory -> sessions mapping
    let sessions = this.directoryToSessions.get(normalizedDir)
    if (!sessions) {
      sessions = new Set()
      this.directoryToSessions.set(normalizedDir, sessions)
    }
    sessions.add(sessionId)

    // Check if we already have a watcher for this directory
    if (this.watchers.has(normalizedDir)) {
      console.log(`[FileWatcher] Session ${sessionId} joined existing watcher for ${normalizedDir}`)
      return
    }

    // Create new watcher
    console.log(`[FileWatcher] Creating watcher for ${normalizedDir}`)

    try {
      // Use native fs.watch with recursive: true
      // On macOS, this uses FSEvents which doesn't have EMFILE issues
      const watcher = watch(normalizedDir, { recursive: true }, (eventType, filename) => {
        // filename can be null in some edge cases
        if (!filename) return

        // Check if this path should be ignored
        if (this.shouldIgnore(filename)) {
          return
        }

        const fullPath = path.join(normalizedDir, filename)

        // Use directory + filename as debounce key for per-file debouncing
        const debounceKey = `${normalizedDir}:${filename}`

        this.debounce(debounceKey, () => {
          this.notifyChange(normalizedDir, eventType, fullPath)
        })
      })

      watcher.on('error', (error) => {
        const errCode = (error as NodeJS.ErrnoException).code
        console.error(`[FileWatcher] Error watching ${normalizedDir}:`, error)

        // If EMFILE error, clean up this watcher to prevent spam
        // (Reference: Craft Agents just logs errors, but we add cleanup for safety)
        if (errCode === 'EMFILE') {
          console.warn(`[FileWatcher] EMFILE error, closing watcher for ${normalizedDir}`)
          // Clear pending debounce timers for this directory
          for (const [key, timer] of this.debounceTimers) {
            if (key.startsWith(`${normalizedDir}:`)) {
              clearTimeout(timer)
              this.debounceTimers.delete(key)
            }
          }
          watcher.close()
          this.watchers.delete(normalizedDir)
        }
      })

      this.watchers.set(normalizedDir, watcher)
    } catch (error) {
      console.error(`[FileWatcher] Failed to create watcher for ${normalizedDir}:`, error)
    }
  }

  /**
   * Stop watching a directory for a session
   */
  unwatch(sessionId: string): void {
    const directory = this.sessionToDirectory.get(sessionId)
    if (!directory) return

    this.sessionToDirectory.delete(sessionId)

    const sessions = this.directoryToSessions.get(directory)
    if (sessions) {
      sessions.delete(sessionId)

      // If no more sessions watching this directory, close the watcher
      if (sessions.size === 0) {
        this.directoryToSessions.delete(directory)

        // Clear any pending debounce timers for this directory
        for (const [key, timer] of this.debounceTimers) {
          if (key.startsWith(`${directory}:`)) {
            clearTimeout(timer)
            this.debounceTimers.delete(key)
          }
        }

        const watcher = this.watchers.get(directory)
        if (watcher) {
          console.log(`[FileWatcher] Closing watcher for ${directory}`)
          watcher.close()
          this.watchers.delete(directory)
        }
      }
    }
  }

  /**
   * Stop all watchers
   */
  unwatchAll(): void {
    // Clear all debounce timers
    for (const timer of this.debounceTimers.values()) {
      clearTimeout(timer)
    }
    this.debounceTimers.clear()

    // Close all watchers
    for (const [directory, watcher] of this.watchers) {
      console.log(`[FileWatcher] Closing watcher for ${directory}`)
      watcher.close()
    }
    this.watchers.clear()
    this.sessionToDirectory.clear()
    this.directoryToSessions.clear()
  }

  /**
   * Notify renderer of file change
   */
  private notifyChange(directory: string, eventType: string, filePath: string): void {
    const mainWindow = this.getMainWindow()
    if (!mainWindow || mainWindow.isDestroyed()) return

    const sessions = this.directoryToSessions.get(directory)
    if (!sessions || sessions.size === 0) return

    // Map fs.watch eventType to our event types
    // fs.watch only provides 'rename' (add/delete) and 'change' (modify)
    const normalizedEventType = eventType === 'rename' ? 'change' : eventType

    // Send change notification with affected session IDs
    mainWindow.webContents.send(IPC_CHANNELS.FS_FILE_CHANGED, {
      directory,
      eventType: normalizedEventType,
      path: filePath,
      sessionIds: Array.from(sessions)
    })
  }

  /**
   * Get watching status for a session
   */
  isWatching(sessionId: string): boolean {
    return this.sessionToDirectory.has(sessionId)
  }

  /**
   * Get directory being watched for a session
   */
  getWatchedDirectory(sessionId: string): string | undefined {
    return this.sessionToDirectory.get(sessionId)
  }
}
