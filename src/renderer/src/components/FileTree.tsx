/**
 * FileTree component for displaying directory structure
 * With lazy loading and context menu support
 */
import { useState, useEffect, useCallback, useRef, useMemo } from 'react'
import {
  ChevronRightIcon,
  FolderIcon,
  FolderOpenIcon,
  FileIcon,
  ImageIcon,
  LockIcon,
  FolderX,
  Terminal,
  Code2,
  Hammer,
  Copy,
  AppWindowMac
} from 'lucide-react'
import { cn } from '@/lib/utils'
import { toast } from 'sonner'
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuTrigger
} from '@/components/ui/context-menu'
import {
  isConfigFile,
  isGitFile,
  ConfigIcon,
  GitIcon,
  TypeScriptIcon,
  JavaScriptIcon,
  JsonIcon,
  YamlIcon,
  MarkdownIcon,
  CssIcon,
  HtmlIcon
} from './FileIcons'
import type { FileTreeNode, DetectedApp } from '../../../shared/electron-api'
import { useFileChangeStore } from '../stores/fileChangeStore'
import { useUIStore } from '../stores/uiStore'
import { excludeHiddenFiles } from '../utils/fileTree'

interface FileTreeProps {
  rootPath: string
  directoryExists?: boolean
  onCreateSession?: (cwd: string) => void
}

interface TreeItemProps {
  node: FileTreeNode
  level: number
  expandedPaths: Set<string>
  onToggle: (path: string) => void
  getChildren: (path: string) => FileTreeNode[] | undefined
  loadChildren: (path: string) => Promise<void>
  availableApps: DetectedApp[]
  onCreateSession?: (cwd: string) => void
}

// Get the appropriate icon for a file/directory
function FileTreeIcon({
  node,
  isExpanded
}: {
  node: FileTreeNode
  isExpanded?: boolean
}): React.JSX.Element {
  const className = 'h-4 w-4 flex-shrink-0'

  if (node.type === 'directory') {
    if (isGitFile(node.name)) {
      return <GitIcon className={className} />
    }
    return isExpanded ? (
      <FolderOpenIcon className={cn(className, 'text-muted-foreground')} />
    ) : (
      <FolderIcon className={cn(className, 'text-muted-foreground')} />
    )
  }

  // Check for config files first
  if (isConfigFile(node.name)) {
    return <ConfigIcon className={cn(className, 'text-muted-foreground')} />
  }

  // Check for git files
  if (isGitFile(node.name)) {
    return <GitIcon className={className} />
  }

  // Check for file type icon based on extension
  const ext = node.extension
  if (ext === 'ts' || ext === 'tsx' || ext === 'mts' || ext === 'cts') {
    return <TypeScriptIcon className={className} />
  }
  if (ext === 'js' || ext === 'jsx' || ext === 'mjs' || ext === 'cjs') {
    return <JavaScriptIcon className={className} />
  }
  if (ext === 'json') {
    return <JsonIcon className={className} />
  }
  if (ext === 'yaml' || ext === 'yml') {
    return <YamlIcon className={className} />
  }
  if (ext === 'md' || ext === 'mdx') {
    return <MarkdownIcon className={className} />
  }
  if (ext === 'css' || ext === 'scss' || ext === 'sass' || ext === 'less') {
    return <CssIcon className={className} />
  }
  if (ext === 'html' || ext === 'htm') {
    return <HtmlIcon className={className} />
  }

  // Image files
  if (['png', 'jpg', 'jpeg', 'gif', 'svg', 'webp', 'ico'].includes(node.extension || '')) {
    return <ImageIcon className={cn(className, 'text-muted-foreground')} />
  }

  // Default file icon
  return <FileIcon className={cn(className, 'text-muted-foreground')} />
}

