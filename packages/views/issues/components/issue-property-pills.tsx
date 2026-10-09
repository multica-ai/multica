"use client";

import type { Issue, UpdateIssueRequest } from "@multica/core/types";
import { PillButton } from "../../common/pill-button";
import { ProjectPicker } from "../../projects/components/project-picker";
import {
  AssigneePicker,
  DueDatePicker,
  LabelPicker,
  PriorityPicker,
  StartDatePicker,
  StatusPicker,
} from "./pickers";

/**
 * The issue's core properties as one wrapping row of pills, each opening the
 * same picker the detail sidebar uses. Used where there is no room for the
 * sidebar — the board's side peek — so triage stays one click per field.
 *
 * Start date is shown only once set: it is the least-used field, and an empty
 * pill for it would push the rest onto a second line. Custom properties stay
 * on the full page.
 */
export function IssuePropertyPills({
  issue,
  onUpdate,
  onMarkDuplicate,
  isDuplicate,
}: {
  issue: Issue;
  onUpdate: (updates: Partial<UpdateIssueRequest>) => void;
  onMarkDuplicate?: () => void;
  isDuplicate?: boolean;
}) {
  return (
    <div className="flex flex-wrap items-center gap-1.5">
      <StatusPicker
        status={issue.status}
        onUpdate={onUpdate}
        triggerRender={<PillButton />}
        align="start"
        onMarkDuplicate={onMarkDuplicate}
        isDuplicate={isDuplicate}
      />
      <PriorityPicker
        priority={issue.priority}
        onUpdate={onUpdate}
        triggerRender={<PillButton />}
        align="start"
      />
      <AssigneePicker
        assigneeType={issue.assignee_type}
        assigneeId={issue.assignee_id}
        onUpdate={onUpdate}
        triggerRender={<PillButton />}
        align="start"
      />
      <ProjectPicker
        projectId={issue.project_id}
        onUpdate={onUpdate}
        triggerRender={<PillButton />}
      />
      {issue.start_date && (
        <StartDatePicker
          startDate={issue.start_date}
          onUpdate={onUpdate}
          triggerRender={<PillButton />}
          align="start"
        />
      )}
      <DueDatePicker
        dueDate={issue.due_date}
        onUpdate={onUpdate}
        triggerRender={<PillButton />}
        align="start"
      />
      <LabelPicker issueId={issue.id} triggerRender={<PillButton />} align="start" />
    </div>
  );
}
