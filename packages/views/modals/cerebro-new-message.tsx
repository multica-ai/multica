"use client";

// CEREBRO-PATCH(sidebar-new-message-modal): JEH-1296 global wrapper so the
// new-message modal can be triggered from the sidebar (and anywhere else).

import { useModalStore } from "@multica/core/modals";
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

  const handleAgentChatStarted = () => {
    close();
    push(p.inbox());
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
