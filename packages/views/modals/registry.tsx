"use client";

import { useModalStore } from "@multica/core/modals";
import { CreateIssueModal } from "./create-issue";

export function ModalRegistry() {
  const modal = useModalStore((s) => s.modal);
  const data = useModalStore((s) => s.data);
  const close = useModalStore((s) => s.close);

  switch (modal) {
    case "create-issue":
      return <CreateIssueModal onClose={close} data={data} />;
    default:
      return null;
  }
}
