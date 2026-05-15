"use client";

// CEREBRO-PATCH(sidebar-new-message-modal): JEH-1296 global wrapper so the
// new-message modal can be triggered from the sidebar (and anywhere else).

import { useModalStore } from "@multica/core/modals";
import { useChatStore } from "@multica/core/chat"; // CEREBRO-PATCH(sidebar-agent-chat-start): JEH-1443 selected sidebar agent opens a new inbox chat.
import { useNavigation } from "../navigation";
import { useWorkspacePaths } from "@multica/core/paths";
import { NewMessageModal } from "../channels";
import type { Channel } from "@multica/core/types";

export function CerebroNewMessageModal() {
  const { push } = useNavigation();
  const p = useWorkspacePaths();
  const close = useModalStore((s) => s.close);

  const handleCreated = (channel: Channel) => {
    close();
    push(p.issueDetail(channel.id));
  };

  const handleAgentChatStarted = (agentId: string) => {
    // CEREBRO-PATCH(sidebar-agent-chat-start): carry the selected agent into the inbox chat surface.
    useChatStore.getState().setSelectedAgentId(agentId);
    close();
    push(`${p.inbox()}?chat=new-chat&agent=${encodeURIComponent(agentId)}`); // CEREBRO-PATCH(sidebar-agent-chat-start-url): JEH-1443 keep selected agent across inbox navigation.
  };

  return (
    <NewMessageModal
      open
      onClose={close}
      onCreated={handleCreated}
      onAgentChatStarted={handleAgentChatStarted}
    />
  );
}
