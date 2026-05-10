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
 *  formats it with the i18n template.
 *
 *  Two valid release modes:
 *    - merge-train: all PRs are open + mergeable + green + approved.
 *      Release starts at "assembling", user starts the merge train.
 *    - tracking-only: all PRs are already merged (state="merged").
 *      Release starts at "in_staging" with merged_main_sha set —
 *      no merge train. Useful for bundling already-merged PRs
 *      sitting in MERGED · PRE-STAGING into a coordinated rollout.
 *
 *  Mixed selections (some open, some merged) are rejected because
 *  the merge train can't operate on already-merged PRs and tracking-
 *  only mode can't merge open ones. */
function eligibilityReason(pr: PullRequest): string | null {
  if (pr.state === "merged") {
    // Merged PRs are eligible by construction — they already cleared
    // GitHub's own merge gate. Skip the rest of the open-PR rules.
    return null;
  }
  if (pr.state === "closed") return "is closed";
  if (pr.is_draft) return "is a draft";
  if (pr.mergeable === "CONFLICTING") return "has merge conflicts";
  if (pr.ci_status && pr.ci_status !== "success") return `CI ${pr.ci_status}`;
  if (pr.review_decision && pr.review_decision !== "APPROVED") {
    return `review: ${pr.review_decision}`;
  }
  return null;
}

/** Auto-suggest a release title from the PR set.
 *
 *  Priority (most descriptive first):
 *   1. Conventional-commit scope shared across PRs ("docs(auth):" → "auth")
 *      → "{scope} release · {N} PRs"
 *   2. Conventional-commit type shared ("feat:", "fix:") → "{type} release · {N} PRs"
 *   3. Common leading word across ≥ half the titles → "{word} release · {N} PRs"
 *   4. Single PR → use its title verbatim (truncated to 60)
 *   5. Multi-PR with no overlap → "{N}-PR release · YYYY-MM-DD"
 *
 *  Earlier versions copied the first PR's title verbatim, which made
 *  multi-PR releases misleading (a 3-PR release looked like one PR).
 *  Goal: the suggestion is descriptive of the *whole batch* in one
 *  glance — the user can always edit. */
function suggestTitle(prs: PullRequest[]): string {
  if (prs.length === 0) return "";
  const titles = prs.map((p) => p.title.trim()).filter(Boolean);
  if (titles.length === 0) return "";
  if (titles.length === 1) {
    // Single PR — verbatim title (lightly trimmed) is the best signal.
    const only = titles[0]!;
    return only.length > 60 ? only.slice(0, 59) + "…" : only;
  }

  // Multi-PR — try to find a shared theme.
  const N = titles.length;

  // 1. Conventional-commit scope. Pattern: type(scope): subject.
  //    If ≥ ceil(N/2) PRs share the same scope, use it.
  const conventionalRe = /^([a-z]+)(?:\(([^)]+)\))?:/i;
  const scopes: Record<string, number> = {};
  const types: Record<string, number> = {};
  for (const t of titles) {
    const m = conventionalRe.exec(t);
    if (!m) continue;
    if (m[1]) types[m[1].toLowerCase()] = (types[m[1].toLowerCase()] ?? 0) + 1;
    if (m[2]) scopes[m[2].toLowerCase()] = (scopes[m[2].toLowerCase()] ?? 0) + 1;
  }
  const halfCount = Math.ceil(N / 2);
  const topScope = topEntry(scopes);
  if (topScope && topScope[1] >= halfCount) {
    return `${topScope[0]} release · ${N} PRs`;
  }
  const topType = topEntry(types);
  if (topType && topType[1] >= halfCount) {
    return `${topType[0]} release · ${N} PRs`;
  }

  // 2. Common leading word (non-conventional commits).
  const heads = titles.map((t) => firstWord(t));
  const headCounts: Record<string, number> = {};
  for (const h of heads) {
    if (!h) continue;
    headCounts[h.toLowerCase()] = (headCounts[h.toLowerCase()] ?? 0) + 1;
  }
  const topHead = topEntry(headCounts);
  if (topHead && topHead[1] >= halfCount) {
    return `${topHead[0]} release · ${N} PRs`;
  }

  // 3. No shared theme. Date-stamped fallback. Date in user-local
  //    isoDate format (YYYY-MM-DD) so titles sort chronologically.
  const today = new Date().toISOString().slice(0, 10);
  return `${N}-PR release · ${today}`;
}

function topEntry(counts: Record<string, number>): [string, number] | null {
  let best: [string, number] | null = null;
  for (const [k, v] of Object.entries(counts)) {
    if (!best || v > best[1]) best = [k, v];
  }
  return best;
}

function firstWord(s: string): string {
  // Drop leading "type(scope):" prefix if present so the word
  // detection doesn't pick the type as the theme.
  const stripped = s.replace(/^[a-z]+(?:\([^)]+\))?:\s*/i, "");
  return stripped.split(/\s+/)[0] ?? "";
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

  /** Workspace's configured rule for the highest-risk tier of the
   *  selected PRs. Pulls from the new ship_hub_approval_* fields,
   *  falling back to the legacy hardcoded behavior when the
   *  workspace row predates migration 090.
   *
   *  Decides:
   *   - Show the approver picker when the rule is "approver" or "two".
   *     ("admin" doesn't need a per-release approver — the gate
   *     resolves via member.role at verify time.)
   *   - Show the second-approver picker only when the rule is "two".
   */
  const approvalRule = useMemo<"member" | "admin" | "approver" | "two">(() => {
    const norm = (
      v: string | undefined,
      fallback: "member" | "admin" | "approver" | "two",
    ): "member" | "admin" | "approver" | "two" => {
      if (v === "member" || v === "admin" || v === "approver" || v === "two") {
        return v;
      }
      return fallback;
    };
    if (!workspace) return "member";
    switch (highestRisk) {
      case "low":
        return norm(workspace.ship_hub_approval_low, "member");
      case "medium":
        return norm(workspace.ship_hub_approval_medium, "member");
      case "high":
        return norm(workspace.ship_hub_approval_high, "approver");
      case "critical":
        return norm(workspace.ship_hub_approval_critical, "two");
      default:
        return "member";
    }
  }, [workspace, highestRisk]);

  const showApproverField =
    approvalRule === "approver" || approvalRule === "two";
  const showSecondApproverField = approvalRule === "two";

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
  // submission. Now driven by the workspace's configured rule
  // (Phase 7d follow-up): only nag for an approver when the rule
  // for the highest-risk tier actually needs one.
  const softWarnings = useMemo(() => {
    const out: string[] = [];
    if (showApproverField && !approverId) {
      out.push(
        t(($) => $.releases.create_dialog.no_approver_required, {
          level: highestRisk,
        }),
      );
    }
    if (showSecondApproverField && !secondApproverId) {
      out.push(t(($) => $.releases.create_dialog.no_second_approver_required));
    }
    return out;
  }, [showApproverField, showSecondApproverField, highestRisk, approverId, secondApproverId, t]);

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

          {showApproverField && (
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
          )}

          {showSecondApproverField && (
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
