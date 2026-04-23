"use client";

import { useState } from "react";
import { Workflow } from "lucide-react";
import { usePipelines } from "@multica/core/pipeline";
import { useIssueViewStore } from "@multica/core/issues/stores/view-store";
import { PropertyPicker, PickerItem } from "./property-picker";

export function PipelinePicker({ wsId }: { wsId: string }) {
  const [open, setOpen] = useState(false);
  const activePipelineId = useIssueViewStore((s) => s.activePipelineId);
  const setActivePipeline = useIssueViewStore((s) => s.setActivePipeline);
  const { data: pipelines = [] } = usePipelines(wsId);

  const activePipeline = pipelines.find((p) => p.id === activePipelineId);
  const label = activePipeline?.name ?? "Default";

  return (
    <PropertyPicker
      open={open}
      onOpenChange={setOpen}
      width="w-48"
      align="start"
      trigger={
        <>
          <Workflow className="h-3.5 w-3.5 shrink-0" />
          <span className="min-w-0 max-w-[7rem] truncate sm:max-w-[10rem]">{label}</span>
        </>
      }
    >
      <PickerItem
        selected={activePipelineId === null}
        hoverClassName="hover:bg-accent"
        onClick={() => { setActivePipeline(null); setOpen(false); }}
      >
        <Workflow className="h-3.5 w-3.5" />
        <span>Default</span>
      </PickerItem>
      {pipelines.map((p) => (
        <PickerItem
          key={p.id}
          selected={p.id === activePipelineId}
          hoverClassName="hover:bg-accent"
          onClick={() => { setActivePipeline(p.id); setOpen(false); }}
        >
          <Workflow className="h-3.5 w-3.5" />
          <span>{p.name}</span>
        </PickerItem>
      ))}
    </PropertyPicker>
  );
}
