/**
 * Status bar component - top bar with session name and sidebar toggles
 */
import { useSidebar } from '@/components/ui/sidebar'
import { SidebarTrigger, RightPanelTrigger } from './layout'
import { cn } from '@/lib/utils'

interface StatusBarProps {
  sessionTitle?: string
}

export function StatusBar({ sessionTitle }: StatusBarProps): React.JSX.Element {
  const { state, isMobile } = useSidebar()

  // Need left padding for traffic lights when sidebar is not visible
  const needsTrafficLightPadding = state === 'collapsed' || isMobile

  return (
    <div
      className={cn(
        'titlebar-drag-region flex h-11 items-center justify-between px-4',
        needsTrafficLightPadding && 'pl-24'
      )}
    >
      {/* Left: Sidebar trigger */}
      <div className="titlebar-no-drag">
        <SidebarTrigger className="-ml-1" />
      </div>

      {/* Center: Session title */}
      {sessionTitle && (
        <span className="text-sm font-medium text-muted-foreground truncate max-w-[200px]">
          {sessionTitle}
        </span>
      )}

      {/* Right: Right panel trigger */}
      <div className="titlebar-no-drag">
        <RightPanelTrigger />
      </div>
    </div>
  )
}
