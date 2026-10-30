"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { ChevronRight, FileCode, FilePlus, FileText, Link2, X } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { Input } from "@multica/ui/components/ui/input";
import { cn } from "@multica/ui/lib/utils";
import {
  docTreeOptions,
  projectDocsOptions,
  useCreateDoc,
  useLinkProjectDoc,
  useUnlinkProjectDoc,
} from "@multica/core/docs";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import type { DocSummary } from "@multica/core/types";
import { AppLink, useNavigation } from "../../navigation";
import { useT } from "../../i18n";

function DocIcon({ doc }: { doc: Pick<DocSummary, "format"> }) {
  return doc.format === "html" ? (
    <FileCode className="size-3.5 shrink-0 text-muted-foreground" />
  ) : (
    <FileText className="size-3.5 shrink-0 text-muted-foreground" />
  );
}

/** A project's docs: the knowledge base pages linked to it, many-to-many. */
export function ProjectDocsSection({ projectId }: { projectId: string }) {
  const { t } = useT("docs");
  const wsId = useWorkspaceId();
  const p = useWorkspacePaths();
  const { push } = useNavigation();
  const [open, setOpen] = useState(true);
  const [filter, setFilter] = useState("");

  const { data: linked = [] } = useQuery(projectDocsOptions(wsId, projectId));
  const { data: tree } = useQuery(docTreeOptions(wsId));
  const link = useLinkProjectDoc();
  const unlink = useUnlinkProjectDoc();
  const create = useCreateDoc();

  const linkable = useMemo(() => {
    const linkedIds = new Set(linked.map((d) => d.id));
    const query = filter.trim().toLowerCase();
    return (tree?.docs ?? [])
      .filter((d) => !linkedIds.has(d.id) && (!query || d.title.toLowerCase().includes(query)))
      .slice(0, 50);
  }, [tree, linked, filter]);

  const onLinkError = () => toast.error(t(($) => $.errors.link));

  const createForProject = async () => {
    let docId: string;
    try {
      docId = (await create.mutateAsync({ title: t(($) => $.untitled), format: "markdown", content: "" })).id;
    } catch {
      toast.error(t(($) => $.errors.create_doc));
      return;
    }
    // Link before leaving: the doc is useless to the project if the link silently failed.
    await link.mutateAsync({ projectId, docId }).catch(onLinkError);
    push(p.docDetail(docId));
  };

  return (
    <div>
      <button
        type="button"
        aria-expanded={open}
        className={cn(
          "mb-2 flex w-full items-center gap-1 rounded-md px-2 py-1 text-caption font-medium transition-colors hover:bg-accent/70",
          !open && "text-muted-foreground hover:text-foreground",
        )}
        onClick={() => setOpen(!open)}
      >
        {t(($) => $.project.title)}
        <ChevronRight
          className={cn(
            "!size-3 shrink-0 stroke-[2.5] text-muted-foreground transition-transform",
            open && "rotate-90",
          )}
        />
      </button>
      {open && (
        <div className="space-y-1.5 pl-2">
          {linked.length === 0 ? (
            <p className="text-caption text-muted-foreground">{t(($) => $.project.empty)}</p>
          ) : (
            <ul className="max-h-64 space-y-px overflow-y-auto pr-1">
              {linked.map((doc) => (
                <li
                  key={doc.id}
                  className="group/doc flex h-7 items-center gap-2 rounded-md px-1.5 hover:bg-surface-hover"
                >
                  <DocIcon doc={doc} />
                  <AppLink href={p.docDetail(doc.id)} className="min-w-0 flex-1 truncate text-caption">
                    {doc.title || t(($) => $.untitled)}
                  </AppLink>
                  <button
                    type="button"
                    aria-label={t(($) => $.project.unlink, { title: doc.title })}
                    onClick={() => unlink.mutate({ projectId, docId: doc.id }, { onError: onLinkError })}
                    className="flex size-5 items-center justify-center rounded text-muted-foreground opacity-0 hover:text-foreground focus-visible:opacity-100 group-hover/doc:opacity-100 [@media(pointer:coarse)]:opacity-100"
                  >
                    <X className="size-3.5" />
                  </button>
                </li>
              ))}
            </ul>
          )}
          <div className="flex items-center gap-1">
            <DropdownMenu onOpenChange={(next) => !next && setFilter("")}>
              <DropdownMenuTrigger
                render={
                  <Button
                    variant="ghost"
                    size="sm"
                    className="h-7 px-2 text-caption text-muted-foreground hover:text-foreground"
                  />
                }
              >
                <Link2 className="size-3" />
                {t(($) => $.project.link)}
              </DropdownMenuTrigger>
              <DropdownMenuContent align="start" className="w-72">
                <div className="p-1">
                  <Input
                    value={filter}
                    onChange={(event) => setFilter(event.target.value)}
                    onKeyDown={(event) => event.stopPropagation()}
                    aria-label={t(($) => $.project.search)}
                    placeholder={t(($) => $.project.search)}
                    className="h-7"
                  />
                </div>
                <div className="max-h-64 overflow-y-auto">
                  {linkable.length === 0 ? (
                    <div className="px-2 py-2 text-caption text-muted-foreground">
                      {t(($) => $.project.no_results)}
                    </div>
                  ) : (
                    linkable.map((doc) => (
                      <DropdownMenuItem
                        key={doc.id}
                        onClick={() => link.mutate({ projectId, docId: doc.id }, { onError: onLinkError })}
                      >
                        <DocIcon doc={doc} />
                        <span className="truncate">{doc.title || t(($) => $.untitled)}</span>
                      </DropdownMenuItem>
                    ))
                  )}
                </div>
              </DropdownMenuContent>
            </DropdownMenu>
            <Button
              variant="ghost"
              size="sm"
              disabled={create.isPending || link.isPending}
              onClick={() => void createForProject()}
              className="h-7 px-2 text-caption text-muted-foreground hover:text-foreground"
            >
              <FilePlus className="size-3" />
              {t(($) => $.actions.new_doc)}
            </Button>
          </div>
        </div>
      )}
    </div>
  );
}
