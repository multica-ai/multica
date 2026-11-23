"use client";

import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  ArrowUp,
  Calendar,
  CalendarClock,
  Check,
  CircleDot,
  FolderKanban,
  Link2,
  Milestone,
  SlidersHorizontal,
  Tag,
  Trash2,
  Unlink,
  UserMinus,
  X,
} from "@multica/ui/icons";
import { copyText } from "@multica/ui/lib/clipboard";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths, issueRouteRef } from "@multica/core/paths";
import { PRIORITY_CONFIG, PRIORITY_DISPLAY_ORDER } from "@multica/core/issues/config";
import { addDaysDateOnly, todayDateOnly } from "@multica/core/issues/date";
import type { CommonIssueFields } from "@multica/core/issues/batch";
import { projectListOptions } from "@multica/core/projects/queries";
import { propertyListOptions } from "@multica/core/properties";
import { paletteColor } from "@multica/core/appearance";
import type {
  BatchIssuePropertyOps,
  Issue,
  IssueProperty,
  IssuePropertyValue,
  UpdateIssueRequest,
} from "@multica/core/types";
import type { MenuPrimitives } from "../../common/menu-primitives";
import { PropertyIcon } from "../../common/property-icon";
import { ProjectIcon } from "../../projects/components/project-icon";
import { useNavigation } from "../../navigation";
import { SubmenuPanel } from "../actions/issue-actions-menu-items";
import { useIssueStatusOptions } from "../utils/status-options";
import { AssigneePickerPanel } from "./pickers";
import { BatchLabelPanel, type BatchLabelApply } from "./pickers/batch-label-picker";
import { stageOptions } from "./pickers/stage-picker";
import { StatusIcon } from "./status-icon";
import { PriorityIcon } from "./priority-icon";
import { useT } from "../../i18n";

/** Editors too rich for a menu row; the toolbar opens them above its "More" button. */
export type BatchEditor =
  | { kind: "start_date" }
  | { kind: "due_date" }
  | { kind: "property"; property: IssueProperty };

/** Property types whose values fit in a submenu of plain rows. */
const INLINE_PROPERTY_TYPES = new Set(["select", "checkbox"]);

function dateUpdate(field: "start_date" | "due_date", value: string | null): Partial<UpdateIssueRequest> {
  return field === "start_date" ? { start_date: value } : { due_date: value };
}

/** The value every issue shares for a property, or undefined when they differ or none has one. */
export function sharedPropertyValue(issues: readonly Issue[], propertyId: string): IssuePropertyValue | undefined {
  const first = issues[0]?.properties?.[propertyId];
  if (first === undefined) return undefined;
  const key = JSON.stringify(first);
  return issues.every((issue) => JSON.stringify(issue.properties?.[propertyId]) === key) ? first : undefined;
}

/** Copies the selected issues' links, one per line. */
export function useCopyIssueLinks(issues: readonly Issue[]) {
  const { t } = useT("issues");
  const paths = useWorkspacePaths();
  const { getShareableUrl } = useNavigation();
  return () => {
    const links = issues.map((issue) => getShareableUrl(paths.issueDetail(issueRouteRef(issue))));
    void copyText(links.join("\n")).then((ok) => {
      if (ok) toast.success(t(($) => $.batch.links_copied, { count: issues.length }));
    });
  };
}

interface BatchMenuItemsProps {
  primitives: MenuPrimitives;
  /** `more` is the toolbar's overflow menu; `context` is the right-click menu, which repeats the toolbar's own fields. */
  variant: "more" | "context";
  issues: Issue[];
  common: CommonIssueFields;
  projectId: string | null;
  allowedStatuses: ReadonlySet<string> | null;
  disabled: boolean;
  /** False for a guest, who edits no issue: only copying links and clearing the selection remain. */
  editable: boolean;
  /** False for a viewer who can delete no issue at all (a guest): the entry is left out. */
  offersDelete: boolean;
  canDelete: boolean;
  onStatus: (status: string) => void;
  onAssignee: (updates: Partial<UpdateIssueRequest>) => void;
  onUpdate: (updates: Partial<UpdateIssueRequest>) => void;
  onLabels: BatchLabelApply;
  onProperties: (ops: BatchIssuePropertyOps) => void;
  onEditor: (editor: BatchEditor) => void;
  onSetParent: () => void;
  onClearSelection?: () => void;
  onDelete: () => void;
  onClose: () => void;
}

