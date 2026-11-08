"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentMember } from "@multica/core/permissions";
import { linkListOptions, useDeleteLink, useUpdateLink } from "@multica/core/links";
import { ArrowUp, ArrowDown, Trash2 } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { useT } from "../../i18n";
import { LinkEditor } from "./link-editor";
import { LinkIcon } from "./link-icon";

export function LinksSection({projectId}: {projectId?: string}) {
  const { t } = useT("settings"); const wsId = useWorkspaceId();
  const {role} = useCurrentMember(wsId);
  const admin = role === "owner" || role === "admin";
  const {data: links = [], isLoading, isError} = useQuery(linkListOptions(wsId, projectId));
  const remove = useDeleteLink(); const update = useUpdateLink();
  const [failed, setFailed] = useState(false);
  const pending = remove.isPending || update.isPending;
  const reorder = async (index: number, direction: number) => {
    setFailed(false);
    const ordered = [...links]; const other = index + direction;
    [ordered[index], ordered[other]] = [ordered[other]!, ordered[index]!];
    try { for (let i = 0; i < ordered.length; i++) await update.mutateAsync({id: ordered[i]!.id, position: i}); } catch { setFailed(true); }
  };
  if (!admin && !links.length) return null;
  return <section className="grid gap-3 p-4">
    <div className="flex items-center justify-between"><h2 className="text-body font-medium">{t(($) => $.links.title)}</h2>{admin && <LinkEditor projectId={projectId} />}</div>
    {isLoading && <p>{t(($) => $.links.loading)}</p>}
    {(isError || failed) && <p role="alert">{t(($) => $.links.failed)}</p>}
    {links.map((link, index) => <div key={link.id} className="flex min-w-0 items-center gap-2 rounded-md border p-2"><LinkIcon icon={link.icon} /><span className="min-w-0 flex-1 truncate">{link.name}</span>
      {admin && <><Button variant="ghost" size="icon-sm" aria-label={t(($) => $.links.up)} disabled={pending || index === 0} onClick={() => void reorder(index, -1)}><ArrowUp /></Button><Button variant="ghost" size="icon-sm" aria-label={t(($) => $.links.down)} disabled={pending || index === links.length - 1} onClick={() => void reorder(index, 1)}><ArrowDown /></Button><LinkEditor link={link} projectId={projectId} /><Button variant="ghost" size="icon-sm" aria-label={t(($) => $.links.delete)} disabled={pending} onClick={async () => {setFailed(false); try {await remove.mutateAsync(link.id);} catch {setFailed(true);} }}><Trash2 /></Button></>}
    </div>)}
  </section>;
}
