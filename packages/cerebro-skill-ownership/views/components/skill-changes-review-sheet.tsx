"use client";

// FIR-2742 — cross-skill change-request review sheet. Opened from the Skills
// page alert/toolbar, it lists every pending change on skills the user owns or
// approves (toggle to all skills), filterable by text and sortable by age, so
// an owner can work through the queue one change at a time. Picking a row loads
// that skill's current body and shows the diff + approve/reject inline — the
// same review affordance as the per-skill queue, but without leaving the page.

import { useMemo, useState, type ReactNode } from "react";
import {
  ArrowLeft,
  Check,
  Clock,
  GitPullRequestArrow,
  MessageSquare,
  Search,
  X,
} from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import type { MemberWithUser } from "@multica/core/types";
import { skillDetailOptions } from "@multica/core/workspace/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
} from "@multica/ui/components/ui/sheet";
import { useIsMobile } from "@multica/ui/hooks/use-mobile";
import {
  filterSkillChanges,
  sortSkillChanges,
  type EnrichedSkillChange,
  type SkillChangeScope,
  type SkillChangeSort,
} from "../../core/select-changes";
import { useReviewAnyChangeRequest } from "../../core/mutations";
import { SkillDiffView } from "./skill-diff-view";

interface Props {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** All pending changes visible to the user (enriched with skill + mine). */
  changes: EnrichedSkillChange[];
  members: MemberWithUser[];
  /** Whether the current user owns/approves at least one skill — controls
   *  whether the "My skills" scope is offered as the default. */
  hasMine: boolean;
}

function formatRelativeTime(iso: string): string {
  const diff = Date.now() - new Date(iso).getTime();
  const minutes = Math.floor(diff / 60000);
  if (minutes < 1) return "just now";
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  return `${Math.floor(hours / 24)}d ago`;
}

function SegButton({
  active,
  onClick,
  children,
}: {
  active: boolean;
  onClick: () => void;
  children: ReactNode;
}) {
  return (
    <Button
      type="button"
      size="sm"
      variant="outline"
      className={
        active
          ? "h-7 shrink-0 bg-accent text-accent-foreground hover:bg-accent/80"
          : "h-7 shrink-0 text-muted-foreground"
      }
      onClick={onClick}
    >
      {children}
    </Button>
  );
}

