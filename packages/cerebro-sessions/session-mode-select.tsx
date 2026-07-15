"use client";

import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { cn } from "@multica/ui/lib/utils";
import {
  SESSION_MODES,
  SESSION_MODE_STYLES,
  normalizeSessionMode,
  type SessionMode,
} from "./modes";

export function SessionModeSelect({
  value,
  onValueChange,
  ariaLabel = "Session mode",
}: {
  value: string | null | undefined;
  onValueChange: (value: SessionMode) => void;
  ariaLabel?: string;
}) {
  const mode = normalizeSessionMode(value);
  return (
    <Select
      value={mode}
      items={Object.fromEntries(SESSION_MODES.map(({ value: key, label }) => [key, label]))}
      onValueChange={(next) => onValueChange(next as SessionMode)}
    >
      <SelectTrigger
        size="sm"
        aria-label={ariaLabel}
        className={cn(
          "h-auto rounded-full border px-2 py-0.5 text-xs transition-colors",
          SESSION_MODE_STYLES[mode],
        )}
      >
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {SESSION_MODES.map(({ value: key, label }) => (
          <SelectItem key={key} value={key}>{label}</SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}
