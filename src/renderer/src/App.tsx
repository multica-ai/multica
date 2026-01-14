/**
 * Main App component
 */
import { useState, useEffect } from 'react'
import { useApp } from './hooks/useApp'
import { ChatView, MessageInput, StatusBar, Settings } from './components'
import { AppSidebar } from './components/AppSidebar'
import { ThemeProvider } from './contexts/ThemeContext'
import { SidebarProvider } from '@/components/ui/sidebar'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'

function AppContent(): React.JSX.Element {
  const {
    // State
    sessions,
    currentSession,
    sessionUpdates,
    agentStatus,
    isProcessing,
    error,

    // Actions
    createSession,
    selectSession,
    deleteSession,
    startAgent,
    stopAgent,
    switchAgent,
    sendPrompt,
    cancelRequest,
    clearError,
  } = useApp()

  // New session dialog state
  const [showNewSession, setShowNewSession] = useState(false)
  const [newSessionCwd, setNewSessionCwd] = useState('')

  // Settings dialog state
  const [showSettings, setShowSettings] = useState(false)

  // Auto-show new session dialog when agent is running but no session
  useEffect(() => {
    if (agentStatus.state === 'running' && !currentSession && sessions.length === 0) {
      setNewSessionCwd('')
      setShowNewSession(true)
    }
  }, [agentStatus.state, currentSession, sessions.length])

  const handleNewSession = () => {
    setNewSessionCwd('')
    setShowNewSession(true)
  }

  const handleCreateSession = async () => {
    if (!newSessionCwd.trim()) return

    // Ensure agent is running
    if (agentStatus.state !== 'running') {
      await startAgent('opencode')
    }

    await createSession(newSessionCwd.trim())
    setShowNewSession(false)
    setNewSessionCwd('')
  }

  const handleSelectSession = async (sessionId: string) => {
    // Ensure agent is running
    if (agentStatus.state !== 'running') {
      const session = sessions.find((s) => s.id === sessionId)
      if (session) {
        await startAgent(session.agentId)
      }
    }
    await selectSession(sessionId)
  }

  return (
    <div className="flex h-screen flex-col bg-[var(--color-background)] text-[var(--color-text)]">
      {/* Error banner */}
      {error && (
        <div className="flex items-center justify-between bg-red-600 px-4 py-2 text-sm text-white">
          <span>{error}</span>
          <button onClick={clearError} className="hover:underline">
            Dismiss
          </button>
        </div>
      )}

      {/* Main content */}
      <SidebarProvider className="flex-1 overflow-hidden">
        {/* Sidebar */}
        <AppSidebar
          sessions={sessions}
          currentSessionId={currentSession?.id ?? null}
          onSelect={handleSelectSession}
          onDelete={deleteSession}
          onNewSession={handleNewSession}
        />

        {/* Main area */}
        <main className="flex flex-1 flex-col">
          {/* Status bar */}
          <StatusBar
            agentStatus={agentStatus}
            currentSession={currentSession}
            onStartAgent={() => startAgent('opencode')}
            onStopAgent={stopAgent}
            onOpenSettings={() => setShowSettings(true)}
          />

          {/* Chat view */}
          <ChatView
            updates={sessionUpdates}
            isProcessing={isProcessing}
            hasSession={!!currentSession}
            onNewSession={handleNewSession}
          />

          {/* Input */}
          <MessageInput
            onSend={sendPrompt}
            onCancel={cancelRequest}
            isProcessing={isProcessing}
            disabled={!currentSession || agentStatus.state !== 'running'}
          />
        </main>
      </SidebarProvider>

      {/* New session dialog */}
      <Dialog open={showNewSession} onOpenChange={setShowNewSession}>
        <DialogContent className="max-w-md">
          <DialogHeader>
            <DialogTitle>New Session</DialogTitle>
          </DialogHeader>

          <div className="space-y-4">
            <div className="space-y-2">
              <label className="text-sm text-muted-foreground">
                Working Directory
              </label>
              <div className="flex gap-2">
                <Input
                  value={newSessionCwd}
                  onChange={(e) => setNewSessionCwd(e.target.value)}
                  placeholder="Select a directory..."
                  onKeyDown={(e) => {
                    if (e.key === 'Enter') handleCreateSession()
                  }}
                />
                <Button
                  variant="outline"
                  onClick={async () => {
                    const dir = await window.electronAPI.selectDirectory()
                    if (dir) setNewSessionCwd(dir)
                  }}
                >
                  Browse...
                </Button>
              </div>
            </div>
          </div>

          <DialogFooter>
            <Button variant="ghost" onClick={() => setShowNewSession(false)}>
              Cancel
            </Button>
            <Button onClick={handleCreateSession} disabled={!newSessionCwd.trim()}>
              Create
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Settings dialog */}
      <Settings
        isOpen={showSettings}
        onClose={() => setShowSettings(false)}
        currentAgentId={agentStatus.state === 'running' ? agentStatus.agentId : null}
        onSwitchAgent={switchAgent}
      />
    </div>
  )
}

function App(): React.JSX.Element {
  return (
    <ThemeProvider>
      <AppContent />
    </ThemeProvider>
  )
}

export default App
