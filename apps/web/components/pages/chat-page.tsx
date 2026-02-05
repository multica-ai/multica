"use client";

import { Header } from "@/app/header";
import { Loading } from "@multica/ui/components/ui/loading";
import { ChatView } from "@multica/ui/components/chat-view";
import { DevicePairing } from "@multica/ui/components/device-pairing";
import { useGatewayConnection } from "@multica/hooks/use-gateway-connection";
import { useGatewayChat } from "@multica/hooks/use-gateway-chat";

const ChatPage = () => {
  const { pageState, connectionState, identity, error, client, pairingKey, connect, disconnect } =
    useGatewayConnection();

  return (
    <div className="h-full flex flex-col bg-background">
      <Header />
      <div className="flex-1 overflow-hidden min-h-0 flex flex-col">
        {pageState === "loading" && (
          <div className="flex-1 flex items-center justify-center gap-2 text-muted-foreground text-sm">
            <Loading />
            Loading...
          </div>
        )}

        {(pageState === "not-connected" || pageState === "connecting") && (
          <DevicePairing
            key={pairingKey}
            connectionState={connectionState}
            lastError={error}
            onConnect={connect}
            onCancel={disconnect}
          />
        )}

        {pageState === "connected" && client && identity && (
          <ConnectedChat
            client={client}
            hubId={identity.hubId}
            agentId={identity.agentId}
            onDisconnect={disconnect}
          />
        )}
      </div>
    </div>
  );
};

function ConnectedChat({
  client,
  hubId,
  agentId,
  onDisconnect,
}: {
  client: NonNullable<ReturnType<typeof useGatewayConnection>["client"]>;
  hubId: string;
  agentId: string;
  onDisconnect: () => void;
}) {
  const chat = useGatewayChat({ client, hubId, agentId });
  return <ChatView {...chat} onDisconnect={onDisconnect} />;
}

export default ChatPage;
