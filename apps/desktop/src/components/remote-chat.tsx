import { Loading } from '@multica/ui/components/ui/loading'
import { ChatView } from '@multica/ui/components/chat-view'
import { DevicePairing } from '@multica/ui/components/device-pairing'
import { useGatewayChat } from '@multica/hooks/use-gateway-chat'
import type { UseGatewayConnectionReturn } from '@multica/hooks/use-gateway-connection'

export function RemoteChat({ gateway }: { gateway: UseGatewayConnectionReturn }) {
  const { pageState, connectionState, error, client, identity, pairingKey, connect, disconnect } = gateway

  return (
    <div className="h-full flex flex-col overflow-hidden w-full">
      {pageState === 'loading' && (
        <div className="flex-1 flex items-center justify-center gap-2 text-muted-foreground text-sm">
          <Loading />
          Loading...
        </div>
      )}

      {(pageState === 'not-connected' || pageState === 'connecting') && (
        <DevicePairing
          key={pairingKey}
          connectionState={connectionState}
          lastError={error}
          onConnect={connect}
          onCancel={disconnect}
        />
      )}

      {pageState === 'connected' && client && identity && (
        <ConnectedChat
          client={client}
          hubId={identity.hubId}
          agentId={identity.agentId}
        />
      )}
    </div>
  )
}

function ConnectedChat({
  client,
  hubId,
  agentId,
}: {
  client: NonNullable<UseGatewayConnectionReturn['client']>
  hubId: string
  agentId: string
}) {
  const chat = useGatewayChat({ client, hubId, agentId })
  return <ChatView {...chat} />
}
