"use client";

import { useState, type ReactNode } from "react";
import { useModalStore } from "@multica/core/modals";
import { CreateIssueDialog } from "./create-issue-dialog";
import { CreateProjectModal } from "./create-project";
import { CreateSquadModal } from "./create-squad";
import { FeedbackModal } from "./feedback";
import { KeyboardShortcutsModal } from "./keyboard-shortcuts";
import { SetParentIssueModal } from "./set-parent-issue";
import { MarkDuplicateIssueModal } from "./mark-duplicate-issue";
import { AddChildIssueModal } from "./add-child-issue";
import { AddBlockerIssueModal } from "./add-blocker-issue";
import { AddProjectIssueModal } from "./add-project-issue";
import { RenameIssueModal } from "./rename-issue";
import { RenameProjectModal } from "./rename-project";
import { getProjectFoldersExtension } from "../extensions";
import { DeleteIssueConfirmModal } from "./delete-issue-confirm";
import { RunConfirmModal } from "./run-confirm";
import { WorkflowHandoffModal } from "./workflow-handoff";
import { LimitRecoveryDialog } from "./limit-recovery-dialog";

type ActiveModalName = NonNullable<ReturnType<typeof useModalStore.getState>["modal"]>;

type RetainedModal = {
  modal: ActiveModalName;
  data: Record<string, unknown> | null;
  sessionKey: number;
  closing: boolean;
};

export function ModalRegistry() {
  const modal = useModalStore((s) => s.modal);
  const data = useModalStore((s) => s.data);
  const close = useModalStore((s) => s.close);
  const [retained, setRetained] = useState<RetainedModal | null>(null);

  if (modal !== null) {
    if (
      retained === null ||
      retained.closing ||
      retained.modal !== modal ||
      retained.data !== data
    ) {
      setRetained({
        modal,
        data,
        sessionKey: (retained?.sessionKey ?? 0) + 1,
        closing: false,
      });
    }
  } else if (retained !== null && !retained.closing) {
    setRetained({ ...retained, closing: true });
  }

  const snapshot: RetainedModal | null =
    modal !== null
      ? retained !== null &&
        !retained.closing &&
        retained.modal === modal &&
        retained.data === data
        ? retained
        : {
            modal,
            data,
            sessionKey: (retained?.sessionKey ?? 0) + 1,
            closing: false,
          }
      : retained;

  const isOpen = modal !== null;

  let activeModal: ReactNode = null;
  if (snapshot !== null) {
    const { modal: currentModal, data: currentData, sessionKey } = snapshot;
    const handleOpenChangeComplete = (nextOpen: boolean) => {
      if (!nextOpen) {
        setRetained((prev) =>
          prev !== null && prev.sessionKey === sessionKey && prev.closing
            ? null
            : prev,
        );
      }
    };

    switch (currentModal) {
      // Both modal types open the same shell so the in-modal mode switch is
      // instant — only the inner panel swaps, the Dialog Root stays mounted.
      case "create-issue":
        activeModal = (
          <CreateIssueDialog
            key={sessionKey}
            open={isOpen}
            onClose={close}
            onOpenChangeComplete={handleOpenChangeComplete}
            initialMode="manual"
            data={currentData}
          />
        );
        break;
      case "quick-create-issue":
        activeModal = (
          <CreateIssueDialog
            key={sessionKey}
            open={isOpen}
            onClose={close}
            onOpenChangeComplete={handleOpenChangeComplete}
            initialMode="agent"
            data={currentData}
          />
        );
        break;
      case "create-project":
        activeModal = (
          <CreateProjectModal
            key={sessionKey}
            open={isOpen}
            onClose={close}
            onOpenChangeComplete={handleOpenChangeComplete}
            data={currentData}
          />
        );
        break;
      case "create-squad":
        activeModal = (
          <CreateSquadModal
            key={sessionKey}
            open={isOpen}
            onClose={close}
            onOpenChangeComplete={handleOpenChangeComplete}
          />
        );
        break;
      case "feedback":
        activeModal = (
          <FeedbackModal
            key={sessionKey}
            open={isOpen}
            onClose={close}
            onOpenChangeComplete={handleOpenChangeComplete}
            data={currentData}
          />
        );
        break;
      case "keyboard-shortcuts":
        activeModal = (
          <KeyboardShortcutsModal
            key={sessionKey}
            open={isOpen}
            onClose={close}
            onOpenChangeComplete={handleOpenChangeComplete}
          />
        );
        break;
      case "issue-set-parent":
        activeModal = (
          <SetParentIssueModal
            key={sessionKey}
            open={isOpen}
            onClose={close}
            onOpenChangeComplete={handleOpenChangeComplete}
            data={currentData}
          />
        );
        break;
      case "issue-mark-duplicate":
        activeModal = (
          <MarkDuplicateIssueModal
            key={sessionKey}
            open={isOpen}
            onClose={close}
            onOpenChangeComplete={handleOpenChangeComplete}
            data={currentData}
          />
        );
        break;
      case "issue-add-child":
        activeModal = (
          <AddChildIssueModal
            key={sessionKey}
            open={isOpen}
            onClose={close}
            onOpenChangeComplete={handleOpenChangeComplete}
            data={currentData}
          />
        );
        break;
      case "issue-add-blocker":
      case "issue-add-blocking":
        activeModal = (
          <AddBlockerIssueModal
            key={sessionKey}
            open={isOpen}
            onClose={close}
            onOpenChangeComplete={handleOpenChangeComplete}
            data={currentData}
            direction={currentModal === "issue-add-blocking" ? "blocking" : "blocked_by"}
          />
        );
        break;
      case "project-add-issue":
        activeModal = (
          <AddProjectIssueModal
            key={sessionKey}
            open={isOpen}
            onClose={close}
            onOpenChangeComplete={handleOpenChangeComplete}
            data={currentData}
          />
        );
        break;
      case "project-rename":
        activeModal = (
          <RenameProjectModal
            key={sessionKey}
            open={isOpen}
            onClose={close}
            onOpenChangeComplete={handleOpenChangeComplete}
            data={currentData}
          />
        );
        break;
      case "issue-rename":
        activeModal = (
          <RenameIssueModal
            key={sessionKey}
            open={isOpen}
            onClose={close}
            onOpenChangeComplete={handleOpenChangeComplete}
            data={currentData}
          />
        );
        break;
      case "issue-delete-confirm":
        activeModal = (
          <DeleteIssueConfirmModal
            key={sessionKey}
            open={isOpen}
            onClose={close}
            onOpenChangeComplete={handleOpenChangeComplete}
            data={currentData}
          />
        );
        break;
      case "issue-run-confirm":
        activeModal = (
          <RunConfirmModal
            key={sessionKey}
            open={isOpen}
            onClose={close}
            onOpenChangeComplete={handleOpenChangeComplete}
            data={currentData}
          />
        );
        break;
      case "issue-workflow-handoff":
        activeModal = (
          <WorkflowHandoffModal
            key={sessionKey}
            open={isOpen}
            onClose={close}
            onOpenChangeComplete={handleOpenChangeComplete}
            data={currentData}
          />
        );
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
