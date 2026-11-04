"use client";

import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { briefListOptions, hasUnreadBrief, useCompanyBriefStore } from "@multica/core/company";
import { useWorkspaceId } from "@multica/core/hooks";
import { Button } from "@multica/ui/components/ui/button";
import { Newspaper } from "@multica/ui/icons";
import { useT } from "../../i18n";
import { CompanyBriefDialog } from "./company-brief-dialog";
import { useCompanyAdmin } from "../lib/use-company-admin";

const EMPTY_BRIEFS: never[] = [];

/**
 * The Brief button in the company header. A dot marks a brief newer than the one
 * the viewer last opened; opening the dialog marks the newest one as read.
 */
export function CompanyBriefButton({ focusBriefId = null }: {
  /** A brief the page was linked to (`?brief=`); the dialog opens on it. */
  focusBriefId?: string | null;
}) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const canManage = useCompanyAdmin();
  const { data: briefs = EMPTY_BRIEFS } = useQuery(briefListOptions(wsId));
  const lastSeenBriefId = useCompanyBriefStore((s) => s.lastSeenBriefId);
  const markBriefSeen = useCompanyBriefStore((s) => s.markBriefSeen);
  const [open, setOpen] = useState(false);

  useEffect(() => {
    if (focusBriefId) setOpen(true);
  }, [focusBriefId]);

  const latest = briefs[0];
  const unread = hasUnreadBrief(latest?.id, lastSeenBriefId);

  const openDialog = () => {
    if (latest) markBriefSeen(latest.id);
    setOpen(true);
  };

  return (
    <>
      <Button type="button" variant="outline" size="sm" onClick={openDialog}>
        <Newspaper className="size-3.5" />
        {t(($) => $.company.brief_open)}
        {unread ? (
          <>
            <span aria-hidden className="size-1.5 rounded-full bg-brand" />
            <span className="sr-only">{t(($) => $.company.brief_unread)}</span>
          </>
        ) : null}
      </Button>
      <CompanyBriefDialog
        open={open}
        briefs={briefs}
        canManage={canManage}
        focusBriefId={focusBriefId}
        onClose={() => setOpen(false)}
      />
    </>
  );
}
