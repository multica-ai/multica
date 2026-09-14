"use client";

import { useWorkspaceId } from "@multica/core/hooks";
import {
  useCreateManualProjectPlan,
  useCreateProjectPlanPart,
  useCreateProjectPlanPhase,
  useDeleteProjectPlan,
  useDeleteProjectPlanPart,
  useDeleteProjectPlanPhase,
  useLinkProjectPlanPartIssue,
  useSupersedeProjectPlan,
  useUnlinkProjectPlanPartIssue,
  useUpdateProjectPlan,
  useUpdateProjectPlanPart,
  useUpdateProjectPlanPhase,
} from "@multica/core/project-plan/mutations";
import { useT } from "../../../../i18n";
import { usePlanAuthoring } from "./plan-authoring-context";
import { PlanConfirmDialog } from "./plan-confirm-dialog";
import { PlanFormDialog } from "./plan-form-dialog";
import { PlanLinkIssuesDialog } from "./plan-link-issues-dialog";
import { nextPosition } from "./plan-ordering";

/**
 * The single kind of plan this release accepts. `requireSupportedKind` in
 * server/internal/projectplan/service.go refuses anything else ("only prd
 * plans are supported in this release"), so there is no kind picker to offer —
 * spec/sprint plan kinds are a later phase.
 */
const SUPPORTED_PLAN_KIND = "prd";

/**
 * Renders whichever authoring dialog the context says is open. Mounted once,
 * next to the panes, so no pane has to know about dialog state and a dialog
 * survives the pane switching between Document / Pipeline / Coverage.
 */
