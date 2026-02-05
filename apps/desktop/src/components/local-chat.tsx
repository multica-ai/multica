import { Loading } from '@multica/ui/components/ui/loading'
import { ChatView } from '@multica/ui/components/chat-view'
import { useLocalChat } from '../hooks/use-local-chat'

export function LocalChat() {
  const {
    agentId,
    initError,
    messages,
    streamingIds,
    isLoading,
    isLoadingHistory,
    error,
    pendingApprovals,
    sendMessage,
    resolveApproval,
  } = useLocalChat()

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

  return (
    <ChatView
      messages={messages}
      streamingIds={streamingIds}
      isLoading={isLoading}
      isLoadingHistory={isLoadingHistory}
      error={error}
      pendingApprovals={pendingApprovals}
      sendMessage={sendMessage}
      resolveApproval={resolveApproval}
    />
  )
}
