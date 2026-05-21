"use client";

import { useState } from "react";
import { Loader2 } from "lucide-react";
import { toast } from "sonner";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import { useNavigation } from "@multica/views/navigation";
import { useCurrentWorkspace, paths } from "@multica/core/paths";
import type { QuestionnaireAnswers } from "@multica/core/onboarding";
import { pinKeys } from "@multica/core/pins";
import { projectKeys } from "@multica/core/projects";
import { issueKeys } from "@multica/core/issues/queries";
import {
  memberListOptions,
  workspaceKeys,
} from "@multica/core/workspace/queries";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import {
  buildImportPayload,
  type StarterContentLocale,
} from "../utils/starter-content-templates";
import { useT } from "../../i18n";

/**
 * Post-onboarding opt-in dialog.
 *
 * Shown exactly once per user, on the first workspace landing where
 * `user.starter_content_state === null`. The dialog is mandatory —
 * Import and Dismiss are the only exits. Both are terminal state
 * transitions server-side (NULL → 'imported' or NULL → 'dismissed'),
 * so the dialog never reappears on a subsequent visit.
 *
 * Client-side knowledge of agents is INTENTIONALLY zero here. The
 * dialog description is branch-agnostic and the POST payload carries
 * both sub-issue template arrays plus a welcome-issue template. The
 * SERVER inspects the workspace's agent list and picks the branch —
 * no client-side cache timing, no stale decisions, no Unknown bugs.
 */
export function StarterContentPrompt() {
  const { t, i18n } = useT("onboarding");
  const workspace = useCurrentWorkspace();
  const user = useAuthStore((s) => s.user);
  const refreshMe = useAuthStore((s) => s.refreshMe);
  const { push } = useNavigation();
  const qc = useQueryClient();

  const [submitting, setSubmitting] = useState<"import" | "dismiss" | null>(
    null,
  );

  // Member-list fetch is the proxy we use to detect "did this user CREATE
  // this workspace, or were they invited into it?" An invitee is by definition
  // not the only member (the inviter is also there); a fresh self-created
  // workspace has exactly one member — the creator. `starter_content_state`
  // is a user-level field and can't represent (user, workspace) state directly,
  // so we layer this membership check on top until that field is migrated to
  // the `member` table. See follow-up issue: starter_content_state per-workspace.
  const { data: members = [] } = useQuery({
    ...memberListOptions(workspace?.id ?? ""),
    enabled: !!workspace?.id,
  });
  const isSoloMember =
    members.length === 1 && members[0]?.user_id === user?.id;

  const shouldShow =
    !!user &&
    !!workspace &&
    user.onboarded_at != null &&
    user.starter_content_state == null &&
    isSoloMember;

  if (!shouldShow || !workspace || !user) return null;

  const onImport = async () => {
    if (submitting) return;
    setSubmitting("import");
    try {
      const questionnaire = mergeQuestionnaire(user.onboarding_questionnaire);
      const payload = buildImportPayload({
        workspaceId: workspace.id,
        userName: user.name || user.email,
        questionnaire,
        locale: resolveLocale(i18n.language),
      });
      const result = await api.importStarterContent(payload);

      // Mirror the `onSettled` pattern used by other mutations
      // (useCreatePin / useDeletePin / useReorderPins): the originating
      // session invalidates locally so the sidebar + board refresh
      // synchronously, independent of the WS round-trip. The server still
      // publishes `pin:created` / `project:created` / `issue:created` for
      // OTHER sessions; on this session both paths run and the second
      // invalidate is a no-op.
      //
      // Agents are invalidated too: the server picks the welcome issue's
      // assignee from its own agent list, and the issue-detail page we
      // navigate to immediately resolves that ID through the cached agent
      // list. If the cache is stale (or never populated since
      // onboarding-flow created the agent without invalidating), the
      // assignee renders as "Unknown Agent". Awaiting Promise.all
      // guarantees every relevant query is at least marked stale before
      // the navigation kicks in, so the next mount refetches.
      await Promise.all([
        qc.invalidateQueries({ queryKey: pinKeys.all(workspace.id, user.id) }),
        qc.invalidateQueries({ queryKey: projectKeys.all(workspace.id) }),
        qc.invalidateQueries({ queryKey: issueKeys.all(workspace.id) }),
        qc.invalidateQueries({ queryKey: workspaceKeys.agents(workspace.id) }),
      ]);

      // Sync the new starter_content_state into the auth store so this
      // component unmounts cleanly on the next render.
      await refreshMe();

      toast.success(t(($) => $.starter_content.success_toast));

      // If the server took the agent-guided branch, a welcome issue
      // exists and we jump to it. Otherwise, stay on the issues list —
      // the new Getting Started project appears via realtime events.
      if (result.welcome_issue_id) {
        push(
          paths.workspace(workspace.slug).issueDetail(result.welcome_issue_id),
        );
      }
    } catch (err) {
      toast.error(
        err instanceof Error ? err.message : t(($) => $.starter_content.import_failed),
      );
      setSubmitting(null);
    }
  };

  const onDismiss = async () => {
    if (submitting) return;
    setSubmitting("dismiss");
    try {
      await api.dismissStarterContent({ workspace_id: workspace.id });
      await refreshMe();
    } catch (err) {
      toast.error(
        err instanceof Error
          ? err.message
          : t(($) => $.starter_content.dismiss_failed),
      );
      setSubmitting(null);
    }
  };

  return (
    <Dialog
      open
      // `disablePointerDismissal` stops outside-click close; the
      // `onOpenChange` handler cancels Base UI's ESC-close path via
      // `eventDetails.cancel()`. Import / Dismiss are the only exits.
      disablePointerDismissal
      onOpenChange={(_open, eventDetails) => {
        eventDetails.cancel();
      }}
    >
      <DialogContent showCloseButton={false} className="sm:max-w-[440px]">
        <DialogHeader>
          <DialogTitle className="text-balance font-serif text-[22px] leading-[1.2] font-medium tracking-tight">
            {t(($) => $.starter_content.title)}
          </DialogTitle>
          <DialogDescription className="pt-2 text-[14px] leading-[1.55]">
            {t(($) => $.starter_content.description_prefix)}
            <span className="font-medium text-foreground">
              {t(($) => $.starter_content.description_term)}
            </span>
            {t(($) => $.starter_content.description_suffix)}
          </DialogDescription>
        </DialogHeader>

        <DialogFooter className="mt-2 gap-2 sm:justify-end">
          <Button
            variant="ghost"
            onClick={onDismiss}
            disabled={submitting !== null}
          >
            {submitting === "dismiss" && (
              <Loader2 className="h-4 w-4 animate-spin" />
            )}
            {t(($) => $.starter_content.dismiss_action)}
          </Button>
          <Button onClick={onImport} disabled={submitting !== null}>
            {submitting === "import" && (
              <Loader2 className="h-4 w-4 animate-spin" />
            )}
            {t(($) => $.starter_content.import_action)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// i18next resolves locale names like "zh-Hans-CN" or "en-US"; we only
// ship en + zh-Hans starter content, so default everything else to en.
function resolveLocale(language: string): StarterContentLocale {
  return language.startsWith("zh") ? "zh-Hans" : "en";
}

// Local helper — mirrors the onboarding flow's mergeQuestionnaire.
function mergeQuestionnaire(
  raw: Record<string, unknown>,
): QuestionnaireAnswers {
  const empty: QuestionnaireAnswers = {
    team_size: null,
    team_size_other: null,
    role: null,
    role_other: null,
    use_case: null,
    use_case_other: null,
  };
  return { ...empty, ...(raw as Partial<QuestionnaireAnswers>) };
}
