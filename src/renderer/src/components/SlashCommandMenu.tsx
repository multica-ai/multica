/**
 * Slash command autocomplete menu
 *
 * Displays a list of available commands when the user types "/" in the message input.
 * Supports keyboard navigation and command selection.
 */
import { useEffect, useRef, useCallback } from 'react'
import type { AvailableCommand } from '../../../shared/types'
import { cn } from '@/lib/utils'

// Re-export utility functions for convenience
export { parseSlashCommand, validateCommand } from '../utils/slashCommand'

interface SlashCommandMenuProps {
  /** Available commands to display */
  commands: AvailableCommand[]
  /** Current filter string (after the "/") */
  filter: string
  /** Currently selected index */
  selectedIndex: number
  /** Callback when an index is selected */
  onSelect: (command: AvailableCommand) => void
  /** Callback when selected index changes */
  onIndexChange: (index: number) => void
  /** Callback when menu should close */
  onClose: () => void
  /** Whether the menu is visible */
  visible: boolean
}

export function SlashCommandMenu({
  commands,
  filter,
  selectedIndex,
  onSelect,
  onIndexChange,
  onClose,
  visible
}: SlashCommandMenuProps) {
  const menuRef = useRef<HTMLDivElement>(null)
  const itemRefs = useRef<Map<number, HTMLButtonElement>>(new Map())

  // Filter commands based on input
  const filteredCommands = commands.filter((cmd) =>
    cmd.name.toLowerCase().startsWith(filter.toLowerCase())
  )

  // Clamp selected index to valid range
  const clampedIndex = Math.max(0, Math.min(selectedIndex, filteredCommands.length - 1))

  // Scroll selected item into view
  useEffect(() => {
    const selectedItem = itemRefs.current.get(clampedIndex)
    if (selectedItem && menuRef.current) {
      selectedItem.scrollIntoView({ block: 'nearest', behavior: 'smooth' })
    }
  }, [clampedIndex])

  // Handle keyboard navigation
  const handleKeyDown = useCallback(
    (e: KeyboardEvent) => {
      if (!visible || filteredCommands.length === 0) return

      switch (e.key) {
        case 'ArrowDown':
          e.preventDefault()
          onIndexChange((clampedIndex + 1) % filteredCommands.length)
          break
        case 'ArrowUp':
          e.preventDefault()
          onIndexChange((clampedIndex - 1 + filteredCommands.length) % filteredCommands.length)
          break
        case 'Enter':
        case 'Tab':
          e.preventDefault()
          if (filteredCommands[clampedIndex]) {
            onSelect(filteredCommands[clampedIndex])
          }
          break
        case 'Escape':
          e.preventDefault()
          onClose()
          break
      }
    },
    [visible, filteredCommands, clampedIndex, onIndexChange, onSelect, onClose]
  )

  // Register keyboard listener
  useEffect(() => {
    if (visible) {
      document.addEventListener('keydown', handleKeyDown, true)
      return () => document.removeEventListener('keydown', handleKeyDown, true)
    }
    return undefined
  }, [visible, handleKeyDown])

  // Don't render if not visible or no matching commands
  if (!visible || filteredCommands.length === 0) {
    return null
  }

  return (
    <div
      ref={menuRef}
      className="absolute bottom-full left-0 mb-2 w-full max-w-md bg-popover border border-border rounded-lg shadow-lg overflow-hidden z-50"
    >
      <div className="max-h-[240px] overflow-y-auto py-1">
        {filteredCommands.map((command, index) => (
          <button
            key={command.name}
            ref={(el) => {
              if (el) {
                itemRefs.current.set(index, el)
              } else {
                itemRefs.current.delete(index)
              }
            }}
            onClick={() => onSelect(command)}
            className={cn(
              'w-full text-left px-3 py-2 flex flex-col gap-0.5 transition-colors',
              index === clampedIndex ? 'bg-accent' : 'hover:bg-accent/50'
            )}
          >
            <div className="flex items-center gap-2">
              <span className="text-sm font-medium text-foreground">/{command.name}</span>
              {command.input && (
                <span className="text-xs text-muted-foreground">{'{argument}'}</span>
              )}
            </div>
            {command.description && (
              <span className="text-xs text-muted-foreground line-clamp-1">
                {command.description}
              </span>
            )}
          </button>
        ))}
      </div>
    </div>
  )
}