function TreeItem({
  node,
  level,
  expandedPaths,
  onToggle,
  getChildren,
  loadChildren,
  availableApps,
  onCreateSession
}: TreeItemProps): React.JSX.Element {
  const isExpanded = expandedPaths.has(node.path)
  const isDirectory = node.type === 'directory'
  const children = getChildren(node.path)
  const [isLoading, setIsLoading] = useState(false)
  const [hasError, setHasError] = useState(false)

  const handleClick = useCallback(async () => {
    if (!isDirectory) return

    if (!isExpanded && !children) {
      setIsLoading(true)
      setHasError(false)
      try {
        await loadChildren(node.path)
      } catch {
        setHasError(true)
      }
      setIsLoading(false)
    }
    onToggle(node.path)
  }, [isDirectory, isExpanded, children, loadChildren, node.path, onToggle])

  const handleOpenWith = useCallback(
    async (appId: string) => {
      try {
        await window.electronAPI.openWith({ path: node.path, appId })
        if (appId === 'copy-path') {
          toast.success('Path copied to clipboard')
        }
      } catch (error) {
        console.error('Failed to open with app:', error)
      }
    },
    [node.path]
  )

  // Separate apps into categories
  const finderApp = availableApps.find((app) => app.id === 'finder')
  const editorApps = availableApps.filter((app) => ['cursor', 'vscode', 'xcode'].includes(app.id))
  const terminalApps = availableApps.filter((app) =>
    ['ghostty', 'iterm', 'terminal'].includes(app.id)
  )
  const copyPathApp = availableApps.find((app) => app.id === 'copy-path')

  return (
    <>
      <ContextMenu>
        <ContextMenuTrigger asChild>
          <div
            className={cn(
              'flex items-center gap-1 py-0.5 px-1 rounded-sm cursor-pointer',
              'hover:bg-accent',
              hasError && 'opacity-50'
            )}
            style={{ paddingLeft: `${level * 16 + 4}px` }}
            onClick={handleClick}
          >
            {/* Chevron for directories */}
            {isDirectory ? (
              <ChevronRightIcon
                className={cn(
                  'h-3.5 w-3.5 flex-shrink-0 text-muted-foreground transition-transform',
                  isExpanded && 'rotate-90'
                )}
              />
            ) : (
              <span className="w-3.5 flex-shrink-0" />
            )}

            {/* File/folder icon */}
            {hasError ? (
              <LockIcon className="h-4 w-4 flex-shrink-0 text-muted-foreground" />
            ) : (
              <FileTreeIcon node={node} isExpanded={isExpanded} />
            )}

            {/* Name */}
            <span className="truncate text-sm">{node.name}</span>

            {/* Loading indicator */}
            {isLoading && <span className="text-xs text-muted-foreground ml-auto">...</span>}
          </div>
        </ContextMenuTrigger>

        <ContextMenuContent className="w-48">
          {/* Finder */}
          {finderApp && (
            <ContextMenuItem onClick={() => handleOpenWith('finder')}>
              <span className="flex items-center gap-2">
                <FolderOpenIcon className="h-4 w-4" />
                <span>Finder</span>
              </span>
              <span className="ml-auto text-xs text-muted-foreground">⌘O</span>
            </ContextMenuItem>
          )}

          {/* Apps (Multica + Editors) */}
          {(isDirectory && onCreateSession) || editorApps.length > 0 ? (
            <>
              <ContextMenuSeparator />
              {/* Multica - only for directories */}
              {isDirectory && onCreateSession && (
                <ContextMenuItem onClick={() => onCreateSession(node.path)}>
                  <span className="flex items-center gap-2">
                    <AppWindowMac className="h-4 w-4" />
                    <span>Multica</span>
                  </span>
                </ContextMenuItem>
              )}
              {/* Editors */}
              {editorApps.map((app) => (
                <ContextMenuItem key={app.id} onClick={() => handleOpenWith(app.id)}>
                  <span className="flex items-center gap-2">
                    {app.id === 'xcode' ? (
                      <Hammer className="h-4 w-4" />
                    ) : (
                      <Code2 className="h-4 w-4" />
                    )}
                    <span>{app.name}</span>
                  </span>
                </ContextMenuItem>
              ))}
            </>
          ) : null}

          {/* Terminals */}
          {terminalApps.length > 0 && (
            <>
              <ContextMenuSeparator />
              {terminalApps.map((app) => (
                <ContextMenuItem key={app.id} onClick={() => handleOpenWith(app.id)}>
                  <span className="flex items-center gap-2">
                    <Terminal className="h-4 w-4" />
                    <span>{app.name}</span>
                  </span>
                </ContextMenuItem>
              ))}
            </>
          )}

          {/* Copy path */}
          {copyPathApp && (
            <>
              <ContextMenuSeparator />
              <ContextMenuItem onClick={() => handleOpenWith('copy-path')}>
                <span className="flex items-center gap-2">
                  <Copy className="h-4 w-4" />
                  <span>Copy path</span>
                </span>
              </ContextMenuItem>
            </>
          )}
        </ContextMenuContent>
      </ContextMenu>

      {/* Render children if expanded */}
      {isExpanded && children && (
        <>
          {children.length === 0 ? (
            <div
              className="text-xs text-muted-foreground py-0.5"
              style={{ paddingLeft: `${(level + 1) * 16 + 24}px` }}
            >
              (empty)
            </div>
          ) : (
            children.map((child) => (
              <TreeItem
                key={child.path}
                node={child}
                level={level + 1}
                expandedPaths={expandedPaths}
                onToggle={onToggle}
                getChildren={getChildren}
                loadChildren={loadChildren}
                availableApps={availableApps}
                onCreateSession={onCreateSession}
              />
            ))
          )}
        </>
      )}
    </>
  )
}

/**
 * Displayed when the session's directory no longer exists
 */
function DirectoryNotFound(): React.JSX.Element {
  return (
    <div className="flex flex-col items-center justify-center h-32 text-muted-foreground">
      <FolderX className="h-8 w-8 mb-2 opacity-40" />
      <span className="text-sm">Directory not found</span>
    </div>
  )
}

