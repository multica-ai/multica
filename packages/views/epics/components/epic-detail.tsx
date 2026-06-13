"use client";

import { useRef } from "react";
import { useQuery } from "@tanstack/react-query";
import { Diamond, Link2, Trash2 } from "lucide-react";
import { epicDetailOptions, useUpdateEpic, useDeleteEpic } from "@multica/core/epics";
import { EPIC_STATUS_CONFIG, DEFAULT_EPIC_COLORS } from "@multica/core/epics/config";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import type { EpicStatus } from "@multica/core/types";
import { cn } from "@multica/ui/lib/utils";
import { copyText } from "@multica/ui/lib/clipboard";
import { toast } from "sonner";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Button } from "@multica/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { TitleEditor, ContentEditor, type ContentEditorRef } from "../../editor";
import { useNavigation } from "../../navigation";
import { useT } from "../../i18n";

export function EpicDetail({ epicId }: { epicId: string }) {
  const { t } = useT("epics");
  const router = useNavigation();
  const wsPaths = useWorkspacePaths();
  const wsId = useWorkspaceId();
  const { data: epic, isLoading } = useQuery(epicDetailOptions(wsId, epicId));
  const updateEpic = useUpdateEpic();
  const deleteEpic = useDeleteEpic();
  const descRef = useRef<ContentEditorRef>(null);

  if (isLoading) {
    return (
      <div className="flex flex-1 flex-col p-6 gap-4">
        <Skeleton className="h-8 w-64" />
        <Skeleton className="h-4 w-full max-w-md" />
        <Skeleton className="h-32 w-full" />
      </div>
    );
  }

  if (!epic) {
    return (
      <div className="flex flex-1 items-center justify-center text-muted-foreground">
        {t(($) => $.detail.not_found)}
      </div>
    );
  }

  const statusCfg = EPIC_STATUS_CONFIG[epic.status];

  const handleDelete = () => {
    deleteEpic.mutate(epic.id, {
      onSuccess: () => {
        router.push(wsPaths.epics());
        toast.success("Epic deleted");
      },
    });
  };

  return (
    <div className="flex flex-1 min-h-0 flex-col">
      {/* Header */}
      <div className="flex items-center justify-between border-b px-5 py-3">
        <div className="flex items-center gap-2 text-sm text-muted-foreground">
          <Diamond className="h-3.5 w-3.5" />
          <span>{t(($) => $.detail.breadcrumb_fallback)}</span>
        </div>
        <div className="flex items-center gap-1">
          <Button
            variant="ghost"
            size="sm"
            onClick={() =>
              copyText(window.location.href).then(() =>
                toast.success("Link copied"),
              )
            }
          >
            <Link2 className="h-3.5 w-3.5" />
          </Button>
          <DropdownMenu>
            <DropdownMenuTrigger
              render={<Button variant="ghost" size="sm"><Trash2 className="h-3.5 w-3.5" /></Button>}
            />
            <DropdownMenuContent align="end">
              <DropdownMenuItem
                className="text-destructive"
                onClick={handleDelete}
              >
                {t(($) => $.detail.delete_action)}
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </div>

      {/* Content */}
      <div className="flex flex-1 min-h-0 overflow-y-auto">
        <div className="flex-1 max-w-3xl mx-auto px-6 py-6 space-y-6">
          {/* Color + Title */}
          <div className="flex items-start gap-3">
            <DropdownMenu>
              <DropdownMenuTrigger
                render={
                  <button
                    type="button"
                    className="mt-1 size-6 rounded-sm shrink-0 cursor-pointer hover:opacity-80 transition-opacity"
                    style={{ backgroundColor: epic.color }}
                  />
                }
              />
              <DropdownMenuContent align="start">
                <div className="flex items-center gap-2 p-2">
                  {DEFAULT_EPIC_COLORS.map((c) => (
                    <button
                      key={c}
                      type="button"
                      onClick={() =>
                        updateEpic.mutate({ id: epic.id, color: c })
                      }
                      className={cn(
                        "size-5 rounded-full transition-all",
                        epic.color === c
                          ? "ring-2 ring-offset-1 ring-foreground"
                          : "hover:scale-110",
                      )}
                      style={{ backgroundColor: c }}
                    />
                  ))}
                </div>
              </DropdownMenuContent>
            </DropdownMenu>
            <TitleEditor
              defaultValue={epic.title}
              placeholder={t(($) => $.detail.title_placeholder)}
              onChange={(title) =>
                updateEpic.mutate({ id: epic.id, title })
              }
              className="text-xl font-semibold"
            />
          </div>

          {/* Status */}
          <div className="flex items-center gap-2">
            <span className="text-xs text-muted-foreground w-16">{t(($) => $.table.status)}</span>
            <DropdownMenu>
              <DropdownMenuTrigger
                render={
                  <button
                    type="button"
                    className={cn(
                      "text-xs px-2 py-0.5 rounded-full cursor-pointer",
                      epic.status === "open"
                        ? "bg-primary/10 text-primary"
                        : "bg-muted text-muted-foreground",
                    )}
                  >
                    {statusCfg.label}
                  </button>
                }
              />
              <DropdownMenuContent align="start">
                {(
                  Object.entries(EPIC_STATUS_CONFIG) as [
                    EpicStatus,
                    (typeof EPIC_STATUS_CONFIG)[EpicStatus],
                  ][]
                ).map(([key, cfg]) => (
                  <DropdownMenuItem
                    key={key}
                    onClick={() =>
                      updateEpic.mutate({ id: epic.id, status: key })
                    }
                  >
                    {cfg.label}
                  </DropdownMenuItem>
                ))}
              </DropdownMenuContent>
            </DropdownMenu>
          </div>

          {/* Progress */}
          {epic.issue_count > 0 && (
            <div className="flex items-center gap-2">
              <span className="text-xs text-muted-foreground w-16">
                {t(($) => $.table.progress)}
              </span>
              <span className="text-xs tabular-nums">
                {epic.done_count}/{epic.issue_count} {t(($) => $.table.issues)}
              </span>
            </div>
          )}

          {/* Description */}
          <div>
            <div className="text-xs font-medium text-muted-foreground mb-2">
              {t(($) => $.detail.section_description)}
            </div>
            <ContentEditor
              ref={descRef}
              defaultValue={epic.description ?? ""}
              placeholder={t(($) => $.detail.description_placeholder)}
              onBlur={() => {
                const md = descRef.current?.getMarkdown() ?? "";
                updateEpic.mutate({
                  id: epic.id,
                  description: md || null,
                });
              }}
            />
          </div>
        </div>
      </div>
    </div>
  );
}
