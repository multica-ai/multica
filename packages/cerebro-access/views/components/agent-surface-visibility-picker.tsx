"use client";

import { useState } from "react";
import { Eye, EyeOff, SlidersHorizontal } from "lucide-react";
import { Label } from "@multica/ui/components/ui/label";
import { Switch } from "@multica/ui/components/ui/switch";
import { RadioGroup, RadioGroupItem } from "@multica/ui/components/ui/radio-group";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import { cn } from "@multica/ui/lib/utils";
import {
  AGENT_SURFACES,
  SURFACE_LABEL,
  surfaceVisibilityPreset,
  presetToSurfaceMap,
  type AgentSurface,
  type SurfaceVisibilityPreset,
} from "../surface-visibility";

/**
 * TECH-3670 — editor for an agent's per-surface discovery visibility.
 *
 * Simple mode (A): three presets — Visible (everyone), Skjult (hidden from
 * non-owners everywhere), and Tilpasset (advanced). Advanced mode (B): one
 * switch per discovery surface. The value is the `surface_visibility` map
 * (key → "visible to non-owners"); the component emits `{}` for Visible.
 */
export function AgentSurfaceVisibilityPicker({
  value,
  canEdit = true,
  onChange,
}: {
  value: Record<string, boolean> | null | undefined;
  canEdit?: boolean;
  onChange: (next: Record<string, boolean>) => Promise<void> | void;
}) {
  const [open, setOpen] = useState(false);
  const preset = surfaceVisibilityPreset(value);

  const summary = PRESET_LABEL[preset];
  const Icon = PRESET_ICON[preset];

  if (!canEdit) {
    return (
      <span className="flex items-center gap-1.5 text-muted-foreground">
        <Icon className="h-3 w-3 shrink-0" />
        <span className="truncate">{summary}</span>
      </span>
    );
  }

  const setPreset = (next: SurfaceVisibilityPreset) => {
    if (next === "custom") {
      // Entering advanced from a non-custom preset: seed an explicit map so the
      // switches have something to toggle. Default everything hidden so the
      // advanced choice is deliberate (the owner came here to restrict).
      const seed =
        preset === "custom"
          ? (value ?? {})
          : presetToSurfaceMap("hidden");
      void onChange({ ...seed });
      return;
    }
    void onChange(presetToSurfaceMap(next));
  };

  const toggleSurface = (surface: AgentSurface, visible: boolean) => {
    const base: Record<string, boolean> = { ...(value ?? {}) };
    base[surface] = visible;
    void onChange(base);
  };

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        className={cn(
          "flex items-center gap-1.5 rounded px-1.5 py-0.5 text-sm hover:bg-muted",
        )}
        aria-label={`Surface visibility · ${summary}`}
      >
        <Icon className="h-3 w-3 shrink-0 text-muted-foreground" />
        <span className="truncate">{summary}</span>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-72 p-3">
        <RadioGroup
          value={preset}
          onValueChange={(v) => setPreset(v as SurfaceVisibilityPreset)}
          className="gap-3"
        >
          <PresetRow
            value="visible"
            title="Visible to everyone"
            description="All members can find and assign this agent."
          />
          <PresetRow
            value="hidden"
            title="Skjult (hidden)"
            description="Hidden from non-owners in lists, search, @-mention, chat and channels. Still shows on public issues it works on. Owner + admins always see it."
          />
          <PresetRow
            value="custom"
            title="Advanced — per surface"
            description="Choose exactly where non-owners can discover it."
          />
        </RadioGroup>

        {preset === "custom" && (
          <div className="mt-3 flex flex-col gap-2 border-t pt-3">
            {AGENT_SURFACES.map((surface) => {
              const visible = (value ?? {})[surface] !== false;
              return (
                <label
                  key={surface}
                  className="flex items-center justify-between gap-2 text-sm"
                >
                  <span className="truncate">{SURFACE_LABEL[surface]}</span>
                  <Switch
                    checked={visible}
                    onCheckedChange={(checked) =>
                      toggleSurface(surface, checked === true)
                    }
                    aria-label={`Visible on ${SURFACE_LABEL[surface]}`}
                  />
                </label>
              );
            })}
            <p className="text-xs text-muted-foreground">
              On = non-owners can discover it there. Off = hidden.
            </p>
          </div>
        )}
      </PopoverContent>
    </Popover>
  );
}

function PresetRow({
  value,
  title,
  description,
}: {
  value: SurfaceVisibilityPreset;
  title: string;
  description: string;
}) {
  return (
    <Label className="flex cursor-pointer items-start gap-2 font-normal">
      <RadioGroupItem value={value} className="mt-0.5" />
      <span className="flex flex-col gap-0.5">
        <span className="font-medium">{title}</span>
        <span className="text-xs text-muted-foreground">{description}</span>
      </span>
    </Label>
  );
}

const PRESET_LABEL: Record<SurfaceVisibilityPreset, string> = {
  visible: "Visible to everyone",
  hidden: "Skjult (hidden)",
  custom: "Advanced",
};

const PRESET_ICON: Record<
  SurfaceVisibilityPreset,
  typeof Eye
> = {
  visible: Eye,
  hidden: EyeOff,
  custom: SlidersHorizontal,
};
