"use client";

import { useState } from "react";
import { Workflow, X } from "lucide-react";
import { usePipelines } from "@multica/core/pipeline";
import { PropertyPicker, PickerItem } from "./property-picker";

export function IssuePipelinePicker({
  wsId,
  pipelineId,
  onUpdate,
  trigger,
}: {
  wsId: string;
  pipelineId: string | null;
  onUpdate: (updates: { pipeline_id: string | null }) => void;
  trigger?: React.ReactNode;
}) {
  const [open, setOpen] = useState(false);
  const { data: pipelines = [] } = usePipelines(wsId);
  const activePipeline = pipelines.find((p) => p.id === pipelineId);

  const defaultTrigger = (
    <span className="inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-xs font-medium bg-muted/60 text-muted-foreground">
      <Workflow className="h-3 w-3 shrink-0" />
      <span className="truncate max-w-[120px]">{activePipeline?.name ?? "No pipeline"}</span>
    </span>
  );

  return (
    <PropertyPicker
      open={open}
      onOpenChange={setOpen}
      width="w-52"
      align="start"
      trigger={trigger ?? defaultTrigger}
    >
      <PickerItem
        selected={pipelineId === null}
        hoverClassName="hover:bg-accent"
        onClick={() => { onUpdate({ pipeline_id: null }); setOpen(false); }}
      >
        <X className="h-3.5 w-3.5 text-muted-foreground" />
        <span>No pipeline</span>
      </PickerItem>
      {pipelines.map((p) => (
        <PickerItem
          key={p.id}
          selected={p.id === pipelineId}
          hoverClassName="hover:bg-accent"
          onClick={() => { onUpdate({ pipeline_id: p.id }); setOpen(false); }}
        >
          <Workflow className="h-3.5 w-3.5" />
          <span>{p.name}</span>
        </PickerItem>
      ))}
    </PropertyPicker>
  );
}
