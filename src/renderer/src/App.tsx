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
import { useAgentStore } from './stores/agentStore'
import { RightPanel, RightPanelHeader, RightPanelContent } from './components/layout'
import { FileTree } from './components/FileTree'
import { Toaster } from '@/components/ui/sonner'
import { useChatScroll } from './hooks/useChatScroll'
import { ChevronDown, Eye, EyeOff } from 'lucide-react'
import { cn } from '@/lib/utils'
import { getBaseName } from './utils/path'
import { getDefaultModeForAgent } from '../../shared/mode-semantic'

function AppContent(): React.JSX.Element {
  const {
    // State
    projects,
    sessionsByProject,
    currentSession,
    sessionUpdates,
    runningSessionsStatus,
    isProcessing,
    isInitializing,
    sessionModeState,
    sessionModelState,
    isSwitchingAgent,

    // Actions
    createProject,
    toggleProjectExpanded,
    reorderProjects,
    deleteProject,
    createSession,
    selectSession,
    deleteSession,
    archiveSession,
    unarchiveSession,
    sendPrompt,
    cancelRequest,
    switchSessionAgent,
    setSessionMode,
    setSessionModel,
    updateSessionTitle
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

  // Agent store - for checking installation status
  const getAgent = useAgentStore((s) => s.getAgent)
  const currentAgentId = currentSession?.agentId
  const currentAgentInstalled = currentAgentId
    ? getAgent(currentAgentId)?.installed !== false
    : true

  // Default agent for new sessions (persisted in localStorage)
  const [defaultAgentId, setDefaultAgentId] = useState(() => {
    const saved = localStorage.getItem('multica:defaultAgentId')
    console.log('[App] Loading defaultAgentId from localStorage:', saved)
    return saved || 'claude-code'
  })

  // Wrapper to also persist to localStorage
  const handleSetDefaultAgent = useCallback((agentId: string) => {
    console.log('[App] Setting default agent:', agentId)
    localStorage.setItem('multica:defaultAgentId', agentId)
    console.log('[App] localStorage after save:', localStorage.getItem('multica:defaultAgentId'))
    setDefaultAgentId(agentId)
  }, [])

  // Default modes for each agent (persisted in localStorage)
  const [defaultModes, setDefaultModes] = useState<Record<string, string>>(() => {
    const modes: Record<string, string> = {}
    const agentIds = ['claude-code', 'codex', 'opencode']
    for (const agentId of agentIds) {
      const saved = localStorage.getItem(`multica:defaultMode:${agentId}`)
      if (saved) {
        modes[agentId] = saved
      }
    }
    return modes
  })

  // Wrapper to also persist to localStorage
  const handleSetDefaultMode = useCallback((agentId: string, modeId: string) => {
    localStorage.setItem(`multica:defaultMode:${agentId}`, modeId)
    setDefaultModes((prev) => ({ ...prev, [agentId]: modeId }))
  }, [])

  // Handler for "New Project" button - opens directory selector
  const handleNewProject = useCallback(async () => {
    const dir = await window.electronAPI.selectDirectory()
    if (dir) {
      // Check if the default agent is installed before creating project + session
      const agentCheck = await window.electronAPI.checkAgent(defaultAgentId)
      if (!agentCheck?.installed) {
        // Agent not installed - open Settings with highlight and pending folder
        openModal('settings', { highlightAgent: defaultAgentId, pendingFolder: dir })
        return
      }
      // Create project and first session
      const project = await createProject(dir)
      if (project) {
        const modeToUse = defaultModes[defaultAgentId] || getDefaultModeForAgent(defaultAgentId)
        await createSession(project.id, defaultAgentId, modeToUse)
      }
    }
  }, [createProject, createSession, defaultAgentId, defaultModes, openModal])

  // Handler for "+" button on a project - creates new session in that project
  const handleNewSessionInProject = useCallback(
    async (projectId: string) => {
      // Check if the default agent is installed
      const agentCheck = await window.electronAPI.checkAgent(defaultAgentId)
      if (!agentCheck?.installed) {
        // Agent not installed - open Settings with highlight
        openModal('settings', { highlightAgent: defaultAgentId })
        return
      }
      const modeToUse = defaultModes[defaultAgentId] || getDefaultModeForAgent(defaultAgentId)
      await createSession(projectId, defaultAgentId, modeToUse)
    },
    [createSession, defaultAgentId, defaultModes, openModal]
  )

  // Used by FileTree when creating session from folder
  const handleCreateSessionFromFolder = useCallback(
    async (cwd: string) => {
      // Check if the default agent is installed before creating session
      const agentCheck = await window.electronAPI.checkAgent(defaultAgentId)
      if (!agentCheck?.installed) {
        // Agent not installed - open Settings with highlight and pending folder
        openModal('settings', { highlightAgent: defaultAgentId, pendingFolder: cwd })
        return
      }
      // Create or get project, then create session
      const project = await createProject(cwd)
      if (project) {
        const modeToUse = defaultModes[defaultAgentId] || getDefaultModeForAgent(defaultAgentId)
        await createSession(project.id, defaultAgentId, modeToUse)
      }
    },
    [createProject, createSession, defaultAgentId, defaultModes, openModal]
  )

  const handleSelectSession = useCallback(
    async (sessionId: string) => {
      // Select session (agent starts automatically via resumeSession)
      await selectSession(sessionId)
    },
    [selectSession]
  )

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
          projects={projects}
          sessionsByProject={sessionsByProject}
          currentSessionId={currentSession?.id ?? null}
          processingSessionIds={runningSessionsStatus.processingSessionIds}
          permissionPendingSessionId={permissionPendingSessionId}
          onSelectSession={handleSelectSession}
          onNewProject={handleNewProject}
          onNewSession={handleNewSessionInProject}
          onToggleProjectExpanded={toggleProjectExpanded}
          onReorderProjects={reorderProjects}
          onUpdateSessionTitle={updateSessionTitle}
        />

        {/* Main area */}
        <main className="flex min-w-0 flex-1 flex-col overflow-hidden">
          {/* Status bar - fixed height */}
          <StatusBar
            sessionTitle={
              currentSession
                ? currentSession.title || `Session · ${currentSession.id.slice(0, 6)}`
                : undefined
            }
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
                  onSelectFolder={handleNewProject}
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
                {/* Warning when current agent is not installed */}
                {currentSession && !currentAgentInstalled && (
                  <div className="px-4 py-2 text-sm text-amber-600 dark:text-amber-400 bg-amber-500/10 border border-amber-500/20 rounded-md mx-4 mb-2">
                    Current agent is not installed. Please switch to another agent or reinstall it
                    in Settings.
                  </div>
                )}
                <MessageInput
                  sessionId={currentSession?.id}
                  onSend={sendPrompt}
                  onCancel={cancelRequest}
                  isProcessing={isProcessing}
                  disabled={
                    !currentSession ||
                    currentSession.directoryExists === false ||
                    !currentAgentInstalled
                  }
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
            <span className="text-sm font-medium truncate" title={currentSession?.workingDirectory}>
              {currentSession?.workingDirectory
                ? getBaseName(currentSession.workingDirectory)
                : 'All files'}
            </span>
            <button
              onClick={toggleShowHiddenFiles}
              className={cn(
                'p-1 rounded hover:bg-accent transition-colors flex-shrink-0',
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
            {currentSession && currentSession.workingDirectory ? (
              <FileTree
                rootPath={currentSession.workingDirectory}
                directoryExists={currentSession.directoryExists}
                onCreateSession={handleCreateSessionFromFolder}
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
        defaultModes={defaultModes}
        onSetDefaultMode={handleSetDefaultMode}
        onCreateSession={handleCreateSessionFromFolder}
        onDeleteSession={deleteSession}
        onArchiveSession={archiveSession}
        onUnarchiveSession={unarchiveSession}
        onDeleteProject={deleteProject}
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
