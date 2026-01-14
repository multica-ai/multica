/**
 * Main App component
 */
import { useEffect, useState } from 'react'
import { useApp } from './hooks/useApp'
import { ChatView, MessageInput, StatusBar } from './components'
import { AppSidebar } from './components/AppSidebar'
import { Modals } from './components/Modals'
import { ThemeProvider } from './contexts/ThemeContext'
import { SidebarProvider } from '@/components/ui/sidebar'
import { useModalStore } from './stores/modalStore'

function AppContent(): React.JSX.Element {
  const {
    // State
    sessions,
    currentSession,
    sessionUpdates,
    runningSessionsStatus,
    isProcessing,
    error,

    // Actions
    createSession,
    selectSession,
    deleteSession,
    sendPrompt,
    cancelRequest,
    clearError,
  } = useApp()

  const openModal = useModalStore((s) => s.openModal)

  // Default agent for new sessions
  const [defaultAgentId, setDefaultAgentId] = useState('opencode')

  // Auto-show new session dialog when no sessions exist
  useEffect(() => {
    if (!currentSession && sessions.length === 0) {
      openModal('newSession')
    }
  }, [currentSession, sessions.length, openModal])

  const handleNewSession = () => {
    openModal('newSession')
  }

  const handleCreateSession = async (cwd: string) => {
    // Create session with default agent (agent starts automatically)
    await createSession(cwd, defaultAgentId)
  }

  const handleSelectSession = async (sessionId: string) => {
    // Select session (agent starts automatically via resumeSession)
    await selectSession(sessionId)
  }

  // Check if current session has a running agent
  const isCurrentSessionRunning = currentSession
    ? runningSessionsStatus.sessionIds.includes(currentSession.id)
    : false

  return (
    <div className="flex h-screen flex-col bg-background text-foreground">
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
          onNewSession={handleNewSession}
        />

        {/* Main area */}
        <main className="flex flex-1 flex-col">
          {/* Status bar */}
          <StatusBar
            runningSessionsCount={runningSessionsStatus.runningSessions}
            currentSession={currentSession}
            isCurrentSessionRunning={isCurrentSessionRunning}
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
            disabled={!currentSession}
          />
        </main>
      </SidebarProvider>

      {/* Global modals */}
      <Modals
        defaultAgentId={defaultAgentId}
        onSetDefaultAgent={setDefaultAgentId}
        onCreateSession={handleCreateSession}
        onDeleteSession={deleteSession}
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