export function PlanAuthoringDialogs() {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const { enabled, projectId, overview, dialog, open, close } = usePlanAuthoring();

  const createPlan = useCreateManualProjectPlan(wsId, projectId);
  const updatePlan = useUpdateProjectPlan(wsId, projectId);
  const supersedePlan = useSupersedeProjectPlan(wsId, projectId);
  const deletePlan = useDeleteProjectPlan(wsId, projectId);
  const createPhase = useCreateProjectPlanPhase(wsId, projectId);
  const updatePhase = useUpdateProjectPlanPhase(wsId, projectId);
  const deletePhase = useDeleteProjectPlanPhase(wsId, projectId);
  const createPart = useCreateProjectPlanPart(wsId, projectId);
  const updatePart = useUpdateProjectPlanPart(wsId, projectId);
  const deletePart = useDeleteProjectPlanPart(wsId, projectId);
  const linkIssue = useLinkProjectPlanPartIssue(wsId, projectId);
  const unlinkIssue = useUnlinkProjectPlanPartIssue(wsId, projectId);

  if (!enabled || !dialog) return null;

  // Every branch below except `create-plan` writes to an existing plan. When
  // there is no plan there is no id to write to, so those dialogs simply do
  // not render rather than sending a request with a placeholder id.
  const planId = overview?.plan.id ?? null;
  if (dialog.kind !== "create-plan" && !planId) return null;

  switch (dialog.kind) {
    case "create-plan":
      return (
        <PlanFormDialog
          open
          onOpenChange={close}
          heading={t(($) => $.plan.authoring.create_plan.title)}
          description={t(($) => $.plan.authoring.create_plan.description)}
          submitLabel={t(($) => $.plan.authoring.create_plan.submit)}
          fields={[
            {
              name: "title",
              label: t(($) => $.plan.authoring.field.plan_title),
              initial: "",
              placeholder: t(($) => $.plan.authoring.create_plan.title_placeholder),
            },
            {
              name: "description",
              label: t(($) => $.plan.authoring.field.description),
              initial: "",
              multiline: true,
              rows: 4,
            },
          ]}
          onSubmit={async (values) =>
            createPlan.mutateAsync({
              kind: SUPPORTED_PLAN_KIND,
              title: (values.title ?? "").trim(),
              description: values.description ?? "",
            })
          }
        />
      );

    case "edit-plan":
      return (
        <PlanFormDialog
          open
          onOpenChange={close}
          heading={t(($) => $.plan.authoring.edit_plan.title)}
          description={t(($) => $.plan.authoring.edit_plan.description)}
          submitLabel={t(($) => $.plan.authoring.save)}
          fields={[
            {
              name: "title",
              label: t(($) => $.plan.authoring.field.plan_title),
              initial: overview!.plan.title,
            },
            {
              name: "description",
              label: t(($) => $.plan.authoring.field.description),
              initial: overview!.plan.description,
              multiline: true,
              rows: 4,
            },
          ]}
          onSubmit={async (values) =>
            updatePlan.mutateAsync({
              planId: planId!,
              data: {
                title: (values.title ?? "").trim(),
                description: values.description ?? "",
              },
            })
          }
        />
      );

    case "supersede-plan":
      return (
        <PlanFormDialog
          open
          onOpenChange={close}
          heading={t(($) => $.plan.authoring.supersede.title)}
          description={t(($) => $.plan.authoring.supersede.description, {
            version: overview!.plan.version,
            next: overview!.plan.version + 1,
          })}
          submitLabel={t(($) => $.plan.authoring.continue_label)}
          fields={[
            {
              name: "title",
              label: t(($) => $.plan.authoring.field.plan_title),
              initial: overview!.plan.title,
            },
            {
              name: "description",
              label: t(($) => $.plan.authoring.field.description),
              initial: overview!.plan.description,
              multiline: true,
              rows: 3,
            },
          ]}
          extra={
            <div className="rounded-md border border-dashed border-border bg-muted/30 px-3 py-2 text-caption text-muted-foreground">
              {t(($) => $.plan.authoring.supersede.consequences, {
                version: overview!.plan.version,
              })}
            </div>
          }
          // Advancing replaces this dialog with the confirmation; closing
          // afterwards would wipe the stage we just opened.
          closeOnSubmit={false}
          // Stage 1 collects the new version's fields and sends NOTHING. The
          // write is authorised by the confirmation below:
          // supersede archives the current version and rotates which plan is
          // active, which is not something a form's save button should do on
          // its own.
          onSubmit={async (values) =>
            open({
              kind: "supersede-plan-confirm",
              patch: {
                title: (values.title ?? "").trim(),
                description: values.description ?? "",
              },
            })
          }
        />
      );

    case "supersede-plan-confirm":
      return (
        <PlanConfirmDialog
          open
          onOpenChange={close}
          heading={t(($) => $.plan.authoring.supersede.confirm_title)}
          description={t(($) => $.plan.authoring.supersede.description, {
            version: overview!.plan.version,
            next: overview!.plan.version + 1,
          })}
          consequences={t(($) => $.plan.authoring.supersede.consequences, {
            version: overview!.plan.version,
          })}
          confirmLabel={t(($) => $.plan.authoring.supersede.submit)}
          // Not destructive: the old version is retained and stays readable.
          variant="default"
          onConfirm={async () => {
            await supersedePlan.mutateAsync({ planId: planId!, data: dialog.patch });
          }}
        />
      );

    case "delete-plan":
      return (
        <PlanConfirmDialog
          open
          onOpenChange={close}
          heading={t(($) => $.plan.authoring.delete_plan.title)}
          description={t(($) => $.plan.authoring.delete_plan.description, {
            title: overview!.plan.title,
          })}
          consequences={t(($) => $.plan.authoring.delete_plan.consequences)}
          confirmLabel={t(($) => $.plan.authoring.delete_plan.submit)}
          onConfirm={async () => {
            await deletePlan.mutateAsync(planId!);
          }}
        />
      );

    case "add-phase":
      return (
        <PlanFormDialog
          open
          onOpenChange={close}
          heading={t(($) => $.plan.authoring.add_phase.title)}
          description={t(($) => $.plan.authoring.add_phase.description)}
          submitLabel={t(($) => $.plan.authoring.add_phase.submit)}
          fields={[
            {
              name: "title",
              label: t(($) => $.plan.authoring.field.phase_title),
              initial: "",
              placeholder: t(($) => $.plan.authoring.add_phase.title_placeholder),
            },
            {
              name: "description",
              label: t(($) => $.plan.authoring.field.description),
              initial: "",
              multiline: true,
              rows: 3,
            },
          ]}
          onSubmit={async (values) =>
            createPhase.mutateAsync({
              planId: planId!,
              data: {
                title: (values.title ?? "").trim(),
                description: values.description ?? "",
                position: nextPosition(overview!.phases),
              },
            })
          }
        />
      );

    case "edit-phase":
      return (
        <PlanFormDialog
          open
          onOpenChange={close}
          heading={t(($) => $.plan.authoring.edit_phase.title)}
          description={t(($) => $.plan.authoring.edit_phase.description)}
          submitLabel={t(($) => $.plan.authoring.save)}
          fields={[
            {
              name: "title",
              label: t(($) => $.plan.authoring.field.phase_title),
              initial: dialog.phase.title,
            },
            {
              name: "description",
              label: t(($) => $.plan.authoring.field.description),
              initial: dialog.phase.description,
              multiline: true,
              rows: 3,
            },
          ]}
          onSubmit={async (values) =>
            updatePhase.mutateAsync({
              planId: planId!,
              phaseId: dialog.phase.id,
              data: {
                title: (values.title ?? "").trim(),
                description: values.description ?? "",
              },
            })
          }
        />
      );

    case "delete-phase":
      return (
        <PlanConfirmDialog
          open
          onOpenChange={close}
          heading={t(($) => $.plan.authoring.delete_phase.title)}
          description={t(($) => $.plan.authoring.delete_phase.description, {
            title: dialog.phase.title,
          })}
          // A phase with no parts has nothing to enumerate; the pluralized
          // sentence would read "Its 0 parts are deleted too".
          consequences={
            dialog.phase.parts.length === 0
              ? t(($) => $.plan.authoring.delete_phase.consequences_empty)
              : t(($) => $.plan.authoring.delete_phase.consequences, {
                  count: dialog.phase.parts.length,
                })
          }
          confirmLabel={t(($) => $.plan.authoring.delete_phase.submit)}
          onConfirm={async () => {
            await deletePhase.mutateAsync({ planId: planId!, phaseId: dialog.phase.id });
          }}
        />
      );

    case "add-part":
      return (
        <PlanFormDialog
          open
          onOpenChange={close}
          heading={t(($) => $.plan.authoring.add_part.title)}
          description={t(($) => $.plan.authoring.add_part.description, {
            phase: dialog.phase.title,
          })}
          submitLabel={t(($) => $.plan.authoring.add_part.submit)}
          fields={[
            {
              name: "title",
              label: t(($) => $.plan.authoring.field.part_title),
              initial: "",
              placeholder: t(($) => $.plan.authoring.add_part.title_placeholder),
            },
            {
              name: "description",
              label: t(($) => $.plan.authoring.field.description),
              initial: "",
              multiline: true,
              rows: 3,
            },
            {
              name: "acceptance_criteria",
              label: t(($) => $.plan.authoring.field.acceptance_criteria),
              initial: "",
              multiline: true,
              rows: 3,
            },
          ]}
          extra={
            // Says up front what the read model will report for a part with no
            // issues yet, so the amber "no tasks yet" state that appears right
            // after this dialog closes reads as expected, not as a failure.
            <div className="rounded-md border border-dashed border-warning/40 bg-warning/5 px-3 py-2 text-caption text-muted-foreground">
              {t(($) => $.plan.authoring.add_part.coverage_note)}
            </div>
          }
          onSubmit={async (values) =>
            createPart.mutateAsync({
              planId: planId!,
              phaseId: dialog.phase.id,
              data: {
                title: (values.title ?? "").trim(),
                description: values.description ?? "",
                acceptance_criteria: values.acceptance_criteria ?? "",
                position: nextPosition(dialog.phase.parts),
              },
            })
          }
        />
      );

    case "edit-part":
      return (
        <PlanFormDialog
          open
          onOpenChange={close}
          heading={t(($) => $.plan.authoring.edit_part.title)}
          description={t(($) => $.plan.authoring.edit_part.description)}
          submitLabel={t(($) => $.plan.authoring.save)}
          fields={[
            {
              name: "title",
              label: t(($) => $.plan.authoring.field.part_title),
              initial: dialog.part.title,
            },
            {
              name: "description",
              label: t(($) => $.plan.authoring.field.description),
              initial: dialog.part.description,
              multiline: true,
              rows: 3,
            },
            {
              name: "acceptance_criteria",
              label: t(($) => $.plan.authoring.field.acceptance_criteria),
              initial: dialog.part.acceptance_criteria,
              multiline: true,
              rows: 3,
            },
          ]}
          onSubmit={async (values) =>
            updatePart.mutateAsync({
              planId: planId!,
              partId: dialog.part.id,
              data: {
                title: (values.title ?? "").trim(),
                description: values.description ?? "",
                acceptance_criteria: values.acceptance_criteria ?? "",
              },
            })
          }
        />
      );

    case "delete-part":
      return (
        <PlanConfirmDialog
          open
          onOpenChange={close}
          heading={t(($) => $.plan.authoring.delete_part.title)}
          description={t(($) => $.plan.authoring.delete_part.description, {
            title: dialog.part.title,
          })}
          consequences={t(($) => $.plan.authoring.delete_part.consequences)}
          confirmLabel={t(($) => $.plan.authoring.delete_part.submit)}
          onConfirm={async () => {
            await deletePart.mutateAsync({ planId: planId!, partId: dialog.part.id });
          }}
        />
      );

    case "link-issues":
      return (
        <PlanLinkIssuesDialog
          open
          onOpenChange={close}
          projectId={projectId}
          // Plan-wide, not part-wide: see PlanLinkIssuesDialog's docstring on
          // project_plan_part_issue_plan_issue_key.
          linkedElsewhere={
            new Set(
              overview!.phases.flatMap((phase) =>
                phase.parts.flatMap((part) =>
                  part.issues.map((issue) => issue.id).filter((id): id is string => !!id),
                ),
              ),
            )
          }
          // Re-read the live part out of the overview rather than using the
          // one captured when the dialog opened, so the linked list updates
          // after each write instead of going stale mid-session.
          part={
            overview!.phases
              .find((phase) => phase.id === dialog.phase.id)
              ?.parts.find((part) => part.id === dialog.part.id) ?? dialog.part
          }
          onLink={async (issueId) => {
            await linkIssue.mutateAsync({ planId: planId!, partId: dialog.part.id, issueId });
          }}
          onUnlink={async (issueId) => {
            await unlinkIssue.mutateAsync({ planId: planId!, partId: dialog.part.id, issueId });
          }}
        />
      );
  }
}
