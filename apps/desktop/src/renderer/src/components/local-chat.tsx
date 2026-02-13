import { useState, useCallback, useEffect, useRef } from 'react'
import { useNavigate } from 'react-router-dom'
import { Loading } from '@multica/ui/components/ui/loading'
import { ChatView } from '@multica/ui/components/chat-view'
import { useLocalChat } from '../hooks/use-local-chat'
import { useSubagentPolling } from '../hooks/use-subagent-polling'
import { useSubagentsStore } from '../stores/subagents'
import { useProviderStore } from '../stores/provider'
import { ApiKeyDialog } from './api-key-dialog'
import { OAuthDialog } from './oauth-dialog'
import { SubagentStatusBar } from './subagent-status-bar'
import { SubagentDashboard } from './subagent-dashboard'

interface LocalChatProps {
  initialPrompt?: string
}

export function LocalChat({ initialPrompt }: LocalChatProps) {
  const navigate = useNavigate()
  const {
    agentId,
    initError,
    messages,
    streamingIds,
    isLoading,
    isLoadingHistory,
    isLoadingMore,
    hasMore,
    error,
    pendingApprovals,
    sendMessage,
    abortGeneration,
    loadMore,
    resolveApproval,
    clearError,
  } = useLocalChat()

  const { providers, current, setProvider: switchProvider, refresh: refreshProviders } = useProviderStore()

  // Subagent polling + dashboard
  useSubagentPolling(agentId)
  const subagentRuns = useSubagentsStore((s) => s.runs)
  const [dashboardOpen, setDashboardOpen] = useState(false)

  // Provider config dialog state
  const [apiKeyDialogOpen, setApiKeyDialogOpen] = useState(false)
  const [oauthDialogOpen, setOauthDialogOpen] = useState(false)

  const handleConfigureProvider = useCallback(() => {
    const providerId = current?.provider
    if (!providerId) return

    const meta = providers.find((p) => p.id === providerId)
    if (!meta) return

    if (meta.authMethod === 'oauth') {
      setOauthDialogOpen(true)
    } else {
      setApiKeyDialogOpen(true)
    }
  }, [current, providers])

  const handleProviderConfigSuccess = useCallback(async () => {
    const providerId = current?.provider
    if (!providerId) return

    await refreshProviders()
    await switchProvider(providerId)
    clearError()
  }, [current, refreshProviders, switchProvider, clearError])

  // Derive provider info for dialogs
  const currentMeta = current ? providers.find((p) => p.id === current.provider) : null

  // Auto-send initial prompt after a short delay
  const hasSentInitialPrompt = useRef(false)
  useEffect(() => {
    if (!agentId || !initialPrompt || hasSentInitialPrompt.current) return

    const timer = setTimeout(() => {
      hasSentInitialPrompt.current = true
      sendMessage(initialPrompt)
      // Remove prompt from URL to prevent re-sending on back navigation
      navigate('/chat', { replace: true })
    }, 500)

    return () => clearTimeout(timer)
  }, [agentId, initialPrompt, sendMessage, navigate])

  if (initError) {
    return (
      <div className="flex-1 flex items-center justify-center text-sm text-destructive">
        {initError}
      </div>
    )
  }

  if (!agentId) {
    return (
      <div className="flex-1 flex items-center justify-center gap-2 text-muted-foreground text-sm">
        <Loading />
        Initializing...
      </div>
    )
  }

  // Show "Configure" button when error is about provider/API key
  const errorAction = error?.code === 'AGENT_ERROR' && currentMeta
    ? { label: 'Configure', onClick: handleConfigureProvider }
    : undefined

  return (
    <>
      <ChatView
        messages={messages}
        streamingIds={streamingIds}
        isLoading={isLoading}
        isLoadingHistory={isLoadingHistory}
        isLoadingMore={isLoadingMore}
        hasMore={hasMore}
        error={error}
        pendingApprovals={pendingApprovals}
        sendMessage={sendMessage}
        onAbort={abortGeneration}
        loadMore={loadMore}
        resolveApproval={resolveApproval}
        errorAction={errorAction}
        bottomSlot={
          <SubagentStatusBar
            runs={subagentRuns}
            onViewClick={() => setDashboardOpen(true)}
          />
        }
      />

      {currentMeta && currentMeta.authMethod === 'api-key' && (
        <ApiKeyDialog
          open={apiKeyDialogOpen}
          onOpenChange={setApiKeyDialogOpen}
          providerId={currentMeta.id}
          providerName={currentMeta.name}
          onSuccess={handleProviderConfigSuccess}
        />
      )}

      {currentMeta && currentMeta.authMethod === 'oauth' && (
        <OAuthDialog
          open={oauthDialogOpen}
          onOpenChange={setOauthDialogOpen}
          providerId={currentMeta.id}
          providerName={currentMeta.name}
          loginCommand={currentMeta.loginCommand}
          onSuccess={handleProviderConfigSuccess}
        />
      )}

      <SubagentDashboard
        open={dashboardOpen}
        onOpenChange={setDashboardOpen}
        runs={subagentRuns}
      />
    </>
  )
}
