/**
 * Layout components - panels and triggers
 * Desktop only - hidden on mobile
 */
import * as React from 'react'
import { PanelLeftIcon, PanelRightIcon } from 'lucide-react'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { useUIStore, RIGHT_PANEL_MIN_WIDTH, RIGHT_PANEL_MAX_WIDTH } from '@/stores/uiStore'
import { useSidebar } from '@/components/ui/sidebar'
import { useResize } from '@/hooks/useResize'

interface RightPanelProps {
  children: React.ReactNode
  className?: string
}

export function RightPanel({ children, className }: RightPanelProps): React.JSX.Element {
  const isOpen = useUIStore((s) => s.rightPanelOpen)
  const rightPanelWidth = useUIStore((s) => s.rightPanelWidth)
  const setRightPanelWidth = useUIStore((s) => s.setRightPanelWidth)

  const { isResizing, handleProps } = useResize({
    width: rightPanelWidth,
    minWidth: RIGHT_PANEL_MIN_WIDTH,
    maxWidth: RIGHT_PANEL_MAX_WIDTH,
    onWidthChange: setRightPanelWidth,
    direction: 'left'
  })

  return (
    <div
      className={cn(
        'hidden lg:block',
        // Disable transition during resize for smoother dragging
        !isResizing && 'transition-[width] duration-200 ease-linear',
        isOpen ? 'w-[var(--right-panel-width)]' : 'w-0'
      )}
      style={{ '--right-panel-width': `${rightPanelWidth}px` } as React.CSSProperties}
    >
      <div
        className={cn(
          'fixed inset-y-0 right-0 z-10 h-svh border-l bg-background flex flex-col',
          // Disable transition during resize for smoother dragging
          !isResizing && 'transition-[transform,opacity,width] duration-200 ease-linear',
          isOpen ? 'translate-x-0 opacity-100' : 'translate-x-full opacity-0',
          className
        )}
        style={{ width: `${rightPanelWidth}px` }}
      >
        {/* Resize handle */}
        {isOpen && (
          <div
            className={cn(
              'absolute inset-y-0 left-0 w-1 hover:bg-primary/20 active:bg-primary/30',
              isResizing && 'bg-primary/30'
            )}
            {...handleProps}
          />
        )}
        {children}
      </div>
    </div>
  )
}

// Trigger button - desktop only, secondary variant when panel is open
export function RightPanelTrigger({
  className,
  ...props
}: React.ComponentProps<typeof Button>): React.JSX.Element {
  const isOpen = useUIStore((s) => s.rightPanelOpen)
  const toggle = useUIStore((s) => s.toggleRightPanel)

  return (
    <Button
      variant={isOpen ? 'secondary' : 'ghost'}
      size="icon"
      className={cn('hidden lg:inline-flex size-7', className)}
      onClick={toggle}
      {...props}
    >
      <PanelRightIcon className="h-4 w-4" />
      <span className="sr-only">Toggle Right Panel</span>
    </Button>
  )
}

// Sidebar trigger - uses uiStore, secondary variant when open
export function SidebarTrigger({
  className,
  onClick,
  ...props
}: React.ComponentProps<typeof Button>): React.JSX.Element {
  const { toggleSidebar } = useSidebar()
  const isOpen = useUIStore((s) => s.sidebarOpen)

  return (
    <Button
      variant={isOpen ? 'secondary' : 'ghost'}
      size="icon"
      className={cn('size-7', className)}
      onClick={(event) => {
        onClick?.(event)
        toggleSidebar()
      }}
      {...props}
    >
      <PanelLeftIcon className="h-4 w-4" />
      <span className="sr-only">Toggle Sidebar</span>
    </Button>
  )
}

// Sub-components for consistent structure
export function RightPanelHeader({
  className,
  children,
  ...props
}: React.ComponentProps<'div'>): React.JSX.Element {
  return (
    <div className={cn('flex h-11 items-center border-b px-4', className)} {...props}>
      {children}
    </div>
  )
}

export function RightPanelContent({
  className,
  ...props
}: React.ComponentProps<'div'>): React.JSX.Element {
  return <div className={cn('flex-1 overflow-auto p-4', className)} {...props} />
}

export function RightPanelFooter({
  className,
  ...props
}: React.ComponentProps<'div'>): React.JSX.Element {
  return <div className={cn('border-t p-4', className)} {...props} />
}
