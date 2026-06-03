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
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Badge } from "@multica/ui/components/ui/badge";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { skillChangeRequestsOptions } from "../../core/queries";
import { useReviewSkillChangeRequest } from "../../core/mutations";
import { SkillDiffView } from "./skill-diff-view";

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

export function SkillChangeRequestQueue({ skill, wsId, members }: Props) {
  const [open, setOpen] = useState(false);
  const [reviewTarget, setReviewTarget] = useState<SkillChangeRequest | null>(null);
  const [reviewComment, setReviewComment] = useState("");
  const [reviewAction, setReviewAction] = useState<"approve" | "reject" | null>(null);

  const { data: requests = [], isLoading } = useQuery(
    skillChangeRequestsOptions(skill.id),
  );

  const pendingCount = requests.filter((r) => r.status === "pending").length;
  const mutation = useReviewSkillChangeRequest(skill.id, wsId);

  const proposerName = (userId: string) =>
    members.find((m) => m.user_id === userId)?.name ?? userId.slice(0, 8);

  const handleReview = async () => {
    if (!reviewTarget || !reviewAction) return;
    try {
      await mutation.mutateAsync({
        crId: reviewTarget.id,
        data: { action: reviewAction, comment: reviewComment || undefined },
      });
      toast.success(
        reviewAction === "approve" ? "Change approved" : "Change rejected",
      );
      setReviewTarget(null);
      setReviewComment("");
      setReviewAction(null);
    } catch {
      toast.error("Failed to review change request");
    }
  };

  const openReview = (
    req: SkillChangeRequest,
    action: "approve" | "reject",
  ) => {
    setReviewTarget(req);
    setReviewAction(action);
    setReviewComment("");
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
        <div className="mt-2 space-y-2">
          {isLoading && (
            <div className="text-xs text-muted-foreground">Loading…</div>
          )}
          {!isLoading && requests.length === 0 && (
            <div className="rounded-md border border-dashed px-3 py-3 text-center text-xs text-muted-foreground">
              No change requests yet.
            </div>
          )}
          {requests.map((req) => (
            <div key={req.id} className="rounded-md border bg-card">
              <div className="flex items-start gap-2 px-2.5 py-2">
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-1.5">
                    <span className="truncate text-xs font-medium">
                      {req.title}
                    </span>
                    <span
                      className={`rounded-full px-1.5 py-0.5 text-[10px] font-medium ${STATUS_CLASS[req.status]}`}
                    >
                      {STATUS_LABEL[req.status]}
                    </span>
                  </div>
                  <div className="mt-0.5 flex items-center gap-1 text-xs text-muted-foreground">
                    <Clock className="h-2.5 w-2.5" />
                    {formatRelativeTime(req.created_at)}
                    <span>· by {proposerName(req.proposed_by)}</span>
                  </div>
                  {req.description && (
                    <div className="mt-1 rounded bg-muted/40 px-2 py-1 text-xs text-muted-foreground">
                      <MessageSquare className="mr-1 inline h-2.5 w-2.5" />
                      {req.description}
                    </div>
                  )}
                </div>
              </div>

              <div className="border-t px-2.5 py-2">
                <SkillDiffView
                  base={skill.content}
                  proposed={req.proposed_content}
                  baseLabel={`current (${skill.current_version})`}
                  proposedLabel={req.proposed_version}
                />
              </div>

              {req.status === "pending" && (
                <div className="flex items-center gap-1.5 border-t px-2.5 py-2">
                  <Button
                    type="button"
                    size="xs"
                    variant="outline"
                    className="h-6 gap-1 border-emerald-500/40 text-emerald-700 hover:bg-emerald-500/10 dark:text-emerald-400"
                    onClick={() => openReview(req, "approve")}
                  >
                    <Check className="h-3 w-3" />
                    Approve
                  </Button>
                  <Button
                    type="button"
                    size="xs"
                    variant="outline"
                    className="h-6 gap-1 border-destructive/40 text-destructive hover:bg-destructive/10"
                    onClick={() => openReview(req, "reject")}
                  >
                    <X className="h-3 w-3" />
                    Reject
                  </Button>
                </div>
              )}

              {req.review_comment && (
                <div className="border-t px-2.5 py-2 text-xs text-muted-foreground">
                  <span className="font-medium">Comment: </span>
                  {req.review_comment}
                </div>
              )}
            </div>
          ))}
        </div>
      )}

      <Dialog
        open={!!reviewTarget}
        onOpenChange={(v) => {
          if (!v && !mutation.isPending) {
            setReviewTarget(null);
            setReviewAction(null);
          }
        }}
      >
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>
              {reviewAction === "approve" ? "Approve" : "Reject"} change:{" "}
              {reviewTarget?.title}
            </DialogTitle>
          </DialogHeader>
          <div className="space-y-3">
            <Textarea
              value={reviewComment}
              onChange={(e) => setReviewComment(e.target.value)}
              placeholder="Optional comment for the proposer…"
              rows={3}
              className="resize-none text-sm"
            />
          </div>
          <DialogFooter>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={() => setReviewTarget(null)}
              disabled={mutation.isPending}
            >
              Cancel
            </Button>
            <Button
              type="button"
              size="sm"
              variant={reviewAction === "reject" ? "destructive" : "default"}
              onClick={handleReview}
              disabled={mutation.isPending}
            >
              {mutation.isPending
                ? "Saving…"
                : reviewAction === "approve"
                  ? "Approve"
                  : "Reject"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
