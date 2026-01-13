/**
 * Main App component
 */
import { useState, useEffect } from 'react'
import { useApp } from './hooks/useApp'
import { SessionList, ChatView, MessageInput, StatusBar, Settings } from './components'

function App(): React.JSX.Element {
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
    setNewSessionCwd(process.cwd?.() || '')
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
      {/* Title bar drag region (macOS) */}
      <div className="titlebar-drag-region h-8 flex-shrink-0" />

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
      <div className="flex flex-1 overflow-hidden">
        {/* Sidebar */}
        <SessionList
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
      </div>

      {/* New session dialog */}
      {showNewSession && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50">
          <div className="w-full max-w-md rounded-lg bg-[var(--color-surface)] p-6 shadow-xl">
            <h2 className="mb-4 text-lg font-semibold">New Session</h2>

            <label className="mb-2 block text-sm text-[var(--color-text-muted)]">
              Working Directory
            </label>
            <div className="mb-4 flex gap-2">
              <input
                type="text"
                value={newSessionCwd}
                onChange={(e) => setNewSessionCwd(e.target.value)}
                placeholder="Select a directory..."
                className="flex-1 rounded-lg border border-[var(--color-border)] bg-[var(--color-background)] px-4 py-2 text-[var(--color-text)] outline-none placeholder:text-[var(--color-text-muted)] focus:border-[var(--color-primary)]"
                onKeyDown={(e) => {
                  if (e.key === 'Enter') handleCreateSession()
                  if (e.key === 'Escape') setShowNewSession(false)
                }}
              />
              <button
                onClick={async () => {
                  const dir = await window.electronAPI.selectDirectory()
                  if (dir) setNewSessionCwd(dir)
                }}
                className="flex-shrink-0 rounded-lg border border-[var(--color-border)] bg-[var(--color-surface-hover)] px-4 py-2 text-[var(--color-text)] transition-colors hover:bg-[var(--color-border)]"
              >
                Browse...
              </button>
            </div>

            <div className="flex justify-end gap-2">
              <button
                onClick={() => setShowNewSession(false)}
                className="rounded-lg px-4 py-2 text-[var(--color-text-muted)] transition-colors hover:bg-[var(--color-surface-hover)]"
              >
                Cancel
              </button>
              <button
                onClick={handleCreateSession}
                disabled={!newSessionCwd.trim()}
                className="rounded-lg bg-[var(--color-primary)] px-4 py-2 font-medium text-white transition-colors hover:bg-[var(--color-primary-dark)] disabled:opacity-50"
              >
                Create
              </button>
            </div>
          </div>
        </div>
      )}

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

export default App
