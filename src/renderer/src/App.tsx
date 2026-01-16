/**
 * Main App component
 */
import { useState } from 'react'
import { useApp } from './hooks/useApp'
import { ChatView, MessageInput, StatusBar, UpdateNotification } from './components'
import { AppSidebar } from './components/AppSidebar'
import { Modals } from './components/Modals'
import { ThemeProvider } from './contexts/ThemeContext'
import { SidebarProvider } from '@/components/ui/sidebar'
import { useUIStore } from './stores/uiStore'
import { usePermissionStore } from './stores/permissionStore'
import { useModalStore } from './stores/modalStore'
import { RightPanel, RightPanelHeader, RightPanelContent } from './components/layout'
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
    isInitializing,
    isSwitchingAgent,

    // Actions
    createSession,
    selectSession,
    deleteSession,
    clearCurrentSession,
    sendPrompt,
    cancelRequest,
    switchSessionAgent
  } = useApp()

  // UI state
  const sidebarOpen = useUIStore((s) => s.sidebarOpen)
  const setSidebarOpen = useUIStore((s) => s.setSidebarOpen)

  // Permission state - get the session ID that has a pending permission request
  const pendingPermission = usePermissionStore((s) => s.pendingRequests[0] ?? null)
  const permissionPendingSessionId = pendingPermission?.multicaSessionId ?? null

  // Modal actions
  const openModal = useModalStore((s) => s.openModal)

  // Default agent for new sessions (persisted in localStorage)
  const [defaultAgentId, setDefaultAgentId] = useState(() => {
    // Load from localStorage on initial render
    const saved = localStorage.getItem('multica:defaultAgentId')
    return saved || 'claude-code' // Default to claude-code if not set
  })

  // Wrapper to also persist to localStorage
  const handleSetDefaultAgent = (agentId: string) => {
    localStorage.setItem('multica:defaultAgentId', agentId)
    setDefaultAgentId(agentId)
  }

  const handleNewSession = () => {
    clearCurrentSession()
  }

  const handleCreateSession = async (cwd: string) => {
    // Create session with default agent (agent starts automatically)
    await createSession(cwd, defaultAgentId)
  }

  const handleSelectFolder = async () => {
    const dir = await window.electronAPI.selectDirectory()
    if (dir) {
      // Check if the default agent is installed before creating session
      const agentCheck = await window.electronAPI.checkAgent(defaultAgentId)
      if (!agentCheck?.installed) {
        // Agent not installed - open Settings with highlight and pending folder
        openModal('settings', { highlightAgent: defaultAgentId, pendingFolder: dir })
        return
      }
      await createSession(dir, defaultAgentId)
    }
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
          processingSessionIds={runningSessionsStatus.processingSessionIds}
          permissionPendingSessionId={permissionPendingSessionId}
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
            isInitializing={isInitializing}
            currentSessionId={currentSession?.id ?? null}
            onSelectFolder={handleSelectFolder}
          />

          {/* Input */}
          <MessageInput
            onSend={sendPrompt}
            onCancel={cancelRequest}
            isProcessing={isProcessing}
            disabled={!currentSession}
            workingDirectory={currentSession?.workingDirectory}
            currentAgentId={currentSession?.agentId}
            onAgentChange={switchSessionAgent}
            isSwitchingAgent={isSwitchingAgent}
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
        onSetDefaultAgent={handleSetDefaultAgent}
        onCreateSession={handleCreateSession}
        onDeleteSession={deleteSession}
      />

      {/* Toast notifications */}
      <Toaster position="bottom-right" />

      {/* Update notification */}
      <UpdateNotification />
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
