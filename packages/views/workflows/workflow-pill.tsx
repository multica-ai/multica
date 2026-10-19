"use client";

import { Check, Workflow } from "lucide-react";
import { useFeatureEnabled } from "@multica/core/config";
import { PROJECT_WORKFLOWS_V1_FLAG } from "@multica/core/feature-flags";
import { useWorkspaceId } from "@multica/core/hooks";
import { useIssueWorkflows } from "@multica/core/issue-workflows";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { PillButton } from "../common/pill-button";
import { useT } from "../i18n";

/**
 * The workflow a new project starts on (MUL-7420); null is Default. Shown
 * while workflows are on, so the choice is visible even before the workspace
 * has more than Default.
 */
export function WorkflowPill({
  value,
  onChange,
}: {
  value: string | null;
  onChange: (workflowId: string | null) => void;
}) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const enabled = useFeatureEnabled(PROJECT_WORKFLOWS_V1_FLAG, false);
  const { workflows } = useIssueWorkflows(wsId);
  const named = workflows.filter((w) => !w.is_default);
  if (!enabled) return null;
  const defaultName = workflows.find((w) => w.is_default)?.name ?? t(($) => $.workflows.default_name);
  const current = named.find((w) => w.id === value);
  const options = [{ id: null as string | null, name: defaultName, columns: workflows.find((w) => w.is_default)?.steps.length }, ...named.map((w) => ({ id: w.id as string | null, name: w.name, columns: w.steps.length }))];

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <PillButton>
            <Workflow className="size-3" />
            <span className="max-w-[10rem] truncate">{current?.name ?? defaultName}</span>
          </PillButton>
        }
      />
      <DropdownMenuContent align="start" className="w-60">
        <DropdownMenuGroup>
          <DropdownMenuLabel>{t(($) => $.workflows.project.picker_title)}</DropdownMenuLabel>
          {options.map((option) => (
            <DropdownMenuItem key={option.id ?? "default"} onClick={() => onChange(option.id)}>
              <span className="min-w-0 flex-1">
                <span className="block truncate">{option.name}</span>
                {option.columns !== undefined && (
                  <span className="block text-caption text-muted-foreground">
                    {t(($) => $.workflows.settings.statuses, { count: option.columns })}
                  </span>
                )}
              </span>
              {option.id === (current?.id ?? null) && <Check className="size-3.5 shrink-0" />}
            </DropdownMenuItem>
          ))}
        </DropdownMenuGroup>
        <p className="border-t border-surface-border px-2 pb-1 pt-2 text-caption leading-5 text-muted-foreground">
          {t(($) => $.workflows.project.picker_more)}
        </p>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
