"use client";

import { useEffect, useRef, useState } from "react";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../i18n";

/**
 * In-place name editor. Enter or blur commits, Escape cancels; an empty name
 * keeps the old one.
 */
export function ViewNameInput({
  initial,
  onCommit,
  onDone,
  className,
}: {
  initial: string;
  onCommit: (name: string) => void;
  onDone: () => void;
  className?: string;
}) {
  const { t } = useT("issues");
  const [value, setValue] = useState(initial);
  const ref = useRef<HTMLInputElement>(null);
  const finished = useRef(false);

  useEffect(() => {
    ref.current?.focus();
    ref.current?.select();
  }, []);

  const finish = (commit: boolean) => {
    if (finished.current) return;
    finished.current = true;
    if (commit && value.trim()) onCommit(value);
    onDone();
  };

  return (
    <input
      ref={ref}
      value={value}
      maxLength={80}
      aria-label={t(($) => $.views.rename)}
      onChange={(event) => setValue(event.target.value)}
      onBlur={() => finish(true)}
      onKeyDown={(event) => {
        event.stopPropagation();
        if (event.key === "Enter") finish(true);
        else if (event.key === "Escape") finish(false);
      }}
      onPointerDown={(event) => event.stopPropagation()}
      className={cn(
        "h-6 min-w-0 rounded-md border border-ring bg-background px-1.5 text-body text-foreground outline-none",
        className,
      )}
    />
  );
}