export function SkillChangesReviewSheet({
  open,
  onOpenChange,
  changes,
  members,
  hasMine,
}: Props) {
  const isMobile = useIsMobile();
  const wsId = useWorkspaceId();
  const [scope, setScope] = useState<SkillChangeScope>(hasMine ? "mine" : "all");
  const [search, setSearch] = useState("");
  const [sort, setSort] = useState<SkillChangeSort>("newest");
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [reviewComment, setReviewComment] = useState("");

  const mutation = useReviewAnyChangeRequest(wsId);

  const visible = useMemo(
    () => sortSkillChanges(filterSkillChanges(changes, { scope, search }), sort),
    [changes, scope, search, sort],
  );

  const selected = changes.find((c) => c.id === selectedId) ?? null;

  // Lazy-load the target skill's live body so we can diff it against the
  // proposal — the pending queue rows don't carry skill content.
  const { data: skillDetail, isLoading: detailLoading } = useQuery({
    ...skillDetailOptions(wsId, selected?.skill_id ?? ""),
    enabled: !!selected,
  });

  const proposerName = (userId: string) =>
    members.find((m) => m.user_id === userId)?.name ?? userId.slice(0, 8);

  const backToList = () => {
    if (mutation.isPending) return;
    setSelectedId(null);
    setReviewComment("");
  };

  const handleReview = async (action: "approve" | "reject") => {
    if (!selected) return;
    try {
      await mutation.mutateAsync({
        crId: selected.id,
        skillId: selected.skill_id,
        data: { action, comment: reviewComment || undefined },
      });
      toast.success(action === "approve" ? "Change approved" : "Change rejected");
      setSelectedId(null);
      setReviewComment("");
    } catch {
      toast.error("Failed to review change request");
    }
  };

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side={isMobile ? "bottom" : "right"}
        className={
          isMobile
            ? "flex h-[85vh]! flex-col gap-0 rounded-t-xl p-0"
            : "flex w-[52vw]! max-w-[52vw]! flex-col gap-0 p-0 sm:max-w-[52vw]!"
        }
      >
        <SheetHeader className="border-b p-4">
          <SheetTitle className="flex items-center gap-2">
            <GitPullRequestArrow className="h-4 w-4 text-warning" />
            {selected ? "Review change" : "Changes to review"}
          </SheetTitle>
        </SheetHeader>

        {selected ? (
          <div className="flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto p-4">
            <button
              type="button"
              onClick={backToList}
              className="flex items-center gap-1 self-start text-xs text-muted-foreground hover:text-foreground"
            >
              <ArrowLeft className="h-3.5 w-3.5" />
              Back to list
            </button>

            <div>
              <div className="text-sm font-medium">{selected.title}</div>
              <div className="mt-1 flex flex-wrap items-center gap-1.5 text-xs text-muted-foreground">
                <span className="font-medium text-foreground">
                  {selected.skill_name}
                </span>
                <Clock className="h-3 w-3" />
                {formatRelativeTime(selected.created_at)}
                <span>· by {proposerName(selected.proposed_by)}</span>
                <span>
                  · {selected.current_version} → {selected.proposed_version}
                </span>
              </div>
            </div>

            {selected.description && (
              <div className="rounded bg-muted/40 px-2.5 py-1.5 text-xs text-muted-foreground">
                <MessageSquare className="mr-1 inline h-3 w-3" />
                {selected.description}
              </div>
            )}

            {detailLoading || !skillDetail ? (
              <Skeleton className="h-40 w-full rounded-md" />
            ) : (
              <SkillDiffView
                base={skillDetail.content}
                proposed={selected.proposed_content}
                baseLabel={`current (${selected.current_version})`}
                proposedLabel={selected.proposed_version}
              />
            )}

            <Textarea
              value={reviewComment}
              onChange={(e) => setReviewComment(e.target.value)}
              placeholder="Optional comment for the proposer…"
              rows={3}
              className="resize-none text-sm"
            />
            <div className="flex items-center justify-end gap-1.5">
              <Button
                type="button"
                size="sm"
                variant="outline"
                className="gap-1 border-destructive/40 text-destructive hover:bg-destructive/10"
                onClick={() => handleReview("reject")}
                disabled={mutation.isPending}
              >
                <X className="h-3.5 w-3.5" />
                Reject
              </Button>
              <Button
                type="button"
                size="sm"
                className="gap-1"
                onClick={() => handleReview("approve")}
                disabled={mutation.isPending}
              >
                <Check className="h-3.5 w-3.5" />
                {mutation.isPending ? "Saving…" : "Approve"}
              </Button>
            </div>
          </div>
        ) : (
          <>
            <div className="flex flex-col gap-2 border-b p-3">
              <div className="relative">
                <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
                <Input
                  value={search}
                  onChange={(e) => setSearch(e.target.value)}
                  placeholder="Search by skill or title…"
                  className="h-8 w-full pl-8 text-sm"
                />
              </div>
              <div className="flex flex-wrap items-center gap-2">
                <span className="text-xs text-muted-foreground">Show</span>
                <SegButton active={scope === "mine"} onClick={() => setScope("mine")}>
                  My skills
                </SegButton>
                <SegButton active={scope === "all"} onClick={() => setScope("all")}>
                  All skills
                </SegButton>
                <span className="ml-2 text-xs text-muted-foreground">Sort</span>
                <SegButton
                  active={sort === "newest"}
                  onClick={() => setSort("newest")}
                >
                  Newest
                </SegButton>
                <SegButton
                  active={sort === "oldest"}
                  onClick={() => setSort("oldest")}
                >
                  Oldest
                </SegButton>
              </div>
            </div>

            <div className="min-h-0 flex-1 space-y-1 overflow-y-auto p-3">
              {visible.length === 0 ? (
                <div className="flex flex-col items-center justify-center gap-2 py-16 text-center text-muted-foreground">
                  <GitPullRequestArrow className="h-8 w-8 text-muted-foreground/40" />
                  <p className="text-sm">No pending changes.</p>
                </div>
              ) : (
                visible.map((cr) => (
                  <button
                    key={cr.id}
                    type="button"
                    onClick={() => {
                      setSelectedId(cr.id);
                      setReviewComment("");
                    }}
                    className="flex w-full items-center gap-2 rounded-md border bg-card px-2.5 py-2 text-left hover:bg-accent"
                  >
                    <span
                      className="h-1.5 w-1.5 shrink-0 rounded-full bg-amber-500"
                      aria-hidden
                    />
                    <div className="min-w-0 flex-1">
                      <div className="truncate text-xs font-medium">
                        {cr.title}
                      </div>
                      <div className="flex items-center gap-1 text-[11px] text-muted-foreground">
                        <span className="truncate font-medium text-foreground/80">
                          {cr.skill_name}
                        </span>
                        <Clock className="h-2.5 w-2.5" />
                        {formatRelativeTime(cr.created_at)}
                        <span className="truncate">
                          · {proposerName(cr.proposed_by)}
                        </span>
                      </div>
                    </div>
                  </button>
                ))
              )}
            </div>
          </>
        )}
      </SheetContent>
    </Sheet>
  );
}
