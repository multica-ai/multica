"use client";

import { X } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { IssueDetail } from "../issues/components/issue-detail";
import { useT } from "../i18n";

export function IssueDetailModal({
  onClose,
  data,
}: {
  onClose: () => void;
  data: Record<string, unknown> | null;
}) {
  const { t } = useT("modals");
  const issueId = typeof data?.issueId === "string" ? data.issueId : "";

  if (!issueId) return null;

  return (
    <Dialog open onOpenChange={(open) => { if (!open) onClose(); }}>
      <DialogContent
        showCloseButton={false}
        className="!h-[90vh] !w-[92vw] !max-w-[1480px] gap-0 overflow-visible p-0"
      >
        <DialogTitle className="sr-only">
          {t(($) => $.issue_detail.title)}
        </DialogTitle>
        <DialogClose
          render={
            <Button
              variant="outline"
              size="icon-sm"
              className="absolute -right-3 -top-3 z-30 rounded-full bg-card shadow-md"
            />
          }
        >
          <X />
          <span className="sr-only">{t(($) => $.common.close)}</span>
        </DialogClose>
        <div className="flex h-full min-h-0 overflow-hidden rounded-xl bg-background">
          <IssueDetail
            issueId={issueId}
            onDelete={onClose}
            defaultSidebarOpen
            layoutId="multica_issue_detail_modal_layout"
            openIssueLinksInModal
          />
        </div>
      </DialogContent>
    </Dialog>
  );
}
