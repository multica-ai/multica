"use client";

import { useState } from "react";
import {
  Popover,
  PopoverTrigger,
  PopoverContent,
} from "@multica/ui/components/ui/popover";
import {
  TriggerConfigSection,
  type TriggerConfig,
} from "../trigger-config";

export function SchedulePicker({
  config,
  onChange,
  triggerRender,
}: {
  config: TriggerConfig;
  onChange: (cfg: TriggerConfig) => void;
  triggerRender: React.ReactElement;
}) {
  const [open, setOpen] = useState(false);
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger render={triggerRender} />
      <PopoverContent align="start" className="w-96 p-3">
        <TriggerConfigSection config={config} onChange={onChange} />
      </PopoverContent>
    </Popover>
  );
}
