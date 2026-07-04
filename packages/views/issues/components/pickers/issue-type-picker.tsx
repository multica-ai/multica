"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { PropertyPicker, PickerItem } from "./property-picker";
import { IssueTypeIcon } from "../issue-type-icon";
import { listIssueTypesOptions } from "@multica/core/issue-types/queries";
import type { IssueType } from "@multica/core/types";
import { Box } from "lucide-react";

export function IssueTypePicker({
  issueTypeId,
  onUpdate,
  trigger: customTrigger,
  triggerRender,
  open: controlledOpen,
  onOpenChange: controlledOnOpenChange,
  align,
}: {
  issueTypeId: string | null;
  onUpdate: (updates: { issue_type_id: string | null }) => void;
  trigger?: React.ReactNode;
  triggerRender?: React.ReactElement;
  open?: boolean;
  onOpenChange?: (v: boolean) => void;
  align?: "start" | "center" | "end";
}) {
  const [internalOpen, setInternalOpen] = useState(false);
  const open = controlledOpen ?? internalOpen;
  const setOpen = controlledOnOpenChange ?? setInternalOpen;
  const wsId = useWorkspaceId();

  const { data = [] } = useQuery(listIssueTypesOptions(wsId));
  const issueTypes = data as IssueType[];
  const selectedType = issueTypes.find((t: IssueType) => t.id === issueTypeId);

  return (
    <PropertyPicker
      open={open}
      onOpenChange={setOpen}
      width="w-44"
      align={align}
      triggerRender={triggerRender}
      trigger={
        customTrigger ??
        (selectedType ? (
          <>
            <IssueTypeIcon icon={selectedType.icon} color={selectedType.color} className="h-3.5 w-3.5 shrink-0" />
            <span className="truncate">{selectedType.name}</span>
          </>
        ) : (
          <>
            <Box className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
            <span className="truncate text-muted-foreground">Type</span>
          </>
        ))
      }
    >
      {issueTypes.map((t: IssueType) => {
        return (
          <PickerItem
            key={t.id}
            selected={t.id === issueTypeId}
            onClick={() => {
              onUpdate({ issue_type_id: t.id });
              setOpen(false);
            }}
          >
            <IssueTypeIcon icon={t.icon} color={t.color} className="h-3.5 w-3.5" />
            <span>{t.name}</span>
          </PickerItem>
        );
      })}
    </PropertyPicker>
  );
}
