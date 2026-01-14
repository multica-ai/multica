/**
 * Layout components - panels and triggers
 * Desktop only - hidden on mobile
 */
import * as React from 'react'
import { PanelLeftIcon, PanelRightIcon } from 'lucide-react'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { useUIStore } from '@/stores/uiStore'
import { useSidebar } from '@/components/ui/sidebar'

const RIGHT_PANEL_WIDTH = '20rem' // 320px

interface RightPanelProps {
  children: React.ReactNode
  className?: string
}

export function RightPanel({ children, className }: RightPanelProps) {
  const isOpen = useUIStore((s) => s.rightPanelOpen)

  return (
    <div
      className={cn(
        'hidden lg:block',
        'transition-[width] duration-200 ease-linear',
        isOpen ? 'w-[var(--right-panel-width)]' : 'w-0'
      )}
      style={{ '--right-panel-width': RIGHT_PANEL_WIDTH } as React.CSSProperties}
    >
      <div
        className={cn(
          'fixed inset-y-0 right-0 z-10 h-svh border-l bg-background',
          'transition-[transform,opacity] duration-200 ease-linear',
          isOpen ? 'translate-x-0 opacity-100' : 'translate-x-full opacity-0',
          className
        )}
        style={{ width: RIGHT_PANEL_WIDTH }}
      >
        {children}
      </div>
    </div>
  )
}

// Trigger button - desktop only, secondary variant when panel is open
export function RightPanelTrigger({
  className,
  ...props
}: React.ComponentProps<typeof Button>) {
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
}: React.ComponentProps<typeof Button>) {
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
}: React.ComponentProps<'div'>) {
  const toggle = useUIStore((s) => s.toggleRightPanel)

  return (
    <div
      className={cn('group flex h-11 items-center border-b px-4', className)}
      {...props}
    >
      {children}
      <Button
        variant="ghost"
        size="icon"
        className="ml-auto size-7 opacity-0 group-hover:opacity-100 transition-opacity"
        onClick={toggle}
      >
        <PanelRightIcon className="h-4 w-4" />
        <span className="sr-only">Close Right Panel</span>
      </Button>
    </div>
  )
}

export function RightPanelContent({
  className,
  ...props
}: React.ComponentProps<'div'>) {
  return (
    <div
      className={cn('flex-1 overflow-auto p-4', className)}
      {...props}
    />
  )
}

export function RightPanelFooter({
  className,
  ...props
}: React.ComponentProps<'div'>) {
  return (
    <div
      className={cn('border-t p-4', className)}
      {...props}
    />
  )
}
