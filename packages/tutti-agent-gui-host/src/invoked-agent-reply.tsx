import { createContext, useContext, type ReactNode } from "react";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
import type { AgentMessageContentVM } from "../../../vendor/tutti/packages/agent/gui/shared/agentConversation/contracts/agentMessageRowVM";
import type { AgentGuiWorkspaceState } from "./protocol";
import { observer } from "mobx-react-lite";
export const InvokedAgentReply = observer(InvokedAgentReplyRender);

const InvokedAgentsContext = createContext<AgentGuiWorkspaceState["agents"]>(
  []
);
export const InvokedAgentsProvider = InvokedAgentsContext.Provider;

/** Resolve the reply's author, independently of the conversation's local runner. */
function InvokedAgentReplyRender({
  message,
  children
}: {
  message: Pick<AgentMessageContentVM, "sourceTimelineItems">;
  children: ReactNode;
}) {
  const agents = useContext(InvokedAgentsContext);
  const identity = message.sourceTimelineItems
    ?.map((item) => item.payload?.invokedAgent)
    .find((value): value is { id: string; name: string } =>
      Boolean(
        value &&
          typeof value === "object" &&
          "id" in value &&
          typeof value.id === "string" &&
          "name" in value &&
          typeof value.name === "string"
      )
    );
  if (!identity) return <>{children}</>;
  const descriptor = agents.find(({ agent }) => agent.id === identity.id);
  return (
    <>
      <div
        data-invoked-agent={identity.id}
        className="mb-2 flex min-w-0 items-center gap-2 text-body font-semibold text-foreground"
      >
        <ActorAvatar
          name={identity.name}
          initials=""
          isAgent
          size="sm"
          avatarUrl={descriptor?.iconUrl}
        />
        <span className="min-w-0 break-words" title={identity.name}>
          {identity.name}
        </span>
      </div>
      {children}
    </>
  );
}
