"use client";

import { useCallback, useState, type ReactNode } from "react";
import { useModalStore } from "@multica/core/modals";
import { CreateIssueDialog } from "./create-issue-dialog";
import { CreateProjectModal } from "./create-project";
import { CreateSquadModal } from "./create-squad";
import { FeedbackModal } from "./feedback";
import { SetParentIssueModal } from "./set-parent-issue";
import { MarkDuplicateIssueModal } from "./mark-duplicate-issue";
import { AddChildIssueModal } from "./add-child-issue";
import { AddProjectIssueModal } from "./add-project-issue";
import { RenameIssueModal } from "./rename-issue";
import { RenameProjectModal } from "./rename-project";
import { getProjectFoldersExtension } from "../extensions";
import { DeleteIssueConfirmModal } from "./delete-issue-confirm";
import { RunConfirmModal } from "./run-confirm";
import { WorkflowHandoffModal } from "./workflow-handoff";
import { LimitRecoveryDialog } from "./limit-recovery-dialog";

type ActiveModal = NonNullable<ReturnType<typeof useModalStore.getState>["modal"]>;

type PresentedModal = {
  modal: ActiveModal;
  data: Record<string, unknown> | null;
  id: number;
};

export function ModalRegistry() {
  const modal = useModalStore((s) => s.modal);
  const data = useModalStore((s) => s.data);
  const close = useModalStore((s) => s.close);

  // Keep the last open modal mounted while its dialog exit animation plays,
  // clearing it only once Base UI signals `onOpenChangeComplete(false)`.
  const [presented, setPresented] = useState<PresentedModal | null>(() =>
    modal ? { modal, data, id: 1 } : null,
  );
  if (modal && (!presented || presented.modal !== modal || presented.data !== data)) {
    setPresented({ modal, data, id: (presented?.id ?? 0) + 1 });
  }

  const handleExitComplete = useCallback((closedId: number) => {
    setPresented((current) =>
      current?.id === closedId && !useModalStore.getState().modal ? null : current,
    );
  }, []);

  const isOpen = modal !== null;
  const current = presented;

  let activeModal: ReactNode = null;
  if (current) {
    const shared = {
      open: isOpen,
      onClose: close,
      onExitComplete: () => handleExitComplete(current.id),
    };
    switch (current.modal) {
      // Both modal types open the same shell so the in-modal mode switch is
      // instant — only the inner panel swaps, the Dialog Root stays mounted.
      case "create-issue":
        activeModal = (
          <CreateIssueDialog
            key={current.id}
            {...shared}
            initialMode="manual"
            data={current.data}
          />
        );
        break;
      case "quick-create-issue":
        activeModal = (
          <CreateIssueDialog
            key={current.id}
            {...shared}
            initialMode="agent"
            data={current.data}
          />
        );
        break;
      case "create-project":
        activeModal = <CreateProjectModal key={current.id} {...shared} data={current.data} />;
        break;
      case "create-squad":
        activeModal = <CreateSquadModal key={current.id} {...shared} />;
        break;
      case "feedback":
        activeModal = <FeedbackModal key={current.id} {...shared} data={current.data} />;
        break;
      case "issue-set-parent":
        activeModal = <SetParentIssueModal key={current.id} {...shared} data={current.data} />;
        break;
      case "issue-mark-duplicate":
        activeModal = <MarkDuplicateIssueModal key={current.id} {...shared} data={current.data} />;
        break;
      case "issue-add-child":
        activeModal = <AddChildIssueModal key={current.id} {...shared} data={current.data} />;
        break;
      case "project-add-issue":
        activeModal = <AddProjectIssueModal key={current.id} {...shared} data={current.data} />;
        break;
      case "project-rename":
        activeModal = <RenameProjectModal key={current.id} {...shared} data={current.data} />;
        break;
      case "issue-rename":
        activeModal = <RenameIssueModal key={current.id} {...shared} data={current.data} />;
        break;
      case "issue-delete-confirm":
        activeModal = <DeleteIssueConfirmModal key={current.id} {...shared} data={current.data} />;
        break;
      case "issue-run-confirm":
        activeModal = <RunConfirmModal key={current.id} {...shared} data={current.data} />;
        break;
      case "issue-workflow-handoff":
        activeModal = <WorkflowHandoffModal key={current.id} {...shared} data={current.data} />;
        break;
    }
  }

  const FoldersHost = getProjectFoldersExtension()?.Host;

  return (
    <>
      {activeModal}
      <LimitRecoveryDialog />
      {FoldersHost && <FoldersHost />}
    </>
  );
}