export function BatchMenuItems({
  primitives: P,
  variant,
  issues,
  common,
  projectId,
  allowedStatuses,
  disabled,
  editable,
  offersDelete,
  canDelete,
  onStatus,
  onAssignee,
  onUpdate,
  onLabels,
  onProperties,
  onEditor,
  onSetParent,
  onClearSelection,
  onDelete,
  onClose,
}: BatchMenuItemsProps) {
  const { t } = useT("issues");
  const { t: tLayout } = useT("layout");
  const wsId = useWorkspaceId();
  const { data: properties = [] } = useQuery(propertyListOptions(wsId));
  const count = issues.length;
  const anyParent = issues.some((issue) => !!issue.parent_issue_id);
  const allChildren = count > 0 && issues.every((issue) => !!issue.parent_issue_id);
  const sharedStage = issues.every((issue) => issue.stage === issues[0]?.stage) ? (issues[0]?.stage ?? null) : null;
  const maxStage = issues.reduce((max, issue) => Math.max(max, issue.stage ?? 0), 0);

  const copyLinks = useCopyIssueLinks(issues);

  const dateItems = (field: "start_date" | "due_date") => {
    const hasValue = issues.some((issue) => !!issue[field]);
    const labels =
      field === "start_date"
        ? {
            today: t(($) => $.actions.start_today),
            tomorrow: t(($) => $.actions.start_tomorrow),
            nextWeek: t(($) => $.actions.start_next_week),
            clear: t(($) => $.actions.start_clear),
          }
        : {
            today: t(($) => $.actions.due_today),
            tomorrow: t(($) => $.actions.due_tomorrow),
            nextWeek: t(($) => $.actions.due_next_week),
            clear: t(($) => $.actions.due_clear),
          };
    return (
      <>
        <P.Item onClick={() => onUpdate(dateUpdate(field, todayDateOnly()))}>{labels.today}</P.Item>
        <P.Item onClick={() => onUpdate(dateUpdate(field, addDaysDateOnly(1)))}>{labels.tomorrow}</P.Item>
        <P.Item onClick={() => onUpdate(dateUpdate(field, addDaysDateOnly(7)))}>{labels.nextWeek}</P.Item>
        <P.Item onClick={() => onEditor({ kind: field })}>{t(($) => $.batch.pick_date)}</P.Item>
        {hasValue && (
          <>
            <P.Separator />
            <P.Item onClick={() => onUpdate(dateUpdate(field, null))}>{labels.clear}</P.Item>
          </>
        )}
      </>
    );
  };

  const activeProperties = properties.filter((property) => !property.archived);

  const header = variant === "context" && (
    <>
      <div className="px-2 pt-1 pb-1.5 text-caption text-muted-foreground">
        {t(($) => $.batch.selected, { count })}
      </div>
      <P.Separator />
    </>
  );
  const selectionItems = (
    <>
      <P.Item onClick={copyLinks}>
        <Link2 className="h-3.5 w-3.5" />
        {t(($) => $.batch.copy_links)}
      </P.Item>
      {onClearSelection && (
        <P.Item onClick={onClearSelection}>
          <X className="h-3.5 w-3.5" />
          {tLayout(($) => $.collection.clear_selection)}
        </P.Item>
      )}
    </>
  );

  if (!editable) {
    return (
      <>
        {header}
        {selectionItems}
      </>
    );
  }

  return (
    <>
      {header}
      {variant === "context" && (
        <>
          <StatusSub P={P} projectId={projectId} allowedStatuses={allowedStatuses} common={common} disabled={disabled} onStatus={onStatus} />
          <P.Sub>
            <P.SubTrigger disabled={disabled}>
              <PriorityIcon priority={common.priority ?? "none"} />
              {t(($) => $.batch.priority)}
            </P.SubTrigger>
            <P.SubContent>
              {PRIORITY_DISPLAY_ORDER.map((p) => (
                <P.Item key={p} onClick={() => onUpdate({ priority: p })}>
                  <span
                    className={`inline-flex items-center gap-1 rounded-xs px-1.5 py-0.5 text-caption font-medium ${PRIORITY_CONFIG[p].badgeBg} ${PRIORITY_CONFIG[p].badgeText}`}
                  >
                    <PriorityIcon priority={p} className="h-3 w-3" inheritColor />
                    {t(($) => $.priority[p])}
                  </span>
                  {common.priority === p && <Check className="ml-auto size-3.5 text-muted-foreground" />}
                </P.Item>
              ))}
            </P.SubContent>
          </P.Sub>
          <P.Sub>
            <P.SubTrigger disabled={disabled}>
              <UserMinus className="h-3.5 w-3.5" />
              {t(($) => $.batch.assignee)}
            </P.SubTrigger>
            <P.SubContent className="w-64 p-0">
              <SubmenuPanel>
                <AssigneePickerPanel
                  assigneeType={common.assignee?.type ?? null}
                  assigneeId={common.assignee?.id ?? null}
                  onUpdate={onAssignee}
                  onPicked={onClose}
                />
              </SubmenuPanel>
            </P.SubContent>
          </P.Sub>
          <P.Sub>
            <P.SubTrigger disabled={disabled}>
              <Tag className="h-3.5 w-3.5" />
              {t(($) => $.batch.labels)}
            </P.SubTrigger>
            <P.SubContent className="w-72 p-0">
              <SubmenuPanel>
                <BatchLabelPanel issues={issues} onApply={onLabels} onClose={onClose} />
              </SubmenuPanel>
            </P.SubContent>
          </P.Sub>
          <ProjectSub P={P} issues={issues} disabled={disabled} onUpdate={onUpdate} />
          <P.Sub>
            <P.SubTrigger disabled={disabled}>
              <Calendar className="h-3.5 w-3.5" />
              {t(($) => $.batch.due_date)}
            </P.SubTrigger>
            <P.SubContent>{dateItems("due_date")}</P.SubContent>
          </P.Sub>
          <P.Separator />
        </>
      )}

      <P.Sub>
        <P.SubTrigger disabled={disabled}>
          <CalendarClock className="h-3.5 w-3.5" />
          {t(($) => $.actions.start_date)}
        </P.SubTrigger>
        <P.SubContent>{dateItems("start_date")}</P.SubContent>
      </P.Sub>
      <P.Item disabled={disabled} onClick={onSetParent}>
        <ArrowUp className="h-3.5 w-3.5" />
        {t(($) => $.actions.set_parent_issue)}
      </P.Item>
      {anyParent && (
        <P.Item disabled={disabled} onClick={() => onUpdate({ parent_issue_id: null })}>
          <Unlink className="h-3.5 w-3.5" />
          {t(($) => $.actions.remove_parent_issue)}
        </P.Item>
      )}
      {allChildren && (
        <P.Sub>
          <P.SubTrigger disabled={disabled}>
            <Milestone className="h-3.5 w-3.5" />
            {t(($) => $.batch.stage)}
          </P.SubTrigger>
          <P.SubContent>
            <P.Item onClick={() => onUpdate({ stage: null })}>
              {t(($) => $.stage.none)}
              {issues.every((issue) => issue.stage == null) && <Check className="ml-auto size-3.5 text-muted-foreground" />}
            </P.Item>
            {stageOptions(sharedStage, maxStage).map((n) => (
              <P.Item key={n} onClick={() => onUpdate({ stage: n })}>
                {t(($) => $.stage.value, { n })}
                {sharedStage === n && <Check className="ml-auto size-3.5 text-muted-foreground" />}
              </P.Item>
            ))}
          </P.SubContent>
        </P.Sub>
      )}
      {activeProperties.length > 0 && (
        <P.Sub>
          <P.SubTrigger disabled={disabled}>
            <SlidersHorizontal className="h-3.5 w-3.5" />
            {t(($) => $.batch.custom_properties)}
          </P.SubTrigger>
          <P.SubContent className="max-h-80 overflow-y-auto">
            {activeProperties.map((property) => (
              <PropertyEntry
                key={property.id}
                P={P}
                issues={issues}
                property={property}
                onProperties={onProperties}
                onEditor={onEditor}
              />
            ))}
          </P.SubContent>
        </P.Sub>
      )}

      <P.Separator />
      {selectionItems}
      {offersDelete && (
        <>
          <P.Separator />
          <P.Item
            variant="destructive"
            disabled={disabled || !canDelete}
            title={!canDelete ? t(($) => $.batch.delete_restricted) : undefined}
            onClick={onDelete}
          >
            <Trash2 className="h-3.5 w-3.5" />
            {t(($) => $.batch.delete_count, { count })}
          </P.Item>
        </>
      )}
    </>
  );
}

