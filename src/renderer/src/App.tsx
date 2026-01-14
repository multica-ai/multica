/**
 * Main App component
 */
import { useState } from 'react'
import { useApp } from './hooks/useApp'
import { ChatView, MessageInput, StatusBar } from './components'
import { AppSidebar } from './components/AppSidebar'
import { Modals } from './components/Modals'
import { ThemeProvider } from './contexts/ThemeContext'
import { SidebarProvider } from '@/components/ui/sidebar'
import { useModalStore } from './stores/modalStore'
import { useUIStore } from './stores/uiStore'
import {
  RightPanel,
  RightPanelHeader,
  RightPanelContent,
} from './components/layout'
import { FileTree } from './components/FileTree'
import { Toaster } from '@/components/ui/sonner'

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

  // UI state
  const sidebarOpen = useUIStore((s) => s.sidebarOpen)
  const setSidebarOpen = useUIStore((s) => s.setSidebarOpen)

  // Default agent for new sessions
  const [defaultAgentId, setDefaultAgentId] = useState('opencode')

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
      <SidebarProvider
        open={sidebarOpen}
        onOpenChange={setSidebarOpen}
        className="flex-1 overflow-hidden"
      >
        {/* Sidebar */}
        <AppSidebar
          sessions={sessions}
          currentSessionId={currentSession?.id ?? null}
          onSelect={handleSelectSession}
          onNewSession={handleNewSession}
        />

        {/* Main area */}
        <main className="flex min-w-0 flex-1 flex-col">
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

        {/* Right panel - file tree */}
        <RightPanel>
          <RightPanelHeader>
            <span className="text-sm font-medium">All files</span>
          </RightPanelHeader>
          <RightPanelContent className="p-0">
            {currentSession ? (
              <FileTree rootPath={currentSession.workingDirectory} />
            ) : (
              <div className="flex h-full items-center justify-center text-muted-foreground p-4">
                <p className="text-sm">No session selected</p>
              </div>
            )}
          </RightPanelContent>
        </RightPanel>
      </SidebarProvider>

      {/* Global modals */}
      <Modals
        defaultAgentId={defaultAgentId}
        onSetDefaultAgent={setDefaultAgentId}
        onCreateSession={handleCreateSession}
        onDeleteSession={deleteSession}
      />

      {/* Toast notifications */}
      <Toaster position="bottom-right" />
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
