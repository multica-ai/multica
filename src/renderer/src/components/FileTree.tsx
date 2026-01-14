/**
 * FileTree component for displaying directory structure
 * With lazy loading and context menu support
 */
import { useState, useEffect, useCallback } from 'react'
import {
  ChevronRightIcon,
  FolderIcon,
  FolderOpenIcon,
  FileIcon,
  ImageIcon,
  LockIcon,
} from 'lucide-react'
import { cn } from '@/lib/utils'
import { toast } from 'sonner'
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuTrigger,
} from '@/components/ui/context-menu'
import {
  getFileIcon,
  isConfigFile,
  isGitFile,
  ConfigIcon,
  GitIcon,
} from './FileIcons'
import type { FileTreeNode, DetectedApp } from '../../../shared/electron-api'

interface FileTreeProps {
  rootPath: string
}

interface TreeItemProps {
  node: FileTreeNode
  level: number
  expandedPaths: Set<string>
  onToggle: (path: string) => void
  childrenCache: Map<string, FileTreeNode[]>
  loadChildren: (path: string) => Promise<void>
  availableApps: DetectedApp[]
}

// Get the appropriate icon for a file/directory
function FileTreeIcon({
  node,
  isExpanded,
}: {
  node: FileTreeNode
  isExpanded?: boolean
}) {
  const className = 'h-4 w-4 flex-shrink-0'

  if (node.type === 'directory') {
    if (isGitFile(node.name)) {
      return <GitIcon className={className} />
    }
    return isExpanded ? (
      <FolderOpenIcon className={cn(className, 'text-sky-500')} />
    ) : (
      <FolderIcon className={cn(className, 'text-sky-500')} />
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

  // Check for file type icon
  const TypeIcon = getFileIcon(node.extension)
  if (TypeIcon) {
    return <TypeIcon className={className} />
  }

  // Image files
  if (['png', 'jpg', 'jpeg', 'gif', 'svg', 'webp', 'ico'].includes(node.extension || '')) {
    return <ImageIcon className={cn(className, 'text-green-500')} />
  }

  // Default file icon
  return <FileIcon className={cn(className, 'text-muted-foreground')} />
}

function TreeItem({
  node,
  level,
  expandedPaths,
  onToggle,
  childrenCache,
  loadChildren,
  availableApps,
}: TreeItemProps) {
  const isExpanded = expandedPaths.has(node.path)
  const isDirectory = node.type === 'directory'
  const children = childrenCache.get(node.path)
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
  const editorApps = availableApps.filter((app) =>
    ['cursor', 'vscode', 'xcode'].includes(app.id)
  )
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
            {isLoading && (
              <span className="text-xs text-muted-foreground ml-auto">...</span>
            )}
          </div>
        </ContextMenuTrigger>

        <ContextMenuContent className="w-48">
          {/* Finder */}
          {finderApp && (
            <ContextMenuItem onClick={() => handleOpenWith('finder')}>
              <span className="flex items-center gap-2">
                <span>📁</span>
                <span>Finder</span>
              </span>
              <span className="ml-auto text-xs text-muted-foreground">⌘O</span>
            </ContextMenuItem>
          )}

          {/* Editors */}
          {editorApps.length > 0 && (
            <>
              <ContextMenuSeparator />
              {editorApps.map((app) => (
                <ContextMenuItem key={app.id} onClick={() => handleOpenWith(app.id)}>
                  <span className="flex items-center gap-2">
                    {app.id === 'cursor' && <span>🔵</span>}
                    {app.id === 'vscode' && <span>🔷</span>}
                    {app.id === 'xcode' && <span>🔧</span>}
                    <span>{app.name}</span>
                  </span>
                </ContextMenuItem>
              ))}
            </>
          )}

          {/* Terminals */}
          {terminalApps.length > 0 && (
            <>
              <ContextMenuSeparator />
              {terminalApps.map((app) => (
                <ContextMenuItem key={app.id} onClick={() => handleOpenWith(app.id)}>
                  <span className="flex items-center gap-2">
                    {app.id === 'ghostty' && <span>👻</span>}
                    {app.id === 'iterm' && <span>📟</span>}
                    {app.id === 'terminal' && <span>⬛</span>}
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
                  <span>📋</span>
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
                childrenCache={childrenCache}
                loadChildren={loadChildren}
                availableApps={availableApps}
              />
            ))
          )}
        </>
      )}
    </>
  )
}

export function FileTree({ rootPath }: FileTreeProps) {
  const [rootChildren, setRootChildren] = useState<FileTreeNode[]>([])
  const [expandedPaths, setExpandedPaths] = useState<Set<string>>(new Set())
  const [childrenCache, setChildrenCache] = useState<Map<string, FileTreeNode[]>>(new Map())
  const [availableApps, setAvailableApps] = useState<DetectedApp[]>([])
  const [isLoading, setIsLoading] = useState(true)

  // Load root directory and detect apps on mount
  useEffect(() => {
    async function init() {
      setIsLoading(true)
      try {
        const [children, apps] = await Promise.all([
          window.electronAPI.listDirectory(rootPath),
          window.electronAPI.detectApps(),
        ])
        setRootChildren(children)
        setAvailableApps(apps)
      } catch (error) {
        console.error('Failed to load file tree:', error)
      }
      setIsLoading(false)
    }
    init()
  }, [rootPath])

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
    if (childrenCache.has(path)) return
    const children = await window.electronAPI.listDirectory(path)
    setChildrenCache((prev) => new Map(prev).set(path, children))
  }, [childrenCache])

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
          childrenCache={childrenCache}
          loadChildren={loadChildren}
          availableApps={availableApps}
        />
      ))}
    </div>
  )
}
