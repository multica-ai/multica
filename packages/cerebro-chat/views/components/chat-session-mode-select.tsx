"use client";

import {
  SESSION_MODES,
  SESSION_MODE_STYLES,
  normalizeSessionMode,
  type SessionMode,
} from "@multica/cerebro-sessions";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { cn } from "@multica/ui/lib/utils";

export function ChatSessionModeSelect({
  mode,
  onChange,
  disabled = false,
}: {
  mode?: string | null;
  onChange: (mode: SessionMode) => void;
  disabled?: boolean;
}) {
  const value = normalizeSessionMode(mode);
  return (
    <Select
      value={value}
      items={Object.fromEntries(SESSION_MODES.map(({ value: key, label }) => [key, label]))}
      onValueChange={(next) => onChange(next as SessionMode)}
      disabled={disabled}
    >
      <SelectTrigger
        size="sm"
        aria-label="Session mode"
        className={cn(
          "h-auto rounded-full border px-2 py-0.5 text-xs transition-colors",
          SESSION_MODE_STYLES[value],
        )}
      >
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {SESSION_MODES.map(({ value: key, label }) => (
          <SelectItem key={key} value={key}>
            {label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}
