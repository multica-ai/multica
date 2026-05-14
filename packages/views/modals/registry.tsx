"use client";

import { useModalStore } from "@multica/core/modals";
import { CreateWorkspaceModal } from "./create-workspace";
import { CreateIssueDialog } from "./create-issue-dialog";
import { CreateProjectModal } from "./create-project";
import { FeedbackModal } from "./feedback";
import { SetParentIssueModal } from "./set-parent-issue";
import { AddChildIssueModal } from "./add-child-issue";
import { DeleteIssueConfirmModal } from "./delete-issue-confirm";
import { BacklogAgentHintModal } from "./backlog-agent-hint";
// CEREBRO-PATCH(sidebar-new-message-modal): JEH-1296 global new-message modal
import { CerebroNewMessageModal } from "./cerebro-new-message";

export function ModalRegistry() {
  const modal = useModalStore((s) => s.modal);
  const data = useModalStore((s) => s.data);
  const close = useModalStore((s) => s.close);

  switch (modal) {
    case "create-workspace":
      return <CreateWorkspaceModal onClose={close} />;
    // Both modal types open the same shell so the in-modal mode switch is
    // instant — only the inner panel swaps, the Dialog Root stays mounted.
    case "create-issue":
      return <CreateIssueDialog onClose={close} initialMode="manual" data={data} />;
    case "quick-create-issue":
      return <CreateIssueDialog onClose={close} initialMode="agent" data={data} />;
    case "create-project":
      return <CreateProjectModal onClose={close} />;
    case "feedback":
      return <FeedbackModal onClose={close} />;
    case "issue-set-parent":
      return <SetParentIssueModal onClose={close} data={data} />;
    case "issue-add-child":
      return <AddChildIssueModal onClose={close} data={data} />;
    case "issue-delete-confirm":
      return <DeleteIssueConfirmModal onClose={close} data={data} />;
    case "issue-backlog-agent-hint":
      return <BacklogAgentHintModal onClose={close} data={data} />;
    // CEREBRO-PATCH(sidebar-new-message-modal): JEH-1296 global new-message modal
    case "new-message":
      return <CerebroNewMessageModal />;
    default:
      return null;
  }
}
