"use client";

// Phase 7a — Create Release dialog.
//
// Trigger: the "Create release" button in `ShipSelectionBar`.
// Validates the PR set client-side (eligibility, risk-tier
// approver requirements), submits, navigates to the new release
// detail page on success.
//
// Per CLAUDE.md the dialog never imports next/* or react-router-dom
// — navigation flows through `useNavigation` from
// `@multica/views/navigation`.

import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { AlertTriangle, Rocket } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Button } from "@multica/ui/components/ui/button";
import { Textarea } from "@multica/ui/components/ui/textarea";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { useCreateRelease, useShipSelection } from "@multica/core/ship";
import type { PullRequest } from "@multica/core/types";
import { memberListOptions } from "@multica/core/workspace/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { useT } from "../../i18n";
import { useNavigation } from "../../navigation";
import { useCurrentWorkspace } from "@multica/core/paths";

interface CreateReleaseDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  projectId: string;
  selectedPullRequests: PullRequest[];
}

const RISK_RANK: Record<string, number> = {
  low: 0,
  medium: 1,
  high: 2,
  critical: 3,
};

/** Pull-request eligibility check. Mirrors releaseEligibilityReason()
 *  in server/internal/service/ship/release.go so the dialog can
 *  disable submit before round-tripping. The server still re-checks;
 *  this is purely UX. Returns the raw reason string; the caller
 *  formats it with the i18n template. */
function eligibilityReason(pr: PullRequest): string | null {
  if (pr.state !== "open") return "is not open";
  if (pr.is_draft) return "is a draft";
  if (pr.mergeable === "CONFLICTING") return "has merge conflicts";
  if (pr.ci_status && pr.ci_status !== "success") return `CI ${pr.ci_status}`;
  if (pr.review_decision && pr.review_decision !== "APPROVED") {
    return `review: ${pr.review_decision}`;
  }
  return null;
}

/** Auto-suggest a release title from the PR set. We pick the
 *  longest common alphanumeric prefix of the PR titles, falling
 *  back to "Release {date}" when no useful overlap exists. The
 *  goal is "smart enough to remove busywork, not so smart it
 *  surprises you" — the user can always edit the field. */
function suggestTitle(prs: PullRequest[]): string {
  if (prs.length === 0) return "";
  const titles = prs.map((p) => p.title.trim()).filter(Boolean);
  if (titles.length === 0) return "";
  // Tokenize on whitespace and pick the most common leading token
  // across the set. Cheap heuristic — beats anything more complex
  // for the small-team workflow.
  const heads = titles.map((t) => t.split(/\s+/)[0] ?? "");
  const counts: Record<string, number> = {};
  for (const h of heads) {
    if (!h) continue;
    counts[h] = (counts[h] ?? 0) + 1;
  }
  let best = "";
  let bestN = 0;
  for (const [k, v] of Object.entries(counts)) {
    if (v > bestN) {
      best = k;
      bestN = v;
    }
  }
  if (bestN >= 2) {
    return `${best} release`;
  }
  // Fall back: a short stable label.
  const first = titles[0] ?? "";
  return first.length > 40 ? first.slice(0, 39) + "…" : first;
}

