/**
 * Main App component
 */
import { useState, useEffect, useCallback } from 'react'
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
import { useChatScroll } from './hooks/useChatScroll'
import { ChevronDown, Eye, EyeOff } from 'lucide-react'
import { cn } from '@/lib/utils'

function AppContent(): React.JSX.Element {
  const {
    // State
    sessions,
    currentSession,
    sessionUpdates,
    runningSessionsStatus,
    isProcessing,
    isInitializing,
    sessionModeState,
    sessionModelState,
    isSwitchingAgent,

    // Actions
    createSession,
    selectSession,
    deleteSession,
    clearCurrentSession,
    sendPrompt,
    cancelRequest,
    switchSessionAgent,
    setSessionMode,
    setSessionModel
  } = useApp()

  // UI state
  const sidebarOpen = useUIStore((s) => s.sidebarOpen)
  const setSidebarOpen = useUIStore((s) => s.setSidebarOpen)
  const showHiddenFiles = useUIStore((s) => s.showHiddenFiles)
  const toggleShowHiddenFiles = useUIStore((s) => s.toggleShowHiddenFiles)

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
  const handleSetDefaultAgent = useCallback((agentId: string) => {
    localStorage.setItem('multica:defaultAgentId', agentId)
    setDefaultAgentId(agentId)
  }, [])

  const handleNewSession = useCallback(() => {
    clearCurrentSession()
  }, [clearCurrentSession])

  const handleCreateSession = useCallback(
    async (cwd: string) => {
      // Create session with default agent (agent starts automatically)
      await createSession(cwd, defaultAgentId)
    },
    [createSession, defaultAgentId]
  )

  const handleSelectFolder = useCallback(async () => {
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
  }, [createSession, defaultAgentId, openModal])

  const handleSelectSession = useCallback(
    async (sessionId: string) => {
      // Select session (agent starts automatically via resumeSession)
      await selectSession(sessionId)
    },
    [selectSession]
  )

  // Check if current session has a running agent
  const isCurrentSessionRunning = currentSession
    ? runningSessionsStatus.sessionIds.includes(currentSession.id)
    : false

  // Handler for deleting the current session (opens delete modal)
  const handleDeleteCurrentSession = useCallback(() => {
    if (!currentSession) return
    openModal('deleteSession', currentSession)
  }, [currentSession, openModal])

  // Chat scroll - managed at App level for unified scroll context
  const { containerRef, bottomRef, isAtBottom, handleScroll, scrollToBottom, onContentUpdate } =
    useChatScroll({
      sessionId: currentSession?.id ?? null,
      isStreaming: isProcessing
    })

  // Memoized scroll button handler
  const handleScrollToBottom = useCallback(() => scrollToBottom(true), [scrollToBottom])

  // Trigger scroll update when content changes
  useEffect(() => {
    onContentUpdate()
  }, [sessionUpdates, onContentUpdate])

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
        <main className="flex min-w-0 flex-1 flex-col overflow-hidden">
          {/* Status bar - fixed height */}
          <StatusBar
            runningSessionsCount={runningSessionsStatus.runningSessions}
            currentSession={currentSession}
            isCurrentSessionRunning={isCurrentSessionRunning}
          />

          {/* Chat and Input container */}
          <div className="relative flex-1 overflow-hidden flex flex-col">
            {/* Chat scroll area - only messages */}
            <div ref={containerRef} onScroll={handleScroll} className="flex-1 overflow-y-auto px-4">
              <div className="mx-auto max-w-3xl pb-12 px-8 min-h-full flex flex-col">
                <ChatView
                  updates={sessionUpdates}
                  isProcessing={isProcessing}
                  hasSession={!!currentSession}
                  isInitializing={isInitializing}
                  currentSessionId={currentSession?.id ?? null}
                  onSelectFolder={handleSelectFolder}
                  bottomRef={bottomRef}
                  currentModelName={
                    sessionModelState?.availableModels.find(
                      (m) => m.modelId === sessionModelState?.currentModelId
                    )?.name
                  }
                  currentAgentId={currentSession?.agentId}
                />
              </div>
            </div>

            {/* Input area - fixed at bottom, outside scroll */}
            <div>
              <div className="relative mx-auto max-w-3xl px-4">
                {/* Scroll to bottom button - above input, left-aligned */}
                {!isAtBottom && currentSession && (
                  <div className="absolute bottom-full left-4 pb-2 pointer-events-none">
                    <button
                      onClick={handleScrollToBottom}
                      className={cn(
                        'pointer-events-auto',
                        'flex items-center gap-1.5 px-2.5 py-1 rounded-md',
                        'bg-card/80 backdrop-blur-sm border border-border/50',
                        'text-xs text-muted-foreground hover:text-foreground hover:bg-card',
                        'shadow-md hover:shadow-lg',
                        'transition-all duration-200 ease-out cursor-pointer',
                        'animate-in fade-in slide-in-from-bottom-2 duration-200'
                      )}
                    >
                      <ChevronDown className="h-3 w-3" />
                      <span>Scroll to bottom</span>
                    </button>
                  </div>
                )}
                <MessageInput
                  sessionId={currentSession?.id}
                  onSend={sendPrompt}
                  onCancel={cancelRequest}
                  isProcessing={isProcessing}
                  disabled={!currentSession || currentSession.directoryExists === false}
                  workingDirectory={currentSession?.workingDirectory}
                  currentAgentId={currentSession?.agentId}
                  onAgentChange={switchSessionAgent}
                  isSwitchingAgent={isSwitchingAgent}
                  isInitializing={isInitializing}
                  directoryExists={currentSession?.directoryExists}
                  onDeleteSession={handleDeleteCurrentSession}
                  sessionModeState={sessionModeState}
                  sessionModelState={sessionModelState}
                  onModeChange={setSessionMode}
                  onModelChange={setSessionModel}
                />
              </div>
            </div>
          </div>
        </main>

        {/* Right panel - file tree */}
        <RightPanel>
          <RightPanelHeader className="justify-between">
            <span className="text-sm font-medium">All files</span>
            <button
              onClick={toggleShowHiddenFiles}
              className={cn(
                'p-1 rounded hover:bg-accent transition-colors',
                showHiddenFiles ? 'text-foreground' : 'text-muted-foreground'
              )}
              title={showHiddenFiles ? 'Hide hidden files' : 'Show hidden files'}
              aria-label={showHiddenFiles ? 'Hide hidden files' : 'Show hidden files'}
              aria-pressed={showHiddenFiles}
            >
              {showHiddenFiles ? (
                <Eye className="h-3.5 w-3.5" />
              ) : (
                <EyeOff className="h-3.5 w-3.5" />
              )}
            </button>
          </RightPanelHeader>
          <RightPanelContent className="p-0">
            {currentSession ? (
              <FileTree
                rootPath={currentSession.workingDirectory}
                directoryExists={currentSession.directoryExists}
                onCreateSession={handleCreateSession}
              />
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