function StatusSub({
  P,
  projectId,
  allowedStatuses,
  common,
  disabled,
  onStatus,
}: {
  P: MenuPrimitives;
  projectId: string | null;
  allowedStatuses: ReadonlySet<string> | null;
  common: CommonIssueFields;
  disabled: boolean;
  onStatus: (status: string) => void;
}) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const { options } = useIssueStatusOptions(wsId, projectId ?? undefined, common.status);
  const statusOptions = allowedStatuses ? options.filter((o) => allowedStatuses.has(o.key)) : options;
  return (
    <P.Sub>
      <P.SubTrigger disabled={disabled}>
        {common.status ? (
          <StatusIcon status={common.status} className="h-3.5 w-3.5" />
        ) : (
          <CircleDot className="h-3.5 w-3.5" />
        )}
        {t(($) => $.batch.status)}
      </P.SubTrigger>
      <P.SubContent>
        {statusOptions.map((option) => (
          <P.Item key={option.key} onClick={() => onStatus(option.key)}>
            <StatusIcon
              status={option.key}
              category={option.category}
              color={option.color}
              icon={option.icon}
              className="h-3.5 w-3.5"
            />
            {option.label}
            {common.status === option.key && <Check className="ml-auto size-3.5 text-muted-foreground" />}
          </P.Item>
        ))}
      </P.SubContent>
    </P.Sub>
  );
}

