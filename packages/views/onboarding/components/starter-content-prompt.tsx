"use client";

import { useState } from "react";
import { Loader2 } from "lucide-react";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import { useNavigation } from "@multica/views/navigation";
import { useCurrentWorkspace, paths } from "@multica/core/paths";
import type { QuestionnaireAnswers } from "@multica/core/onboarding";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { buildImportPayload } from "../utils/starter-content-templates";

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
  const workspace = useCurrentWorkspace();
  const user = useAuthStore((s) => s.user);
  const refreshMe = useAuthStore((s) => s.refreshMe);
  const { push } = useNavigation();

  const [submitting, setSubmitting] = useState<"import" | "dismiss" | null>(
    null,
  );

  const shouldShow =
    !!user &&
    !!workspace &&
    user.onboarded_at != null &&
    user.starter_content_state == null;

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
      });
      const result = await api.importStarterContent(payload);

      // Sync the new starter_content_state into the auth store so this
      // component unmounts cleanly on the next render.
      await refreshMe();

      toast.success("Starter tasks added — check your sidebar");

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
        err instanceof Error ? err.message : "Import failed — please retry",
      );
      setSubmitting(null);
    }
  };

  const onDismiss = async () => {
    if (submitting) return;
    setSubmitting("dismiss");
    try {
      await api.dismissStarterContent();
      await refreshMe();
    } catch (err) {
      toast.error(
        err instanceof Error
          ? err.message
          : "Could not dismiss — please retry",
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
            Welcome — add starter tasks?
          </DialogTitle>
          <DialogDescription className="pt-2 text-[14px] leading-[1.55]">
            A{" "}
            <span className="font-medium text-foreground">
              Getting Started
            </span>{" "}
            project with short tasks that walk through how agents, issues,
            and context work in Multica.
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
            Start blank workspace
          </Button>
          <Button onClick={onImport} disabled={submitting !== null}>
            {submitting === "import" && (
              <Loader2 className="h-4 w-4 animate-spin" />
            )}
            Add starter tasks
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
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