export function FileTree({
  rootPath,
  directoryExists = true,
  onCreateSession
}: FileTreeProps): React.JSX.Element {
  // Hidden files toggle state (controlled by parent via uiStore)
  const showHiddenFiles = useUIStore((s) => s.showHiddenFiles)

  // Store raw (unfiltered) data
  const [rawRootChildren, setRawRootChildren] = useState<FileTreeNode[]>([])
  const [expandedPaths, setExpandedPaths] = useState<Set<string>>(new Set())
  const [rawChildrenCache, setRawChildrenCache] = useState<Map<string, FileTreeNode[]>>(new Map())

  // Compute filtered root children based on showHiddenFiles preference
  const rootChildren = useMemo(
    () => (showHiddenFiles ? rawRootChildren : excludeHiddenFiles(rawRootChildren)),
    [rawRootChildren, showHiddenFiles]
  )

  // Helper to get filtered children for a given path (filters on demand)
  const getFilteredChildren = useCallback(
    (path: string): FileTreeNode[] | undefined => {
      const children = rawChildrenCache.get(path)
      if (!children) return undefined
      return showHiddenFiles ? children : excludeHiddenFiles(children)
    },
    [rawChildrenCache, showHiddenFiles]
  )
  const [availableApps, setAvailableApps] = useState<DetectedApp[]>([])
  // Initialize loading state based on directoryExists
  const [isLoading, setIsLoading] = useState(directoryExists)

  // Subscribe to file change events for auto-refresh
  const refreshCounter = useFileChangeStore((s) => s.refreshCounter)
  const isInitialMount = useRef(true)

  // Load root directory and detect apps on mount
  useEffect(() => {
    // Skip if directory doesn't exist
    if (!directoryExists) {
      return
    }

    async function init(): Promise<void> {
      setIsLoading(true)
      try {
        const [children, apps] = await Promise.all([
          window.electronAPI.listDirectory(rootPath),
          window.electronAPI.detectApps()
        ])
        setRawRootChildren(children)
        setAvailableApps(apps)
      } catch (error) {
        console.error('Failed to load file tree:', error)
      }
      setIsLoading(false)
    }
    void init()
  }, [rootPath, directoryExists])

  // Keep a ref to expandedPaths for use in refresh effect without triggering it
  const expandedPathsRef = useRef(expandedPaths)
  useEffect(() => {
    expandedPathsRef.current = expandedPaths
  }, [expandedPaths])

  // Refresh file tree when files change (from file system watcher)
  useEffect(() => {
    // Skip on initial mount (already loaded above)
    if (isInitialMount.current) {
      isInitialMount.current = false
      return
    }

    // Skip if directory doesn't exist
    if (!directoryExists) return

    console.log('[FileTree] Refresh triggered, counter:', refreshCounter)

    // Refresh: clear cache and re-fetch root + expanded directories
    async function refresh(): Promise<void> {
      try {
        // Re-fetch root directory
        console.log('[FileTree] Fetching root directory:', rootPath)
        const children = await window.electronAPI.listDirectory(rootPath)
        console.log(
          '[FileTree] Root directory result:',
          children.length,
          'items',
          children.map((c) => c.name)
        )
        setRawRootChildren(children)

        // Re-fetch all expanded directories to update them
        const currentExpanded = Array.from(expandedPathsRef.current)
        if (currentExpanded.length > 0) {
          console.log('[FileTree] Refreshing expanded dirs:', currentExpanded)
          const newCache = new Map<string, FileTreeNode[]>()
          const results = await Promise.all(
            currentExpanded.map(async (path) => {
              try {
                const dirChildren = await window.electronAPI.listDirectory(path)
                return { path, children: dirChildren }
              } catch {
                // Directory may no longer exist
                return { path, children: [] }
              }
            })
          )
          for (const { path, children: dirChildren } of results) {
            newCache.set(path, dirChildren)
          }
          setRawChildrenCache(newCache)
        }
      } catch (error) {
        console.error('[FileTree] Failed to refresh file tree:', error)
      }
    }

    void refresh()
  }, [refreshCounter, rootPath, directoryExists])

  const handleToggle = useCallback((path: string) => {
    setExpandedPaths((prev) => {
      const next = new Set(prev)
      if (next.has(path)) {
        next.delete(path)
      } else {
        next.add(path)
      }
      return next
    })
  }, [])

  const loadChildren = useCallback(async (path: string) => {
    // Fetch children first, then check cache in the setter to avoid stale closure
    const children = await window.electronAPI.listDirectory(path)
    setRawChildrenCache((prev) => {
      // Skip if already cached (handles race conditions)
      if (prev.has(path)) return prev
      return new Map(prev).set(path, children)
    })
  }, [])

  // If directory doesn't exist, show the not found UI
  if (directoryExists === false) {
    return <DirectoryNotFound />
  }

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-32 text-muted-foreground">
        <span className="text-sm">Loading...</span>
      </div>
    )
  }

  if (rootChildren.length === 0) {
    return (
      <div className="flex items-center justify-center h-32 text-muted-foreground">
        <span className="text-sm">Empty directory</span>
      </div>
    )
  }

  return (
    <div className="py-1">
      {rootChildren.map((node) => (
        <TreeItem
          key={node.path}
          node={node}
          level={0}
          expandedPaths={expandedPaths}
          onToggle={handleToggle}
          getChildren={getFilteredChildren}
          loadChildren={loadChildren}
          availableApps={availableApps}
          onCreateSession={onCreateSession}
        />
      ))}
    </div>
  )
}