function ProjectSub({
  P,
  issues,
  disabled,
  onUpdate,
}: {
  P: MenuPrimitives;
  issues: Issue[];
  disabled: boolean;
  onUpdate: (updates: Partial<UpdateIssueRequest>) => void;
}) {
  const { t } = useT("issues");
  const { t: tProjects } = useT("projects");
  const wsId = useWorkspaceId();
  const { data: projects = [] } = useQuery(projectListOptions(wsId));
  const first = issues[0]?.project_id ?? null;
  const current = issues.every((issue) => (issue.project_id ?? null) === first) ? first : undefined;
  const live = projects.filter((project) => project.status !== "cancelled" || project.id === current);
  return (
    <P.Sub>
      <P.SubTrigger disabled={disabled}>
        <FolderKanban className="h-3.5 w-3.5" />
        {t(($) => $.batch.project)}
      </P.SubTrigger>
      <P.SubContent className="max-h-80 overflow-y-auto">
        <P.Item onClick={() => onUpdate({ project_id: null })}>
          <FolderKanban className="h-3.5 w-3.5 text-muted-foreground" />
          <span className="text-muted-foreground">{tProjects(($) => $.picker.no_project)}</span>
          {current === null && <Check className="ml-auto size-3.5 text-muted-foreground" />}
        </P.Item>
        {live.map((project) => (
          <P.Item key={project.id} onClick={() => onUpdate({ project_id: project.id })}>
            <ProjectIcon project={project} size="sm" />
            <span className="max-w-56 truncate">{project.title}</span>
            {current === project.id && <Check className="ml-auto size-3.5 text-muted-foreground" />}
          </P.Item>
        ))}
      </P.SubContent>
    </P.Sub>
  );
}

/**
 * One custom property in the batch menu. Select and checkbox values are rows
 * in a submenu; every other type opens the property's own editor.
 */
function PropertyEntry({
  P,
  issues,
  property,
  onProperties,
  onEditor,
}: {
  P: MenuPrimitives;
  issues: Issue[];
  property: IssueProperty;
  onProperties: (ops: BatchIssuePropertyOps) => void;
  onEditor: (editor: BatchEditor) => void;
}) {
  const { t } = useT("issues");
  const label = (
    <>
      <PropertyIcon property={property} className="size-3.5" />
      <span className="max-w-48 truncate">{property.name}</span>
    </>
  );
  // Clearing is its own action: a selection with differing values has no
  // shared value for the editor to clear from.
  const setCount = issues.filter((issue) => issue.properties?.[property.id] !== undefined).length;
  const clearItem = setCount > 0 && (
    <>
      <P.Separator />
      <P.Item onClick={() => onProperties({ unset: [property.id] })}>
        <X className="h-3.5 w-3.5" />
        {t(($) => $.batch.property_clear, { count: setCount })}
      </P.Item>
    </>
  );
  if (!INLINE_PROPERTY_TYPES.has(property.type)) {
    const edit = () => onEditor({ kind: "property", property });
    if (!clearItem) return <P.Item onClick={edit}>{label}</P.Item>;
    return (
      <P.Sub>
        <P.SubTrigger>{label}</P.SubTrigger>
        <P.SubContent className="min-w-48">
          <P.Item onClick={edit}>{t(($) => $.batch.property_edit)}</P.Item>
          {clearItem}
        </P.SubContent>
      </P.Sub>
    );
  }
  const countOf = (value: IssuePropertyValue) =>
    issues.filter((issue) => issue.properties?.[property.id] === value).length;
  const set = (value: IssuePropertyValue) => onProperties({ set: { [property.id]: value } });
  const valueRow = (key: string, value: IssuePropertyValue, content: React.ReactNode) => {
    const n = countOf(value);
    return (
      <P.Item key={key} onClick={() => set(value)}>
        {content}
        {n > 0 && (
          <span className="ml-auto pl-3 text-caption tabular-nums text-muted-foreground">
            {n}/{issues.length}
          </span>
        )}
      </P.Item>
    );
  };
  return (
    <P.Sub>
      <P.SubTrigger>{label}</P.SubTrigger>
      <P.SubContent className="max-h-80 min-w-48 overflow-y-auto">
        {property.type === "select"
          ? (property.config.options ?? []).map((option) =>
              valueRow(
                option.id,
                option.id,
                <>
                  <span className="size-2.5 shrink-0 rounded-full" style={{ backgroundColor: paletteColor(option.color) }} />
                  <span className="max-w-48 truncate">{option.name}</span>
                </>,
              ),
            )
          : [
              valueRow("true", true, t(($) => $.pickers.custom_property.true_label)),
              valueRow("false", false, t(($) => $.pickers.custom_property.false_label)),
            ]}
        {clearItem}
      </P.SubContent>
    </P.Sub>
  );
}
