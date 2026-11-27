"use client";

import { useQuery } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { issueKeys } from "@multica/core/issues/queries";
import { useModalStore } from "@multica/core/modals";
import { paths } from "@multica/core/paths";
import { useCurrentMember } from "@multica/core/permissions";
import type { Issue } from "@multica/core/types";
import { Link2, Unlink } from "@multica/ui/icons";
import { copyText } from "@multica/ui/lib/clipboard";
import { toast } from "sonner";
import type { MenuPrimitives } from "../../common/menu-primitives";
import { useT } from "../../i18n";
import { useNavigation } from "../../navigation";

export function IssueShareMenuItems({
  issue,
  wsId,
  Item,
}: {
  issue: Issue;
  wsId: string;
  Item: MenuPrimitives["Item"];
}) {
  const { t } = useT("issues");
  const { userId, role } = useCurrentMember(wsId);
  const { getShareableUrl } = useNavigation();
  const openModal = useModalStore((state) => state.open);
  const canShare = !!userId && (
    role === "owner" || role === "admin" || (
      role === "member" && (
        (issue.creator_type === "member" && issue.creator_id === userId)
        || (issue.assignee_type === "member" && issue.assignee_id === userId)
      )
    )
  );
  const share = useQuery({
    queryKey: issueKeys.share(wsId, issue.id, userId),
    queryFn: ({ signal }) => api.getIssueShare(wsId, issue.id, signal),
    enabled: canShare && !!wsId,
    refetchOnMount: "always",
    retry: false,
  });

  if (!canShare || share.isFetching || share.isError || !share.data) return null;

  const { token } = share.data;
  const openDialog = () => openModal("issue-share", { wsId, issueId: issue.id, userId, token });

  if (!token) {
    return (
      <Item onClick={openDialog}>
        <Link2 className="size-3.5" />
        {t(($) => $.sharing.generate)}
      </Item>
    );
  }

  const copyLink = async () => {
    if (await copyText(getShareableUrl(paths.sharedIssue(token)))) {
      toast.success(t(($) => $.detail.link_copied));
    } else {
      toast.error(t(($) => $.detail.link_copy_failed));
    }
  };

  return (
    <>
      <Item onClick={copyLink}>
        <Link2 className="size-3.5" />
        {t(($) => $.sharing.copy)}
      </Item>
      <Item onClick={openDialog}>
        <Unlink className="size-3.5" />
        {t(($) => $.sharing.revoke)}
      </Item>
    </>
  );
}
