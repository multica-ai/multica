"use client";

import * as React from "react";
import { useModalStore } from "@multica/core/modals";

const CreateWorkspaceModal = React.lazy(() =>
  import("./create-workspace").then((m) => ({ default: m.CreateWorkspaceModal })),
);
const CreateIssueDialog = React.lazy(() =>
  import("./create-issue-dialog").then((m) => ({ default: m.CreateIssueDialog })),
);
const CreateProjectModal = React.lazy(() =>
  import("./create-project").then((m) => ({ default: m.CreateProjectModal })),
);
const CreateSquadModal = React.lazy(() =>
  import("./create-squad").then((m) => ({ default: m.CreateSquadModal })),
);
const FeedbackModal = React.lazy(() =>
  import("./feedback").then((m) => ({ default: m.FeedbackModal })),
);
const SetParentIssueModal = React.lazy(() =>
  import("./set-parent-issue").then((m) => ({ default: m.SetParentIssueModal })),
);
const AddChildIssueModal = React.lazy(() =>
  import("./add-child-issue").then((m) => ({ default: m.AddChildIssueModal })),
);
const DeleteIssueConfirmModal = React.lazy(() =>
  import("./delete-issue-confirm").then((m) => ({
    default: m.DeleteIssueConfirmModal,
  })),
);
const BacklogAgentHintModal = React.lazy(() =>
  import("./backlog-agent-hint").then((m) => ({
    default: m.BacklogAgentHintModal,
  })),
);

export function ModalRegistry() {
  const modal = useModalStore((s) => s.modal);
  const data = useModalStore((s) => s.data);
  const close = useModalStore((s) => s.close);

  let content: React.ReactNode = null;
  switch (modal) {
    case "create-workspace":
      content = <CreateWorkspaceModal onClose={close} />;
      break;
    // Both modal types open the same shell so the in-modal mode switch is
    // instant — only the inner panel swaps, the Dialog Root stays mounted.
    case "create-issue":
      content = (
        <CreateIssueDialog onClose={close} initialMode="manual" data={data} />
      );
      break;
    case "quick-create-issue":
      content = (
        <CreateIssueDialog onClose={close} initialMode="agent" data={data} />
      );
      break;
    case "create-project":
      content = <CreateProjectModal onClose={close} />;
      break;
    case "create-squad":
      content = <CreateSquadModal onClose={close} />;
      break;
    case "feedback":
      content = <FeedbackModal onClose={close} />;
      break;
    case "issue-set-parent":
      content = <SetParentIssueModal onClose={close} data={data} />;
      break;
    case "issue-add-child":
      content = <AddChildIssueModal onClose={close} data={data} />;
      break;
    case "issue-delete-confirm":
      content = <DeleteIssueConfirmModal onClose={close} data={data} />;
      break;
    case "issue-backlog-agent-hint":
      content = <BacklogAgentHintModal onClose={close} data={data} />;
      break;
  }

  return <React.Suspense fallback={null}>{content}</React.Suspense>;
}
