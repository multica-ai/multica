"use client";

import { useState } from "react";
import {
  Check,
  ChevronDown,
  ChevronRight,
  Clock,
  GitPullRequest,
  MessageSquare,
  X,
} from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import type { MemberWithUser, Skill, SkillChangeRequest } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import {
  Sheet,
  SheetContent,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@multica/ui/components/ui/sheet";
import { useIsMobile } from "@multica/ui/hooks/use-mobile";
import { useMobileViewportHeight } from "@multica/cerebro-duplicate-check/views";
import { Badge } from "@multica/ui/components/ui/badge";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { skillChangeRequestsOptions } from "../../core/queries";
import { useReviewSkillChangeRequest } from "../../core/mutations";
import { SkillDiffView } from "./skill-diff-view";
import { SkillDiffEditor } from "./skill-diff-editor";

interface Props {
  skill: Skill;
  wsId: string;
  members: MemberWithUser[];
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

const STATUS_LABEL: Record<SkillChangeRequest["status"], string> = {
  pending: "Pending",
  approved: "Approved",
  rejected: "Rejected",
  merged: "Merged",
};

const STATUS_CLASS: Record<SkillChangeRequest["status"], string> = {
  pending: "bg-amber-500/15 text-amber-700 dark:text-amber-400",
  approved: "bg-emerald-500/15 text-emerald-700 dark:text-emerald-400",
  rejected: "bg-destructive/15 text-destructive",
  merged: "bg-blue-500/15 text-blue-700 dark:text-blue-400",
};

const STATUS_DOT: Record<SkillChangeRequest["status"], string> = {
  pending: "bg-amber-500",
  approved: "bg-emerald-500",
  rejected: "bg-destructive",
  merged: "bg-blue-500",
};

export function SkillChangeRequestQueue({ skill, wsId, members }: Props) {
  const [open, setOpen] = useState(true);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [reviewComment, setReviewComment] = useState("");
  const [editedContent, setEditedContent] = useState<string | undefined>(undefined);
  const isMobile = useIsMobile();
  const mobileVvh = useMobileViewportHeight();

  const { data: requests = [], isLoading } = useQuery(
    skillChangeRequestsOptions(skill.id),
  );

  const pendingCount = requests.filter((r) => r.status === "pending").length;
  const mutation = useReviewSkillChangeRequest(skill.id, wsId);

  const selected = requests.find((r) => r.id === selectedId) ?? null;

  const proposerName = (userId: string) =>
    members.find((m) => m.user_id === userId)?.name ?? userId.slice(0, 8);

  const openReview = (req: SkillChangeRequest) => {
    setSelectedId(req.id);
    setReviewComment("");
    setEditedContent(undefined);
  };

  const closeReview = () => {
    if (mutation.isPending) return;
    setSelectedId(null);
    setReviewComment("");
    setEditedContent(undefined);
  };

  const handleReview = async (action: "approve" | "reject") => {
    if (!selected) return;
    try {
      await mutation.mutateAsync({
        crId: selected.id,
        data: {
          action,
          comment: reviewComment || undefined,
          // Edits only apply on approve — a reject always refers to the
          // proposal as originally submitted.
          edited_content:
            action === "approve" && editedContent !== undefined && editedContent !== selected.proposed_content
              ? editedContent
              : undefined,
        },
      });
      toast.success(action === "approve" ? "Change approved" : "Change rejected");
      setSelectedId(null);
      setReviewComment("");
      setEditedContent(undefined);
    } catch {
      toast.error("Failed to review change request");
    }
  };

  return (
    <div>
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="flex w-full items-center justify-between text-xs font-medium uppercase tracking-wider text-muted-foreground hover:text-foreground"
      >
        <span className="flex items-center gap-1.5">
          <GitPullRequest className="h-3 w-3" />
          Change requests
          {pendingCount > 0 && (
            <Badge
              variant="secondary"
              className="h-4 bg-amber-500/20 text-xs text-amber-700 dark:text-amber-400"
            >
              {pendingCount}
            </Badge>
          )}
        </span>
        {open ? (
          <ChevronDown className="h-3 w-3" />
        ) : (
          <ChevronRight className="h-3 w-3" />
        )}
      </button>

      {open && (
        <div className="mt-2 space-y-1">
          {isLoading && (
            <div className="text-xs text-muted-foreground">Loading…</div>
          )}
          {!isLoading && requests.length === 0 && (
            <div className="rounded-md border border-dashed px-3 py-3 text-center text-xs text-muted-foreground">
              No change requests yet.
            </div>
          )}
          {requests.map((req) => (
            <button
              key={req.id}
              type="button"
              onClick={() => openReview(req)}
              className="flex w-full items-center gap-2 rounded-md border bg-card px-2.5 py-1.5 text-left hover:bg-accent"
            >
              <span
                className={`h-1.5 w-1.5 shrink-0 rounded-full ${STATUS_DOT[req.status]}`}
                aria-hidden
              />
              <div className="min-w-0 flex-1">
                <div className="truncate text-xs font-medium">{req.title}</div>
                <div className="flex items-center gap-1 text-[11px] text-muted-foreground">
                  <Clock className="h-2.5 w-2.5" />
                  {formatRelativeTime(req.created_at)}
                  <span className="truncate">· {proposerName(req.proposed_by)}</span>
                </div>
              </div>
              {req.status === "pending" ? (
                <ChevronRight className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
              ) : (
                <span
                  className={`shrink-0 rounded-full px-1.5 py-0.5 text-[10px] font-medium ${STATUS_CLASS[req.status]}`}
                >
                  {STATUS_LABEL[req.status]}
                </span>
              )}
            </button>
          ))}
        </div>
      )}

      <Sheet open={!!selected} onOpenChange={(v) => !v && closeReview()}>
        <SheetContent
          side={isMobile ? "bottom" : "right"}
          className={
            isMobile
              ? "inset-0! h-[var(--cerebro-vvh,100dvh)]! w-full! max-w-none! translate-x-0! translate-y-0! gap-3 overflow-y-auto rounded-none! p-4 pb-[max(env(safe-area-inset-bottom),1rem)]!"
              : "w-[60vw]! max-w-[60vw]! gap-3 overflow-y-auto p-6 sm:max-w-[60vw]!"
          }
          // Full-screen (not a partial bottom sheet) on mobile: the diff needs
          // all the vertical room it can get, and --cerebro-vvh tracks the
          // on-screen keyboard so Approve/Reject stay reachable while the
          // review-comment field is focused (same fix as FIR-2504).
          style={
            isMobile && mobileVvh != null
              ? ({ ["--cerebro-vvh" as string]: `${mobileVvh}px` } as React.CSSProperties)
              : undefined
          }
        >
          {selected && (
            <>
              <SheetHeader className="p-0">
                <SheetTitle className="flex items-center gap-2 pr-6">
                  <span className="truncate">{selected.title}</span>
                  <span
                    className={`shrink-0 rounded-full px-1.5 py-0.5 text-[10px] font-medium ${STATUS_CLASS[selected.status]}`}
                  >
                    {STATUS_LABEL[selected.status]}
                  </span>
                </SheetTitle>
              </SheetHeader>

              <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
                <Clock className="h-3 w-3" />
                {formatRelativeTime(selected.created_at)}
                <span>· by {proposerName(selected.proposed_by)}</span>
                <span>· {skill.current_version} → {selected.proposed_version}</span>
              </div>

              {selected.description && (
                <div className="rounded bg-muted/40 px-2.5 py-1.5 text-xs text-muted-foreground">
                  <MessageSquare className="mr-1 inline h-3 w-3" />
                  {selected.description}
                </div>
              )}

              {selected.status === "pending" ? (
                <SkillDiffEditor
                  base={skill.content}
                  proposed={selected.proposed_content}
                  baseLabel={`current (${skill.current_version})`}
                  proposedLabel={selected.proposed_version}
                  editedContent={editedContent}
                  onEditedContentChange={setEditedContent}
                />
              ) : (
                <SkillDiffView
                  base={skill.content}
                  proposed={selected.proposed_content}
                  baseLabel={`current (${skill.current_version})`}
                  proposedLabel={selected.proposed_version}
                />
              )}

              {selected.status === "pending" ? (
                <>
                  <Textarea
                    value={reviewComment}
                    onChange={(e) => setReviewComment(e.target.value)}
                    placeholder="Optional comment for the proposer…"
                    rows={3}
                    className="resize-none text-sm"
                  />
                  <SheetFooter className="flex-row items-center justify-between gap-1.5 p-0">
                    <Button
                      type="button"
                      variant="ghost"
                      size="sm"
                      onClick={closeReview}
                      disabled={mutation.isPending}
                    >
                      Cancel
                    </Button>
                    <div className="flex items-center gap-1.5">
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
                  </SheetFooter>
                </>
              ) : (
                <>
                  {selected.review_comment && (
                    <div className="rounded bg-muted/40 px-2.5 py-1.5 text-xs text-muted-foreground">
                      <span className="font-medium">Review comment: </span>
                      {selected.review_comment}
                    </div>
                  )}
                  <SheetFooter className="flex-row justify-end p-0">
                    <Button type="button" variant="ghost" size="sm" onClick={closeReview}>
                      Close
                    </Button>
                  </SheetFooter>
                </>
              )}
            </>
          )}
        </SheetContent>
      </Sheet>
    </div>
  );
}
