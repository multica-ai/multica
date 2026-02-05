import { Button } from '@multica/ui/components/ui/button'
import { RemoteChat } from '../components/remote-chat'
import { LocalChat } from '../components/local-chat'
import { useChatModeStore } from '../stores/chat-mode'
import { useGatewayConnection, type UseGatewayConnectionReturn } from '@multica/hooks/use-gateway-connection'

function ModeNav({ gateway }: { gateway: UseGatewayConnectionReturn }) {
  const { mode, setMode } = useChatModeStore()

  if (mode === 'select') return null

  return (
    <div className="flex items-center gap-1 px-6 py-1 shrink-0">
      <NavButton active={mode === 'local'} onClick={() => setMode('local')}>
        Local
      </NavButton>
      <NavButton active={mode === 'remote'} onClick={() => setMode('remote')}>
        Remote
      </NavButton>

      {mode === 'remote' && gateway.pageState === 'connected' && (
        <>
          <div className="flex-1" />
          <button
            onClick={gateway.disconnect}
            className="text-xs text-muted-foreground hover:text-foreground"
          >
            Disconnect
          </button>
        </>
      )}
    </div>
  )
}

function NavButton({
  active,
  onClick,
  children,
}: {
  active: boolean
  onClick: () => void
  children: React.ReactNode
}) {
  return (
    <button
      onClick={onClick}
      className={`px-3 py-1 text-xs rounded-md transition-colors ${
        active
          ? 'bg-accent text-accent-foreground font-medium'
          : 'text-muted-foreground hover:text-foreground hover:bg-accent/50'
      }`}
    >
      {children}
    </button>
  )
}

function ModeSelect() {
  const setMode = useChatModeStore((s) => s.setMode)

  return (
    <div className="h-full flex flex-col items-center justify-center gap-6 p-4">
      <div className="text-center space-y-2">
        <h2 className="text-lg font-semibold">Start a Conversation</h2>
        <p className="text-sm text-muted-foreground">
          Choose how you want to connect
        </p>
      </div>

      <div className="flex flex-col gap-3 w-full max-w-xs">
        <Button
          size="lg"
          onClick={() => setMode('local')}
          className="w-full"
        >
          Local Agent
          <span className="text-xs ml-2 opacity-70">(Direct IPC)</span>
        </Button>

        <Button
          size="lg"
          variant="outline"
          onClick={() => setMode('remote')}
          className="w-full"
        >
          Remote Agent
          <span className="text-xs ml-2 opacity-70">(Via Gateway)</span>
        </Button>
      </div>
    </div>
  )
}

export default function ChatPage() {
  const mode = useChatModeStore((s) => s.mode)
  const gateway = useGatewayConnection()

  return (
    <div className="h-full flex flex-col overflow-hidden">
      <ModeNav gateway={gateway} />

      {mode === 'select' && <ModeSelect />}

      {mode === 'local' && <LocalChat />}

      <ChatPanel visible={mode === 'remote'}>
        <RemoteChat gateway={gateway} />
      </ChatPanel>
    </div>
  )
}

function ChatPanel({
  visible,
  children,
}: {
  visible: boolean
  children: React.ReactNode
}) {
  return (
    <div
      className={`flex-1 min-h-0 ${visible ? 'flex flex-col' : 'hidden'}`}
    >
      {children}
    </div>
  )
}
