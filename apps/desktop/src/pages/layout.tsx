import { Outlet, NavLink, useLocation } from 'react-router-dom'
import { Toaster } from '@multica/ui/components/ui/sonner'
import { Button } from '@multica/ui/components/ui/button'
import { HugeiconsIcon } from '@hugeicons/react'
import {
  Settings02Icon,
  Home01Icon,
  CodeIcon,
  PlugIcon,
  Comment01Icon,
} from '@hugeicons/core-free-icons'
import { cn } from '@multica/ui/lib/utils'
import { DeviceConfirmDialog } from '../components/device-confirm-dialog'
import ChatPage from './chat'

const tabs = [
  { path: '/', label: 'Home', icon: Home01Icon, exact: true },
  { path: '/chat', label: 'Chat', icon: Comment01Icon },
  { path: '/tools', label: 'Tools', icon: CodeIcon },
  { path: '/skills', label: 'Skills', icon: PlugIcon },
]

export default function Layout() {
  const location = useLocation()

  return (
    <div className="h-dvh flex flex-col bg-background">
      {/* Header */}
      <header className="flex items-center justify-between px-4 py-3 border-b">
        <div className="flex items-center gap-2">
          <span className="text-lg font-semibold">Multica</span>
        </div>
        <div className="flex items-center gap-1">
          <Button variant="ghost" size="icon">
            <HugeiconsIcon icon={Settings02Icon} className="size-5" />
          </Button>
        </div>
      </header>

      {/* Tabs */}
      <nav className="flex gap-1 px-4 py-2 border-b">
        {tabs.map((tab) => {
          const isActive = tab.exact
            ? location.pathname === tab.path
            : location.pathname.startsWith(tab.path)

          return (
            <NavLink key={tab.path} to={tab.path}>
              <Button
                variant={isActive ? 'secondary' : 'ghost'}
                size="sm"
                className={cn('gap-2', isActive && 'bg-secondary')}
              >
                <HugeiconsIcon icon={tab.icon} className="size-4" />
                {tab.label}
              </Button>
            </NavLink>
          )
        })}
      </nav>

      {/* Content */}
      <main className="flex-1 overflow-auto relative">
        {/* ChatPage is always mounted (cached), hidden via CSS */}
        <div className={cn('absolute inset-0', location.pathname === '/chat' ? '' : 'hidden')}>
          <ChatPage />
        </div>

        {/* Other routes render normally via Outlet */}
        {location.pathname !== '/chat' && (
          <div className="p-4 h-full">
            <Outlet />
          </div>
        )}
      </main>
      <Toaster />
      <DeviceConfirmDialog />
    </div>
  )
}