export function CreateReleaseDialog({
  open,
  onOpenChange,
  projectId,
  selectedPullRequests,
}: CreateReleaseDialogProps) {
  const { t } = useT("ship");
  const wsId = useWorkspaceId();
  const workspace = useCurrentWorkspace();
  const navigation = useNavigation();
  const create = useCreateRelease(projectId);
  const clearSelection = useShipSelection((s) => s.clear);
  const { data: members = [] } = useQuery(memberListOptions(wsId));

  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [approverId, setApproverId] = useState<string>("");
  const [secondApproverId, setSecondApproverId] = useState<string>("");
  const [submitError, setSubmitError] = useState<string | null>(null);

  // Reset state when the dialog opens. Auto-suggest the title from
  // the PR set so the user only needs to confirm in the common case.
  useEffect(() => {
    if (!open) return;
    setTitle(suggestTitle(selectedPullRequests));
    setDescription("");
    setApproverId("");
    setSecondApproverId("");
    setSubmitError(null);
  }, [open, selectedPullRequests]);

  // Compute the highest risk across the PR set. Mirrors
  // highestRisk() in the server.
  const highestRisk = useMemo(() => {
    let best = "low";
    for (const pr of selectedPullRequests) {
      const candidate = pr.risk_level ?? "medium";
      if ((RISK_RANK[candidate] ?? 0) > (RISK_RANK[best] ?? 0)) {
        best = candidate;
      }
    }
    return selectedPullRequests.length === 0 ? "medium" : best;
  }, [selectedPullRequests]);

  // Per-PR ineligibility messages. Renders as a list at the bottom
  // of the dialog AND blocks submit when non-empty.
  const ineligibilityMessages = useMemo(() => {
    return selectedPullRequests
      .map((pr) => {
        const reason = eligibilityReason(pr);
        if (!reason) return null;
        return t(($) => $.releases.create_dialog.ineligible_pr, {
          number: pr.number,
          reason,
        });
      })
      .filter((m): m is string => m !== null);
  }, [selectedPullRequests, t]);

  // Soft warnings — same shape the server returns, computed
  // client-side so the dialog can surface them inline before
  // submission.
  const softWarnings = useMemo(() => {
    const out: string[] = [];
    if (highestRisk !== "low" && !approverId) {
      out.push(
        t(($) => $.releases.create_dialog.no_approver_required, {
          level: highestRisk,
        }),
      );
    }
    if (highestRisk === "critical" && !secondApproverId) {
      out.push(t(($) => $.releases.create_dialog.no_second_approver_required));
    }
    return out;
  }, [highestRisk, approverId, secondApproverId, t]);

  const submitDisabled =
    create.isPending ||
    !title.trim() ||
    selectedPullRequests.length === 0 ||
    ineligibilityMessages.length > 0;

  const handleSubmit = async () => {
    setSubmitError(null);
    try {
      const resp = await create.mutateAsync({
        title: title.trim(),
        description: description.trim() || undefined,
        pull_request_ids: selectedPullRequests.map((p) => p.id),
        approver_id: approverId || undefined,
        second_approver_id: secondApproverId || undefined,
      });
      toast.success(t(($) => $.releases.create_dialog.title));
      clearSelection();
      onOpenChange(false);
      // Navigate to the detail page. Path is workspace-scoped so
      // we use the slug from the current workspace.
      const slug = workspace?.slug ?? "";
      if (slug) {
        navigation.push(`/${slug}/ship/release/${resp.release.id}`);
      }
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      setSubmitError(msg);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Rocket className="size-4" aria-hidden />
            {t(($) => $.releases.create_dialog.title)}
          </DialogTitle>
          <DialogDescription>
            {t(($) => $.releases.create_dialog.description)}
          </DialogDescription>
        </DialogHeader>

        <div className="grid gap-4 py-2">
          <div className="grid gap-1.5">
            <Label htmlFor="release-title">
              {t(($) => $.releases.create_dialog.title_label)}
            </Label>
            <Input
              id="release-title"
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              placeholder={t(($) => $.releases.create_dialog.title_placeholder)}
              data-testid="release-title-input"
            />
          </div>

          <div className="grid gap-1.5">
            <Label htmlFor="release-description">
              {t(($) => $.releases.create_dialog.description_label)}
            </Label>
            <Textarea
              id="release-description"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder={t(($) => $.releases.create_dialog.description_placeholder)}
              rows={3}
            />
          </div>

          <div className="grid gap-1.5">
            <Label htmlFor="release-approver">
              {t(($) => $.releases.create_dialog.approver_label)}
            </Label>
            <Select value={approverId} onValueChange={(v) => setApproverId(v ?? "")}>
              <SelectTrigger id="release-approver" data-testid="release-approver-select">
                <SelectValue
                  placeholder={t(($) => $.releases.create_dialog.approver_placeholder)}
                />
              </SelectTrigger>
              <SelectContent>
                {members.map((m) => (
                  <SelectItem key={m.user_id} value={m.user_id}>
                    {m.name || m.email}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          {highestRisk === "critical" && (
            <div className="grid gap-1.5">
              <Label htmlFor="release-second-approver">
                {t(($) => $.releases.create_dialog.second_approver_label)}
              </Label>
              <Select value={secondApproverId} onValueChange={(v) => setSecondApproverId(v ?? "")}>
                <SelectTrigger id="release-second-approver">
                  <SelectValue
                    placeholder={t(($) => $.releases.create_dialog.approver_placeholder)}
                  />
                </SelectTrigger>
                <SelectContent>
                  {members
                    .filter((m) => m.user_id !== approverId)
                    .map((m) => (
                      <SelectItem key={m.user_id} value={m.user_id}>
                        {m.name || m.email}
                      </SelectItem>
                    ))}
                </SelectContent>
              </Select>
            </div>
          )}

          <div className="grid gap-1.5">
            <Label>{t(($) => $.releases.create_dialog.prs_label)}</Label>
            <ul className="rounded border bg-muted/30 p-2 text-sm">
              {selectedPullRequests.map((pr) => (
                <li
                  key={pr.id}
                  className="flex items-center gap-2 py-1 text-muted-foreground"
                >
                  <span className="tabular-nums">#{pr.number}</span>
                  <span className="truncate text-foreground">{pr.title}</span>
                  <span className="ml-auto text-[11px] uppercase tracking-wide">
                    {pr.risk_level ?? "medium"}
                  </span>
                </li>
              ))}
            </ul>
          </div>

          {(softWarnings.length > 0 || ineligibilityMessages.length > 0) && (
            <div
              className="rounded border border-amber-500/40 bg-amber-500/5 p-2"
              data-testid="release-warnings"
            >
              <div className="mb-1 flex items-center gap-1.5 text-xs font-medium text-amber-700 dark:text-amber-400">
                <AlertTriangle className="size-3.5" />
                {t(($) => $.releases.create_dialog.warning_title)}
              </div>
              <ul className="ml-4 list-disc space-y-1 text-xs text-muted-foreground">
                {ineligibilityMessages.map((m, i) => (
                  <li key={`ineligible-${i}`} className="text-destructive">
                    {m}
                  </li>
                ))}
                {softWarnings.map((m, i) => (
                  <li key={`soft-${i}`}>{m}</li>
                ))}
              </ul>
            </div>
          )}

          {submitError && (
            <p className="text-xs text-destructive">{submitError}</p>
          )}
        </div>

        <DialogFooter>
          <Button
            variant="ghost"
            onClick={() => onOpenChange(false)}
            disabled={create.isPending}
          >
            {t(($) => $.releases.create_dialog.cancel)}
          </Button>
          <Button
            onClick={handleSubmit}
            disabled={submitDisabled}
            data-testid="release-submit"
          >
            {create.isPending
              ? t(($) => $.releases.create_dialog.submitting)
              : t(($) => $.releases.create_dialog.submit)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
